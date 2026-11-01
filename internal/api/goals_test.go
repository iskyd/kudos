package api

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"
)

func createGoal(t *testing.T, c *apiClient, in GoalInput) Goal {
	t.Helper()
	return decodeJSON[Goal](t, c.do(http.MethodPost, "/api/goals", in, nil), http.StatusCreated)
}

func day(s string) openapi_types.Date {
	d, _ := time.Parse(time.DateOnly, s) // the tests' literals are valid
	return openapi_types.Date{Time: d}
}

func TestGoalProgress(t *testing.T) {
	t.Parallel()
	c, _, _ := loggedIn(t, "UTC")
	run(t, c, "2026-10-01T07:00", 10000, 3000)
	run(t, c, "2026-10-02T07:00", 21000, 7000)
	running := ActivitySport(SportRunning)

	// Any day in October gives the whole month; 31 km of 30 km is achieved.
	got := createGoal(t, c, GoalInput{Period: GoalInputPeriodMonth, Start: day("2026-10-15"), Sport: &running, Metric: GoalInputMetricDistance, Target: 30000})
	if got.Start != day("2026-10-01") || got.End != day("2026-10-31") || got.Value != 31000 || got.Status != "achieved" || got.ProjectedFinish != nil {
		t.Errorf("achieved goal = %+v", got)
	}
	// 10,000 s of 20,000 s when October ended is missed; the projection is the value.
	got = createGoal(t, c, GoalInput{Period: GoalInputPeriodMonth, Start: day("2026-10-01"), Metric: GoalInputMetricDuration, Target: 20000})
	if got.Value != 10000 || got.Projected != 10000 || got.Status != "missed" {
		t.Errorf("missed goal = %+v", got)
	}
	// No cycling counts toward a cycling goal.
	cycling := ActivitySport(SportCycling)
	got = createGoal(t, c, GoalInput{Period: GoalInputPeriodYear, Start: day("2026-06-01"), Sport: &cycling, Metric: GoalInputMetricDistance, Target: 1e6})
	if got.Start != day("2026-01-01") || got.End != day("2026-12-31") || got.Value != 0 {
		t.Errorf("cycling goal = %+v", got)
	}
	// A week runs Monday to Sunday.
	got = createGoal(t, c, GoalInput{Period: GoalInputPeriodWeek, Start: day("2026-10-01"), Metric: GoalInputMetricAscent, Target: 500})
	if got.Start != day("2026-09-28") || got.End != day("2026-10-04") {
		t.Errorf("week goal = %+v", got)
	}
	got = createGoal(t, c, GoalInput{Period: GoalInputPeriodYear, Start: day("2100-03-01"), Metric: GoalInputMetricDistance, Target: 1})
	if got.Status != "upcoming" || got.Value != 0 {
		t.Errorf("future goal = %+v", got)
	}
	// This week without activities is behind, with nothing projected.
	got = createGoal(t, c, GoalInput{Period: GoalInputPeriodWeek, Start: openapi_types.Date{Time: time.Now().UTC()}, Metric: GoalInputMetricDistance, Target: 1})
	if got.Status != "behind" || got.Projected != 0 || got.ProjectedFinish != nil {
		t.Errorf("this week's goal = %+v", got)
	}

	page := decodeJSON[GoalPage](t, c.do(http.MethodGet, "/api/goals?limit=4", nil, nil), http.StatusOK)
	if len(page.Items) != 4 || page.NextCursor == nil || page.Items[0].Id != got.Id {
		t.Fatalf("first page = %+v", page)
	}
	rest := decodeJSON[GoalPage](t, c.do(http.MethodGet, "/api/goals?limit=4&cursor="+*page.NextCursor, nil, nil), http.StatusOK)
	if len(rest.Items) != 2 || rest.NextCursor != nil || rest.Items[1].Status != "achieved" {
		t.Errorf("second page = %+v", rest)
	}

	if rec := c.do(http.MethodDelete, fmt.Sprintf("/api/goals/%d", got.Id), nil, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body)
	}
	if all := decodeJSON[GoalPage](t, c.do(http.MethodGet, "/api/goals", nil, nil), http.StatusOK); len(all.Items) != 5 {
		t.Errorf("after delete: %d goals", len(all.Items))
	}
}

func TestGoalValidation(t *testing.T) {
	t.Parallel()
	c, _, _ := loggedIn(t, "UTC")
	valid := GoalInput{Period: GoalInputPeriodMonth, Start: day("2026-10-01"), Metric: GoalInputMetricDistance, Target: 1}
	unknown := ActivitySport("rowing")
	for name, in := range map[string]GoalInput{
		"period": {Period: "day", Start: valid.Start, Metric: valid.Metric, Target: 1},
		"metric": {Period: valid.Period, Start: valid.Start, Metric: "load", Target: 1},
		"sport":  {Period: valid.Period, Start: valid.Start, Sport: &unknown, Metric: valid.Metric, Target: 1},
		"target": {Period: valid.Period, Start: valid.Start, Metric: valid.Metric, Target: 0},
	} {
		t.Run(name, func(t *testing.T) {
			wantProblem(t, c.do(http.MethodPost, "/api/goals", in, nil), KindInvalidRequest)
		})
	}
	wantProblem(t, c.do(http.MethodGet, "/api/goals?limit=0", nil, nil), KindInvalidRequest)
	wantProblem(t, c.do(http.MethodDelete, "/api/goals/999999", nil, nil), KindNotFound)
}

func TestGoalIsolationAPI(t *testing.T) {
	t.Parallel()
	c, _, _ := loggedIn(t, "UTC")
	run(t, c, "2026-10-01T07:00", 10000, 3000)
	mine := createGoal(t, c, GoalInput{Period: GoalInputPeriodMonth, Start: day("2026-10-01"), Metric: GoalInputMetricDistance, Target: 5000})

	bob := &apiClient{t: t, h: c.h}
	bob.session(bob.do(http.MethodPost, "/api/auth/register", register("bob"), nil), http.StatusCreated)
	wantProblem(t, bob.do(http.MethodDelete, fmt.Sprintf("/api/goals/%d", mine.Id), nil, nil), KindNotFound)
	if page := decodeJSON[GoalPage](t, bob.do(http.MethodGet, "/api/goals", nil, nil), http.StatusOK); len(page.Items) != 0 {
		t.Errorf("bob lists %+v", page.Items)
	}
	// Bob's goal over the same month counts none of alice's runs.
	if got := createGoal(t, bob, GoalInput{Period: GoalInputPeriodMonth, Start: day("2026-10-01"), Metric: GoalInputMetricDistance, Target: 5000}); got.Value != 0 {
		t.Errorf("bob's goal counts %v m", got.Value)
	}
	if page := decodeJSON[GoalPage](t, c.do(http.MethodGet, "/api/goals", nil, nil), http.StatusOK); len(page.Items) != 1 || page.Items[0].Value != 10000 {
		t.Errorf("alice's goals changed: %+v", page.Items)
	}
}
