package analytics

import (
	"math"
	"testing"
	"time"
)

// powerSamples records each power value for step seconds, then one closing sample.
func powerSamples(step float64, watts ...float64) []Sample {
	out := make([]Sample, 0, len(watts)+1)
	for i, w := range watts {
		out = append(out, Sample{Time: at(float64(i) * step), Power: new(w)})
	}
	return append(out, Sample{Time: at(float64(len(watts)) * step), Power: new(0.0)})
}

func repeat(v float64, n int) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = v
	}
	return out
}

func TestNormalizedPower(t *testing.T) {
	t.Parallel()
	// 30 s at 100 W then 30 s at 300 W: the 31 full windows average
	// 100 + 200·m/30 W for m = 0…30, whose fourth-power mean's root is 223.0695 W.
	steps := append(repeat(100, 30), repeat(300, 30)...)
	// The same power recorded every 5 s (smart recording) holds until the next sample.
	smart := powerSamples(5, append(repeat(100, 6), repeat(300, 6)...)...)
	// Two stretches of 29 s split by a pause never fill a window.
	paused := append(powerSamples(1, repeat(400, 29)...), powerSamples(1, repeat(400, 29)...)...)
	for i := 29 + 1; i < len(paused); i++ {
		paused[i].Time = paused[i].Time.Add(time.Minute)
	}
	noPower := powerSamples(1, repeat(200, 60)...)
	noPower[10].Power = nil
	for _, tc := range []struct {
		name string
		segs [][]Sample
		want float64
		ok   bool
	}{
		{"steps", [][]Sample{powerSamples(1, steps...)}, 223.0694887793096, true},
		{"smart recording", [][]Sample{smart}, 223.0694887793096, true},
		{"constant", [][]Sample{powerSamples(1, repeat(250, 120)...)}, 250, true},
		{"pause restarts the window", [][]Sample{paused}, 0, false},
		{"segments restart the window", [][]Sample{powerSamples(1, repeat(400, 29)...), powerSamples(1, repeat(400, 29)...)}, 0, false},
		{"a sample without power restarts it", [][]Sample{noPower}, 200, true},
		{"empty", [][]Sample{nil, powerSamples(1)}, 0, false},
	} {
		got, ok := NormalizedPower(tc.segs)
		if ok != tc.ok || math.Abs(got-tc.want) > 1e-9 {
			t.Errorf("%s: %v %v, want %v %v", tc.name, got, ok, tc.want, tc.ok)
		}
	}
}

func TestLoadMethods(t *testing.T) {
	t.Parallel()
	ftp, rFTP := new(250.0), new(378.0)
	hour200 := [][]Sample{powerSamples(1, repeat(200, 3600)...)}
	for _, tc := range []struct {
		name   string
		in     LoadInput
		want   float64
		method LoadMethod
	}{
		// 1 h × (200/250)² × 100.
		{"cycling power", LoadInput{Sport: ZoneCycling, DurationS: 3600, Segments: hour200, AvgHR: new(150.0), Own: Thresholds{FTP: ftp, LTHR: new(170.0)}}, 64, LoadPowerTSS},
		// Running power against the running FTP: 1 h × (200/378)² × 100.
		{"running power", LoadInput{Sport: ZoneRunning, DurationS: 3600, Segments: hour200, AvgSpeed: new(3.0), Own: Thresholds{FTP: rFTP, ThresholdSpeed: new(4.0)}}, 100 * 200.0 * 200 / (378 * 378), LoadPowerTSS},
		// A running FTP without power samples falls to rTSS: 50 min × (3/4)² × 100.
		{"pace", LoadInput{Sport: ZoneRunning, DurationS: 3000, AvgSpeed: new(3.0), Own: Thresholds{FTP: rFTP, ThresholdSpeed: new(4.0)}}, 46.875, LoadPaceRTSS},
		// 30 min × (1/1.25)³ × 100.
		{"swim", LoadInput{Sport: ZoneSwimming, DurationS: 1800, AvgSpeed: new(1.0), AvgHR: new(140.0), Own: Thresholds{CSS: new(1.25), LTHR: new(160.0)}}, 25.6, LoadSwimTSS},
		// 1 h × ((150 − 50)/(170 − 50))² × 100, resting HR from the general version.
		{"heart rate", LoadInput{Sport: ZoneCycling, DurationS: 3600, AvgHR: new(150.0), Own: Thresholds{LTHR: new(170.0)}, General: Thresholds{RestingHR: new(50.0)}}, 100 * 100.0 * 100 / (120 * 120), LoadHRTSS},
		// The general LTHR, no resting HR: 1 h × (150/170)² × 100.
		{"heart rate, general LTHR", LoadInput{Sport: ZoneGeneral, DurationS: 3600, AvgHR: new(150.0), General: Thresholds{LTHR: new(170.0)}}, 100 * 150.0 * 150 / (170 * 170), LoadHRTSS},
		// Below resting HR scores zero, not a negative intensity.
		{"heart rate below rest", LoadInput{Sport: ZoneGeneral, DurationS: 3600, AvgHR: new(40.0), Own: Thresholds{LTHR: new(170.0), RestingHR: new(50.0)}}, 0, LoadHRTSS},
		// A hike's FTP is not a cycling or running one; 90 min at RPE 7: 7 × 1.5 × 10.
		{"session RPE", LoadInput{Sport: ZoneGeneral, DurationS: 5400, Segments: hour200, RPE: new(7.0), Own: Thresholds{FTP: ftp}}, 105, LoadSessionRPE},
		// An LTHR at or below resting HR is unusable.
		{"bad thresholds", LoadInput{Sport: ZoneGeneral, DurationS: 3600, AvgHR: new(150.0), RPE: new(5.0), Own: Thresholds{LTHR: new(50.0), RestingHR: new(60.0)}}, 50, LoadSessionRPE},
		{"nothing", LoadInput{Sport: ZoneCycling, DurationS: 3600, Own: Thresholds{FTP: ftp}}, 0, ""},
		{"no duration", LoadInput{Sport: ZoneGeneral, RPE: new(5.0)}, 0, ""},
	} {
		got, method, ok := Load(tc.in)
		if ok != (tc.method != "") || method != tc.method || math.Abs(got-tc.want) > 1e-9 {
			t.Errorf("%s: %v %q %v, want %v %q", tc.name, got, method, ok, tc.want, tc.method)
		}
	}
}

func TestFitness(t *testing.T) {
	t.Parallel()
	day := func(d int) time.Time { return time.Date(2026, 10, d, 0, 0, 0, 0, time.UTC) }
	// 100 on the 1st (as two activities), nothing after:
	//   1st: CTL 100/42, ATL 100/7, TSB 0
	//   2nd: CTL ×41/42, ATL ×6/7, TSB = 100/42 − 100/7 from the day before
	loads := []DayLoad{{day(1), 60}, {day(1), 40}}
	want := []FitnessDay{
		{day(1), 100, 2.380952380952381, 14.285714285714286, 0},
		{day(2), 0, 2.324263038548753, 12.244897959183675, -11.904761904761905},
		{day(3), 0, 2.26892344239283, 10.495626822157435, -9.920634920634921},
	}
	check := func(name string, got, want []FitnessDay) {
		t.Helper()
		if len(got) != len(want) {
			t.Fatalf("%s: %d days, want %d", name, len(got), len(want))
		}
		for i, w := range want {
			g := got[i]
			if !g.Day.Equal(w.Day) || math.Abs(g.Load-w.Load) > 1e-9 || math.Abs(g.CTL-w.CTL) > 1e-9 ||
				math.Abs(g.ATL-w.ATL) > 1e-9 || math.Abs(g.TSB-w.TSB) > 1e-9 {
				t.Errorf("%s day %d: %+v, want %+v", name, i, g, w)
			}
		}
	}
	check("from the first load", Fitness(loads, day(1), day(3)), want)
	// Loads before from still build fitness and fatigue.
	check("warmed up", Fitness(loads, day(2), day(3)), want[1:])
	// Days before the first load stay at zero.
	got := Fitness(loads[1:], day(1).AddDate(0, 0, -1), day(1))
	if got[0].CTL != 0 || got[0].ATL != 0 || math.Abs(got[1].CTL-40.0/42) > 1e-9 {
		t.Errorf("before the first load: %+v", got)
	}
	if got := Fitness(nil, day(3), day(2)); got != nil {
		t.Errorf("empty range: %+v", got)
	}
}
