package store_test

import (
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/iskyd/kudos/internal/store/db"
	"github.com/iskyd/kudos/internal/testutil"
)

func TestGoalsPaginateNewestFirst(t *testing.T) {
	t.Parallel()
	pool := pg.NewDB(t)
	alice, _ := testutil.TwoUsers(t, pool)
	q := db.New(pool)
	ctx := t.Context()

	var ids []int64
	for _, g := range []db.CreateGoalParams{
		{Period: "year", StartDay: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Sport: ptr("running"), Metric: "distance", Target: 2e6},
		{Period: "month", StartDay: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), Metric: "duration", Target: 36000},
		{Period: "week", StartDay: time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC), Sport: ptr("cycling"), Metric: "ascent", Target: 3000},
	} {
		g.UserID = alice.ID
		created, err := q.CreateGoal(ctx, g)
		check(t, err)
		ids = append(ids, created.ID)
	}

	page, err := q.ListGoals(ctx, db.ListGoalsParams{UserID: alice.ID, MaxRows: 2})
	check(t, err)
	if len(page) != 2 || page[0].ID != ids[2] || page[1].ID != ids[1] || page[0].Metric != "ascent" || *page[0].Sport != "cycling" {
		t.Fatalf("first page = %+v", page)
	}
	rest, err := q.ListGoals(ctx, db.ListGoalsParams{UserID: alice.ID, Before: &page[1].ID, MaxRows: 2})
	check(t, err)
	if len(rest) != 1 || rest[0].ID != ids[0] || rest[0].Target != 2e6 {
		t.Fatalf("second page = %+v", rest)
	}

	if n, err := q.DeleteGoal(ctx, db.DeleteGoalParams{UserID: alice.ID, ID: ids[1]}); err != nil || n != 1 {
		t.Fatalf("delete: %d, %v", n, err)
	}
	if all, err := q.ListGoals(ctx, db.ListGoalsParams{UserID: alice.ID, MaxRows: 10}); err != nil || len(all) != 2 {
		t.Errorf("after delete: %+v, %v", all, err)
	}
}

func TestGoalChecks(t *testing.T) {
	t.Parallel()
	pool := pg.NewDB(t)
	alice, _ := testutil.TwoUsers(t, pool)
	q := db.New(pool)
	monday := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)
	for name, g := range map[string]db.CreateGoalParams{
		"week not from Monday":    {Period: "week", StartDay: monday.AddDate(0, 0, 1), Metric: "distance", Target: 1},
		"month not from the 1st":  {Period: "month", StartDay: time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC), Metric: "distance", Target: 1},
		"year not from 1 January": {Period: "year", StartDay: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC), Metric: "distance", Target: 1},
		"unknown period":          {Period: "day", StartDay: monday, Metric: "distance", Target: 1},
		"unknown metric":          {Period: "week", StartDay: monday, Metric: "load", Target: 1},
		"zero target":             {Period: "week", StartDay: monday, Metric: "distance", Target: 0},
	} {
		g.UserID = alice.ID
		var pgErr *pgconn.PgError
		if _, err := q.CreateGoal(t.Context(), g); !errors.As(err, &pgErr) || pgErr.Code != "23514" {
			t.Errorf("%s: %v, want a check violation", name, err)
		}
	}
}

func TestGoalIsolation(t *testing.T) {
	t.Parallel()
	pool := pg.NewDB(t)
	alice, bob := testutil.TwoUsers(t, pool)
	q := db.New(pool)
	ctx := t.Context()

	goal, err := q.CreateGoal(ctx, db.CreateGoalParams{
		UserID: alice.ID, Period: "year", StartDay: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Metric: "distance", Target: 2e6,
	})
	check(t, err)
	if rows, err := q.ListGoals(ctx, db.ListGoalsParams{UserID: bob.ID, MaxRows: 10}); err != nil || len(rows) != 0 {
		t.Errorf("bob lists %+v, %v", rows, err)
	}
	if n, err := q.DeleteGoal(ctx, db.DeleteGoalParams{UserID: bob.ID, ID: goal.ID}); err != nil || n != 0 {
		t.Errorf("bob deletes %d rows, %v", n, err)
	}
	if rows, err := q.ListGoals(ctx, db.ListGoalsParams{UserID: alice.ID, MaxRows: 10}); err != nil || len(rows) != 1 {
		t.Errorf("alice's goals changed: %+v, %v", rows, err)
	}
}
