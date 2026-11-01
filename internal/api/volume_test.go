package api

import (
	"fmt"
	"net/http"
	"slices"
	"testing"

	"github.com/iskyd/kudos/internal/store/db"
)

func TestVolume(t *testing.T) {
	t.Parallel()
	c, q, me := loggedIn(t, "Europe/Rome")
	derived := func(id int64, d db.UpsertActivityDerivedParams) {
		t.Helper()
		d.UserID, d.ActivityID, d.Version = me.Id, id, 1
		if err := q.UpsertActivityDerived(t.Context(), d); err != nil {
			t.Fatal(err)
		}
	}
	// Sunday 23:30 and Monday 00:30 in Rome are 22:30 and 23:30 UTC on Sunday:
	// the week boundary is the local midnight.
	sunday := run(t, c, "2026-10-25T23:30", 10000, 3000)
	monday := run(t, c, "2026-10-26T00:30", 5000, 1500)
	bike := createGear(t, c, GearInput{Kind: GearInputKindBike, Name: "Road"})
	ride := decodeJSON[CreatedActivity](t, c.do(http.MethodPost, "/api/activities",
		ManualActivityRequest{Sport: SportCycling, LocalStartTime: "2026-11-02T08:00", DurationS: 5400, DistanceM: new(40000.0)}, nil), http.StatusCreated).Id
	setGear(t, c, ride, &bike.Id)
	derived(sunday, db.UpsertActivityDerivedParams{Load: new(50.0), LoadMethod: new("hr_tss"), HrZoneModel: new("friel_run_hr"), HrZoneS: []float64{60, 120}})
	derived(monday, db.UpsertActivityDerivedParams{Load: new(30.0), LoadMethod: new("hr_tss"), HrZoneModel: new("friel_run_hr"), HrZoneS: []float64{10, 20}})
	derived(ride, db.UpsertActivityDerivedParams{Load: new(80.0), LoadMethod: new("power_tss"), PowerZoneModel: new("coggan_power"), PowerZoneS: []float64{100, 200, 300}})

	get := func(c *apiClient, query string) Volume {
		t.Helper()
		return decodeJSON[Volume](t, c.do(http.MethodGet, "/api/volume?"+query, nil, nil), http.StatusOK)
	}
	periods := func(v Volume) []string {
		var out []string
		for _, p := range v.Periods {
			out = append(out, fmt.Sprintf("%s %d %v %v %v %v", p.Start.Format("2006-01-02"), p.Activities, p.DistanceM, p.DurationS, p.AscentM, p.Load))
		}
		return out
	}
	for _, tt := range []struct {
		query string
		want  []string
	}{
		{"period=week&from=2026-10-19&to=2026-11-08", []string{
			"2026-10-19 1 10000 3000 0 50", "2026-10-26 1 5000 1500 0 30", "2026-11-02 1 40000 5400 0 80",
		}},
		{"period=month&from=2026-10-01&to=2026-11-30", []string{"2026-10-01 2 15000 4500 0 80", "2026-11-01 1 40000 5400 0 80"}},
		{"period=year&from=2025-01-01&to=2026-12-31", []string{"2026-01-01 3 55000 9900 0 160"}},
		// Only the days in range count, though the week starts before from.
		{"period=week&from=2026-10-26&to=2026-11-01", []string{"2026-10-26 1 5000 1500 0 30"}},
		{"period=week&from=2026-10-19&to=2026-11-08&sport=running", []string{"2026-10-19 1 10000 3000 0 50", "2026-10-26 1 5000 1500 0 30"}},
		{fmt.Sprintf("period=month&from=2026-10-01&to=2026-11-30&gear=%d", bike.Id), []string{"2026-11-01 1 40000 5400 0 80"}},
		{"period=year&from=2024-01-01&to=2024-12-31", nil},
	} {
		if got := periods(get(c, tt.query)); !slices.Equal(got, tt.want) {
			t.Errorf("%s: %v, want %v", tt.query, got, tt.want)
		}
	}

	// Time in zones sums each metric and model's zones over the range.
	v := get(c, "period=month&from=2026-10-01&to=2026-11-30")
	if len(v.TimeInZones) != 2 {
		t.Fatalf("time in zones: %+v", v.TimeInZones)
	}
	if z := v.TimeInZones[0]; z.Metric != Hr || z.Model != "friel_run_hr" || !slices.Equal(z.Seconds, []float64{70, 140}) {
		t.Errorf("hr: %+v", z)
	}
	if z := v.TimeInZones[1]; z.Metric != Power || z.Model != "coggan_power" || !slices.Equal(z.Seconds, []float64{100, 200, 300}) {
		t.Errorf("power: %+v", z)
	}
	if v := get(c, "period=month&from=2026-10-01&to=2026-11-30&sport=running"); len(v.TimeInZones) != 1 || v.TimeInZones[0].Metric != Hr {
		t.Errorf("running time in zones: %+v", v.TimeInZones)
	}

	for _, query := range []string{
		"period=day&from=2026-10-01&to=2026-10-31", "period=week&from=2026-10-31&to=2026-10-01",
		"period=week&from=2016-01-01&to=2026-12-31", "period=week&from=2026-10-01&to=2026-10-31&sport=curling",
	} {
		wantProblem(t, c.do(http.MethodGet, "/api/volume?"+query, nil, nil), KindInvalidRequest)
	}
	bob := &apiClient{t: t, h: c.h}
	bob.session(bob.do(http.MethodPost, "/api/auth/register", register("bob"), nil), http.StatusCreated)
	if v := get(bob, "period=year&from=2026-01-01&to=2026-12-31"); len(v.Periods)+len(v.TimeInZones) != 0 {
		t.Errorf("bob: %+v", v)
	}
	wantProblem(t, (&apiClient{t: t, h: c.h}).do(http.MethodGet, "/api/volume?period=year&from=2026-01-01&to=2026-12-31", nil, nil), KindUnauthorized)
}
