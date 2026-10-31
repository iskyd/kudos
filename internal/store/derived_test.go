package store_test

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/iskyd/kudos/internal/store/db"
	"github.com/iskyd/kudos/internal/testutil"
)

func TestActivityDerivedIsolationAndInvalidation(t *testing.T) {
	t.Parallel()
	pool := pg.NewDB(t)
	alice, bob := testutil.TwoUsers(t, pool)
	q := db.New(pool)
	ctx := t.Context()

	// Alice's runs start at 23:30 UTC on 14 and 19 October, two hours ahead
	// locally: 15 and 20 October where they happened.
	var ids []int64
	for i, day := range []int{14, 19} {
		id, err := q.UpsertActivity(ctx, db.UpsertActivityParams{
			UserID: alice.ID, Source: "manual", SourceActivityID: string(rune('a' + i)), Sport: "running", Subtype: "outdoor",
			StartTime: time.Date(2026, 10, day, 23, 30, 0, 0, time.UTC), UtcOffsetS: 7200,
		})
		check(t, err)
		ids = append(ids, id)
	}
	bobs, err := q.UpsertActivity(ctx, db.UpsertActivityParams{UserID: bob.ID, Source: "manual", SourceActivityID: "b", Sport: "running", Subtype: "outdoor", StartTime: start})
	check(t, err)

	needing := func(userID, after int64) []int64 {
		got, err := q.ListActivitiesNeedingDerived(ctx, db.ListActivitiesNeedingDerivedParams{UserID: userID, After: after, Version: 1, MaxRows: 10})
		check(t, err)
		return got
	}
	if got := needing(alice.ID, 0); !slices.Equal(got, ids) {
		t.Fatalf("missing rows: %v, want %v", got, ids)
	}
	if got := needing(alice.ID, ids[0]); !slices.Equal(got, ids[1:]) {
		t.Errorf("after the first: %v", got)
	}
	for _, id := range ids {
		check(t, q.UpsertActivityDerived(ctx, db.UpsertActivityDerivedParams{UserID: alice.ID, ActivityID: id, Version: 1, AvgHrBpm: ptr(150.0), HrZoneS: []float64{1, 2}}))
	}
	if got := needing(alice.ID, 0); len(got) != 0 {
		t.Errorf("after computing: %v", got)
	}
	if got := needing(bob.ID, 0); !slices.Equal(got, []int64{bobs}) {
		t.Errorf("bob needs %v", got)
	}

	// Bob reaches none of Alice's rows: no read, no write, no invalidation.
	if _, err := q.GetActivityDerived(ctx, db.GetActivityDerivedParams{UserID: bob.ID, ActivityID: ids[0]}); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("bob read alice's row: %v", err)
	}
	if err := q.UpsertActivityDerived(ctx, db.UpsertActivityDerivedParams{UserID: bob.ID, ActivityID: ids[0], Version: 1}); err != nil {
		t.Errorf("bob's write: %v", err)
	}
	if d, err := q.GetActivityDerived(ctx, db.GetActivityDerivedParams{UserID: alice.ID, ActivityID: ids[0]}); err != nil || d.AvgHrBpm == nil {
		t.Errorf("bob overwrote alice's row: %+v, %v", d, err)
	}
	if n, err := q.InvalidateDerivedFrom(ctx, db.InvalidateDerivedFromParams{UserID: bob.ID, FromDay: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)}); err != nil || n != 0 {
		t.Errorf("bob invalidated %d: %v", n, err)
	}

	// Invalidation counts local dates: the first run is on 15 October locally.
	n, err := q.InvalidateDerivedFrom(ctx, db.InvalidateDerivedFromParams{UserID: alice.ID, FromDay: time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)})
	check(t, err)
	if n != 2 {
		t.Errorf("invalidated %d from 15 October, want both", n)
	}
	check(t, q.UpsertActivityDerived(ctx, db.UpsertActivityDerivedParams{UserID: alice.ID, ActivityID: ids[0], Version: 1}))
	d, err := q.GetActivityDerived(ctx, db.GetActivityDerivedParams{UserID: alice.ID, ActivityID: ids[0]})
	check(t, err)
	if d.Stale || d.AvgHrBpm != nil {
		t.Errorf("a recompute replaces the row: %+v", d)
	}
	if got := needing(alice.ID, 0); !slices.Equal(got, ids[1:]) {
		t.Errorf("still stale: %v", got)
	}
	check(t, q.UpsertActivityDerived(ctx, db.UpsertActivityDerivedParams{UserID: alice.ID, ActivityID: ids[1], Version: 1}))
	n, err = q.InvalidateDerivedFrom(ctx, db.InvalidateDerivedFromParams{UserID: alice.ID, FromDay: time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)})
	check(t, err)
	if got := needing(alice.ID, 0); n != 1 || !slices.Equal(got, ids[1:]) {
		t.Errorf("from 16 October: %d invalidated, needing %v", n, got)
	}

	// Rows of an older version need recomputing.
	if got, err := q.ListActivitiesNeedingDerived(ctx, db.ListActivitiesNeedingDerivedParams{UserID: alice.ID, Version: 2, MaxRows: 10}); err != nil || len(got) != 2 {
		t.Errorf("for version 2: %v, %v", got, err)
	}

	// Daily loads sum by local date, within the range, for the user only.
	method := "hr_tss"
	for i, l := range []float64{30, 50} {
		check(t, q.UpsertActivityDerived(ctx, db.UpsertActivityDerivedParams{UserID: alice.ID, ActivityID: ids[i], Version: 1, Load: &l, LoadMethod: &method}))
	}
	check(t, q.UpsertActivityDerived(ctx, db.UpsertActivityDerivedParams{UserID: bob.ID, ActivityID: bobs, Version: 1, Load: new(70.0), LoadMethod: &method}))
	loads := func(userID int64, from, to int) []db.ListDailyLoadsRow {
		got, err := q.ListDailyLoads(ctx, db.ListDailyLoadsParams{
			UserID: userID, FromDay: time.Date(2026, 10, from, 0, 0, 0, 0, time.UTC), ToDay: time.Date(2026, 10, to, 0, 0, 0, 0, time.UTC), MaxRows: 10,
		})
		check(t, err)
		return got
	}
	if got := loads(alice.ID, 15, 20); len(got) != 2 || got[0].Day.Day() != 15 || got[0].Load != 30 || got[1].Day.Day() != 20 || got[1].Load != 50 {
		t.Errorf("alice's loads: %+v", got)
	}
	if got := loads(alice.ID, 16, 19); len(got) != 0 {
		t.Errorf("outside the range: %+v", got)
	}
	if got := loads(bob.ID, 1, 31); len(got) != 1 || got[0].Load != 70 {
		t.Errorf("bob's loads: %+v", got)
	}

	users, err := q.ListUserIDs(ctx, db.ListUserIDsParams{MaxRows: 1})
	check(t, err)
	more, err := q.ListUserIDs(ctx, db.ListUserIDsParams{After: users[0], MaxRows: 10})
	check(t, err)
	if all := slices.Concat(users, more); len(users) != 1 || !slices.Contains(all, alice.ID) || !slices.Contains(all, bob.ID) {
		t.Errorf("users paged: %v then %v", users, more)
	}
}

func TestActivityTrendsAndIsolation(t *testing.T) {
	t.Parallel()
	pool := pg.NewDB(t)
	alice, bob := testutil.TwoUsers(t, pool)
	q := db.New(pool)
	ctx := t.Context()
	n := 0
	activity := func(userID int64, sport string, day int, d db.UpsertActivityDerivedParams) int64 {
		n++
		id, err := q.UpsertActivity(ctx, db.UpsertActivityParams{
			UserID: userID, Source: "manual", SourceActivityID: string(rune('a' + n)), Sport: sport, Subtype: "outdoor",
			StartTime: time.Date(2026, 10, day, 7, 0, 0, 0, time.UTC), PoolLengthM: ptr(float32(25)),
		})
		check(t, err)
		d.UserID, d.ActivityID, d.Version = userID, id, 1
		check(t, q.UpsertActivityDerived(ctx, d))
		return id
	}
	road := activity(alice.ID, "running", 1, db.UpsertActivityDerivedParams{EfficiencyMPerBeat: ptr(1.2), DecouplingPct: ptr(5.0), AvgStepLengthM: ptr(1.1)})
	trail := activity(alice.ID, "trail_running", 3, db.UpsertActivityDerivedParams{EfficiencyMPerBeat: ptr(1.3)})
	swim := activity(alice.ID, "pool_swimming", 2, db.UpsertActivityDerivedParams{AvgSwolf: ptr(100.0)})
	activity(alice.ID, "cycling", 4, db.UpsertActivityDerivedParams{})
	virtual := activity(alice.ID, "running", 5, db.UpsertActivityDerivedParams{})
	check(t, q.SetActivityOverrides(ctx, db.SetActivityOverridesParams{UserID: alice.ID, ID: virtual, Overrides: []byte(`{"sport":"running","subtype":"virtual"}`)}))
	merged := activity(alice.ID, "running", 6, db.UpsertActivityDerivedParams{})
	check(t, q.SetActivityMergedInto(ctx, db.SetActivityMergedIntoParams{UserID: alice.ID, ID: merged, MergedInto: &road}))
	bobs := activity(bob.ID, "running", 1, db.UpsertActivityDerivedParams{EfficiencyMPerBeat: ptr(1.0)})

	list := func(userID int64, kind string, from *time.Time, before *db.ListActivityTrendsRow, limit int32) []int64 {
		t.Helper()
		p := db.ListActivityTrendsParams{
			UserID: userID, Kind: kind, FromDay: from, BeforeTime: time.Date(3000, 1, 1, 0, 0, 0, 0, time.UTC), BeforeID: 1 << 62, MaxRows: limit,
		}
		if before != nil {
			p.BeforeTime, p.BeforeID = before.StartTime, before.ID
		}
		rows, err := q.ListActivityTrends(ctx, p)
		check(t, err)
		var ids []int64
		for _, r := range rows {
			ids = append(ids, r.ID)
		}
		return ids
	}
	// Outdoor runs, newest first: not the ride, the virtual run or the merged-away one.
	if got := list(alice.ID, "running", nil, nil, 10); !slices.Equal(got, []int64{trail, road}) {
		t.Errorf("runs: %v, want %v", got, []int64{trail, road})
	}
	rows, err := q.ListActivityTrends(ctx, db.ListActivityTrendsParams{
		UserID: alice.ID, Kind: "running", BeforeTime: time.Date(3000, 1, 1, 0, 0, 0, 0, time.UTC), BeforeID: 1 << 62, MaxRows: 1,
	})
	check(t, err)
	if len(rows) != 1 || *rows[0].EfficiencyMPerBeat != 1.3 || rows[0].Sport != "trail_running" {
		t.Fatalf("first page: %+v", rows)
	}
	if got := list(alice.ID, "running", nil, &rows[0], 10); !slices.Equal(got, []int64{road}) {
		t.Errorf("second page: %v", got)
	}
	if got := list(alice.ID, "running", ptr(time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)), nil, 10); !slices.Equal(got, []int64{trail}) {
		t.Errorf("from 2 October: %v", got)
	}
	if got := list(alice.ID, "swimming", nil, nil, 10); !slices.Equal(got, []int64{swim}) {
		t.Errorf("swims: %v", got)
	}
	if got := list(alice.ID, "cycling", nil, nil, 10); len(got) != 0 {
		t.Errorf("unknown kind: %v", got)
	}
	if got := list(bob.ID, "running", nil, nil, 10); !slices.Equal(got, []int64{bobs}) {
		t.Errorf("bob's runs: %v", got)
	}
}
