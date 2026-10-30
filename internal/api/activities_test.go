package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/iskyd/kudos/internal/store/db"
)

// upload posts a multipart body with one field.
func (c *apiClient) upload(field, name string, content []byte) *httptest.ResponseRecorder {
	c.t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile(field, name)
	if err != nil {
		c.t.Fatal(err)
	}
	if _, err := part.Write(content); err != nil {
		c.t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		c.t.Fatal(err)
	}
	return c.send(http.MethodPost, "/api/activities/uploads", &buf, w.FormDataContentType(), nil)
}

func loggedIn(t *testing.T, tz string) (*apiClient, *db.Queries, Me) {
	t.Helper()
	c, q := newAPI(t, true, nil)
	me := c.session(c.do(http.MethodPost, "/api/auth/register", RegisterRequest{Username: "alice", Password: testPassword, Timezone: &tz}, nil), http.StatusCreated)
	return c, q, me
}

func decodeJSON[T any](t *testing.T, rec *httptest.ResponseRecorder, wantCode int) T {
	t.Helper()
	if rec.Code != wantCode {
		t.Fatalf("status = %d, want %d: %s", rec.Code, wantCode, rec.Body)
	}
	var v T
	if err := json.NewDecoder(rec.Body).Decode(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestUploadActivity(t *testing.T) {
	t.Parallel()
	c, q, me := loggedIn(t, "Europe/Rome")
	data, err := os.ReadFile("../../testdata/gpx/run.gpx")
	if err != nil {
		t.Fatal(err)
	}

	first := decodeJSON[UploadResult](t, c.upload("file", "run.gpx", data), http.StatusCreated)
	again := decodeJSON[UploadResult](t, c.upload("file", "RUN.GPX", data), http.StatusCreated)
	if len(first.ActivityIds) != 1 || len(again.ActivityIds) != 1 || first.ActivityIds[0] != again.ActivityIds[0] {
		t.Fatalf("ids = %v then %v, want the same single activity", first.ActivityIds, again.ActivityIds)
	}
	a, err := q.GetActivity(t.Context(), db.GetActivityParams{UserID: me.Id, ID: first.ActivityIds[0]})
	if err != nil {
		t.Fatal(err)
	}
	// GPX has no offset: the user's timezone (Rome, summer) applies.
	if a.Sport != "running" || a.UtcOffsetS != 7200 {
		t.Errorf("activity = %s offset %d", a.Sport, a.UtcOffsetS)
	}
}

func TestUploadActivityRejects(t *testing.T) {
	t.Parallel()
	c, _, _ := loggedIn(t, "UTC")
	wantProblem(t, c.upload("file", "notes.txt", []byte("hello")), KindUnsupportedFile)
	wantProblem(t, c.upload("file", "run.fit", []byte("not a fit file")), KindInvalidFile)
	wantProblem(t, c.upload("other", "run.gpx", []byte("<gpx/>")), KindInvalidRequest)
	wantProblem(t, c.send(http.MethodPost, "/api/activities/uploads", strings.NewReader("{}"), "application/json", nil), KindInvalidRequest)
	wantProblem(t, c.send(http.MethodPost, "/api/activities/uploads", strings.NewReader("garbage"), "multipart/form-data; boundary=x", nil), KindInvalidRequest)

	// Uploads need a session and the CSRF token.
	c.csrf = ""
	wantProblem(t, c.upload("file", "run.gpx", []byte("<gpx/>")), KindCSRF)
	anon, _ := newAPI(t, true, nil)
	wantProblem(t, anon.upload("file", "run.gpx", []byte("<gpx/>")), KindUnauthorized)
}

func TestReadFilePartSizeLimit(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		size int
		want Kind
	}{
		{size: maxUploadBytes},
		{size: maxUploadBytes + 1, want: KindFileTooLarge},
	} {
		// Stream the body so the test does not hold it twice.
		pr, pw := io.Pipe()
		w := multipart.NewWriter(pw)
		go func() {
			part, err := w.CreateFormFile("file", "big.fit")
			if err == nil {
				_, err = io.CopyN(part, zeros{}, int64(tt.size))
			}
			if err == nil {
				err = w.Close()
			}
			pw.CloseWithError(err)
		}()
		name, data, err := readFilePart(multipart.NewReader(pr, w.Boundary()))
		_, _ = io.Copy(io.Discard, pr) // let the writer finish after an early return
		if tt.want == "" {
			if err != nil || name != "big.fit" || len(data) != tt.size {
				t.Errorf("size %d: name %q, %d bytes, %v", tt.size, name, len(data), err)
			}
			continue
		}
		var apiErr *Error
		if !errors.As(err, &apiErr) || apiErr.Kind != tt.want {
			t.Errorf("size %d: error = %v, want %s", tt.size, err, tt.want)
		}
	}
}

type zeros struct{}

func (zeros) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

func TestCreateActivity(t *testing.T) {
	t.Parallel()
	c, q, me := loggedIn(t, "Europe/Rome")
	body := ManualActivityRequest{
		Sport: SportRunning, LocalStartTime: "2026-01-10T18:30", DurationS: 1800,
		DistanceM: new(5000.0), Rpe: new(6.5), Notes: new("Easy"),
	}
	created := decodeJSON[CreatedActivity](t, c.do(http.MethodPost, "/api/activities", body, nil), http.StatusCreated)
	a, err := q.GetActivity(t.Context(), db.GetActivityParams{UserID: me.Id, ID: created.Id})
	if err != nil {
		t.Fatal(err)
	}
	// 18:30 in Rome in January is 17:30 UTC.
	if a.Source != "manual" || a.Sport != "running" || !a.StartTime.Equal(time.Date(2026, 1, 10, 17, 30, 0, 0, time.UTC)) ||
		a.UtcOffsetS != 3600 || *a.DistanceM != 5000 || *a.Rpe != 6.5 || *a.Notes != "Easy" {
		t.Errorf("activity = %+v", a)
	}

	// The same sport and start updates instead of duplicating.
	again := decodeJSON[CreatedActivity](t, c.do(http.MethodPost, "/api/activities", body, nil), http.StatusCreated)
	if again.Id != created.Id {
		t.Errorf("resubmit created %d, want %d", again.Id, created.Id)
	}
}

func TestCreateActivityRejects(t *testing.T) {
	t.Parallel()
	c, _, _ := loggedIn(t, "UTC")
	valid := func() ManualActivityRequest {
		return ManualActivityRequest{Sport: SportCycling, LocalStartTime: "2026-01-10T18:30", DurationS: 3600}
	}
	for name, change := range map[string]func(*ManualActivityRequest){
		"unknown sport":     func(b *ManualActivityRequest) { b.Sport = "transition" },
		"bad start":         func(b *ManualActivityRequest) { b.LocalStartTime = "2026-01-10 18:30" },
		"zero duration":     func(b *ManualActivityRequest) { b.DurationS = 0 },
		"over 7 days":       func(b *ManualActivityRequest) { b.DurationS = maxDurationS + 1 },
		"negative distance": func(b *ManualActivityRequest) { b.DistanceM = new(-1.0) },
		"huge distance":     func(b *ManualActivityRequest) { b.DistanceM = new(math.MaxFloat64) },
		"rpe over 10":       func(b *ManualActivityRequest) { b.Rpe = new(10.5) },
		"notes over 2000":   func(b *ManualActivityRequest) { b.Notes = new(strings.Repeat("é", maxNotesLen+1)) },
	} {
		t.Run(name, func(t *testing.T) {
			b := valid()
			change(&b)
			wantProblem(t, c.do(http.MethodPost, "/api/activities", b, nil), KindInvalidRequest)
		})
	}
	// The limits themselves are accepted.
	b := valid()
	b.DurationS, b.DistanceM, b.Rpe, b.Notes = maxDurationS, new(0.0), new(10.0), new(strings.Repeat("é", maxNotesLen))
	decodeJSON[CreatedActivity](t, c.do(http.MethodPost, "/api/activities", b, nil), http.StatusCreated)
}

func TestLocation(t *testing.T) {
	t.Parallel()
	if location("Europe/Rome").String() != "Europe/Rome" || location("Not/AZone") != time.UTC {
		t.Error("location fallback broken")
	}
}

// listAll pages through path, limit 2 per page, and returns the activity notes in order.
func listAll(t *testing.T, c *apiClient, query string) []string {
	t.Helper()
	var got []string
	path := "/api/activities?limit=2" + query
	for range 10 {
		page := decodeJSON[ActivityPage](t, c.do(http.MethodGet, path, nil, nil), http.StatusOK)
		for _, a := range page.Items {
			got = append(got, string(a.Sport)+"@"+a.StartTime.Format("01-02"))
		}
		if page.NextCursor == nil {
			return got
		}
		path = "/api/activities?limit=2" + query + "&cursor=" + *page.NextCursor
	}
	t.Fatal("too many pages")
	return nil
}

func TestListActivities(t *testing.T) {
	t.Parallel()
	c, _, _ := loggedIn(t, "UTC")
	for _, a := range []ManualActivityRequest{
		{Sport: SportRunning, LocalStartTime: "2026-10-10T07:00", DurationS: 3000, DistanceM: new(10000.0)},
		{Sport: SportCycling, LocalStartTime: "2026-10-12T07:00", DurationS: 5400, DistanceM: new(40000.0)},
		{Sport: SportRunning, LocalStartTime: "2026-10-14T07:00", DurationS: 1800, DistanceM: new(5000.0)},
		{Sport: SportStrength, LocalStartTime: "2026-10-16T07:00", DurationS: 2400},
	} {
		decodeJSON[CreatedActivity](t, c.do(http.MethodPost, "/api/activities", a, nil), http.StatusCreated)
	}
	bob := &apiClient{t: t, h: c.h}
	bob.session(bob.do(http.MethodPost, "/api/auth/register", register("bob"), nil), http.StatusCreated)
	decodeJSON[CreatedActivity](t, bob.do(http.MethodPost, "/api/activities",
		ManualActivityRequest{Sport: SportRunning, LocalStartTime: "2026-10-13T07:00", DurationS: 99999, DistanceM: new(99000.0)}, nil), http.StatusCreated)

	tests := []struct{ query, want string }{
		{"", "[strength@10-16 running@10-14 cycling@10-12 running@10-10]"},
		{"&sort=distance", "[cycling@10-12 running@10-10 running@10-14 strength@10-16]"},
		{"&sort=duration", "[cycling@10-12 running@10-10 strength@10-16 running@10-14]"},
		{"&sport=running", "[running@10-14 running@10-10]"},
		{"&from=2026-10-12&to=2026-10-14", "[running@10-14 cycling@10-12]"},
		{"&sport=hiking", "[]"},
	}
	for _, tt := range tests {
		if got := fmt.Sprint(listAll(t, c, tt.query)); got != tt.want {
			t.Errorf("%q: %s, want %s", tt.query, got, tt.want)
		}
	}

	page := decodeJSON[ActivityPage](t, c.do(http.MethodGet, "/api/activities?limit=1", nil, nil), http.StatusOK)
	a := page.Items[0]
	if a.Source != Manual || a.Subtype != Indoor || a.TimerTimeS == nil || *a.TimerTimeS != 2400 || a.DistanceM != nil {
		t.Errorf("summary = %+v", a)
	}
	empty := decodeJSON[ActivityPage](t, c.do(http.MethodGet, "/api/activities?sport=hiking", nil, nil), http.StatusOK)
	if empty.Items == nil || empty.NextCursor != nil {
		t.Errorf("empty page = %+v, want an empty items array", empty)
	}

	// A cursor is bound to its sort order.
	wantProblem(t, c.do(http.MethodGet, "/api/activities?sort=distance&cursor="+*page.NextCursor, nil, nil), KindInvalidRequest)
	for _, q := range []string{"sort=load", "limit=0", "limit=101", "sport=curling", "from=2026-10-14&to=2026-10-12", "cursor=bogus", "from=yesterday"} {
		wantProblem(t, c.do(http.MethodGet, "/api/activities?"+q, nil, nil), KindInvalidRequest)
	}
}
