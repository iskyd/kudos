package analytics

import (
	"math"
	"time"

	"github.com/iskyd/kudos/internal/importer"
)

// maxPlannedSteps bounds a workout once its repeats are unrolled, so a malformed
// repeat count cannot make it huge.
const maxPlannedSteps = 1000

// LapActual is what a lap recorded, to compare with its workout step.
type LapActual struct {
	StepIndex                                                *int
	TimerS, DistanceM, AvgSpeed, AvgHR, AvgPower, AvgCadence *float64
}

// StepResult says how a run of a step went against its target.
type StepResult string

// Results: below, within or above the target range, or a planned step nobody ran.
const (
	StepBelow   StepResult = "below"
	StepWithin  StepResult = "within"
	StepAbove   StepResult = "above"
	StepSkipped StepResult = "skipped"
)

// StepExecution is one run of a workout step: the consecutive laps that ran it,
// taken together, or a step the workout planned but nobody ran (Laps 0).
type StepExecution struct {
	StepIndex int
	Laps      int
	// Totals and time-weighted averages over the laps; nil when not recorded.
	TimerS, DistanceM, AvgSpeed, AvgHR, AvgPower, AvgCadence *float64
	// Actual is the value compared with the step's target, in its unit.
	Actual *float64
	// Result is empty when the step has no target that can be compared
	// (none, a zone, a percentage, cadence) or no recorded value for it.
	Result StepResult
}

// Execute groups laps by workout step and compares each run with its target.
// A step can span several laps (auto-laps within a long warm-up); laps without
// a step (after the workout ended) are left out. Runs are matched in order to the
// workout with its repeats unrolled, so steps the athlete skipped show as
// skipped; a run that matches no remaining step is kept without marking skips.
// shortcut: consecutive laps of one step always form one run, so a step repeated
// back to back is shown as a single run; split by the step's duration if one appears.
func Execute(steps []importer.WorkoutStep, laps []LapActual) []StepExecution {
	byIndex := make(map[int]importer.WorkoutStep, len(steps))
	for _, s := range steps {
		byIndex[s.Index] = s
	}
	var runs []StepExecution
	var acc *lapTotals
	flush := func() {
		if acc != nil {
			runs = append(runs, acc.execution(byIndex[acc.step]))
		}
		acc = nil
	}
	for _, l := range laps {
		if l.StepIndex == nil {
			flush()
			continue
		}
		if acc == nil || acc.step != *l.StepIndex {
			flush()
			acc = newLapTotals(*l.StepIndex)
		}
		acc.add(l)
	}
	flush()

	plan := unroll(steps)
	out := make([]StepExecution, 0, len(runs))
	next := 0
	for _, r := range runs {
		j := next
		for j < len(plan) && plan[j] != r.StepIndex {
			j++
		}
		if j < len(plan) {
			for ; next < j; next++ {
				out = append(out, StepExecution{StepIndex: plan[next], Result: StepSkipped})
			}
			next = j + 1
		}
		out = append(out, r)
	}
	for ; next < len(plan); next++ {
		out = append(out, StepExecution{StepIndex: plan[next], Result: StepSkipped})
	}
	return out
}

// unroll lists the indexes of the steps to run, in order, with repeats unrolled:
// a repeat step runs the steps from RepeatFrom up to it RepeatCount times in all.
func unroll(steps []importer.WorkoutStep) []int {
	var out []int
	start := map[int]int{} // step index → where it was first written in out
	for _, s := range steps {
		if s.RepeatFrom == nil {
			start[s.Index] = len(out)
			out = append(out, s.Index)
			continue
		}
		from, ok := start[*s.RepeatFrom]
		if !ok || s.RepeatCount == nil {
			continue
		}
		block := append([]int(nil), out[from:]...)
		for n := 1; n < *s.RepeatCount && len(out)+len(block) <= maxPlannedSteps; n++ {
			out = append(out, block...)
		}
	}
	return out
}

// lapTotals accumulates consecutive laps of one step.
type lapTotals struct {
	step, laps                int
	timer, distance           float64
	hasTimer, hasDistance     bool
	speed, hr, power, cadence *channel
}

func newLapTotals(step int) *lapTotals {
	ch := func() *channel { return &channel{max: math.Inf(-1)} }
	return &lapTotals{step: step, speed: ch(), hr: ch(), power: ch(), cadence: ch()}
}

// add weighs each lap's averages by its timer time (invariant 10).
func (t *lapTotals) add(l LapActual) {
	t.laps++
	w := 0.0
	if l.TimerS != nil {
		w = *l.TimerS
		t.timer += w
		t.hasTimer = true
	}
	if l.DistanceM != nil {
		t.distance += *l.DistanceM
		t.hasDistance = true
	}
	t.speed.add(l.AvgSpeed, w)
	t.hr.add(l.AvgHR, w)
	t.power.add(l.AvgPower, w)
	t.cadence.add(l.AvgCadence, w)
}

func (t *lapTotals) execution(step importer.WorkoutStep) StepExecution {
	e := StepExecution{
		StepIndex: t.step, Laps: t.laps,
		AvgSpeed: t.speed.avg(), AvgHR: t.hr.avg(), AvgPower: t.power.avg(), AvgCadence: t.cadence.avg(),
	}
	if t.hasTimer {
		e.TimerS = new(t.timer)
	}
	if t.hasDistance {
		e.DistanceM = new(t.distance)
	}
	if t.hasTimer && t.hasDistance && t.timer > 0 {
		e.AvgSpeed = new(t.distance / t.timer)
	}
	switch step.TargetType {
	case "speed":
		e.Actual = e.AvgSpeed
	case "heart_rate":
		e.Actual = e.AvgHR
	case "power":
		e.Actual = e.AvgPower
	}
	if e.Actual != nil && step.TargetLow != nil && step.TargetHigh != nil {
		lo, hi := math.Min(*step.TargetLow, *step.TargetHigh), math.Max(*step.TargetLow, *step.TargetHigh)
		switch {
		case *e.Actual < lo:
			e.Result = StepBelow
		case *e.Actual > hi:
			e.Result = StepAbove
		default:
			e.Result = StepWithin
		}
	}
	return e
}

// PowerTolerance is how far from the target power still counts as on target.
const PowerTolerance = 10.0

// TargetSample is a sample of a workout that sets a target power each second (Zwift).
type TargetSample struct {
	Time          time.Time
	Power, Target *float64 // W
}

// PowerInterval is a stretch of one target power.
type PowerInterval struct {
	Start     time.Time
	DurationS float64
	TargetW   float64
	AvgPowerW *float64
	// WithinS is the time within PowerTolerance of the target.
	WithinS float64
}

// PowerCompliance splits a workout into intervals where the target power changes
// and measures how long the recorded power stayed within PowerTolerance of it.
// Samples weigh the time to the next one (none across a pause over MaxGap); a
// target of 0 or none means no workout is running. withinS and totalS count only
// time with recorded power, so a sensor dropout is not held against the rider.
func PowerCompliance(samples []TargetSample) (intervals []PowerInterval, withinS, totalS float64) {
	var cur *PowerInterval
	var avg *channel
	flush := func() {
		if cur != nil && cur.DurationS > 0 { // a lone last sample has no time of its own
			cur.AvgPowerW = avg.avg()
			intervals = append(intervals, *cur)
		}
		cur = nil
	}
	for i, s := range samples {
		if s.Target == nil || *s.Target <= 0 {
			flush()
			continue
		}
		if cur == nil || cur.TargetW != *s.Target {
			flush()
			cur, avg = &PowerInterval{Start: s.Time, TargetW: *s.Target}, &channel{max: math.Inf(-1)}
		}
		w := 0.0
		if i+1 < len(samples) {
			if gap := samples[i+1].Time.Sub(s.Time); gap <= MaxGap {
				w = gap.Seconds()
			}
		}
		cur.DurationS += w
		avg.add(s.Power, w)
		if s.Power == nil {
			continue
		}
		totalS += w
		if math.Abs(*s.Power-*s.Target) <= PowerTolerance {
			cur.WithinS += w
			withinS += w
		}
	}
	flush()
	return intervals, withinS, totalS
}
