package garmin

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/iskyd/kudos/internal/connectors"
	"github.com/iskyd/kudos/internal/secret"
)

const (
	fixtures   = "../../../testdata/garmin"
	testKey    = "test-personal-api-key"
	searchPath = "/activitylist-service/activities/search/activities"
)

var creds = connectors.Credentials{Key: secret.New(testKey)}

func jwt(exp time.Time, id int) string {
	enc := base64.RawURLEncoding.EncodeToString
	return enc([]byte(`{"alg":"none"}`)) + "." + enc(fmt.Appendf(nil, `{"exp":%d,"jti":"%d"}`, exp.Unix(), id)) + ".sig"
}

// replay serves the recorded fixtures by path. override, when set and returning
// true, has written the response instead.
type replay struct {
	t        *testing.T
	files    map[string]string // path → fixture file
	mu       sync.Mutex
	tokens   []string // every token minted, in order
	requests []*http.Request
	override func(w http.ResponseWriter, r *http.Request) bool
}

func newReplay(t *testing.T) *replay {
	t.Helper()
	var index []struct {
		Path string `json:"path"`
		File string `json:"file"`
	}
	b, err := os.ReadFile(filepath.Join(fixtures, "index.json"))
	if err == nil {
		err = json.Unmarshal(b, &index)
	}
	if err != nil {
		t.Fatal(err)
	}
	g := &replay{t: t, files: map[string]string{}}
	for _, e := range index {
		if e.File != "" {
			g.files[strings.SplitN(e.Path, "?", 2)[0]] = e.File
		}
	}
	return g
}

func (g *replay) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	g.requests = append(g.requests, r)
	override := g.override
	g.mu.Unlock()
	if override != nil && override(w, r) {
		return
	}
	if r.URL.Path == tokenPath {
		if r.Method != http.MethodPost || r.Header.Get("Authorization") != "ApiKey "+testKey {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		g.mu.Lock()
		token := jwt(time.Now().Add(24*time.Hour), len(g.tokens)+1)
		g.tokens = append(g.tokens, token)
		g.mu.Unlock()
		_, _ = w.Write([]byte(token))
		return
	}
	g.mu.Lock()
	valid := len(g.tokens) > 0 && r.Header.Get("Authorization") == "Bearer "+g.tokens[len(g.tokens)-1]
	g.mu.Unlock()
	if !valid {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	file, ok := g.files[r.URL.Path]
	if !ok {
		http.NotFound(w, r)
		return
	}
	b, err := os.ReadFile(filepath.Join(fixtures, file))
	if err != nil {
		g.t.Error(err)
	}
	_, _ = w.Write(b)
}

func (g *replay) setOverride(f func(w http.ResponseWriter, r *http.Request) bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.override = f
}

func (g *replay) mints() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.tokens)
}

func (g *replay) paths() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := make([]string, len(g.requests))
	for i, r := range g.requests {
		out[i] = r.URL.Path
	}
	return out
}

// newClient returns a client for g that records its backoff delays instead of sleeping.
func newClient(t *testing.T, g *replay) (*Client, *[]time.Duration) {
	t.Helper()
	srv := httptest.NewServer(g)
	t.Cleanup(srv.Close)
	c := New(srv.URL+"/", srv.Client())
	var slept []time.Duration
	c.sleep = func(ctx context.Context, d time.Duration) error {
		slept = append(slept, d)
		return ctx.Err()
	}
	return c, &slept
}

// session authenticates against g.
func session(t *testing.T, c *Client) *connectors.Session {
	t.Helper()
	s, err := c.Authenticate(t.Context(), creds)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func date(d int) time.Time { return time.Date(2026, 10, d, 0, 0, 0, 0, time.UTC) }

func TestReplay(t *testing.T) {
	t.Parallel()
	g := newReplay(t)
	c, _ := newClient(t, g)
	ctx := t.Context()

	s := session(t, c)
	if s.Token.Reveal() != g.tokens[0] || time.Until(s.Expires) < 23*time.Hour {
		t.Errorf("session expires %v", s.Expires)
	}

	acts, err := c.FetchActivities(ctx, s, date(26), date(28))
	if err != nil {
		t.Fatal(err)
	}
	if len(acts) != 1 || acts[0].SourceID != "100001" || !acts[0].Start.Equal(time.Date(2026, 10, 28, 11, 56, 31, 0, time.UTC)) ||
		acts[0].LocalOffset == nil || *acts[0].LocalOffset != time.Hour {
		t.Errorf("activities = %+v", acts)
	}
	search := g.requests[len(g.requests)-1].URL.Query()
	if search.Get("startDate") != "2026-10-26" || search.Get("endDate") != "2026-10-28" || search.Get("sortOrder") != "asc" {
		t.Errorf("search query = %v", search)
	}

	f, err := c.DownloadActivityFile(ctx, s, "100001")
	if err != nil {
		t.Fatal(err)
	}
	if f.Name != "100001_ACTIVITY.fit" || len(f.Data) < 12 || string(f.Data[8:12]) != ".FIT" {
		t.Errorf("file %q, %d bytes", f.Name, len(f.Data))
	}

	days, err := c.FetchDailyMetrics(ctx, s, date(26), date(28))
	if err != nil {
		t.Fatal(err)
	}
	if len(days) != 3 || !days[0].Day.Equal(date(26)) || !days[2].Day.Equal(date(28)) {
		t.Fatalf("days = %+v", days)
	}
	d := days[0]
	for name, v := range map[string]struct {
		got  *int
		want int
	}{
		"resting HR": {d.RestingHRBpm, 52}, "steps": {d.Steps, 16324}, "stress": {d.Stress, 23},
		"body battery max": {d.BodyBatteryMax, 65}, "body battery min": {d.BodyBatteryMin, 23},
		"sleep": {d.SleepS, 25080}, "deep": {d.SleepDeepS, 5400}, "light": {d.SleepLightS, 14580},
		"rem": {d.SleepRemS, 5100}, "awake": {d.SleepAwakeS, 600}, "sleep score": {d.SleepScore, 77},
	} {
		if v.got == nil || *v.got != v.want {
			t.Errorf("%s = %v, want %d", name, v.got, v.want)
		}
	}
	if d.HRVMs == nil || *d.HRVMs != 32 {
		t.Errorf("HRV = %v, want 32", d.HRVMs)
	}
	// The recording has no weight, hydration or SpO2 average.
	if d.WeightKg != nil || d.HydrationL != nil || d.Spo2Pct != nil {
		t.Errorf("unexpected values: %+v", d)
	}
	if g.mints() != 1 {
		t.Errorf("%d tokens minted, want 1", g.mints())
	}
}

func TestReauthentication(t *testing.T) {
	t.Parallel()

	t.Run("401 re-mints once and succeeds", func(t *testing.T) {
		t.Parallel()
		g := newReplay(t)
		c, _ := newClient(t, g)
		s := session(t, c)
		var once sync.Once
		g.setOverride(func(w http.ResponseWriter, r *http.Request) bool {
			hit := false
			if r.URL.Path == searchPath {
				once.Do(func() { hit = true; w.WriteHeader(http.StatusUnauthorized) })
			}
			return hit
		})
		if _, err := c.FetchActivities(t.Context(), s, date(26), date(28)); err != nil {
			t.Fatal(err)
		}
		if g.mints() != 2 || s.Token.Reveal() != g.tokens[1] {
			t.Errorf("%d mints; session keeps the new token: %v", g.mints(), s.Token.Reveal() == g.tokens[1])
		}
	})

	t.Run("401 twice rejects the key", func(t *testing.T) {
		t.Parallel()
		g := newReplay(t)
		c, slept := newClient(t, g)
		s := session(t, c)
		g.setOverride(func(w http.ResponseWriter, r *http.Request) bool {
			if r.URL.Path != searchPath {
				return false
			}
			w.WriteHeader(http.StatusUnauthorized)
			return true
		})
		if _, err := c.FetchActivities(t.Context(), s, date(26), date(28)); !errors.Is(err, connectors.ErrKeyRejected) {
			t.Errorf("error = %v, want ErrKeyRejected", err)
		}
		if n := len(g.paths()); g.mints() != 2 || n != 4 || len(*slept) != 0 {
			t.Errorf("%d mints, %d requests, %d sleeps: want 2, 4, 0 (no retry loop)", g.mints(), n, len(*slept))
		}
	})

	t.Run("rejected key", func(t *testing.T) {
		t.Parallel()
		c, _ := newClient(t, newReplay(t))
		_, err := c.Authenticate(t.Context(), connectors.Credentials{Key: secret.New("wrong-key")})
		if !errors.Is(err, connectors.ErrKeyRejected) {
			t.Errorf("error = %v, want ErrKeyRejected", err)
		}
		if strings.Contains(fmt.Sprint(err), "wrong-key") {
			t.Error("the error contains the key")
		}
	})

	t.Run("rejected key on re-mint", func(t *testing.T) {
		t.Parallel()
		g := newReplay(t)
		c, _ := newClient(t, g)
		s := session(t, c)
		g.setOverride(func(w http.ResponseWriter, _ *http.Request) bool {
			w.WriteHeader(http.StatusUnauthorized)
			return true
		})
		if _, err := c.FetchActivities(t.Context(), s, date(26), date(28)); !errors.Is(err, connectors.ErrKeyRejected) {
			t.Errorf("error = %v, want ErrKeyRejected", err)
		}
	})

	t.Run("token near expiry is renewed first", func(t *testing.T) {
		t.Parallel()
		g := newReplay(t)
		c, _ := newClient(t, g)
		s := session(t, c)
		c.now = func() time.Time { return s.Expires.Add(-30 * time.Minute) }
		if _, err := c.FetchActivities(t.Context(), s, date(26), date(28)); err != nil {
			t.Fatal(err)
		}
		if p := g.paths(); g.mints() != 2 || p[1] != tokenPath {
			t.Errorf("%d mints, requests %v", g.mints(), p)
		}
	})

	t.Run("session without a token mints one", func(t *testing.T) {
		t.Parallel()
		g := newReplay(t)
		c, _ := newClient(t, g)
		if _, err := c.FetchActivities(t.Context(), &connectors.Session{Credentials: creds}, date(26), date(28)); err != nil {
			t.Fatal(err)
		}
		if g.mints() != 1 {
			t.Errorf("%d mints, want 1", g.mints())
		}
	})
}

func TestBackoff(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		codes   []int
		wantErr error
		sleeps  int
	}{
		{name: "429 then success", codes: []int{429, 429}, sleeps: 2},
		{name: "503 then success", codes: []int{503}, sleeps: 1},
		{name: "persistent 429", codes: []int{429, 429, 429, 429, 429}, wantErr: connectors.ErrRateLimited, sleeps: 4},
		{name: "persistent 500", codes: []int{500, 500, 500, 500, 500}, wantErr: connectors.ErrUpstream, sleeps: 4},
		{name: "404 is not retried", codes: []int{404}, wantErr: connectors.ErrUpstream},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			g := newReplay(t)
			c, slept := newClient(t, g)
			s := session(t, c)
			var mu sync.Mutex
			i := 0
			g.setOverride(func(w http.ResponseWriter, r *http.Request) bool {
				mu.Lock()
				defer mu.Unlock()
				if r.URL.Path != searchPath || i >= len(tt.codes) {
					return false
				}
				if i == 0 && tt.codes[0] == http.StatusTooManyRequests {
					w.Header().Set("Retry-After", "7")
				}
				w.WriteHeader(tt.codes[i])
				i++
				return true
			})
			_, err := c.FetchActivities(t.Context(), s, date(26), date(28))
			if (tt.wantErr == nil) != (err == nil) || !errors.Is(err, tt.wantErr) {
				t.Errorf("error = %v, want %v", err, tt.wantErr)
			}
			if len(*slept) != tt.sleeps {
				t.Fatalf("slept %v, want %d sleeps", *slept, tt.sleeps)
			}
			if tt.codes[0] == http.StatusTooManyRequests && (*slept)[0] != 7*time.Second {
				t.Errorf("first delay %v, want Retry-After 7s", (*slept)[0])
			}
		})
	}
}

func TestBackoffDelay(t *testing.T) {
	t.Parallel()
	for attempt := 1; attempt <= 8; attempt++ {
		ceiling := min(baseDelay<<(attempt-1), maxDelay)
		for range 50 {
			if d := backoff(attempt, errors.New("x")); d < ceiling/2 || d > ceiling {
				t.Fatalf("attempt %d: delay %v outside [%v, %v]", attempt, d, ceiling/2, ceiling)
			}
		}
	}
	if d := backoff(1, &retryError{err: connectors.ErrRateLimited, after: time.Hour}); d != maxDelay {
		t.Errorf("Retry-After of an hour gives %v, want the %v cap", d, maxDelay)
	}
	for header, want := range map[string]time.Duration{"": 0, "-3": 0, "soon": 0, "12": 12 * time.Second} {
		if got := retryAfter(http.Header{"Retry-After": {header}}); got != want {
			t.Errorf("Retry-After %q = %v, want %v", header, got, want)
		}
	}
}

func TestCancellation(t *testing.T) {
	t.Parallel()
	g := newReplay(t)
	g.setOverride(func(w http.ResponseWriter, _ *http.Request) bool {
		w.WriteHeader(http.StatusServiceUnavailable)
		return true
	})
	srv := httptest.NewServer(g)
	defer srv.Close()
	c := New(srv.URL, srv.Client())

	// The real sleep stops when the context ends.
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if _, err := c.Authenticate(ctx, creds); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("error = %v, want DeadlineExceeded", err)
	}
	if err := sleep(t.Context(), time.Millisecond); err != nil {
		t.Errorf("sleep error = %v", err)
	}

	// A cancelled request is not retried.
	cancelled, stop := context.WithCancel(t.Context())
	stop()
	if _, err := c.Authenticate(cancelled, creds); !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want Canceled", err)
	}
}

func TestNetworkErrorIsRetried(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close() // every request fails to connect
	c := New(srv.URL, http.DefaultClient)
	var sleeps int
	c.sleep = func(context.Context, time.Duration) error { sleeps++; return nil }
	if _, err := c.Authenticate(t.Context(), creds); !errors.Is(err, connectors.ErrUpstream) || sleeps != maxAttempts-1 {
		t.Errorf("error = %v after %d sleeps", err, sleeps)
	}
}

func TestMalformedResponses(t *testing.T) {
	t.Parallel()
	zipOf := func(names ...string) []byte {
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		for _, n := range names {
			w, _ := zw.Create(n)
			_, _ = w.Write([]byte("data"))
		}
		_ = zw.Close()
		return buf.Bytes()
	}
	activities := func(c *Client, s *connectors.Session) error {
		_, err := c.FetchActivities(t.Context(), s, date(26), date(28))
		return err
	}
	download := func(c *Client, s *connectors.Session) error {
		_, err := c.DownloadActivityFile(t.Context(), s, "100001")
		return err
	}
	health := func(c *Client, s *connectors.Session) error {
		_, err := c.FetchDailyMetrics(t.Context(), s, date(26), date(28))
		return err
	}
	tests := []struct {
		name, path string
		body       []byte
		call       func(*Client, *connectors.Session) error
	}{
		{"search is not JSON", searchPath, []byte("{not json"), activities},
		{"search without start time", searchPath, []byte(`[{"activityId":5}]`), activities},
		{"download is not a ZIP", "/download-service/files/activity/100001", []byte("not a zip"), download},
		{"download holds two files", "/download-service/files/activity/100001", zipOf("a.fit", "b.fit"), download},
		{"download holds no file", "/download-service/files/activity/100001", zipOf(), download},
		{"summary with a bad date", "/usersummary-service/v2/dailysummaries", []byte(`{"data":[{"calendarDate":"28/10"}]}`), health},
		{"sleep is not JSON", "/sleep-service/sleep/dailySleepSummariesByDate", []byte("<html>"), health},
		{"sleep with a bad date", "/sleep-service/sleep/dailySleepSummariesByDate", []byte(`[{"calendarDate":""}]`), health},
		{"hrv is not JSON", "/hrv-service/hrv/daily/2026-10-26/2026-10-28", []byte("["), health},
		{"hrv with a bad date", "/hrv-service/hrv/daily/2026-10-26/2026-10-28", []byte(`{"hrvSummaries":[{"calendarDate":"x"}]}`), health},
		{"weight is not JSON", "/weight-service/weight/range/2026-10-26/2026-10-28", []byte("nope"), health},
		{"weight with a bad date", "/weight-service/weight/range/2026-10-26/2026-10-28",
			[]byte(`{"dailyWeightSummaries":[{"summaryDate":"x","latestWeight":{"weight":70}}]}`), health},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			g := newReplay(t)
			c, _ := newClient(t, g)
			s := session(t, c)
			g.setOverride(func(w http.ResponseWriter, r *http.Request) bool {
				if r.URL.Path != tt.path {
					return false
				}
				_, _ = w.Write(tt.body)
				return true
			})
			if err := tt.call(c, s); !errors.Is(err, connectors.ErrUpstream) {
				t.Errorf("error = %v, want ErrUpstream", err)
			}
		})
	}

	t.Run("token is not a JWT", func(t *testing.T) {
		t.Parallel()
		g := newReplay(t)
		g.setOverride(func(w http.ResponseWriter, _ *http.Request) bool {
			_, _ = w.Write([]byte("opaque"))
			return true
		})
		c, _ := newClient(t, g)
		if _, err := c.Authenticate(t.Context(), creds); !errors.Is(err, connectors.ErrUpstream) {
			t.Errorf("error = %v, want ErrUpstream", err)
		}
	})
}

func TestExpiry(t *testing.T) {
	t.Parallel()
	enc := base64.RawURLEncoding.EncodeToString
	for name, token := range map[string]string{
		"two parts":   "a.b",
		"bad base64":  "a.!!!.c",
		"no exp":      "a." + enc([]byte(`{"sub":"x"}`)) + ".c",
		"not JSON":    "a." + enc([]byte(`nope`)) + ".c",
		"exp is text": "a." + enc([]byte(`{"exp":"soon"}`)) + ".c",
	} {
		if _, err := expiry(token); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	exp := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	if got, err := expiry(jwt(exp, 1)); err != nil || !got.Equal(exp) {
		t.Errorf("expiry = %v, %v", got, err)
	}
}

func TestPaginationAndChunking(t *testing.T) {
	t.Parallel()
	g := newReplay(t)
	c, _ := newClient(t, g)
	s := session(t, c)
	g.setOverride(func(w http.ResponseWriter, r *http.Request) bool {
		switch {
		case r.URL.Path == searchPath:
			n := pageSize
			if r.URL.Query().Get("start") != "0" {
				n = 1
			}
			page := make([]map[string]any, n)
			for i := range page {
				page[i] = map[string]any{"activityId": i + 1, "startTimeGMT": "2026-10-28 11:56:31"}
			}
			_ = json.NewEncoder(w).Encode(page)
			return true
		case r.URL.Path != tokenPath: // no health data in these ranges
			w.WriteHeader(http.StatusNoContent)
			return true
		}
		return false
	})
	acts, err := c.FetchActivities(t.Context(), s, date(1), date(28))
	if err != nil {
		t.Fatal(err)
	}
	if len(acts) != pageSize+1 || acts[0].LocalOffset != nil {
		t.Errorf("%d activities, first %+v", len(acts), acts[0])
	}

	// 40 days: chunks of 31 and 9 days, each calling four endpoints.
	before := len(g.paths())
	if _, err := c.FetchDailyMetrics(t.Context(), s, date(1), time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	var hrv []string
	for _, p := range g.paths()[before:] {
		if strings.HasPrefix(p, "/hrv-service/") {
			hrv = append(hrv, p)
		}
	}
	want := []string{"/hrv-service/hrv/daily/2026-10-01/2026-10-31", "/hrv-service/hrv/daily/2026-11-01/2026-11-09"}
	if n := len(g.paths()) - before; n != 8 || fmt.Sprint(hrv) != fmt.Sprint(want) {
		t.Errorf("hrv requests %v, want %v; %d requests", hrv, want, n)
	}
}

func TestTooManyActivities(t *testing.T) {
	t.Parallel()
	g := newReplay(t)
	c, _ := newClient(t, g)
	s := session(t, c)
	page := make([]map[string]any, pageSize)
	for i := range page {
		page[i] = map[string]any{"activityId": i + 1, "startTimeGMT": "2026-10-28 11:56:31"}
	}
	g.setOverride(func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path != searchPath {
			return false
		}
		_ = json.NewEncoder(w).Encode(page)
		return true
	})
	if _, err := c.FetchActivities(t.Context(), s, date(1), date(28)); !errors.Is(err, connectors.ErrUpstream) {
		t.Errorf("error = %v, want ErrUpstream", err)
	}
}

func TestHealthValues(t *testing.T) {
	t.Parallel()
	g := newReplay(t)
	c, _ := newClient(t, g)
	s := session(t, c)
	g.setOverride(func(w http.ResponseWriter, r *http.Request) bool {
		var body string
		switch r.URL.Path {
		case "/usersummary-service/v2/dailysummaries":
			body = `{"data":[{"calendarDate":"2026-10-26","heartRate":{"restingValue":0},
				"stress":{"avgLevel":-1},"bodyBattery":{"maxValue":-2,"minValue":5},
				"pulseOx":{"avgValue":95},"hydration":{"consumedInFractionalMl":1750.5}}]}`
		case "/weight-service/weight/range/2026-10-26/2026-10-26":
			body = `{"dailyWeightSummaries":[{"summaryDate":"2026-10-26","latestWeight":
				{"weight":70.4,"bodyFat":14.2,"bodyWater":58.1,"boneMass":3.4,"muscleMass":33.9}},
				{"summaryDate":"2026-10-27","latestWeight":null}]}`
		case "/sleep-service/sleep/dailySleepSummariesByDate":
			body = `[]`
		case "/hrv-service/hrv/daily/2026-10-26/2026-10-26":
			body = `{"hrvSummaries":[]}`
		default:
			return false
		}
		_, _ = w.Write([]byte(body))
		return true
	})
	days, err := c.FetchDailyMetrics(t.Context(), s, date(26), date(26))
	if err != nil {
		t.Fatal(err)
	}
	if len(days) != 1 {
		t.Fatalf("days = %+v", days)
	}
	d := days[0]
	// Garmin's negative "no data" values and a zero resting HR are dropped.
	if d.RestingHRBpm != nil || d.Stress != nil || d.BodyBatteryMax != nil || d.BodyBatteryMin == nil || *d.BodyBatteryMin != 5 {
		t.Errorf("sentinels kept: %+v", d)
	}
	for name, v := range map[string]struct {
		got  *float64
		want float64
	}{
		"spo2": {d.Spo2Pct, 95}, "hydration": {d.HydrationL, 1.7505}, "weight": {d.WeightKg, 70.4},
		"fat": {d.BodyFatPct, 14.2}, "water": {d.BodyWaterPct, 58.1}, "bone": {d.BoneMassKg, 3.4}, "muscle": {d.MuscleMassKg, 33.9},
	} {
		if v.got == nil || *v.got != v.want {
			t.Errorf("%s = %v, want %v", name, v.got, v.want)
		}
	}
}

func TestID(t *testing.T) {
	t.Parallel()
	if id := New(DefaultBaseURL, http.DefaultClient).ID(); id != "garmin" {
		t.Errorf("ID = %q", id)
	}
}
