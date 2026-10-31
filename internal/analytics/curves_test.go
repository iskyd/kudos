package analytics

import (
	"math"
	"slices"
	"testing"
)

func curveMap(ps []CurvePoint, kind CurveKind) map[int]float64 {
	m := map[int]float64{}
	for _, p := range ps {
		if p.Kind == kind {
			m[p.DurationS] = p.Value
		}
	}
	return m
}

func sameCurve(t *testing.T, name string, got, want map[int]float64) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("%s: %v, want %v", name, got, want)
		return
	}
	for d, w := range want {
		if v, ok := got[d]; !ok || math.Abs(v-w) > 1e-9 {
			t.Errorf("%s: %d s = %v, want %v", name, d, v, w)
		}
	}
}

func TestPaceCurve(t *testing.T) {
	t.Parallel()
	// 1500 s at 4 m/s then 1500 s at 5 m/s: windows up to 1500 s run at 5 m/s,
	// longer ones add the end of the slow half.
	run := [][]Sample{distanceSamples(1, append(repeat(4, 1500), repeat(5, 1500)...)...)}
	got := Curves(EffortInput{Sport: "running", Segments: run})
	sameCurve(t, "run", curveMap(got, PaceCurve), map[int]float64{
		60: 5, 120: 5, 300: 5, 600: 5, 1200: 5,
		1800: (1500*5 + 300*4) / 1800.0, 2700: (1500*5 + 1200*4) / 2700.0,
	})
	if len(curveMap(got, PowerCurve)) != 0 {
		t.Errorf("a run has a power curve: %v", got)
	}

	for _, tt := range []struct {
		name string
		segs [][]Sample
		want map[int]float64
	}{
		// Samples every 7 s: windows are interpolated between them.
		{"interpolated", [][]Sample{distanceSamples(7, repeat(2.5, 20)...)}, map[int]float64{60: 2.5, 120: 2.5}},
		// 100 s at 5 m/s, a 60 s stop, 100 s at 5 m/s: the stop counts.
		{"pause counts", [][]Sample{distanceSamples(1, slices.Concat(repeat(5, 100), repeat(0, 60), repeat(5, 100))...)}, map[int]float64{60: 5, 120: 500 / 120.0}},
		// The best minute starts at a sample and ends between two.
		{"ends between samples", [][]Sample{{
			{Time: at(0), Distance: new(0.0)}, {Time: at(10), Distance: new(100.0)}, {Time: at(100), Distance: new(190.0)},
		}}, map[int]float64{60: 2.5}},
		// It ends at a sample and starts between two; samples without distance are skipped.
		{"starts between samples", [][]Sample{{
			{Time: at(0), Distance: new(0.0)}, {Time: at(50), HR: new(140.0)}, {Time: at(90), Distance: new(90.0)}, {Time: at(100), Distance: new(190.0)},
		}}, map[int]float64{60: 2.5}},
		// Standing still for minutes is no pace.
		{"standing", [][]Sample{distanceSamples(1, repeat(0, 200)...)}, map[int]float64{}},
		// 50 s in each part of a merged activity is no minute.
		{"split across segments", [][]Sample{distanceSamples(1, repeat(3, 50)...), distanceSamples(1, repeat(3, 50)...)}, map[int]float64{}},
	} {
		sameCurve(t, tt.name, curveMap(Curves(EffortInput{Sport: "trail_running", Segments: tt.segs}), PaceCurve), tt.want)
	}

	// A virtual run has no pace curve (invariant 18); other sports have none.
	for _, in := range []EffortInput{{Sport: "running", Virtual: true, Segments: run}, {Sport: "hiking", Segments: run}} {
		if got := Curves(in); len(got) != 0 {
			t.Errorf("%s: %v", in.Sport, got)
		}
	}
}

func TestPowerCurve(t *testing.T) {
	t.Parallel()
	// 60 s at 100 W, 10 s at 300 W, 60 s at 100 W, recorded every 2 s; virtual rides count.
	ride := [][]Sample{powerSamples(2, slices.Concat(repeat(100, 30), repeat(300, 5), repeat(100, 30))...)}
	for _, virtual := range []bool{false, true} {
		got := Curves(EffortInput{Sport: "cycling", Virtual: virtual, Segments: ride})
		sameCurve(t, "ride", curveMap(got, PowerCurve), map[int]float64{
			1: 300, 5: 300, 10: 300, 15: (300*10 + 100*5) / 15.0, 30: (300*10 + 100*20) / 30.0,
			60: (300*10 + 100*50) / 60.0, 120: (300*10 + 100*110) / 120.0,
		})
		if len(curveMap(got, PaceCurve)) != 0 {
			t.Errorf("a ride has a pace curve: %v", got)
		}
	}
	if got := Curves(EffortInput{Sport: "cycling", Segments: [][]Sample{powerSamples(1, repeat(0, 100)...)}}); len(got) != 0 {
		t.Errorf("a ride at 0 W: %v", got)
	}
}

func TestCurveDurations(t *testing.T) {
	t.Parallel()
	for _, kind := range []CurveKind{PowerCurve, PaceCurve} {
		ds := CurveDurations(kind)
		if !slices.IsSorted(ds) || !slices.Contains(ds, FTPDurationS) || !slices.Contains(ds, ThresholdPaceDurationS) {
			t.Errorf("%s: %v", kind, ds)
		}
	}
	if CurveDurations(PowerCurve)[0] != 1 || CurveDurations(PaceCurve)[0] != 60 {
		t.Error("shortest durations")
	}
}

func TestEnvelope(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		values []float64
		want   []int
	}{
		{nil, []int{}},
		{[]float64{5, 4, 3}, []int{0, 1, 2}},
		// A longer duration's better value carries back; ties keep the shorter.
		{[]float64{5, 3, 4, 1}, []int{0, 2, 2, 3}},
		{[]float64{2, 2, 3}, []int{2, 2, 2}},
	} {
		got := Envelope(tt.values)
		if !slices.Equal(got, tt.want) {
			t.Errorf("%v: %v, want %v", tt.values, got, tt.want)
		}
		for i := 1; i < len(got); i++ {
			if tt.values[got[i]] > tt.values[got[i-1]] {
				t.Errorf("%v: not monotonic at %d", tt.values, i)
			}
		}
	}
}

func TestEstimates(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct{ in, want float64 }{{263.2, 250}, {300, 285}, {263.1, 249}} {
		if got := EstimateFTP(tt.in); got != tt.want {
			t.Errorf("FTP from %v: %v, want %v", tt.in, got, tt.want)
		}
	}
	// Pace rounds up to a whole second per kilometre: 4:10/km is 4 m/s.
	for _, tt := range []struct{ in, want float64 }{{4, 4}, {4.01, 4}, {3.99, 1000 / 251.0}} {
		if got := EstimateThresholdSpeed(tt.in); math.Abs(got-tt.want) > 1e-12 {
			t.Errorf("threshold speed from %v: %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestEstimatesFromACurve(t *testing.T) {
	t.Parallel()
	// The 30-minute power beats the 20-minute one, as in the Zwift sample: FTP
	// reads the envelope. Points below 20 and 30 minutes never count.
	curve := []CurvePoint{
		{PowerCurve, 300, 400}, {PowerCurve, 1200, 133.5}, {PowerCurve, 1800, 135.2},
		{PaceCurve, 1200, 5}, {PaceCurve, 1800, 3.99}, {PaceCurve, 3600, 3.5},
	}
	ftp, speed := Estimates(curve)
	if ftp == nil || *ftp != 128 || speed == nil || math.Abs(*speed-1000/251.0) > 1e-12 {
		t.Errorf("estimates: %v %v", ftp, speed)
	}
	// Too short for either, or a power below a watt's estimate.
	for _, c := range [][]CurvePoint{nil, {{PowerCurve, 600, 300}, {PaceCurve, 1200, 4}}, {{PowerCurve, 1200, 1}}} {
		if ftp, speed := Estimates(c); ftp != nil || speed != nil {
			t.Errorf("%v: %v %v", c, ftp, speed)
		}
	}
}
