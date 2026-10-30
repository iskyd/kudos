package api

import (
	"fmt"
	"net/http"
	"testing"
)

func execution(t *testing.T, c *apiClient, id int64) WorkoutExecution {
	t.Helper()
	return decodeJSON[WorkoutExecution](t, c.do(http.MethodGet, fmt.Sprintf("/api/activities/%d/execution", id), nil, nil), http.StatusOK)
}

func TestExecutionOfWorkoutSteps(t *testing.T) {
	t.Parallel()
	c, _, _ := loggedIn(t, "UTC")
	got := execution(t, c, uploadCorpus(t, c, "intervals.fit"))
	if got.Kind != Steps || got.Steps == nil || len(*got.Steps) != 22 {
		t.Fatalf("execution: %+v", got)
	}
	steps := *got.Steps
	if steps[0].Laps != 2 || steps[0].Result != nil {
		t.Errorf("warm-up: %+v", steps[0])
	}
	for rep := range 10 {
		if r := steps[1+2*rep]; r.StepIndex != 1 || r.Result == nil || *r.Result != Above || r.Actual == nil {
			t.Errorf("rep %d: %+v", rep+1, r)
		}
	}
	if r := steps[20]; r.Laps != 0 || r.Result == nil || *r.Result != Skipped {
		t.Errorf("last recovery: %+v", r)
	}
	if got.Intervals != nil || got.WithinS != nil {
		t.Errorf("power fields on a steps execution: %+v", got)
	}
}

func TestExecutionAgainstTargetPower(t *testing.T) {
	t.Parallel()
	c, _, _ := loggedIn(t, "UTC")
	id := uploadCorpus(t, c, "zwift.fit")
	got := execution(t, c, id)
	if got.Kind != TargetPower || got.Intervals == nil || got.WithinS == nil || got.TotalS == nil || *got.ToleranceW != 10 {
		t.Fatalf("execution: %+v", got)
	}
	if share := *got.WithinS / *got.TotalS; share < 0.955 || share > 0.965 {
		t.Errorf("%.3f of the time within 10 W", share)
	}
	first := (*got.Intervals)[0]
	if first.StartS != 0 || first.TargetW != 75 {
		t.Errorf("first interval: %+v", first)
	}

	// The view follows edits: after a trim, intervals count from the trimmed start.
	editActivity(t, c, id, ActivityEditRequest{Kind: ActivityEditKindTrim, FromS: new(421.0), ToS: new(541.0)})
	got = execution(t, c, id)
	if n := len(*got.Intervals); n != 2 || (*got.Intervals)[0].TargetW != 135 || (*got.Intervals)[0].StartS != 0 {
		t.Errorf("trimmed intervals: %+v", *got.Intervals)
	}
}

func TestExecutionWithoutWorkout(t *testing.T) {
	t.Parallel()
	c, _, _ := loggedIn(t, "UTC")
	id := uploadCorpus(t, c, "half_marathon.fit")
	if got := execution(t, c, id); got.Kind != None || got.Steps != nil || got.Intervals != nil {
		t.Errorf("execution: %+v", got)
	}

	bob := &apiClient{t: t, h: c.h}
	bob.session(bob.do(http.MethodPost, "/api/auth/register", register("bob"), nil), http.StatusCreated)
	wantProblem(t, bob.do(http.MethodGet, fmt.Sprintf("/api/activities/%d/execution", id), nil, nil), KindNotFound)
}
