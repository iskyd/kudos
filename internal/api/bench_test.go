package api

import (
	"fmt"
	"math"
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/iskyd/kudos/internal/importer/ingest"
	"github.com/iskyd/kudos/internal/jobs"
	"github.com/iskyd/kudos/internal/seed"
	"github.com/iskyd/kudos/internal/store/db"
)

// pageBudget is the spec's server time for the dashboard and the activity page
// with ten years of data.
const pageBudget = 300 * time.Millisecond

// BenchmarkPages times every request the dashboard and the activity page make,
// in sequence, against ten years of seeded data, and fails above pageBudget.
// Seeding takes minutes, so it runs only with -bench (just bench);
// KUDOS_BENCH_YEARS shortens the history.
func BenchmarkPages(b *testing.B) {
	years := 10
	if v := os.Getenv("KUDOS_BENCH_YEARS"); v != "" {
		var err error
		if years, err = strconv.Atoi(v); err != nil {
			b.Fatal(err)
		}
	}
	pool := pg.NewDB(b)
	c, q := newAPIOn(b, pool, true, nil)
	tz := "Europe/Rome"
	me := c.session(c.do(http.MethodPost, "/api/auth/register", RegisterRequest{Username: "bench", Password: testPassword, Timezone: &tz}, nil), http.StatusCreated)
	client, err := jobs.NewClient(pool, discard(), (&ingest.Workers{}).Register)
	if err != nil {
		b.Fatal(err)
	}
	svc := ingest.New(pool, client, b.TempDir())
	loc, err := time.LoadLocation(tz)
	if err != nil {
		b.Fatal(err)
	}
	today := time.Now().In(loc)
	started := time.Now()
	s, err := seed.Run(b.Context(), svc, q, me.Id, loc, seed.Options{Days: 365 * years, End: today, Seed: 1})
	if err != nil {
		b.Fatal(err)
	}
	n, err := svc.RecomputeDerived(b.Context(), me.Id)
	if err != nil {
		b.Fatal(err)
	}
	b.Logf("seeded %d activities and %d samples, derived values of %d, in %s", s.Activities, s.Samples, n, time.Since(started).Round(time.Second))

	// The activity page of the longest activity: the most samples to downsample.
	longest, err := q.ListActivities(b.Context(), db.ListActivitiesParams{
		UserID: me.Id, Sort: "duration", BeforeKey: math.MaxFloat64, BeforeID: math.MaxInt64, MaxRows: 1,
	})
	if err != nil || len(longest) == 0 {
		b.Fatalf("longest activity: %v", err)
	}
	a := longest[0].Activity
	day := a.StartTime.Add(time.Duration(a.UtcOffsetS) * time.Second).Format(time.DateOnly)
	date := today.Format(time.DateOnly)
	pages := []struct {
		name  string
		paths []string
	}{
		// The home dashboard's requests, with twelve weeks of volume.
		{"dashboard", []string{
			"/api/me", "/api/garmin", "/api/readiness?day=" + date,
			"/api/fitness?from=" + today.AddDate(0, 0, -90).Format(time.DateOnly) + "&to=" + date,
			"/api/volume?period=week&from=" + today.AddDate(0, 0, -83).Format(time.DateOnly) + "&to=" + date,
			"/api/activities?limit=5",
		}},
		{"activity", []string{
			fmt.Sprintf("/api/activities/%d", a.ID),
			fmt.Sprintf("/api/activities/%d/streams?maxPoints=1000", a.ID),
			fmt.Sprintf("/api/activities/%d/laps", a.ID),
			fmt.Sprintf("/api/activities/%d/zones", a.ID),
			fmt.Sprintf("/api/activities/%d/execution", a.ID),
			"/api/activities?sport=" + a.Sport + "&sort=date",
			"/api/activities?sort=date&from=" + day + "&to=" + day,
			"/api/gear", "/api/config",
		}},
	}
	for _, page := range pages {
		b.Run(page.name, func(b *testing.B) {
			pc := *c
			pc.t = b
			for b.Loop() {
				for _, p := range page.paths {
					if rec := pc.do(http.MethodGet, p, nil, nil); rec.Code != http.StatusOK {
						b.Fatalf("%s: %d %s", p, rec.Code, rec.Body)
					}
				}
			}
			if per := b.Elapsed() / time.Duration(b.N); per > pageBudget {
				b.Errorf("%s page took %s, over %s", page.name, per, pageBudget)
			}
		})
	}
}
