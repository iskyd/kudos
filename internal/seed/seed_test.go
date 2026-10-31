package seed

import (
	"context"
	"errors"
	"log"
	"log/slog"
	"math"
	"os"
	"testing"
	"time"

	"github.com/iskyd/kudos/internal/importer/ingest"
	"github.com/iskyd/kudos/internal/jobs"
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

func TestRun(t *testing.T) {
	t.Parallel()
	pool := pg.NewDB(t)
	client, err := jobs.NewClient(pool, slog.New(slog.DiscardHandler), (&ingest.Workers{}).Register)
	if err != nil {
		t.Fatal(err)
	}
	svc := ingest.New(pool, client, t.TempDir())
	q := db.New(pool)
	alice, bob := testutil.TwoUsers(t, pool)
	rome, err := time.LoadLocation("Europe/Rome")
	if err != nil {
		t.Fatal(err)
	}
	opts := Options{Days: 21, End: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), Seed: 7}
	days := 0
	opts.Progress = func(time.Time, Stats) { days++ }
	s, err := Run(t.Context(), svc, q, alice.ID, rome, opts)
	if err != nil {
		t.Fatal(err)
	}
	// About two activities a day.
	if s.Days != 21 || days != 21 || s.Activities < 25 || s.Activities > 60 || s.Samples < s.Activities*500 {
		t.Fatalf("stats: %+v after %d days", s, days)
	}

	// Every activity imports with its sport, starts in the period at its local
	// offset in Rome (+2 h in September) and keeps its original file.
	rows, err := q.ListActivities(t.Context(), db.ListActivitiesParams{
		UserID: alice.ID, Sort: "date", BeforeKey: math.MaxFloat64, BeforeID: math.MaxInt64, MaxRows: 1000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != s.Activities {
		t.Fatalf("%d activities stored, %d generated", len(rows), s.Activities)
	}
	sports := map[string]int{}
	var run db.Activity
	for _, r := range rows {
		a := r.Activity
		sports[a.Sport+"/"+a.Subtype]++
		if a.StartTime.Before(time.Date(2026, 9, 9, 0, 0, 0, 0, time.UTC)) || !a.StartTime.Before(opts.End) ||
			a.UtcOffsetS != 7200 || a.OriginalFileID == nil {
			t.Errorf("activity %+v", a)
		}
		if a.Sport == "running" {
			run = a
		}
	}
	for _, sport := range []string{"running/outdoor", "pool_swimming/indoor", "strength/indoor"} {
		if sports[sport] == 0 {
			t.Errorf("no %s in %v", sport, sports)
		}
	}
	if sports["cycling/outdoor"]+sports["cycling/virtual"] == 0 {
		t.Errorf("no rides in %v", sports)
	}

	// A run's positions stay within the loop around 0° N 0° E, with running dynamics.
	samples, err := q.ListActivityStreams(t.Context(), db.ListActivityStreamsParams{UserID: alice.ID, ActivityID: run.ID, MaxRows: 100000})
	if err != nil || len(samples) < 1800 {
		t.Fatalf("run samples: %d, %v", len(samples), err)
	}
	for _, smp := range samples {
		if smp.LatDeg == nil || math.Abs(*smp.LatDeg) > 0.02 || math.Abs(*smp.LonDeg) > 0.02 || smp.Extras == nil {
			t.Fatalf("run sample %+v", smp)
		}
	}

	// Metrics every day and a zone version per sport.
	m, err := q.GetDailyMetrics(t.Context(), db.GetDailyMetricsParams{UserID: alice.ID, Day: time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)})
	if err != nil || m.RestingHrBpm == nil || m.HrvMs == nil || m.SleepS == nil {
		t.Errorf("daily metrics: %+v, %v", m, err)
	}
	zones, err := q.ListZoneSetsAt(t.Context(), db.ListZoneSetsAtParams{UserID: alice.ID, Day: opts.End})
	if err != nil || len(zones) != 4 {
		t.Errorf("zone versions: %d, %v", len(zones), err)
	}

	// The same seed gives the same data; a filled account is refused.
	again, err := Run(t.Context(), svc, q, bob.ID, rome, Options{Days: 21, End: opts.End, Seed: 7})
	if err != nil || again != s {
		t.Errorf("same seed: %+v, %v; want %+v", again, err, s)
	}
	if _, err := Run(t.Context(), svc, q, alice.ID, rome, opts); !errors.Is(err, ErrNotEmpty) {
		t.Errorf("second run: %v", err)
	}
}
