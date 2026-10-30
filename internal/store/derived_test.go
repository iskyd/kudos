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

	users, err := q.ListUserIDs(ctx, db.ListUserIDsParams{MaxRows: 1})
	check(t, err)
	more, err := q.ListUserIDs(ctx, db.ListUserIDsParams{After: users[0], MaxRows: 10})
	check(t, err)
	if all := slices.Concat(users, more); len(users) != 1 || !slices.Contains(all, alice.ID) || !slices.Contains(all, bob.ID) {
		t.Errorf("users paged: %v then %v", users, more)
	}
}
