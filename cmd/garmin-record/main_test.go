package main

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/iskyd/kudos/internal/importer/fit"
)

const testKey = "test-personal-api-key"

// Values a real account would hold; none may reach the output.
var personal = []string{"987654321", "7654321", "Springfield run", "realuser", "Real Name", "55555555-6666-4777-8888-999999999999", "10.123456", "20.987654", testKey}

func jwt(t *testing.T, exp time.Time) string {
	t.Helper()
	claims, err := json.Marshal(map[string]any{"exp": exp.Unix(), "sub": "7654321"})
	if err != nil {
		t.Fatal(err)
	}
	enc := base64.RawURLEncoding.EncodeToString
	return enc([]byte(`{"alg":"RS256"}`)) + "." + enc(claims) + ".signature"
}

// fakeGarmin serves the endpoints with personal data, a real FIT in the download,
// and a 404 for one health endpoint. It records every request.
type fakeGarmin struct {
	t        *testing.T
	token    string
	mu       sync.Mutex
	requests []*http.Request
}

func (g *fakeGarmin) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	g.requests = append(g.requests, r)
	g.mu.Unlock()
	w.Header().Set("X-RateLimit-Remaining", "99")
	if r.URL.Path == tokenPath {
		if r.Method != http.MethodPost || r.Header.Get("Authorization") != "ApiKey "+testKey {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = io.WriteString(w, g.token)
		return
	}
	if r.Header.Get("Authorization") != "Bearer "+g.token {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	switch r.URL.Path {
	case "/activitylist-service/activities/search/activities":
		writeJSON(w, []any{
			map[string]any{"activityId": 987654321, "activityUUID": "55555555-6666-4777-8888-999999999999", "activityName": "Springfield run", "ownerDisplayName": "realuser",
				"ownerFullName": "Real Name", "startLatitude": 10.123456789, "startLongitude": 20.987654321, "distance": 5000.0},
			map[string]any{"activityId": 123123123, "activityName": "Not recorded in full"},
		})
	case "/activity-service/activity/987654321":
		writeJSON(w, map[string]any{"activityId": 987654321, "uuid": "55555555-6666-4777-8888-999999999999", "userProfileId": 7654321, "locationName": "Springfield run",
			"summaryDTO":     map[string]any{"startLatitude": 10.123456789, "duration": 1800.5},
			"geoPolylineDTO": map[string]any{"polyline": []any{map[string]any{"lat": 10.123456789}}}})
	case "/activity-service/activity/987654321/workouts":
		writeJSON(w, []any{})
	case "/download-service/files/activity/987654321":
		w.Header().Set("Content-Type", "application/x-zip-compressed")
		_, _ = w.Write(zipWith(g.t, "987654321_ACTIVITY.fit", readFile(g.t, "../../testdata/fit/hike.fit")))
	case "/hrv-service/hrv/2026-10-28":
		http.Error(w, `{"message":"not found"}`, http.StatusNotFound)
	default:
		writeJSON(w, map[string]any{"calendarDate": "2026-10-28", "userProfilePK": 7654321, "value": 42})
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func zipWith(t *testing.T, name string, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(name)
	if err == nil {
		_, err = w.Write(content)
	}
	if err == nil {
		err = zw.Close()
	}
	if err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func testConfig(t *testing.T, baseURL string) config {
	t.Helper()
	cfg, err := newConfig("2026-10-26", "2026-10-28", 1)
	if err != nil {
		t.Fatal(err)
	}
	cfg.baseURL, cfg.outDir = baseURL, t.TempDir()
	return cfg
}

func TestRecord(t *testing.T) {
	t.Parallel()
	g := &fakeGarmin{t: t, token: jwt(t, time.Now().Add(24*time.Hour))}
	srv := httptest.NewServer(g)
	defer srv.Close()
	cfg := testConfig(t, srv.URL)
	var log bytes.Buffer

	if err := record(t.Context(), srv.Client(), cfg, testKey, &log); err != nil {
		t.Fatal(err)
	}

	// One token, then sequential GETs with the bearer token over the configured range.
	if g.requests[0].URL.Path != tokenPath {
		t.Errorf("first request = %s, want the token exchange", g.requests[0].URL.Path)
	}
	search := g.requests[1].URL.Query()
	if search.Get("startDate") != "2026-10-26" || search.Get("endDate") != "2026-10-28" {
		t.Errorf("search query = %v", search)
	}
	// 1 token + 1 search + 3 for the activity + 13 health endpoints.
	if len(g.requests) != 18 {
		t.Errorf("%d requests, want 18", len(g.requests))
	}

	// Nothing personal, nor the key or token, in any file or in the log.
	entries, err := os.ReadDir(cfg.outDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		b := readFile(t, filepath.Join(cfg.outDir, e.Name()))
		if strings.HasSuffix(e.Name(), ".zip") {
			b = append(b, unzipAll(t, b)...)
		}
		for _, v := range append(personal, g.token) {
			if bytes.Contains(b, []byte(v)) {
				t.Errorf("%s contains a personal value from the fake account", e.Name())
			}
		}
	}
	for _, v := range append(personal, g.token) {
		if strings.Contains(log.String(), v) {
			t.Error("the log contains a personal value or secret")
		}
	}

	// Redacted values keep their shape; activity IDs are renumbered consistently.
	var details map[string]any
	if err := json.Unmarshal(readFile(t, filepath.Join(cfg.outDir, "activity_1.json")), &details); err != nil {
		t.Fatal(err)
	}
	summary, _ := details["summaryDTO"].(map[string]any)
	const fakeUUID = "00000000-0000-4000-8000-000000000001"
	if details["activityId"] != 100001.0 || details["uuid"] != fakeUUID || details["userProfileId"] != 1.0 || details["locationName"] != "redacted" ||
		summary["startLatitude"] != 0.0 || summary["duration"] != 1800.5 || details["geoPolylineDTO"] != nil {
		t.Errorf("activity_1.json = %v", details)
	}
	var found []map[string]any
	if err := json.Unmarshal(readFile(t, filepath.Join(cfg.outDir, "activities_search.json")), &found); err != nil {
		t.Fatal(err)
	}
	if found[0]["activityId"] != 100001.0 || found[0]["activityUUID"] != fakeUUID || found[0]["distance"] != 5000.0 || found[1]["activityName"] != "redacted" {
		t.Errorf("activities_search.json = %v", found)
	}

	// The download keeps a decodable FIT, renamed and shifted to the neutral meridian.
	zr, err := zip.OpenReader(filepath.Join(cfg.outDir, "activity_1.zip"))
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	if len(zr.File) != 1 || zr.File[0].Name != "100001_ACTIVITY.fit" {
		t.Fatalf("zip entries = %v", zr.File)
	}
	rc, err := zr.File[0].Open()
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	acts, err := fit.Decode(rc)
	if err != nil {
		t.Fatal(err)
	}
	if lon := acts[0].Samples[0].Lon; lon == nil || *lon > -29.99 || *lon < -30.01 {
		t.Errorf("first longitude = %v, want about -30", lon)
	}

	// The index records each call, statuses, rate-limit headers and the token's shape.
	var index []entry
	if err := json.Unmarshal(readFile(t, filepath.Join(cfg.outDir, "index.json")), &index); err != nil {
		t.Fatal(err)
	}
	byName := map[string]entry{}
	for _, e := range index {
		byName[e.Name] = e
	}
	tok := byName["token"]
	if strings.Join(tok.TokenClaims, ",") != "exp,sub" || !strings.HasPrefix(tok.TokenLifetime, "2") || tok.RateLimit["X-Ratelimit-Remaining"] != "99" {
		t.Errorf("token entry = %+v", tok)
	}
	if e := byName["hrv_day"]; e.Status != http.StatusNotFound {
		t.Errorf("hrv_day status = %d, want the recorded 404", e.Status)
	}
	if e := byName["activity_1_download"]; e.Path != "/download-service/files/activity/100001" || len(e.ZipEntries) != 1 {
		t.Errorf("download entry = %+v", e)
	}
	if _, ok := byName["activity_2"]; ok {
		t.Error("recorded more activities than asked")
	}
}

func unzipAll(t *testing.T, b []byte) []byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	var all []byte
	for _, f := range zr.File {
		content, err := readZipFile(f)
		if err != nil {
			t.Fatal(err)
		}
		all = append(append(all, f.Name...), content...)
	}
	return all
}

func TestRecordRejectedKey(t *testing.T) {
	t.Parallel()
	g := &fakeGarmin{t: t, token: jwt(t, time.Now().Add(time.Hour))}
	srv := httptest.NewServer(g)
	defer srv.Close()
	err := record(t.Context(), srv.Client(), testConfig(t, srv.URL), "wrong-key", io.Discard)
	if err == nil || !strings.Contains(err.Error(), "HTTP 401") || strings.Contains(err.Error(), "wrong-key") {
		t.Errorf("error = %v, want HTTP 401 without the key", err)
	}
	if len(g.requests) != 1 {
		t.Errorf("%d requests after a rejected key, want 1", len(g.requests))
	}
}

func TestRecordStopsOnLeak(t *testing.T) {
	t.Parallel()
	// The owner's name also appears in a field the redactor does not know.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == tokenPath {
			_, _ = io.WriteString(w, "token")
			return
		}
		writeJSON(w, []any{map[string]any{"ownerDisplayName": "realuser", "greeting": "hi realuser"}})
	}))
	defer srv.Close()
	err := record(t.Context(), srv.Client(), testConfig(t, srv.URL), testKey, io.Discard)
	if err == nil || !strings.Contains(err.Error(), `"ownerDisplayName"`) || strings.Contains(err.Error(), "realuser") {
		t.Errorf("error = %v, want a leak naming the field but not the value", err)
	}
}

func TestRecordRejectsUnexpectedZipContent(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == tokenPath:
			_, _ = io.WriteString(w, "token")
		case strings.HasPrefix(r.URL.Path, "/download-service/"):
			_, _ = w.Write(zipWith(t, "photo.jpg", []byte("jpeg")))
		case strings.HasPrefix(r.URL.Path, "/activitylist-service/"):
			writeJSON(w, []any{map[string]any{"activityId": 1}})
		default:
			writeJSON(w, map[string]any{})
		}
	}))
	defer srv.Close()
	err := record(t.Context(), srv.Client(), testConfig(t, srv.URL), testKey, io.Discard)
	if err == nil || !strings.Contains(err.Error(), `".jpg"`) {
		t.Errorf("error = %v, want the unexpected file type", err)
	}
}

func TestLoadKey(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	file := filepath.Join(dir, "key")
	if err := os.WriteFile(file, []byte("  from-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	empty := filepath.Join(dir, "empty")
	if err := os.WriteFile(empty, []byte("\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name    string
		env     map[string]string
		want    string
		wantErr bool
	}{
		{name: "variable", env: map[string]string{"GARMIN_PERSONAL_API_KEY": " from-env ", "GARMIN_PERSONAL_API_KEY_FILE": file}, want: "from-env"},
		{name: "file", env: map[string]string{"GARMIN_PERSONAL_API_KEY_FILE": file}, want: "from-file"},
		{name: "none", env: map[string]string{}, wantErr: true},
		{name: "missing file", env: map[string]string{"GARMIN_PERSONAL_API_KEY_FILE": filepath.Join(dir, "nope")}, wantErr: true},
		{name: "empty file", env: map[string]string{"GARMIN_PERSONAL_API_KEY_FILE": empty}, wantErr: true},
	} {
		got, err := loadKey(func(k string) string { return tt.env[k] })
		if (err != nil) != tt.wantErr || got != tt.want {
			t.Errorf("%s: loadKey = %q, %v", tt.name, got, err)
		}
	}
}

func TestNewConfig(t *testing.T) {
	t.Parallel()
	cfg, err := newConfig("", "2026-10-28", 2)
	if err != nil || cfg.from.Format(dateLayout) != "2026-10-26" || cfg.activities != 2 {
		t.Errorf("default range = %+v, %v", cfg, err)
	}
	for _, tt := range []struct {
		from, to   string
		activities int
	}{
		{"", "28-10-2026", 2}, {"yesterday", "2026-10-28", 2}, {"2026-10-29", "2026-10-28", 2},
		{"2026-01-01", "2026-10-28", 2}, {"", "2026-10-28", 11},
	} {
		if _, err := newConfig(tt.from, tt.to, tt.activities); err == nil {
			t.Errorf("newConfig(%+v) accepted", tt)
		}
	}
}

func TestDescribeJWT(t *testing.T) {
	t.Parallel()
	for token, want := range map[string]string{
		"opaque":         "not a JWT",
		"a.!!!.c":        "unreadable payload",
		"a.bm90anNvbg.c": "unreadable payload",
		"a." + base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"x"}`)) + ".c": "no exp claim",
	} {
		if _, got := describeJWT(token); got != want {
			t.Errorf("describeJWT(%q) lifetime = %q, want %q", token, got, want)
		}
	}
}
