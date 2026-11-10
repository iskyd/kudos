package api

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/iskyd/kudos/internal/store/db"
)

const intervalSteps = `[
	{"kind": "warmup", "durationType": "time", "duration": 600, "target": {"metric": "heart_rate", "zone": 2}},
	{"kind": "repeat", "count": 5, "steps": [
		{"kind": "interval", "durationType": "distance", "duration": 1000, "target": {"metric": "pace", "low": 3.9, "high": 4.1}},
		{"kind": "recovery", "durationType": "time", "duration": 90}
	]},
	{"kind": "cooldown", "durationType": "open", "note": "easy"}
]`

// planWorkout stores a planned workout for the user registered on c.
func planWorkout(t *testing.T, q *db.Queries, userID int64, day, title string) db.PlannedWorkout {
	t.Helper()
	plan, err := q.CreatePlan(t.Context(), userID)
	if err != nil {
		t.Fatal(err)
	}
	d, _ := time.Parse(time.DateOnly, day)
	w, err := q.CreatePlannedWorkout(t.Context(), db.CreatePlannedWorkoutParams{
		UserID: userID, PlanID: plan.ID, Day: d, Sport: "running", Title: title, Description: new("Track session"), Steps: []byte(intervalSteps),
	})
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func TestPlannedWorkouts(t *testing.T) {
	t.Parallel()
	pool := pg.NewDB(t)
	alice, q := newAPIOn(t, pool, true, nil)
	me := alice.session(alice.do(http.MethodPost, "/api/auth/register", register("alice"), nil), http.StatusCreated)
	bob := chatUser(t, pool, "bob")
	track := planWorkout(t, q, me.Id, "2026-11-12", "5 × 1 km")
	planWorkout(t, q, me.Id, "2026-11-20", "Later")

	got := decodeJSON[PlannedWorkout](t, alice.do(http.MethodGet, fmt.Sprintf("/api/planned-workouts/%d", track.ID), nil, nil), http.StatusOK)
	if got.Title != "5 × 1 km" || got.PlanId != track.PlanID || got.Day.Format(time.DateOnly) != "2026-11-12" || *got.Description != "Track session" || len(got.Steps) != 3 {
		t.Fatalf("got %+v", got)
	}
	rep := got.Steps[1]
	if rep.Kind != PlanStepKindRepeat || *rep.Count != 5 || len(*rep.Steps) != 2 {
		t.Fatalf("repeat %+v", rep)
	}
	interval := (*rep.Steps)[0]
	if *interval.DurationType != PlanStepDurationTypeDistance || *interval.Duration != 1000 || interval.Target.Metric != PlanTargetMetricPace || *interval.Target.Low != 3.9 {
		t.Errorf("interval %+v %+v", interval, interval.Target)
	}
	if cool := got.Steps[2]; *cool.DurationType != PlanStepDurationTypeOpen || cool.Duration != nil || *cool.Note != "easy" {
		t.Errorf("cooldown %+v", cool)
	}

	week := decodeJSON[[]PlannedWorkout](t, alice.do(http.MethodGet, "/api/planned-workouts?from=2026-11-09&to=2026-11-15", nil, nil), http.StatusOK)
	if len(week) != 1 || week[0].Id != track.ID {
		t.Errorf("week %+v", week)
	}
	cal := decodeJSON[Calendar](t, alice.do(http.MethodGet, "/api/calendar?from=2026-11-09&to=2026-11-22", nil, nil), http.StatusOK)
	if len(cal.PlannedWorkouts) != 2 || cal.PlannedWorkouts[1].Title != "Later" {
		t.Errorf("calendar %+v", cal.PlannedWorkouts)
	}

	// Bob sees none of them.
	wantProblem(t, bob.do(http.MethodGet, fmt.Sprintf("/api/planned-workouts/%d", track.ID), nil, nil), KindNotFound)
	if other := decodeJSON[[]PlannedWorkout](t, bob.do(http.MethodGet, "/api/planned-workouts?from=2026-11-09&to=2026-11-15", nil, nil), http.StatusOK); len(other) != 0 {
		t.Errorf("bob lists %+v", other)
	}
	if cal := decodeJSON[Calendar](t, bob.do(http.MethodGet, "/api/calendar?from=2026-11-09&to=2026-11-22", nil, nil), http.StatusOK); len(cal.PlannedWorkouts) != 0 {
		t.Errorf("bob's calendar %+v", cal.PlannedWorkouts)
	}

	for _, path := range []string{
		"/api/planned-workouts?from=2026-11-15&to=2026-11-09",
		"/api/planned-workouts?from=2026-11-01&to=2026-12-31",
		"/api/planned-workouts?from=2026-11-01",
	} {
		wantProblem(t, alice.do(http.MethodGet, path, nil, nil), KindInvalidRequest)
	}
	wantProblem(t, alice.do(http.MethodGet, "/api/planned-workouts/999999", nil, nil), KindNotFound)
}
