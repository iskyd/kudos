package garminsync

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/iskyd/kudos/internal/connectors"
	"github.com/iskyd/kudos/internal/crypto"
	"github.com/iskyd/kudos/internal/importer/ingest"
	"github.com/iskyd/kudos/internal/jobs"
	"github.com/iskyd/kudos/internal/secret"
	"github.com/iskyd/kudos/internal/store/db"
	"github.com/iskyd/kudos/internal/testutil"
)

var pg *testutil.Postgres

func TestMain(m *testing.M) {
	ctx := context.Background()
	var err error
	if pg, err = testutil.StartPostgres(ctx); err != nil {
		log.Fatal(err)
	}
	code := m.Run()
	if err := pg.Terminate(ctx); err != nil {
		log.Print(err)
	}
	os.Exit(code)
}

func day(m time.Month, d int) time.Time { return time.Date(2026, m, d, 0, 0, 0, 0, time.UTC) }

func gpx(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("../../../testdata/gpx/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// fakeGarmin serves activities, files and daily metrics. errs fails a download
// (by activity ID) or the health fetch ("health") once.
type fakeGarmin struct {
	mu        sync.Mutex
	acts      []connectors.RawActivity
	files     map[string]connectors.File
	days      []connectors.DailyMetrics
	errs      map[string]error
	listErr   error
	downloads map[string]int
	reMint    string // when set, the first call renews the session's token
}

func (f *fakeGarmin) ID() string { return "garmin" }

func (f *fakeGarmin) Authenticate(context.Context, connectors.Credentials) (*connectors.Session, error) {
	return nil, errors.New("not used")
}

func (f *fakeGarmin) FetchActivities(_ context.Context, s *connectors.Session, _, _ time.Time) ([]connectors.RawActivity, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.reMint != "" {
		s.Token, s.Expires = secret.New(f.reMint), time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	}
	return f.acts, f.listErr
}

func (f *fakeGarmin) DownloadActivityFile(_ context.Context, _ *connectors.Session, id string) (connectors.File, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.downloads[id]++
	if err := f.errs[id]; err != nil {
		delete(f.errs, id)
		return connectors.File{}, err
	}
	return f.files[id], nil
}

func (f *fakeGarmin) FetchDailyMetrics(_ context.Context, _ *connectors.Session, from, to time.Time) ([]connectors.DailyMetrics, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.errs["health"]; err != nil {
		delete(f.errs, "health")
		return nil, err
	}
	var out []connectors.DailyMetrics
	for _, d := range f.days {
		if !d.Day.Before(from) && !d.Day.After(to) {
			out = append(out, d)
		}
	}
	return out, nil
}

type env struct {
	pool   *pgxpool.Pool
	q      *db.Queries
	svc    *Service
	worker *Worker
	fake   *fakeGarmin
	cipher *crypto.Cipher
	alice  int64
	bob    int64
}

func steps(n int) *int { return &n }

// newEnv connects alice with a key and starts her backfill of 1 Oct..9 Nov
// (40 days): three activities, one of them unreadable, and two days of metrics.
func newEnv(t *testing.T) *env {
	t.Helper()
	pool := pg.NewDB(t)
	logger := slog.New(slog.DiscardHandler)
	cipher, err := crypto.New(bytes.Repeat([]byte{3}, crypto.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	// Wired as in cmd/server: the worker is registered, then given the ingest service.
	w := &Worker{Pool: pool, Cipher: cipher, Logger: logger}
	client, err := jobs.NewClient(pool, logger, func(ws *river.Workers) { river.AddWorker(ws, w) })
	if err != nil {
		t.Fatal(err)
	}
	w.Ingest = ingest.New(pool, client, t.TempDir())
	alice, bob := testutil.TwoUsers(t, pool)
	hour := time.Hour
	fake := &fakeGarmin{
		acts: []connectors.RawActivity{
			{SourceID: "1", Start: time.Date(2026, 5, 1, 6, 0, 0, 0, time.UTC), LocalOffset: &hour},
			{SourceID: "2", Start: time.Date(2026, 5, 2, 14, 0, 0, 0, time.UTC)},
			{SourceID: "3", Start: time.Date(2026, 5, 3, 7, 0, 0, 0, time.UTC)},
		},
		files: map[string]connectors.File{
			"1": {Name: "1_ACTIVITY.gpx", Data: gpx(t, "run.gpx")},
			"2": {Name: "2_ACTIVITY.gpx", Data: gpx(t, "ride_no_hr_no_ele.gpx")},
			"3": {Name: "3_ACTIVITY.fit", Data: []byte("not a fit file")},
		},
		days: []connectors.DailyMetrics{
			{Day: day(10, 2), Steps: steps(1000)},
			{Day: day(11, 8), Steps: steps(2000)},
		},
		errs:      map[string]error{},
		downloads: map[string]int{},
	}
	q := db.New(pool)
	key, _ := cipher.Seal([]byte("the-key"))
	token, _ := cipher.Seal([]byte("the-token"))
	exp := time.Now().Add(20 * time.Hour)
	if _, err := q.UpsertGarminKey(t.Context(), db.UpsertGarminKeyParams{
		UserID: alice.ID, KeyCiphertext: key, KeyExpiresOn: day(12, 31), TokenCiphertext: token, TokenExpiresAt: &exp,
	}); err != nil {
		t.Fatal(err)
	}
	svc := NewService(pool, client)
	if err := svc.Start(t.Context(), alice.ID, day(10, 1), day(11, 9)); err != nil {
		t.Fatal(err)
	}
	w.Connector = fake
	return &env{pool: pool, q: q, svc: svc, worker: w, fake: fake, cipher: cipher, alice: alice.ID, bob: bob.ID}
}

func (e *env) work(t *testing.T) error {
	t.Helper()
	return e.worker.Work(t.Context(), &river.Job[BackfillArgs]{Args: BackfillArgs{UserID: e.alice}})
}

func (e *env) progress(t *testing.T) Progress {
	t.Helper()
	p, err := e.svc.Progress(t.Context(), e.alice)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func (e *env) count(t *testing.T, sql string) int {
	t.Helper()
	var n int
	if err := e.pool.QueryRow(t.Context(), sql, e.alice).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func (e *env) activities(t *testing.T) int {
	return e.count(t, "SELECT count(*) FROM activities WHERE user_id = $1 AND source = 'garmin'")
}

func (e *env) metrics(t *testing.T) int {
	return e.count(t, "SELECT count(*) FROM daily_metrics WHERE user_id = $1")
}

func snoozed(err error) (time.Duration, bool) {
	var s *river.JobSnoozeError
	if errors.As(err, &s) {
		return s.Duration, true
	}
	return 0, false
}

func TestFullRun(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.fake.reMint = "renewed-token"
	ctx := t.Context()

	if err := e.work(t); err != nil {
		t.Fatal(err)
	}
	p := e.progress(t)
	if p.Backfill.Status != "done" || p.Activities.Total != 3 || p.Activities.Done != 2 || p.Activities.Failed != 1 {
		t.Fatalf("progress = %+v", p)
	}
	if !p.Backfill.HealthThrough.Equal(day(11, 9)) {
		t.Errorf("health through %v", p.Backfill.HealthThrough)
	}
	if n := e.activities(t); n != 2 {
		t.Errorf("%d activities, want 2", n)
	}
	if n := e.metrics(t); n != 2 {
		t.Errorf("%d days of metrics, want 2", n)
	}
	// Garmin's offset is used when the file has none (GPX).
	var offset int
	if err := e.pool.QueryRow(ctx, "SELECT utc_offset_s FROM activities WHERE user_id = $1 AND source_activity_id = '1'", e.alice).Scan(&offset); err != nil || offset != 3600 {
		t.Errorf("offset = %d, %v, want 3600 from Garmin", offset, err)
	}

	failures, err := e.svc.Failures(ctx, e.alice, time.Time{}, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(failures) != 1 || failures[0].SourceActivityID != "3" || !strings.Contains(*failures[0].Error, "fit") {
		t.Errorf("failures = %+v", failures)
	}

	conn, err := e.q.GetGarminConnection(ctx, e.alice)
	if err != nil {
		t.Fatal(err)
	}
	if conn.SyncedThrough == nil || !conn.SyncedThrough.Equal(day(11, 9)) || conn.LastSyncAt == nil {
		t.Errorf("connection synced through %v at %v", conn.SyncedThrough, conn.LastSyncAt)
	}
	// The re-minted token is kept, encrypted.
	if token, err := e.cipher.Open(conn.TokenCiphertext); err != nil || string(token) != "renewed-token" {
		t.Errorf("stored token = %q, %v", token, err)
	}

	// A finished backfill does nothing more.
	if err := e.work(t); err != nil {
		t.Fatal(err)
	}
	if e.fake.downloads["1"] != 1 {
		t.Errorf("activity 1 downloaded %d times", e.fake.downloads["1"])
	}
}

func TestInterruptAndResume(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	// Each clock read advances a minute: a run covers about one step, then snoozes.
	var mu sync.Mutex
	clock := time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)
	e.worker.now = func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		clock = clock.Add(time.Minute)
		return clock
	}
	e.worker.budget = 90 * time.Second

	runs := 0
	for ; runs < 20; runs++ {
		err := e.work(t)
		if err == nil {
			break
		}
		if _, ok := snoozed(err); !ok {
			t.Fatal(err)
		}
	}
	if runs < 2 || e.progress(t).Backfill.Status != "done" {
		t.Fatalf("%d runs, status %s", runs, e.progress(t).Backfill.Status)
	}
	for id, n := range e.fake.downloads {
		if n != 1 {
			t.Errorf("activity %s downloaded %d times", id, n)
		}
	}

	// A restart that lost every bookkeeping write redoes the work without duplicates.
	_, err := e.pool.Exec(t.Context(), "UPDATE garmin_activity_imports SET status = 'pending', error = NULL WHERE user_id = $1", e.alice)
	if err == nil {
		_, err = e.pool.Exec(t.Context(),
			"UPDATE garmin_backfills SET status = 'running', health_through = NULL, activities_listed = false WHERE user_id = $1", e.alice)
	}
	if err == nil {
		e.worker.now, e.worker.budget = nil, 0
		err = e.work(t)
	}
	if err != nil {
		t.Fatal(err)
	}
	if a, m := e.activities(t), e.metrics(t); a != 2 || m != 2 {
		t.Errorf("after the rerun: %d activities, %d days, want 2 and 2", a, m)
	}
	if n := e.count(t, "SELECT count(*) FROM garmin_activity_imports WHERE user_id = $1"); n != 3 {
		t.Errorf("%d imports, want 3", n)
	}
}

func TestRateLimitPause(t *testing.T) {
	t.Parallel()
	for _, where := range []string{"2", "health"} {
		t.Run(where, func(t *testing.T) {
			t.Parallel()
			e := newEnv(t)
			e.fake.errs[where] = fmt.Errorf("after 5 attempts: %w", connectors.ErrRateLimited)

			d, ok := snoozed(e.work(t))
			if !ok || d != rateLimitPause {
				t.Fatalf("snooze %v %v, want %v", d, ok, rateLimitPause)
			}
			if b := e.progress(t).Backfill; b.Status != "paused" || *b.PauseReason != "rate_limited" {
				t.Fatalf("backfill = %s %v", b.Status, b.PauseReason)
			}
			// The snoozed job wakes and continues.
			if err := e.work(t); err != nil {
				t.Fatal(err)
			}
			if p := e.progress(t); p.Backfill.Status != "done" || p.Activities.Done != 2 || e.metrics(t) != 2 {
				t.Errorf("after the pause: %+v", p)
			}
		})
	}
}

func TestKeyRejected(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := t.Context()
	e.fake.errs["1"] = fmt.Errorf("GET: %w", connectors.ErrKeyRejected)

	if err := e.work(t); err != nil {
		t.Fatal(err)
	}
	if b := e.progress(t).Backfill; b.Status != "paused" || *b.PauseReason != "key_rejected" {
		t.Fatalf("backfill = %s %v", b.Status, b.PauseReason)
	}
	conn, err := e.q.GetGarminConnection(ctx, e.alice)
	if err != nil || conn.Status != "key_rejected" {
		t.Fatalf("connection status %q, %v", conn.Status, err)
	}
	// Paused jobs do nothing until resumed.
	if err := e.work(t); err != nil || e.fake.downloads["1"] != 1 {
		t.Fatalf("paused run: %v, %d downloads", err, e.fake.downloads["1"])
	}

	// A new key clears the rejection; resuming continues.
	if _, err := e.q.UpsertGarminKey(ctx, db.UpsertGarminKeyParams{
		UserID: e.alice, KeyCiphertext: conn.KeyCiphertext, KeyExpiresOn: conn.KeyExpiresOn,
	}); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.Resume(ctx, e.alice, false); err != nil {
		t.Fatal(err)
	}
	if err := e.work(t); err != nil {
		t.Fatal(err)
	}
	if p := e.progress(t); p.Backfill.Status != "done" || p.Activities.Done != 2 {
		t.Errorf("after resuming: %+v", p)
	}
}

func TestConnectionRejectedOrMissing(t *testing.T) {
	t.Parallel()
	t.Run("rejected before the run", func(t *testing.T) {
		t.Parallel()
		e := newEnv(t)
		if err := e.q.SetGarminKeyRejected(t.Context(), e.alice); err != nil {
			t.Fatal(err)
		}
		if err := e.work(t); err != nil {
			t.Fatal(err)
		}
		if b := e.progress(t).Backfill; *b.PauseReason != "key_rejected" || len(e.fake.downloads) != 0 {
			t.Errorf("backfill = %+v after %d downloads", b, len(e.fake.downloads))
		}
	})
	t.Run("disconnected", func(t *testing.T) {
		t.Parallel()
		e := newEnv(t)
		if _, err := e.q.DeleteGarminConnection(t.Context(), e.alice); err != nil {
			t.Fatal(err)
		}
		if err := e.work(t); err != nil {
			t.Fatal(err)
		}
		if b := e.progress(t).Backfill; b.Status != "paused" || *b.PauseReason != "disconnected" {
			t.Errorf("backfill = %+v", b)
		}
	})
}

func TestFailuresDoNotBlockAndRetry(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := t.Context()
	e.fake.errs["1"] = fmt.Errorf("GET: %w: status 404", connectors.ErrUpstream)

	if err := e.work(t); err != nil {
		t.Fatal(err)
	}
	p := e.progress(t)
	if p.Backfill.Status != "done" || p.Activities.Done != 1 || p.Activities.Failed != 2 {
		t.Fatalf("progress = %+v", p)
	}
	failures, err := e.svc.Failures(ctx, e.alice, time.Time{}, "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(failures) != 1 || failures[0].SourceActivityID != "1" || !strings.HasPrefix(*failures[0].Error, "download failed") {
		t.Fatalf("first page = %+v", failures)
	}
	next, err := e.svc.Failures(ctx, e.alice, failures[0].StartTime, failures[0].SourceActivityID, 1)
	if err != nil || len(next) != 1 || next[0].SourceActivityID != "3" {
		t.Fatalf("second page = %+v, %v", next, err)
	}

	// Garmin recovers and the broken file is fixed: retrying imports both.
	e.fake.files["3"] = connectors.File{Name: "3.gpx", Data: bytes.ReplaceAll(gpx(t, "run.gpx"), []byte("2026-05-01"), []byte("2026-05-03"))}
	if err := e.svc.Resume(ctx, e.alice, true); err != nil {
		t.Fatal(err)
	}
	if b := e.progress(t).Backfill; b.Status != "running" {
		t.Fatalf("status after retry = %s", b.Status)
	}
	if err := e.work(t); err != nil {
		t.Fatal(err)
	}
	if p := e.progress(t); p.Backfill.Status != "done" || p.Activities.Done != 3 || p.Activities.Failed != 0 || e.activities(t) != 3 {
		t.Errorf("after retry: %+v", p)
	}
	// Nothing to retry: resuming a finished backfill leaves it done.
	if err := e.svc.Resume(ctx, e.alice, true); err != nil || e.progress(t).Backfill.Status != "done" {
		t.Errorf("resume of a finished backfill: %v", err)
	}
}

func TestListingErrorIsRetried(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.fake.listErr = fmt.Errorf("%w: status 500", connectors.ErrUpstream)
	if err := e.work(t); !errors.Is(err, connectors.ErrUpstream) {
		t.Fatalf("error = %v, want the upstream error for River to retry", err)
	}
	if b := e.progress(t).Backfill; b.Status != "running" || b.ActivitiesListed {
		t.Errorf("backfill = %+v", b)
	}
}

func TestServiceErrors(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := t.Context()
	if err := e.svc.Start(ctx, e.alice, day(10, 1), day(10, 2)); !errors.Is(err, ErrExists) {
		t.Errorf("second start: %v", err)
	}
	if err := e.svc.Start(ctx, e.bob, day(10, 1), day(10, 2)); !errors.Is(err, ErrNotConnected) {
		t.Errorf("start without a key: %v", err)
	}
	if _, err := e.svc.Progress(ctx, e.bob); !errors.Is(err, ErrNotFound) {
		t.Errorf("bob's progress: %v", err)
	}
	if err := e.svc.Resume(ctx, e.bob, true); !errors.Is(err, ErrNotFound) {
		t.Errorf("bob's resume: %v", err)
	}
	// Bob's job finds nothing and never touches alice's backfill.
	if err := e.worker.Work(ctx, &river.Job[BackfillArgs]{Args: BackfillArgs{UserID: e.bob}}); err != nil {
		t.Fatal(err)
	}
	if err := e.work(t); err != nil {
		t.Fatal(err)
	}
	if rows, err := e.svc.Failures(ctx, e.bob, time.Time{}, "", 10); err != nil || len(rows) != 0 {
		t.Errorf("bob's failures = %v, %v", rows, err)
	}
	if (BackfillArgs{}).Kind() != "garmin_backfill" || e.worker.Timeout(nil) != jobTimeout {
		t.Error("job kind or timeout changed")
	}
}

func TestDailyMetricsConversion(t *testing.T) {
	t.Parallel()
	hrv, score := 45.5, 81
	p := dailyMetrics(7, connectors.DailyMetrics{Day: day(10, 1), HRVMs: &hrv, SleepScore: &score, Steps: steps(12000)})
	if p.UserID != 7 || *p.HrvMs != 45.5 || *p.SleepScore != 81 || *p.Steps != 12000 || p.WeightKg != nil {
		t.Errorf("params = %+v", p)
	}
}
