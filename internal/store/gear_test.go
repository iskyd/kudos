package store_test

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/iskyd/kudos/internal/store/db"
	"github.com/iskyd/kudos/internal/testutil"
)

func gearTotals(t *testing.T, q *db.Queries, userID, id int64) []db.ListGearTotalsRow {
	t.Helper()
	rows, err := q.ListGearTotals(t.Context(), db.ListGearTotalsParams{UserID: userID, ID: &id, After: 0, MaxRows: 10})
	check(t, err)
	return rows
}

func assignGear(t *testing.T, q *db.Queries, userID, activityID int64, gearID *int64) {
	t.Helper()
	n, err := q.UpdateActivityAnnotation(t.Context(), db.UpdateActivityAnnotationParams{UserID: userID, ID: activityID, PainTags: []string{}, GearID: gearID})
	if err != nil || n != 1 {
		t.Fatalf("assigning gear: %d, %v", n, err)
	}
}

func TestGearAccumulatesAndReassigns(t *testing.T) {
	t.Parallel()
	pool := pg.NewDB(t)
	alice, _ := testutil.TwoUsers(t, pool)
	q := db.New(pool)
	ctx := t.Context()

	shoes, err := q.CreateGear(ctx, db.CreateGearParams{UserID: alice.ID, Kind: "shoes", Name: "Daily trainers", ReplacementDistanceM: ptr(800000.0)})
	check(t, err)
	spare, err := q.CreateGear(ctx, db.CreateGearParams{UserID: alice.ID, Kind: "shoes", Name: "Racers"})
	check(t, err)

	ids := map[string]int64{}
	for i, a := range []struct {
		name           string
		distance       *float64
		timer, elapsed *float64
	}{
		{"easy", ptr(10000.0), ptr(3000.0), ptr(3100.0)},
		// No timer time: elapsed time counts.
		{"long", ptr(21000.0), nil, ptr(7000.0)},
		// Nothing recorded adds nothing.
		{"treadmill", nil, nil, nil},
	} {
		id, err := q.UpsertActivity(ctx, db.UpsertActivityParams{
			UserID: alice.ID, Source: "manual", SourceActivityID: a.name, Sport: "running", Subtype: "outdoor",
			StartTime: start.Add(time.Duration(i) * time.Hour), DistanceM: a.distance, TimerTimeS: a.timer, ElapsedTimeS: a.elapsed,
		})
		check(t, err)
		ids[a.name] = id
		assignGear(t, q, alice.ID, id, &shoes.ID)
	}

	want := func(gearID int64, distance, time float64, n int64) {
		t.Helper()
		rows := gearTotals(t, q, alice.ID, gearID)
		if len(rows) != 1 || rows[0].DistanceM != distance || rows[0].TimeS != time || rows[0].Activities != n {
			t.Errorf("gear %d totals = %+v, want %v m, %v s, %d activities", gearID, rows, distance, time, n)
		}
	}
	want(shoes.ID, 31000, 10000, 3)
	want(spare.ID, 0, 0, 0)

	// Reassigning moves the activity's distance and time.
	assignGear(t, q, alice.ID, ids["long"], &spare.ID)
	want(shoes.ID, 10000, 3000, 2)
	want(spare.ID, 21000, 7000, 1)

	// Re-importing keeps the user's gear.
	_, err = q.UpsertActivity(ctx, db.UpsertActivityParams{
		UserID: alice.ID, Source: "manual", SourceActivityID: "long", Sport: "running", Subtype: "outdoor",
		StartTime: start.Add(time.Hour), DistanceM: ptr(21000.0), ElapsedTimeS: ptr(7000.0),
	})
	check(t, err)
	want(spare.ID, 21000, 7000, 1)

	for gearID, wantIDs := range map[int64][]string{shoes.ID: {"treadmill", "easy"}, spare.ID: {"long"}} {
		if got := listIDs(t, q, db.ListActivitiesParams{UserID: alice.ID, Sort: "date", GearID: &gearID}); !reflect.DeepEqual(got, wantIDs) {
			t.Errorf("gear %d lists %v, want %v", gearID, got, wantIDs)
		}
	}

	// Unassigning and deleting gear leave the activities in place.
	assignGear(t, q, alice.ID, ids["easy"], nil)
	want(shoes.ID, 0, 0, 1)
	n, err := q.DeleteGear(ctx, db.DeleteGearParams{UserID: alice.ID, ID: spare.ID})
	if err != nil || n != 1 {
		t.Fatalf("deleting: %d, %v", n, err)
	}
	a, err := q.GetActivity(ctx, db.GetActivityParams{UserID: alice.ID, ID: ids["long"]})
	check(t, err)
	if a.GearID != nil {
		t.Errorf("deleted gear still assigned: %d", *a.GearID)
	}
}

func TestListGearTotalsPaginates(t *testing.T) {
	t.Parallel()
	pool := pg.NewDB(t)
	alice, _ := testutil.TwoUsers(t, pool)
	q := db.New(pool)
	ctx := t.Context()
	for _, name := range []string{"a", "b", "c"} {
		_, err := q.CreateGear(ctx, db.CreateGearParams{UserID: alice.ID, Kind: "bike", Name: name})
		check(t, err)
	}
	var got []string
	var after int64
	for {
		page, err := q.ListGearTotals(ctx, db.ListGearTotalsParams{UserID: alice.ID, After: after, MaxRows: 2})
		check(t, err)
		for _, r := range page {
			got = append(got, r.Gear.Name)
			after = r.Gear.ID
		}
		if len(page) < 2 {
			break
		}
	}
	if !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Errorf("pages = %v", got)
	}
}

func TestGearChecks(t *testing.T) {
	t.Parallel()
	pool := pg.NewDB(t)
	alice, _ := testutil.TwoUsers(t, pool)
	q := db.New(pool)
	for name, p := range map[string]db.CreateGearParams{
		"kind":      {Kind: "skis", Name: "x"},
		"empty":     {Kind: "bike", Name: ""},
		"threshold": {Kind: "bike", Name: "x", ReplacementDistanceM: ptr(0.0)},
	} {
		p.UserID = alice.ID
		var pgErr *pgconn.PgError
		if _, err := q.CreateGear(t.Context(), p); !errors.As(err, &pgErr) || pgErr.Code != "23514" {
			t.Errorf("%s: error = %v, want a check violation", name, err)
		}
	}
}

func TestGearIsolation(t *testing.T) {
	t.Parallel()
	pool := pg.NewDB(t)
	alice, bob := testutil.TwoUsers(t, pool)
	q := db.New(pool)
	ctx := t.Context()

	gear, err := q.CreateGear(ctx, db.CreateGearParams{UserID: alice.ID, Kind: "bike", Name: "Road bike"})
	check(t, err)
	aliceRide, err := q.UpsertActivity(ctx, db.UpsertActivityParams{
		UserID: alice.ID, Source: "manual", SourceActivityID: "ride", Sport: "cycling", Subtype: "outdoor", StartTime: start, DistanceM: ptr(50000.0),
	})
	check(t, err)
	assignGear(t, q, alice.ID, aliceRide, &gear.ID)
	bobRide, err := q.UpsertActivity(ctx, db.UpsertActivityParams{
		UserID: bob.ID, Source: "manual", SourceActivityID: "ride", Sport: "cycling", Subtype: "outdoor", StartTime: start, DistanceM: ptr(90000.0),
	})
	check(t, err)

	if rows := gearTotals(t, q, bob.ID, gear.ID); len(rows) != 0 {
		t.Errorf("bob reads alice's gear: %+v", rows)
	}
	if rows, err := q.ListGearTotals(ctx, db.ListGearTotalsParams{UserID: bob.ID, MaxRows: 10}); err != nil || len(rows) != 0 {
		t.Errorf("bob lists %d gear, %v", len(rows), err)
	}
	if n, err := q.UpdateGear(ctx, db.UpdateGearParams{UserID: bob.ID, ID: gear.ID, Kind: "bike", Name: "mine"}); err != nil || n != 0 {
		t.Errorf("bob updates %d rows, %v", n, err)
	}
	if n, err := q.DeleteGear(ctx, db.DeleteGearParams{UserID: bob.ID, ID: gear.ID}); err != nil || n != 0 {
		t.Errorf("bob deletes %d rows, %v", n, err)
	}
	// Bob cannot put alice's bike on his own ride: the composite key rejects it.
	var pgErr *pgconn.PgError
	_, err = q.UpdateActivityAnnotation(ctx, db.UpdateActivityAnnotationParams{UserID: bob.ID, ID: bobRide, PainTags: []string{}, GearID: &gear.ID})
	if !errors.As(err, &pgErr) || pgErr.Code != "23503" {
		t.Errorf("bob assigns alice's gear: %v, want a foreign key violation", err)
	}
	if got := listIDs(t, q, db.ListActivitiesParams{UserID: bob.ID, Sort: "date", GearID: &gear.ID}); got != nil {
		t.Errorf("bob lists %v", got)
	}

	rows := gearTotals(t, q, alice.ID, gear.ID)
	if len(rows) != 1 || rows[0].Gear.Name != "Road bike" || rows[0].DistanceM != 50000 {
		t.Errorf("alice's gear changed: %+v", rows)
	}
}
