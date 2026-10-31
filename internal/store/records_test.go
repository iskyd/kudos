package store_test

import (
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/iskyd/kudos/internal/store/db"
	"github.com/iskyd/kudos/internal/testutil"
)

func TestRecordHistoryAndIsolation(t *testing.T) {
	t.Parallel()
	pool := pg.NewDB(t)
	alice, bob := testutil.TwoUsers(t, pool)
	q := db.New(pool)
	ctx := t.Context()
	day := func(d int) time.Time { return time.Date(2026, 10, d, 7, 0, 0, 0, time.UTC) }
	activity := func(d int) int64 {
		id, err := q.UpsertActivity(ctx, db.UpsertActivityParams{
			UserID: alice.ID, Source: "manual", SourceActivityID: fmt.Sprint(d), Sport: "running", Subtype: "outdoor", StartTime: day(d),
		})
		check(t, err)
		return id
	}
	efforts := func(userID, id int64, values map[string]float64) {
		p := db.InsertBestEffortsParams{UserID: userID, ActivityID: id}
		for k, v := range values {
			p.Efforts, p.EffortValues = append(p.Efforts, k), append(p.EffortValues, v)
		}
		check(t, q.InsertBestEfforts(ctx, p))
	}
	history := func(userID int64) []string {
		rows, err := q.ListRecordHistory(ctx, db.ListRecordHistoryParams{UserID: userID, MaxRows: 100})
		check(t, err)
		var out []string
		for _, r := range rows {
			out = append(out, fmt.Sprintf("%s %d %v", r.Effort, r.StartTime.Day(), r.Value))
		}
		return out
	}

	if _, err := q.GetRecordsSince(ctx, alice.ID); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("since without activities: %v", err)
	}
	// Created out of order: history follows start time, not ID.
	third, first, second := activity(10), activity(1), activity(5)
	merged := activity(12)
	check(t, q.SetActivityMergedInto(ctx, db.SetActivityMergedIntoParams{UserID: alice.ID, ID: merged, MergedInto: &third}))
	efforts(alice.ID, first, map[string]float64{"fastest_5k": 1500, "power_5s": 500})
	efforts(alice.ID, second, map[string]float64{"fastest_5k": 1400, "power_5s": 400})
	efforts(alice.ID, third, map[string]float64{"fastest_5k": 1450, "power_5s": 600})
	efforts(alice.ID, merged, map[string]float64{"fastest_5k": 1000})

	want := []string{"fastest_5k 1 1500", "fastest_5k 5 1400", "power_5s 1 500", "power_5s 10 600"}
	if got := history(alice.ID); !slices.Equal(got, want) {
		t.Errorf("history: %v, want %v", got, want)
	}
	if since, err := q.GetRecordsSince(ctx, alice.ID); err != nil || !since.Equal(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("since the first activity: %v %v", since, err)
	}
	_, err := q.CreateGarminBackfill(ctx, db.CreateGarminBackfillParams{
		UserID: alice.ID, WindowStart: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), WindowEnd: day(31),
	})
	check(t, err)
	if since, err := q.GetRecordsSince(ctx, alice.ID); err != nil || since.Month() != time.September || since.Day() != 1 {
		t.Errorf("since the backfill window: %v %v", since, err)
	}

	// Bob neither reads, adds to nor clears Alice's efforts.
	efforts(bob.ID, second, map[string]float64{"fastest_5k": 1})
	check(t, q.DeleteBestEfforts(ctx, db.DeleteBestEffortsParams{UserID: bob.ID, ActivityID: first}))
	if got := history(bob.ID); len(got) != 0 {
		t.Errorf("bob's history: %v", got)
	}
	if _, err := q.GetRecordsSince(ctx, bob.ID); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("bob's since: %v", err)
	}
	if got := history(alice.ID); !slices.Equal(got, want) {
		t.Errorf("after bob: %v", got)
	}
	check(t, q.DeleteBestEfforts(ctx, db.DeleteBestEffortsParams{UserID: alice.ID, ActivityID: first}))
	if got := history(alice.ID); !slices.Equal(got, []string{"fastest_5k 5 1400", "power_5s 5 400", "power_5s 10 600"}) {
		t.Errorf("after clearing the first: %v", got)
	}
}

func TestBestCurvePointsAndIsolation(t *testing.T) {
	t.Parallel()
	pool := pg.NewDB(t)
	alice, bob := testutil.TwoUsers(t, pool)
	q := db.New(pool)
	ctx := t.Context()
	// 23:30 UTC with a +2 h offset: the local day is the next one.
	activity := func(d int) int64 {
		id, err := q.UpsertActivity(ctx, db.UpsertActivityParams{
			UserID: alice.ID, Source: "manual", SourceActivityID: fmt.Sprint(d), Sport: "cycling", Subtype: "outdoor",
			StartTime: time.Date(2026, 10, d, 23, 30, 0, 0, time.UTC), UtcOffsetS: 7200,
		})
		check(t, err)
		return id
	}
	points := func(userID, id int64, kind string, durations []int32, values []float64) {
		check(t, q.InsertCurvePoints(ctx, db.InsertCurvePointsParams{
			UserID: userID, ActivityID: id, Kinds: slices.Repeat([]string{kind}, len(durations)), Durations: durations, CurveValues: values,
		}))
	}
	best := func(userID int64, from, to *time.Time) []string {
		rows, err := q.ListBestCurvePoints(ctx, db.ListBestCurvePointsParams{UserID: userID, FromDay: from, ToDay: to, MaxRows: 100})
		check(t, err)
		var out []string
		for _, r := range rows {
			out = append(out, fmt.Sprintf("%s %d %v %d", r.Kind, r.DurationS, r.Value, r.StartTime.UTC().Day()))
		}
		return out
	}
	day := func(d int) *time.Time { return new(time.Date(2026, 10, d, 0, 0, 0, 0, time.UTC)) }

	first, second, third := activity(1), activity(5), activity(10)
	merged := activity(12)
	check(t, q.SetActivityMergedInto(ctx, db.SetActivityMergedIntoParams{UserID: alice.ID, ID: merged, MergedInto: &third}))
	points(alice.ID, first, "power", []int32{5, 60}, []float64{500, 300})
	points(alice.ID, second, "power", []int32{5, 60}, []float64{500, 350})
	points(alice.ID, third, "pace", []int32{60}, []float64{5})
	points(alice.ID, merged, "power", []int32{5}, []float64{900})

	// A tie goes to the earlier activity; the merged-away one is left out.
	if got, want := best(alice.ID, nil, nil), []string{"pace 60 5 10", "power 5 500 1", "power 60 350 5"}; !slices.Equal(got, want) {
		t.Errorf("all time: %v, want %v", got, want)
	}
	// Periods use the local day: the first activity is on 2 October there.
	if got, want := best(alice.ID, day(2), day(9)), []string{"power 5 500 1", "power 60 350 5"}; !slices.Equal(got, want) {
		t.Errorf("2 to 9 October: %v, want %v", got, want)
	}
	if got, want := best(alice.ID, day(3), nil), []string{"pace 60 5 10", "power 5 500 5", "power 60 350 5"}; !slices.Equal(got, want) {
		t.Errorf("from 3 October: %v, want %v", got, want)
	}
	if got := best(alice.ID, nil, day(1)); len(got) != 0 {
		t.Errorf("to 1 October: %v", got)
	}

	// Bob neither reads, adds to nor clears Alice's points.
	points(bob.ID, second, "power", []int32{5}, []float64{1000})
	check(t, q.DeleteCurvePoints(ctx, db.DeleteCurvePointsParams{UserID: bob.ID, ActivityID: first}))
	if got := best(bob.ID, nil, nil); len(got) != 0 {
		t.Errorf("bob's curves: %v", got)
	}
	if got := best(alice.ID, nil, nil); len(got) != 3 || got[1] != "power 5 500 1" {
		t.Errorf("after bob: %v", got)
	}
	check(t, q.DeleteCurvePoints(ctx, db.DeleteCurvePointsParams{UserID: alice.ID, ActivityID: first}))
	if got := best(alice.ID, nil, nil); got[1] != "power 5 500 5" {
		t.Errorf("after clearing the first: %v", got)
	}
}

func TestBestEffortsInPeriodAndIsolation(t *testing.T) {
	t.Parallel()
	pool := pg.NewDB(t)
	alice, bob := testutil.TwoUsers(t, pool)
	q := db.New(pool)
	ctx := t.Context()
	n := 0
	swim := func(userID int64, day int, values map[string]float64) int64 {
		n++
		id, err := q.UpsertActivity(ctx, db.UpsertActivityParams{
			UserID: userID, Source: "manual", SourceActivityID: fmt.Sprint(n), Sport: "pool_swimming", Subtype: "outdoor",
			StartTime: time.Date(2026, 10, day, 7, 0, 0, 0, time.UTC),
		})
		check(t, err)
		p := db.InsertBestEffortsParams{UserID: userID, ActivityID: id}
		for k, v := range values {
			p.Efforts, p.EffortValues = append(p.Efforts, k), append(p.EffortValues, v)
		}
		check(t, q.InsertBestEfforts(ctx, p))
		return id
	}
	first := swim(alice.ID, 1, map[string]float64{"fastest_swim_200m": 170, "fastest_swim_400m": 380})
	swim(alice.ID, 5, map[string]float64{"fastest_swim_200m": 180, "fastest_swim_400m": 360, "longest_swim": 2000})
	merged := swim(alice.ID, 6, map[string]float64{"fastest_swim_200m": 100})
	check(t, q.SetActivityMergedInto(ctx, db.SetActivityMergedIntoParams{UserID: alice.ID, ID: merged, MergedInto: &first}))
	swim(bob.ID, 1, map[string]float64{"fastest_swim_200m": 90})

	best := func(userID int64, from, to *time.Time) []string {
		t.Helper()
		rows, err := q.ListBestEffortsInPeriod(ctx, db.ListBestEffortsInPeriodParams{
			UserID: userID, FromDay: from, ToDay: to, Efforts: []string{"fastest_swim_200m", "fastest_swim_400m"},
		})
		check(t, err)
		var out []string
		for _, r := range rows {
			out = append(out, fmt.Sprintf("%s %v", r.Effort, r.Value))
		}
		return out
	}
	day := func(d int) *time.Time { return new(time.Date(2026, 10, d, 0, 0, 0, 0, time.UTC)) }
	// The fastest of each across activities; merged-away activities and other efforts left out.
	if got, want := best(alice.ID, nil, nil), []string{"fastest_swim_200m 170", "fastest_swim_400m 360"}; !slices.Equal(got, want) {
		t.Errorf("all time: %v, want %v", got, want)
	}
	if got, want := best(alice.ID, day(2), nil), []string{"fastest_swim_200m 180", "fastest_swim_400m 360"}; !slices.Equal(got, want) {
		t.Errorf("from 2 October: %v, want %v", got, want)
	}
	if got, want := best(alice.ID, nil, day(4)), []string{"fastest_swim_200m 170", "fastest_swim_400m 380"}; !slices.Equal(got, want) {
		t.Errorf("to 4 October: %v, want %v", got, want)
	}
	if got, want := best(bob.ID, nil, nil), []string{"fastest_swim_200m 90"}; !slices.Equal(got, want) {
		t.Errorf("bob: %v", got)
	}
}
