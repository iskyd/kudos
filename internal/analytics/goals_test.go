package analytics

import (
	"math"
	"testing"
	"time"
)

func TestProjectGoal(t *testing.T) {
	t.Parallel()
	day := func(m time.Month, d int) time.Time { return time.Date(2026, m, d, 0, 0, 0, 0, time.UTC) }
	november := func(d int) time.Time { return day(time.November, d) }
	first, last := november(1), november(30)
	for _, c := range []struct {
		name          string
		target, value float64
		today         time.Time
		status        GoalStatus
		projected     float64
		finish        *time.Time
	}{
		// 10 of 30 days, 40 of 100 km: 4 km/day ≥ even 3.33; 120 km by the end;
		// 100 km on day ceil(25) = the 25th.
		{"ahead", 100, 40, november(10), GoalAhead, 120, new(november(25))},
		// 15 of 30 days, 30 of 100 km: 2 km/day; 60 km by the end; the target
		// would come on day 50, past the period.
		{"behind", 100, 30, november(15), GoalBehind, 60, nil},
		// Exactly the even pace counts as ahead: 50 by day 15 finishes on day 30.
		{"on pace", 100, 50, november(15), GoalAhead, 100, new(november(30))},
		// Nothing yet: no rate, no finish.
		{"nothing yet", 100, 0, november(3), GoalBehind, 0, nil},
		{"achieved early", 100, 110, november(20), GoalAchieved, 165, nil},
		{"achieved, period ended", 100, 105, day(time.December, 4), GoalAchieved, 105, nil},
		{"missed, period ended", 100, 80, day(time.December, 1), GoalMissed, 80, nil},
		{"upcoming", 100, 0, day(time.October, 31), GoalUpcoming, 0, nil},
		// The last day still projects: 90 over all 30 days.
		{"last day", 100, 90, november(30), GoalBehind, 90, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := ProjectGoal(c.target, c.value, first, last, c.today)
			if got.Status != c.status || math.Abs(got.Projected-c.projected) > 1e-9 {
				t.Errorf("status %s, projected %v; want %s, %v", got.Status, got.Projected, c.status, c.projected)
			}
			if (got.Finish == nil) != (c.finish == nil) || got.Finish != nil && !got.Finish.Equal(*c.finish) {
				t.Errorf("finish %v, want %v", got.Finish, c.finish)
			}
		})
	}
}
