package api

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/iskyd/kudos/internal/store/db"
)

// connect gives the client's user a Garmin key.
func connect(t *testing.T, c *apiClient) {
	t.Helper()
	decodeJSON[GarminConnection](t, c.do(http.MethodPut, "/api/garmin/key", SetGarminKeyRequest{Key: validKey, ExpiresOn: daysAhead(30)}, nil), http.StatusOK)
}

func TestStartGarminBackfill(t *testing.T) {
	t.Parallel()
	c, _, _ := loggedIn(t, "UTC")

	if got := decodeJSON[GarminBackfill](t, c.do(http.MethodGet, "/api/garmin/backfill", nil, nil), http.StatusOK); got.Started {
		t.Fatalf("new user has a backfill: %+v", got)
	}
	wantProblem(t, c.do(http.MethodPost, "/api/garmin/backfill", StartGarminBackfillRequest{}, nil), KindNotFound)

	connect(t, c)
	for _, days := range []int{0, 366} {
		wantProblem(t, c.do(http.MethodPost, "/api/garmin/backfill", StartGarminBackfillRequest{Days: &days}, nil), KindInvalidRequest)
	}
	got := decodeJSON[GarminBackfill](t, c.do(http.MethodPost, "/api/garmin/backfill", StartGarminBackfillRequest{}, nil), http.StatusAccepted)
	today := daysAhead(0).Time
	if !got.Started || *got.Status != GarminBackfillStatusRunning || *got.DaysTotal != 120 || *got.DaysDone != 0 ||
		!got.WindowEnd.Equal(today) || !got.WindowStart.Equal(today.AddDate(0, 0, -119)) || *got.ActivitiesTotal != 0 {
		t.Errorf("backfill = %+v", got)
	}
	wantProblem(t, c.do(http.MethodPost, "/api/garmin/backfill", StartGarminBackfillRequest{}, nil), KindBackfillExists)
	if got := decodeJSON[GarminBackfill](t, c.do(http.MethodGet, "/api/garmin/backfill", nil, nil), http.StatusOK); !got.Started {
		t.Errorf("progress = %+v", got)
	}
}

func TestResumeGarminBackfill(t *testing.T) {
	t.Parallel()
	c, q, me := loggedIn(t, "UTC")
	retry := true
	wantProblem(t, c.do(http.MethodPost, "/api/garmin/backfill/resume", ResumeGarminBackfillRequest{}, nil), KindNotFound)

	connect(t, c)
	days := 7
	decodeJSON[GarminBackfill](t, c.do(http.MethodPost, "/api/garmin/backfill", StartGarminBackfillRequest{Days: &days}, nil), http.StatusAccepted)
	reason := "rate_limited"
	if err := q.PauseGarminBackfill(t.Context(), db.PauseGarminBackfillParams{UserID: me.Id, PauseReason: &reason}); err != nil {
		t.Fatal(err)
	}
	if err := q.SetGarminBackfillHealthThrough(t.Context(), db.SetGarminBackfillHealthThroughParams{UserID: me.Id, HealthThrough: new(daysAhead(-4).Time)}); err != nil {
		t.Fatal(err)
	}
	failImport(t, q, me.Id, "41", time.Date(2026, 10, 1, 7, 0, 0, 0, time.UTC))

	got := decodeJSON[GarminBackfill](t, c.do(http.MethodGet, "/api/garmin/backfill", nil, nil), http.StatusOK)
	if *got.Status != GarminBackfillStatusPaused || *got.PauseReason != GarminBackfillPauseReasonRateLimited ||
		*got.ActivitiesFailed != 1 || *got.DaysDone != 3 || *got.DaysTotal != 7 {
		t.Fatalf("paused backfill = %+v", got)
	}
	got = decodeJSON[GarminBackfill](t, c.do(http.MethodPost, "/api/garmin/backfill/resume", ResumeGarminBackfillRequest{RetryFailed: &retry}, nil), http.StatusOK)
	if *got.Status != GarminBackfillStatusRunning || got.PauseReason != nil || *got.ActivitiesFailed != 0 {
		t.Errorf("resumed backfill = %+v", got)
	}
}

// failImport records a failed import for userID.
func failImport(t *testing.T, q *db.Queries, userID int64, id string, start time.Time) {
	t.Helper()
	reason := "invalid file: not a FIT file"
	if err := q.AddGarminActivityImport(t.Context(), db.AddGarminActivityImportParams{UserID: userID, SourceActivityID: id, StartTime: start}); err != nil {
		t.Fatal(err)
	}
	if err := q.SetGarminActivityImportFailed(t.Context(), db.SetGarminActivityImportFailedParams{UserID: userID, SourceActivityID: id, Error: &reason}); err != nil {
		t.Fatal(err)
	}
}

func TestListGarminBackfillFailures(t *testing.T) {
	t.Parallel()
	c, q, me := loggedIn(t, "UTC")
	start := time.Date(2026, 10, 1, 7, 0, 0, 0, time.UTC)
	for i := range 5 {
		failImport(t, q, me.Id, fmt.Sprint(i+1), start.Add(time.Duration(i)*time.Hour))
	}
	// Another user's failures never show.
	bob := &apiClient{t: t, h: c.h}
	bobMe := bob.session(bob.do(http.MethodPost, "/api/auth/register", register("bob"), nil), http.StatusCreated)
	failImport(t, q, bobMe.Id, "99", start)

	var ids []string
	path := "/api/garmin/backfill/failures?limit=2"
	for range 5 {
		page := decodeJSON[GarminImportFailurePage](t, c.do(http.MethodGet, path, nil, nil), http.StatusOK)
		for _, f := range page.Items {
			ids = append(ids, f.SourceActivityId)
			if f.Reason != "invalid file: not a FIT file" {
				t.Errorf("reason = %q", f.Reason)
			}
		}
		if page.NextCursor == nil {
			break
		}
		path = "/api/garmin/backfill/failures?limit=2&cursor=" + *page.NextCursor
	}
	if fmt.Sprint(ids) != "[1 2 3 4 5]" {
		t.Errorf("ids = %v", ids)
	}

	page := decodeJSON[GarminImportFailurePage](t, bob.do(http.MethodGet, "/api/garmin/backfill/failures", nil, nil), http.StatusOK)
	if len(page.Items) != 1 || page.Items[0].SourceActivityId != "99" || page.NextCursor != nil {
		t.Errorf("bob's failures = %+v", page)
	}
	carol := &apiClient{t: t, h: c.h}
	carol.session(carol.do(http.MethodPost, "/api/auth/register", register("carol"), nil), http.StatusCreated)
	empty := decodeJSON[GarminImportFailurePage](t, carol.do(http.MethodGet, "/api/garmin/backfill/failures", nil, nil), http.StatusOK)
	if empty.Items == nil || len(empty.Items) != 0 {
		t.Errorf("empty page = %+v, want an empty items array", empty)
	}

	wantProblem(t, c.do(http.MethodGet, "/api/garmin/backfill/failures?cursor=bogus", nil, nil), KindInvalidRequest)
	wantProblem(t, c.do(http.MethodGet, "/api/garmin/backfill/failures?limit=0", nil, nil), KindInvalidRequest)
}

func TestNewKeyResumesBackfill(t *testing.T) {
	t.Parallel()
	c, q, me := loggedIn(t, "UTC")
	connect(t, c)
	decodeJSON[GarminBackfill](t, c.do(http.MethodPost, "/api/garmin/backfill", StartGarminBackfillRequest{}, nil), http.StatusAccepted)
	reason := "key_rejected"
	if err := q.PauseGarminBackfill(t.Context(), db.PauseGarminBackfillParams{UserID: me.Id, PauseReason: &reason}); err != nil {
		t.Fatal(err)
	}
	connect(t, c)
	if got := decodeJSON[GarminBackfill](t, c.do(http.MethodGet, "/api/garmin/backfill", nil, nil), http.StatusOK); *got.Status != GarminBackfillStatusRunning {
		t.Errorf("after a new key: %+v", got)
	}
}

func TestSyncGarmin(t *testing.T) {
	t.Parallel()
	c, q, me := loggedIn(t, "UTC")
	wantProblem(t, c.do(http.MethodPost, "/api/garmin/sync", nil, nil), KindNotFound)

	connect(t, c)
	if got := decodeJSON[SyncQueued](t, c.do(http.MethodPost, "/api/garmin/sync", nil, nil), http.StatusAccepted); !got.Queued {
		t.Errorf("first sync = %+v", got)
	}
	if got := decodeJSON[SyncQueued](t, c.do(http.MethodPost, "/api/garmin/sync", nil, nil), http.StatusAccepted); got.Queued {
		t.Errorf("a second sync was queued while one is pending")
	}

	reason := "Garmin failed: status 503"
	if err := q.SetGarminSyncError(t.Context(), db.SetGarminSyncErrorParams{UserID: me.Id, LastError: &reason}); err != nil {
		t.Fatal(err)
	}
	if got := decodeJSON[GarminConnection](t, c.do(http.MethodGet, "/api/garmin", nil, nil), http.StatusOK); got.LastError == nil || *got.LastError != reason {
		t.Errorf("last error = %v", got.LastError)
	}

	decodeJSON[GarminBackfill](t, c.do(http.MethodPost, "/api/garmin/backfill", StartGarminBackfillRequest{}, nil), http.StatusAccepted)
	wantProblem(t, c.do(http.MethodPost, "/api/garmin/sync", nil, nil), KindBackfillRunning)

	if err := q.SetGarminKeyRejected(t.Context(), me.Id); err != nil {
		t.Fatal(err)
	}
	wantProblem(t, c.do(http.MethodPost, "/api/garmin/sync", nil, nil), KindGarminKeyRejected)
}
