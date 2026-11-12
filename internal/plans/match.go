package plans

import (
	"cmp"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/iskyd/kudos/internal/analytics"
)

// Totals sums a workout's time and distance steps with repeats written out;
// open steps add nothing.
func Totals(steps json.RawMessage) (durationS, distanceM float64, err error) {
	var ss []step
	if err := json.Unmarshal(steps, &ss); err != nil {
		return 0, 0, fmt.Errorf("reading steps: %w", err)
	}
	durationS, distanceM = totals(ss)
	return durationS, distanceM, nil
}

func totals(steps []step) (durationS, distanceM float64) {
	for _, s := range steps {
		switch {
		case s.Kind == "repeat" && s.Count != nil:
			d, m := totals(s.Steps)
			durationS += d * float64(*s.Count)
			distanceM += m * float64(*s.Count)
		case s.Duration == nil:
		case s.DurationType == "time":
			durationS += *s.Duration
		case s.DurationType == "distance":
			distanceM += *s.Duration
		}
	}
	return durationS, distanceM
}

// Planned is a planned workout as matching sees it.
type Planned struct {
	ID        int64
	Day       string // local, YYYY-MM-DD
	Sport     string
	Title     string
	DurationS float64 // of its time steps; 0 when none
	DistanceM float64 // of its distance steps; 0 when none
}

// Done is an activity as matching sees it.
type Done struct {
	ID          int64
	Day         string // local, YYYY-MM-DD
	Sport       string
	WorkoutName string // of the structured workout the device ran; empty without one
	DurationS   float64
	DistanceM   float64
}

// Link is the user's own say on a planned workout: the activity that completed
// it, or none when they unlinked it.
type Link struct {
	PlannedID  int64
	ActivityID *int64
}

// Completion is the activity that completed a planned workout.
type Completion struct {
	ActivityID int64
	Manual     bool // linked by the user, not matched
}

// Match pairs planned workouts with the activities that completed them. The
// user's links win: a linked workout takes its activity, an unlinked one none,
// and linked activities are taken. With auto on, the rest is matched within
// each day: first an activity whose recorded workout carries the planned
// title, then, among activities of the same sport (running and trail running
// count as one, as do the swims), the closest in duration, or in distance when
// the plan has no time, pairing the closest first.
func Match(planned []Planned, done []Done, links []Link, auto bool) map[int64]Completion {
	out := map[int64]Completion{}
	linked := map[int64]bool{}
	taken := map[int64]bool{}
	for _, l := range links {
		linked[l.PlannedID] = true
		if l.ActivityID != nil {
			taken[*l.ActivityID] = true
			if slices.ContainsFunc(planned, func(p Planned) bool { return p.ID == l.PlannedID }) {
				out[l.PlannedID] = Completion{ActivityID: *l.ActivityID, Manual: true}
			}
		}
	}
	if !auto {
		return out
	}
	ps := slices.Clone(planned)
	slices.SortFunc(ps, func(a, b Planned) int { return cmp.Or(strings.Compare(a.Day, b.Day), cmp.Compare(a.ID, b.ID)) })
	ds := slices.Clone(done)
	slices.SortFunc(ds, func(a, b Done) int { return cmp.Compare(a.ID, b.ID) })
	open := func(p Planned) bool {
		_, matched := out[p.ID]
		return !linked[p.ID] && !matched
	}

	for _, p := range ps {
		for _, d := range ds {
			if open(p) && !taken[d.ID] && d.Day == p.Day && d.WorkoutName != "" && sameName(d.WorkoutName, p.Title) {
				out[p.ID], taken[d.ID] = Completion{ActivityID: d.ID}, true
			}
		}
	}

	type pair struct {
		p    Planned
		d    Done
		cost float64
	}
	var pairs []pair
	for _, p := range ps {
		for _, d := range ds {
			if open(p) && !taken[d.ID] && d.Day == p.Day && sameSport(p.Sport, d.Sport) {
				pairs = append(pairs, pair{p, d, cost(p, d)})
			}
		}
	}
	slices.SortStableFunc(pairs, func(a, b pair) int { return cmp.Compare(a.cost, b.cost) })
	for _, pr := range pairs {
		if open(pr.p) && !taken[pr.d.ID] {
			out[pr.p.ID], taken[pr.d.ID] = Completion{ActivityID: pr.d.ID}, true
		}
	}
	return out
}

func sameName(a, b string) bool {
	return strings.EqualFold(strings.Join(strings.Fields(a), " "), strings.Join(strings.Fields(b), " "))
}

func sameSport(a, b string) bool {
	za := analytics.ZoneSportOf(a)
	return a == b || za != analytics.ZoneGeneral && za == analytics.ZoneSportOf(b)
}

// cost is how far an activity is from the plan, relative to the plan; 0 for a
// plan with neither time nor distance.
func cost(p Planned, d Done) float64 {
	switch {
	case p.DurationS > 0:
		return math.Abs(d.DurationS-p.DurationS) / p.DurationS
	case p.DistanceM > 0:
		return math.Abs(d.DistanceM-p.DistanceM) / p.DistanceM
	}
	return 0
}
