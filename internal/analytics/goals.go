package analytics

import (
	"math"
	"time"
)

// GoalStatus is where a volume goal stands against its period.
type GoalStatus string

// The goal statuses.
const (
	GoalUpcoming GoalStatus = "upcoming" // the period has not started
	GoalAhead    GoalStatus = "ahead"    // at or above the even pace to the target
	GoalBehind   GoalStatus = "behind"   // below the even pace to the target
	GoalAchieved GoalStatus = "achieved" // the target is reached
	GoalMissed   GoalStatus = "missed"   // the period ended short of the target
)

// GoalProgress projects a volume goal from its value so far.
type GoalProgress struct {
	Status GoalStatus
	// Projected is the value at the period's end at the rate so far; the value
	// itself once the period ended or before it starts.
	Projected float64
	// Finish is the day the target is reached at the rate so far, when that
	// falls within the period and the target is not yet reached.
	Finish *time.Time
}

// ProjectGoal projects value toward target over the calendar days first..last
// (inclusive), as of today. Days are dates at UTC midnight; today counts as elapsed.
func ProjectGoal(target, value float64, first, last, today time.Time) GoalProgress {
	days := daysBetween(first, last) + 1
	elapsed := daysBetween(first, today) + 1
	switch {
	case value >= target:
		return GoalProgress{Status: GoalAchieved, Projected: value * float64(days) / float64(min(max(elapsed, 1), days))}
	case elapsed < 1:
		return GoalProgress{Status: GoalUpcoming, Projected: value}
	case elapsed > days:
		return GoalProgress{Status: GoalMissed, Projected: value}
	}
	p := GoalProgress{Status: GoalBehind, Projected: value * float64(days) / float64(elapsed)}
	if value >= target*float64(elapsed)/float64(days) {
		p.Status = GoalAhead
	}
	if value > 0 {
		// The day whose cumulative value at this rate first reaches the target.
		day := int(math.Ceil(target*float64(elapsed)/value)) - 1
		if day < days {
			finish := first.AddDate(0, 0, day)
			p.Finish = &finish
		}
	}
	return p
}

func daysBetween(a, b time.Time) int {
	return int(math.Round(b.Sub(a).Hours() / 24))
}
