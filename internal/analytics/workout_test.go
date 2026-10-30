package analytics

import (
	"math"
	"os"
	"slices"
	"testing"

	"github.com/iskyd/kudos/internal/importer"
	"github.com/iskyd/kudos/internal/importer/fit"
)

func decodeCorpus(t *testing.T, name string) importer.Activity {
	t.Helper()
	f, err := os.Open("../../testdata/fit/" + name)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	acts, err := fit.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	return acts[0]
}

func lapActuals(laps []importer.Lap) []LapActual {
	out := make([]LapActual, len(laps))
	for i, l := range laps {
		out[i] = LapActual{
			StepIndex: l.WorkoutStepIndex, TimerS: l.TimerTime, DistanceM: l.Distance, AvgSpeed: l.AvgSpeed,
			AvgHR: l.AvgHR, AvgPower: l.AvgPower, AvgCadence: l.AvgCadence,
		}
	}
	return out
}

// paceS is seconds per km.
func paceS(speed float64) float64 { return 1000 / speed }

func TestExecuteIntervalsSample(t *testing.T) {
	t.Parallel()
	a := decodeCorpus(t, "intervals.fit")
	got := Execute(a.Workout.Steps, lapActuals(a.Laps))

	// Warm-up, 10 × (400 m, recovery) with the last recovery skipped, cool-down.
	if len(got) != 22 {
		t.Fatalf("%d runs, want 22", len(got))
	}
	warm := got[0]
	if warm.StepIndex != 0 || warm.Laps != 2 || math.Abs(*warm.DistanceM-2000) > 1e-6 || warm.Result != "" {
		t.Errorf("warm-up: %+v", warm)
	}
	target := a.Workout.Steps[1]
	// The target is 3:50–4:00/km.
	if lo, hi := paceS(*target.TargetHigh), paceS(*target.TargetLow); math.Round(lo) != 230 || math.Round(hi) != 240 {
		t.Errorf("target %v–%v s/km", lo, hi)
	}
	for rep := range 10 {
		r := got[1+2*rep]
		pace := paceS(*r.Actual)
		// Every rep ran at 3:36–3:45/km: faster than the target.
		if r.StepIndex != 1 || r.Laps != 1 || pace < 215.5 || pace > 225.5 || r.Result != StepAbove {
			t.Errorf("rep %d: %+v at %.1f s/km", rep+1, r, pace)
		}
	}
	for rec := range 9 {
		if r := got[2+2*rec]; r.StepIndex != 2 || r.Laps != 1 || r.Result != "" {
			t.Errorf("recovery %d: %+v", rec+1, r)
		}
	}
	if r := got[20]; r.StepIndex != 2 || r.Laps != 0 || r.Result != StepSkipped || r.TimerS != nil {
		t.Errorf("last recovery: %+v", r)
	}
	// The cool-down spans two laps; the lap after the workout ended is left out.
	if r := got[21]; r.StepIndex != 4 || r.Laps != 2 || math.Abs(*r.DistanceM-2000) > 1e-6 {
		t.Errorf("cool-down: %+v", r)
	}
}

func TestExecuteWeighsLapsAndComparesTargets(t *testing.T) {
	t.Parallel()
	step := func(i int) *int { return &i }
	steps := []importer.WorkoutStep{
		{Index: 0, TargetType: "heart_rate", TargetLow: new(140.0), TargetHigh: new(150.0)},
		{Index: 1, TargetType: "power", TargetLow: new(300.0), TargetHigh: new(250.0)},
		{Index: 2, TargetType: "heart_rate_percent", TargetLow: new(80.0), TargetHigh: new(90.0)},
	}
	got := Execute(steps, []LapActual{
		// 100 s at 130 bpm and 300 s at 150 bpm: 145 bpm, not the 140 of a plain mean.
		{StepIndex: step(0), TimerS: new(100.0), AvgHR: new(130.0), AvgSpeed: new(3.0)},
		{StepIndex: step(0), TimerS: new(300.0), AvgHR: new(150.0), AvgSpeed: new(5.0)},
		{StepIndex: step(1), TimerS: new(60.0), AvgPower: new(320.0)},
		{StepIndex: step(1), AvgPower: new(200.0)},
		{StepIndex: step(2), TimerS: new(60.0), AvgHR: new(170.0)},
		// A step the workout does not have next: kept, nothing marked skipped.
		{StepIndex: step(0), TimerS: new(10.0)},
	})
	if len(got) != 4 {
		t.Fatalf("%d runs: %+v", len(got), got)
	}
	near(t, "weighted HR", got[0].Actual, 145)
	near(t, "speed without distance", got[0].AvgSpeed, 4.5)
	if got[0].Result != StepWithin || got[0].DistanceM != nil {
		t.Errorf("first run: %+v", got[0])
	}
	// A lap without timer time weighs nothing; the range is read either way round.
	near(t, "power", got[1].Actual, 320)
	if got[1].Result != StepAbove || got[1].Laps != 2 {
		t.Errorf("second run: %+v", got[1])
	}
	if got[2].Actual != nil || got[2].Result != "" {
		t.Errorf("percent target compared: %+v", got[2])
	}
	if got[3].Result != "" || got[3].StepIndex != 0 || got[3].Laps != 1 {
		t.Errorf("unplanned run: %+v", got[3])
	}

	got = Execute(steps[:1], []LapActual{{StepIndex: step(0), TimerS: new(1.0), AvgHR: new(100.0)}})
	if got[0].Result != StepBelow {
		t.Errorf("below: %+v", got[0])
	}
	if got := Execute(steps, nil); len(got) != 3 || got[0].Result != StepSkipped {
		t.Errorf("nothing run: %+v", got)
	}
}

func TestUnroll(t *testing.T) {
	t.Parallel()
	n := func(i int) *int { return &i }
	tests := []struct {
		name  string
		steps []importer.WorkoutStep
		want  []int
	}{
		{"plain", []importer.WorkoutStep{{Index: 0}, {Index: 1}}, []int{0, 1}},
		{"repeat", []importer.WorkoutStep{
			{Index: 0}, {Index: 1}, {Index: 2}, {Index: 3, RepeatFrom: n(1), RepeatCount: n(3)}, {Index: 4},
		}, []int{0, 1, 2, 1, 2, 1, 2, 4}},
		{"nested", []importer.WorkoutStep{
			{Index: 0}, {Index: 1}, {Index: 2, RepeatFrom: n(1), RepeatCount: n(2)}, {Index: 3, RepeatFrom: n(0), RepeatCount: n(2)},
		}, []int{0, 1, 1, 0, 1, 1}},
		{"broken repeats", []importer.WorkoutStep{
			{Index: 0}, {Index: 1, RepeatFrom: n(7), RepeatCount: n(2)}, {Index: 2, RepeatFrom: n(0)},
		}, []int{0}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := unroll(tt.steps); !slices.Equal(got, tt.want) {
				t.Errorf("unroll = %v, want %v", got, tt.want)
			}
		})
	}
	huge := unroll([]importer.WorkoutStep{{Index: 0}, {Index: 1}, {Index: 2, RepeatFrom: n(0), RepeatCount: n(1 << 30)}})
	if len(huge) > maxPlannedSteps {
		t.Errorf("unrolled to %d steps", len(huge))
	}
}

func TestPowerComplianceZwiftSample(t *testing.T) {
	t.Parallel()
	a := decodeCorpus(t, "zwift.fit")
	samples := make([]TargetSample, len(a.Samples))
	for i, s := range a.Samples {
		samples[i] = TargetSample{Time: s.Time, Power: s.Power}
		if v, ok := s.Extras["target_power_w"]; ok {
			samples[i].Target = &v
		}
	}
	intervals, within, total := PowerCompliance(samples)
	// About 96% of seconds within 10 W of the target.
	if share := within / total; share < 0.955 || share > 0.965 {
		t.Errorf("%.1f%% of %v s within %v W", 100*share, total, PowerTolerance)
	}
	// The workout ends with a target of 0 at 1:00:01.
	last := intervals[len(intervals)-1]
	if last.TargetW != 75 || last.Start.Sub(a.StartTime).Seconds() != 59*60+16 {
		t.Errorf("last interval: %+v", last)
	}
	// The first block of 4 × (1 min at 135 W, 1 min at 90 W) starts at 7:01.
	var at135 []PowerInterval
	for _, iv := range intervals {
		if iv.TargetW == 135 {
			at135 = append(at135, iv)
		}
	}
	if len(at135) != 4 || at135[0].Start.Sub(a.StartTime).Seconds() != 7*60+1 || math.Abs(at135[0].DurationS-60) > 1 {
		t.Errorf("135 W intervals: %+v", at135)
	}
}

func TestPowerCompliance(t *testing.T) {
	t.Parallel()
	w := func(v float64) *float64 { return &v }
	intervals, within, total := PowerCompliance([]TargetSample{
		{Time: at(0), Power: w(195), Target: w(200)},
		{Time: at(1), Power: w(215), Target: w(200)},
		{Time: at(3), Target: w(200)}, // dropout: no power
		{Time: at(4), Power: w(300), Target: w(300)},
		// A 60 s pause, then the workout ends.
		{Time: at(64), Power: w(300), Target: w(300)},
		{Time: at(65), Power: w(100), Target: w(0)},
		{Time: at(66), Power: w(100)},
		// A last sample has no time of its own: no interval.
		{Time: at(67), Power: w(250), Target: w(250)},
	})
	if len(intervals) != 2 {
		t.Fatalf("intervals: %+v", intervals)
	}
	// 1 s within, 2 s outside, 1 s without power.
	if iv := intervals[0]; iv.DurationS != 4 || iv.WithinS != 1 || iv.TargetW != 200 {
		t.Errorf("first: %+v", iv)
	}
	near(t, "first avg power", intervals[0].AvgPowerW, (195+2*215)/3.0)
	if iv := intervals[1]; iv.DurationS != 1 || iv.WithinS != 1 {
		t.Errorf("second: %+v", iv)
	}
	if within != 2 || total != 4 {
		t.Errorf("within %v of %v s", within, total)
	}
}
