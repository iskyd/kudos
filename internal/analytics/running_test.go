package analytics

import (
	"math"
	"testing"
)

func TestEfficiencyFactor(t *testing.T) {
	t.Parallel()
	// 3 m/s at 150 bpm (2.5 beats/s) is 1.2 m per beat.
	near(t, "EF", EfficiencyFactor(new(3.0), new(150.0)), 1.2)
	for _, c := range [][2]*float64{{nil, new(150.0)}, {new(3.0), nil}, {new(0.0), new(150.0)}, {new(3.0), new(0.0)}} {
		if got := EfficiencyFactor(c[0], c[1]); got != nil {
			t.Errorf("EF(%v, %v) = %v, want none", deref(c[0]), deref(c[1]), *got)
		}
	}
}

// steady is 1 s samples at speed and heart rate from second from to to.
func steady(from, to int, speed, hr float64) []Sample {
	var out []Sample
	for s := from; s < to; s++ {
		out = append(out, Sample{Time: at(float64(s)), Speed: new(speed), HR: new(hr)})
	}
	return out
}

func join(parts ...[]Sample) []Sample {
	var out []Sample
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

func TestDecoupling(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		segs [][]Sample
		want *float64
	}{
		// Same pace, heart rate 150 then 165: EF falls by 1 − 150/165.
		{"drift", [][]Sample{join(steady(0, 1200, 3, 150), steady(1200, 2401, 3, 165))}, new((1 - 150.0/165) * 100)},
		// Pace falls 10% at the same heart rate.
		{"fade", [][]Sample{join(steady(0, 1200, 4, 150), steady(1200, 2401, 3.6, 150))}, new(10.0)},
		{"coupled", [][]Sample{steady(0, 2401, 3, 150)}, new(0.0)},
		// A 4 s smart-recording gap (1196–1200 s) weighs its 4 s, ending at the middle.
		{"span at the middle", [][]Sample{join(steady(0, 1197, 3, 150), steady(1200, 2401, 3, 165))}, new((1 - 150.0/165) * 100)},
		// A pause and a sample without heart rate weigh nothing; a merged second segment counts.
		{"pause and segments", [][]Sample{
			join(steady(0, 1200, 3, 150), []Sample{{Time: at(1200), Speed: new(9.0)}, {Time: at(1201), Speed: new(3.0), HR: new(150.0)}}, steady(5000, 5001, 3, 150)),
			steady(9000, 10201, 3, 165),
		}, new((1 - 150.0/165) * 100)},
		{"too short", [][]Sample{steady(0, 1200, 3, 150)}, nil},
		{"no heart rate", [][]Sample{{{Time: at(0), Speed: new(3.0)}, {Time: at(1), Speed: new(3.0)}}}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := Decoupling(tt.segs)
			switch {
			case tt.want == nil && got != nil:
				t.Errorf("got %v, want none", *got)
			case tt.want != nil && (got == nil || math.Abs(*got-*tt.want) > 1e-9):
				t.Errorf("got %v, want %v", deref(got), *tt.want)
			}
		})
	}
}

func TestDecouplingSplitsTheMiddleSpan(t *testing.T) {
	t.Parallel()
	// 1199 s at 150 bpm, one 2 s span at 160 across the middle (1200 s of 2400),
	// then 1199 s at 170: each half holds 1 s of 160.
	segs := [][]Sample{join(steady(0, 1199, 3, 150), []Sample{{Time: at(1199), Speed: new(3.0), HR: new(160.0)}}, steady(1201, 2401, 3, 170))}
	first, second := (150.0*1199+160)/1200, (170.0*1199+160)/1200
	want := (1 - first/second) * 100
	if got := Decoupling(segs); got == nil || math.Abs(*got-want) > 1e-9 {
		t.Errorf("got %v, want %v", deref(got), want)
	}
}

// danielsTable is Daniels' published equivalent performances (Daniels' Running
// Formula, VDOT tables), which the formula matches within 0.2%.
func danielsTable() []struct {
	vdot  float64
	races [4]float64 // 5K, 10K, half marathon and marathon in seconds
} {
	hms := func(h, m, s float64) float64 { return h*3600 + m*60 + s }
	return []struct {
		vdot  float64
		races [4]float64
	}{
		{40, [4]float64{hms(0, 24, 8), hms(0, 50, 3), hms(1, 50, 59), hms(3, 49, 45)}},
		{50, [4]float64{hms(0, 19, 57), hms(0, 41, 21), hms(1, 31, 35), hms(3, 10, 49)}},
		{60, [4]float64{hms(0, 17, 3), hms(0, 35, 22), hms(1, 18, 9), hms(2, 43, 25)}},
	}
}

func TestVDOTAgainstDanielsTables(t *testing.T) {
	t.Parallel()
	for _, row := range danielsTable() {
		for i, d := range RaceDistances() {
			if got := VDOT(d, row.races[i]); math.Abs(got-row.vdot) > 0.1 {
				t.Errorf("VDOT(%v m in %v s) = %v, want %v", d, row.races[i], got, row.vdot)
			}
			if got := PredictRace(row.vdot, d); math.Abs(got-row.races[i]) > row.races[i]*0.002 {
				t.Errorf("PredictRace(%v, %v m) = %v s, want %v", row.vdot, d, got, row.races[i])
			}
		}
	}
	// Bisection inverts VDOT.
	if got := VDOT(10000, PredictRace(47.3, 10000)); math.Abs(got-47.3) > 1e-9 {
		t.Errorf("round trip: %v", got)
	}
}

func TestVO2max(t *testing.T) {
	t.Parallel()
	// 19:57 for 5000 m is VDOT 49.95; the 1-minute sprint is too short to
	// count, the hour is slower and power is not pace.
	t5k := 19*60 + 57
	curve := []CurvePoint{
		{PaceCurve, 60, 7},
		{PaceCurve, t5k, 5000 / float64(t5k)},
		{PaceCurve, 3600, 3.5},
		{PowerCurve, 1200, 400},
	}
	if got := VO2max(curve); got == nil || math.Abs(*got-49.95) > 0.01 {
		t.Errorf("VO2max = %v, want 49.95", deref(got))
	}
	if got := VO2max([]CurvePoint{{PaceCurve, 240, 5}, {PowerCurve, 1200, 300}}); got != nil {
		t.Errorf("no long pace: %v", *got)
	}
}
