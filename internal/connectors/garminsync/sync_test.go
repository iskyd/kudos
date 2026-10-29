package garminsync

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/riverqueue/river"

	"github.com/iskyd/kudos/internal/connectors"
)

// backfilled runs alice's backfill (1 Oct..9 Nov) to the end and sets the
// clock to 10 Nov, the day after.
func backfilled(t *testing.T) *env {
	t.Helper()
	e := newEnv(t)
	if err := e.work(t); err != nil {
		t.Fatal(err)
	}
	e.worker.now = func() time.Time { return time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC) }
	return e
}

func (e *env) runSync(t *testing.T) error {
	t.Helper()
	return e.sync.Work(t.Context(), &river.Job[SyncArgs]{Args: SyncArgs{UserID: e.alice}})
}

func (e *env) steps(t *testing.T, d time.Time) (int, bool) {
	t.Helper()
	var n *int
	err := e.pool.QueryRow(t.Context(), "SELECT steps FROM daily_metrics WHERE user_id = $1 AND day = $2", e.alice, d).Scan(&n)
	if err != nil || n == nil {
		return 0, false
	}
	return *n, true
}

func TestSyncContinuesFromTheBackfillWithLookback(t *testing.T) {
	t.Parallel()
	e := backfilled(t)
	ctx := t.Context()
	// A new activity, a revised day (8 Nov, inside the lookback), a new day, and
	// a revision before the lookback that the sync must not fetch.
	e.fake.acts = append(e.fake.acts, connectors.RawActivity{SourceID: "4", Start: time.Date(2026, 11, 10, 6, 0, 0, 0, time.UTC)})
	e.fake.files["4"] = connectors.File{Name: "4.gpx", Data: bytes.ReplaceAll(gpx(t, "run.gpx"), []byte("2026-05-01"), []byte("2026-11-10"))}
	e.fake.days = []connectors.DailyMetrics{
		{Day: day(11, 7), Steps: steps(999)},
		{Day: day(11, 8), Steps: steps(2500)},
		{Day: day(11, 10), Steps: steps(3000)},
	}

	if err := e.runSync(t); err != nil {
		t.Fatal(err)
	}
	if n := e.activities(t); n != 3 {
		t.Errorf("%d activities, want 3", n)
	}
	for d, want := range map[int]int{8: 2500, 10: 3000, 7: 0} {
		got, ok := e.steps(t, day(11, d))
		if want == 0 && ok {
			t.Errorf("7 Nov fetched (%d steps): before the lookback", got)
		}
		if want != 0 && got != want {
			t.Errorf("%d Nov steps = %d, want %d", d, got, want)
		}
	}
	// Activities already imported are not downloaded again; the failed one stays failed.
	if e.fake.downloads["1"] != 1 || e.fake.downloads["3"] != 1 {
		t.Errorf("downloads = %v", e.fake.downloads)
	}
	conn, err := e.q.GetGarminConnection(ctx, e.alice)
	if err != nil {
		t.Fatal(err)
	}
	if !conn.SyncedThrough.Equal(day(11, 10)) || conn.LastAttemptAt == nil || conn.LastError != nil {
		t.Errorf("connection = through %v, attempt %v, error %v", conn.SyncedThrough, conn.LastAttemptAt, conn.LastError)
	}
}

func TestSyncNow(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := t.Context()

	if _, err := e.svc.SyncNow(ctx, e.alice); !errors.Is(err, ErrBackfillRunning) {
		t.Errorf("during the backfill: %v", err)
	}
	if _, err := e.svc.SyncNow(ctx, e.bob); !errors.Is(err, ErrNotConnected) {
		t.Errorf("without a key: %v", err)
	}
	if err := e.work(t); err != nil {
		t.Fatal(err)
	}

	first, err := e.svc.SyncNow(ctx, e.alice)
	if err != nil || !first {
		t.Fatalf("first sync now: %v, %v", first, err)
	}
	// A sync is already queued (or running): no second run.
	again, err := e.svc.SyncNow(ctx, e.alice)
	if err != nil || again {
		t.Errorf("second sync now queued %v, %v", again, err)
	}
	if n := e.count(t, "SELECT count(*) FROM river_job WHERE kind = 'garmin_sync' AND (args->>'user_id')::bigint = $1"); n != 1 {
		t.Errorf("%d sync jobs, want 1", n)
	}

	if err := e.q.SetGarminKeyRejected(ctx, e.alice); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.SyncNow(ctx, e.alice); !errors.Is(err, connectors.ErrKeyRejected) {
		t.Errorf("with a rejected key: %v", err)
	}
}

func TestSyncPausesOnKeyRejection(t *testing.T) {
	t.Parallel()
	e := backfilled(t)
	e.fake.listErr = fmt.Errorf("GET: %w", connectors.ErrKeyRejected)
	lists := e.fake.lists

	if err := e.runSync(t); err != nil {
		t.Fatal(err)
	}
	conn, err := e.q.GetGarminConnection(t.Context(), e.alice)
	if err != nil || conn.Status != "key_rejected" {
		t.Fatalf("status = %q, %v", conn.Status, err)
	}
	// Paused: neither the job nor the scheduler calls Garmin again.
	if err := e.runSync(t); err != nil || e.fake.lists != lists+1 {
		t.Errorf("after the rejection: %v, %d listings", err, e.fake.lists-lists)
	}
	if ids, err := e.q.ListDueGarminSyncs(t.Context(), 10); err != nil || len(ids) != 0 {
		t.Errorf("due = %v, %v", ids, err)
	}
}

func TestSyncRecordsErrors(t *testing.T) {
	t.Parallel()
	for name, tt := range map[string]struct {
		err  error
		want string
	}{
		"rate limited": {connectors.ErrRateLimited, "Garmin is limiting requests"},
		"upstream":     {fmt.Errorf("%w: status 503", connectors.ErrUpstream), "Garmin failed"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			e := backfilled(t)
			e.fake.errs["health"] = tt.err
			if err := e.runSync(t); err != nil {
				t.Fatal(err)
			}
			conn, err := e.q.GetGarminConnection(t.Context(), e.alice)
			if err != nil || conn.LastError == nil || !strings.HasPrefix(*conn.LastError, tt.want) {
				t.Fatalf("last error = %v, %v", conn.LastError, err)
			}
			if !conn.SyncedThrough.Equal(day(11, 9)) {
				t.Errorf("the cursor moved on a failed sync: %v", conn.SyncedThrough)
			}
			// The next sync succeeds and clears the error.
			if err := e.runSync(t); err != nil {
				t.Fatal(err)
			}
			if conn, _ = e.q.GetGarminConnection(t.Context(), e.alice); conn.LastError != nil || !conn.SyncedThrough.Equal(day(11, 10)) {
				t.Errorf("after recovering: error %v, through %v", conn.LastError, conn.SyncedThrough)
			}
		})
	}
}

func TestSyncSkips(t *testing.T) {
	t.Parallel()
	e := newEnv(t) // backfill still running
	if err := e.runSync(t); err != nil || e.fake.lists != 0 {
		t.Errorf("during the backfill: %v, %d listings", err, e.fake.lists)
	}
	if _, err := e.q.DeleteGarminConnection(t.Context(), e.alice); err != nil {
		t.Fatal(err)
	}
	if err := e.runSync(t); err != nil || e.fake.lists != 0 {
		t.Errorf("disconnected: %v, %d listings", err, e.fake.lists)
	}
	if err := e.runSync(t); err != nil {
		t.Fatal(err)
	}
}

func TestSyncBudget(t *testing.T) {
	t.Parallel()
	e := backfilled(t)
	e.fake.acts = append(e.fake.acts, connectors.RawActivity{SourceID: "4", Start: time.Date(2026, 11, 10, 6, 0, 0, 0, time.UTC)})
	e.fake.files["4"] = connectors.File{Name: "4.gpx", Data: bytes.ReplaceAll(gpx(t, "run.gpx"), []byte("2026-05-01"), []byte("2026-11-10"))}
	calls := 0
	e.worker.now = func() time.Time {
		calls++
		return time.Date(2026, 11, 10, 12, calls*5, 0, 0, time.UTC)
	}
	if _, ok := snoozed(e.runSync(t)); !ok {
		t.Fatal("a sync over budget does not snooze")
	}
	if err := e.runSync(t); err != nil {
		t.Fatal(err)
	}
	if n := e.activities(t); n != 3 {
		t.Errorf("%d activities, want 3", n)
	}
}

func TestScheduler(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := t.Context()
	sched := &ScheduleWorker{Pool: e.pool, Service: e.svc}
	schedule := func() int {
		t.Helper()
		if err := sched.Work(ctx, &river.Job[ScheduleArgs]{}); err != nil {
			t.Fatal(err)
		}
		return e.count(t, "SELECT count(*) FROM river_job WHERE kind = 'garmin_sync' AND (args->>'user_id')::bigint = $1")
	}

	if n := schedule(); n != 0 {
		t.Errorf("queued %d syncs during the backfill", n)
	}
	if err := e.work(t); err != nil {
		t.Fatal(err)
	}
	if n := schedule(); n != 1 {
		t.Errorf("queued %d syncs after the backfill, want 1", n)
	}
	// Once attempted, alice is not due again until her interval passes.
	if err := e.q.StartGarminSyncAttempt(ctx, e.alice); err != nil {
		t.Fatal(err)
	}
	if ids, err := e.q.ListDueGarminSyncs(ctx, 10); err != nil || len(ids) != 0 {
		t.Errorf("due right after an attempt: %v, %v", ids, err)
	}
	if _, err := e.pool.Exec(ctx, "UPDATE garmin_connections SET last_attempt_at = now() - interval '61 minutes' WHERE user_id = $1", e.alice); err != nil {
		t.Fatal(err)
	}
	if ids, err := e.q.ListDueGarminSyncs(ctx, 10); err != nil || len(ids) != 1 || ids[0] != e.alice {
		t.Errorf("due after the interval: %v, %v", ids, err)
	}
	if p := SchedulePeriodic(); p == nil || (ScheduleArgs{}).Kind() != "garmin_sync_schedule" || (SyncArgs{}).Kind() != "garmin_sync" {
		t.Error("scheduler wiring changed")
	}
	if e.sync.Timeout(nil) != jobTimeout {
		t.Error("sync timeout changed")
	}
}

func TestSyncFrom(t *testing.T) {
	t.Parallel()
	today := day(11, 10)
	for name, tt := range map[string]struct {
		cursor *time.Time
		want   time.Time
	}{
		"no cursor":        {nil, day(11, 9)},
		"cursor today":     {new(day(11, 10)), day(11, 9)},
		"cursor yesterday": {new(day(11, 9)), day(11, 8)},
		"cursor last week": {new(day(11, 3)), day(11, 2)},
		"cursor years ago": {new(time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)), today.AddDate(0, 0, 1-maxGapDays)},
	} {
		if got := syncFrom(tt.cursor, today); !got.Equal(tt.want) {
			t.Errorf("%s: from = %v, want %v", name, got, tt.want)
		}
	}
}
