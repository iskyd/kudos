package api

import (
	"math"
	"net/http"
	"testing"

	"github.com/iskyd/kudos/internal/store/db"
)

func TestFitnessByLocalDay(t *testing.T) {
	t.Parallel()
	c, q, me := loggedIn(t, "Europe/Rome")
	ctx := t.Context()
	// Jobs don't run here, so the derived loads are stored directly.
	load := func(start string, l float64) int64 {
		id := run(t, c, start, 5000, 1800)
		method := "session_rpe"
		if err := q.UpsertActivityDerived(ctx, db.UpsertActivityDerivedParams{UserID: me.Id, ActivityID: id, Version: 2, Load: &l, LoadMethod: &method}); err != nil {
			t.Fatal(err)
		}
		return id
	}
	// 00:30 in Rome on 1 October is 30 September in UTC: loads count by local day.
	load("2026-10-01T00:30", 60)
	load("2026-10-01T18:00", 40)
	into := load("2026-10-03T07:00", 70)
	merged := load("2026-10-03T08:00", 500)
	if err := q.SetActivityMergedInto(ctx, db.SetActivityMergedIntoParams{UserID: me.Id, ID: merged, MergedInto: &into}); err != nil {
		t.Fatal(err)
	}

	// The 1st is warm-up: 100 on it, nothing on the 2nd, 70 on the 3rd with the
	// merged-away activity left out.
	days := decodeJSON[[]FitnessDay](t, c.do(http.MethodGet, "/api/fitness?from=2026-10-02&to=2026-10-03", nil, nil), http.StatusOK)
	ctl2, atl2 := 100.0/42*41/42, 100.0/7*6/7
	want := []FitnessDay{
		{Load: 0, Ctl: ctl2, Atl: atl2, Tsb: 100.0/42 - 100.0/7},
		{Load: 70, Ctl: ctl2 + (70-ctl2)/42, Atl: atl2 + (70-atl2)/7, Tsb: ctl2 - atl2},
	}
	if len(days) != 2 || days[0].Day.String() != "2026-10-02" || days[1].Day.String() != "2026-10-03" {
		t.Fatalf("days: %+v", days)
	}
	for i, w := range want {
		g := days[i]
		if math.Abs(g.Load-w.Load) > 1e-9 || math.Abs(g.Ctl-w.Ctl) > 1e-9 || math.Abs(g.Atl-w.Atl) > 1e-9 || math.Abs(g.Tsb-w.Tsb) > 1e-9 {
			t.Errorf("day %d: %+v, want %+v", i, g, w)
		}
	}

	for _, path := range []string{
		"/api/fitness?from=2026-10-03&to=2026-10-02",
		"/api/fitness?from=2016-01-01&to=2026-10-02",
		"/api/fitness?from=2026-10-03",
	} {
		wantProblem(t, c.do(http.MethodGet, path, nil, nil), KindInvalidRequest)
	}
}
