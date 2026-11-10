package store_test

import (
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/iskyd/kudos/internal/store/db"
	"github.com/iskyd/kudos/internal/testutil"
)

func TestPlannedWorkouts(t *testing.T) {
	t.Parallel()
	pool := pg.NewDB(t)
	alice, bob := testutil.TwoUsers(t, pool)
	q := db.New(pool)
	ctx := t.Context()
	on := func(s string) time.Time {
		d, _ := time.Parse(time.DateOnly, s)
		return d
	}
	plan, err := q.CreatePlan(ctx, alice.ID)
	check(t, err)
	steps := []byte(`[{"kind": "recovery", "durationType": "time", "duration": 1800}]`)
	add := func(userID, planID int64, day, title string) (db.PlannedWorkout, error) {
		return q.CreatePlannedWorkout(ctx, db.CreatePlannedWorkoutParams{
			UserID: userID, PlanID: planID, Day: on(day), Sport: "running", Title: title, Steps: steps,
		})
	}
	long, err := add(alice.ID, plan.ID, "2026-11-15", "Long run")
	check(t, err)
	_, err = add(alice.ID, plan.ID, "2026-11-12", "Easy run")
	check(t, err)
	_, err = add(alice.ID, plan.ID, "2026-12-01", "Later")
	check(t, err)

	got, err := q.ListPlannedWorkoutsBetween(ctx, db.ListPlannedWorkoutsBetweenParams{UserID: alice.ID, FromDay: on("2026-11-09"), ToDay: on("2026-11-15"), MaxRows: 10})
	check(t, err)
	if len(got) != 2 || got[0].Title != "Easy run" || got[1].Title != "Long run" {
		t.Fatalf("between %+v", got)
	}
	one, err := q.GetPlannedWorkout(ctx, db.GetPlannedWorkoutParams{UserID: alice.ID, ID: long.ID})
	check(t, err)
	if one.PlanID != plan.ID || string(one.Steps) != `[{"kind": "recovery", "duration": 1800, "durationType": "time"}]` {
		t.Errorf("get %+v %s", one, one.Steps)
	}

	// Stored steps are always an array.
	if _, err := q.CreatePlannedWorkout(ctx, db.CreatePlannedWorkoutParams{
		UserID: alice.ID, PlanID: plan.ID, Day: on("2026-11-12"), Sport: "running", Title: "x", Steps: []byte(`{"kind": "recovery"}`),
	}); err == nil {
		t.Error("steps that are not an array were stored")
	}

	// Bob neither reads alice's workouts nor adds to her plan.
	if _, err := q.GetPlannedWorkout(ctx, db.GetPlannedWorkoutParams{UserID: bob.ID, ID: long.ID}); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("bob reads alice's workout: %v", err)
	}
	if rows, err := q.ListPlannedWorkoutsBetween(ctx, db.ListPlannedWorkoutsBetweenParams{UserID: bob.ID, FromDay: on("2026-01-01"), ToDay: on("2026-12-31"), MaxRows: 10}); err != nil || len(rows) != 0 {
		t.Errorf("bob lists %+v, %v", rows, err)
	}
	if _, err := add(bob.ID, plan.ID, "2026-11-12", "Into alice's plan"); err == nil {
		t.Error("bob added a workout to alice's plan")
	}
}
