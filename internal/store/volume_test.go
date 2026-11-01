package store_test

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/iskyd/kudos/internal/store/db"
	"github.com/iskyd/kudos/internal/testutil"
)

func TestVolumeAndIsolation(t *testing.T) {
	t.Parallel()
	pool := pg.NewDB(t)
	alice, bob := testutil.TwoUsers(t, pool)
	q := db.New(pool)
	ctx := t.Context()
	n := 0
	activity := func(userID int64, day int, p db.UpsertActivityParams) int64 {
		n++
		p.UserID, p.Source, p.SourceActivityID, p.Sport, p.Subtype = userID, "manual", fmt.Sprint(n), "running", "outdoor"
		p.StartTime = time.Date(2026, 10, day, 7, 0, 0, 0, time.UTC)
		id, err := q.UpsertActivity(ctx, p)
		check(t, err)
		return id
	}
	// Recorded values; an edited activity whose summary replaces them (ascent
	// unknown); one with only elapsed time and no derived row; a merged-away one.
	recorded := activity(alice.ID, 5, db.UpsertActivityParams{DistanceM: ptr(10000.0), TimerTimeS: ptr(3000.0), ElapsedTimeS: ptr(3100.0), AscentM: ptr(float32(50))})
	edited := activity(alice.ID, 6, db.UpsertActivityParams{DistanceM: ptr(8000.0), TimerTimeS: ptr(2400.0), AscentM: ptr(float32(40))})
	check(t, q.SetActivityOverrides(ctx, db.SetActivityOverridesParams{UserID: alice.ID, ID: edited,
		Overrides: []byte(`{"summary":{"distance_m":6000,"timer_time_s":1800,"ascent_m":null}}`)}))
	activity(alice.ID, 7, db.UpsertActivityParams{DistanceM: ptr(1000.0), ElapsedTimeS: ptr(600.0)})
	merged := activity(alice.ID, 8, db.UpsertActivityParams{DistanceM: ptr(99999.0)})
	check(t, q.SetActivityMergedInto(ctx, db.SetActivityMergedIntoParams{UserID: alice.ID, ID: merged, MergedInto: &recorded}))
	for id, load := range map[int64]float64{recorded: 60, edited: 40, merged: 500} {
		check(t, q.UpsertActivityDerived(ctx, db.UpsertActivityDerivedParams{
			UserID: alice.ID, ActivityID: id, Version: 1, Load: ptr(load), LoadMethod: ptr("hr_tss"),
			HrZoneModel: ptr("friel_run_hr"), HrZoneS: []float64{load, 2 * load},
		}))
	}
	activity(bob.ID, 5, db.UpsertActivityParams{DistanceM: ptr(5000.0)})

	volume := func(userID int64) []string {
		t.Helper()
		rows, err := q.ListVolume(ctx, db.ListVolumeParams{
			UserID: userID, Period: "month", FromDay: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), ToDay: time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC), MaxRows: 10,
		})
		check(t, err)
		var out []string
		for _, r := range rows {
			out = append(out, fmt.Sprintf("%s %d %v %v %v %v", r.Start.Format(time.DateOnly), r.Activities, r.DistanceM, r.DurationS, r.AscentM, r.Load))
		}
		return out
	}
	if got, want := volume(alice.ID), []string{"2026-10-01 3 17000 5400 50 100"}; !slices.Equal(got, want) {
		t.Errorf("alice: %v, want %v", got, want)
	}
	if got, want := volume(bob.ID), []string{"2026-10-01 1 5000 0 0 0"}; !slices.Equal(got, want) {
		t.Errorf("bob: %v, want %v", got, want)
	}

	zones := func(userID int64) []string {
		t.Helper()
		rows, err := q.SumTimeInZones(ctx, db.SumTimeInZonesParams{
			UserID: userID, FromDay: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), ToDay: time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC),
		})
		check(t, err)
		var out []string
		for _, r := range rows {
			out = append(out, fmt.Sprintf("%s %s %d %v", r.Metric, r.Model, r.Zone, r.Seconds))
		}
		return out
	}
	if got, want := zones(alice.ID), []string{"hr friel_run_hr 1 100", "hr friel_run_hr 2 200"}; !slices.Equal(got, want) {
		t.Errorf("alice's zones: %v, want %v", got, want)
	}
	if got := zones(bob.ID); len(got) != 0 {
		t.Errorf("bob's zones: %v", got)
	}
}
