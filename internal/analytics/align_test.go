package analytics

import (
	"fmt"
	"testing"
)

func none(t *testing.T, name string, v Aligned) {
	t.Helper()
	if v != (Aligned{}) {
		t.Errorf("%s has values: %+v", name, v)
	}
}

func TestAlignByTimeDifferentLengthsAndRates(t *testing.T) {
	t.Parallel()
	// a: 1 s sampling for 10 s; b: 2 s smart recording for 20 s. HR rises 1 bpm/s in both.
	var a, b []Sample
	for s := 0.0; s <= 10; s++ {
		a = append(a, Sample{Time: at(s), HR: new(100 + s)})
	}
	for s := 0.0; s <= 20; s += 2 {
		b = append(b, Sample{Time: at(s), HR: new(120 + s), Power: new(200.0)})
	}
	x, out := Align([][]Sample{a}, [][]Sample{b}, AxisTime, 41)
	if len(x) != 41 || x[0] != 0 || x[40] != 20 || x[1] != 0.5 {
		t.Fatalf("axis: %v", x)
	}
	// 7.5 s: between a's samples at 7 and 8, and b's at 6 and 8.
	near(t, "a HR at 7.5 s", out[0][15].HR, 107.5)
	near(t, "a elapsed at 7.5 s", out[0][15].ElapsedS, 7.5)
	near(t, "b HR at 7.5 s", out[1][15].HR, 127.5)
	near(t, "b power at 7.5 s", out[1][15].Power, 200)
	if out[0][15].Power != nil || out[0][15].DistanceM != nil {
		t.Errorf("a's unrecorded channels: %+v", out[0][15])
	}
	near(t, "a HR at its end", out[0][20].HR, 110)
	none(t, "a past its end", out[0][21])
	near(t, "b HR at 20 s", out[1][40].HR, 140)
}

func TestAlignNeverBridgesPausesOrSegments(t *testing.T) {
	t.Parallel()
	// A 30 s pause between 2 s and 32 s; a second segment starts 4 s after the first ends.
	seg1 := []Sample{
		{Time: at(0), HR: new(100.0)}, {Time: at(2), HR: new(102.0)},
		{Time: at(32), HR: new(132.0)}, {Time: at(34), HR: new(134.0)},
	}
	seg2 := []Sample{{Time: at(38), HR: new(90.0)}, {Time: at(40), HR: new(92.0)}}
	_, out := Align([][]Sample{seg1, seg2}, nil, AxisTime, 41)
	a := out[0]
	near(t, "HR at 1 s", a[1].HR, 101)
	none(t, "in the pause", a[17])
	near(t, "HR at 33 s", a[33].HR, 133)
	none(t, "between segments", a[36])
	near(t, "HR at 39 s", a[39].HR, 91)
	for i, v := range out[1] {
		none(t, fmt.Sprintf("b without samples at %d", i), v)
	}
}

func TestAlignByDistance(t *testing.T) {
	t.Parallel()
	// Two merged segments: the second restarts its distance and continues the
	// axis from the first's 8 m; a sample without distance is skipped and a GPS
	// step backwards does not move the axis back.
	seg1 := []Sample{
		{Time: at(0), Distance: new(100.0), Speed: new(2.0)},
		{Time: at(1), Speed: new(9.0)},
		{Time: at(2), Distance: new(104.0), Speed: new(4.0)},
		{Time: at(3), Distance: new(103.0), Speed: new(4.0)},
		{Time: at(4), Distance: new(108.0), Speed: new(6.0)},
	}
	seg2 := []Sample{
		{Time: at(600), Distance: new(0.0), Altitude: new(10.0)},
		{Time: at(602), Distance: new(4.0), Altitude: new(20.0)},
	}
	// b runs 12 m at 1 m/s.
	var b []Sample
	for s := 0.0; s <= 12; s++ {
		b = append(b, Sample{Time: at(s), Distance: new(50 + s)})
	}
	x, out := Align([][]Sample{seg1, seg2}, [][]Sample{b}, AxisDistance, 13)
	if x[12] != 12 || x[1] != 1 {
		t.Fatalf("axis: %v", x)
	}
	a := out[0]
	// 2 m is halfway between 0 m (0 s) and 4 m (2 s).
	near(t, "speed at 2 m", a[2].Speed, 3)
	near(t, "elapsed at 2 m", a[2].ElapsedS, 1)
	near(t, "distance at 2 m", a[2].DistanceM, 2)
	// 6 m: between the backwards step, held at 4 m (3 s), and 8 m (4 s).
	near(t, "elapsed at 6 m", a[6].ElapsedS, 3.5)
	near(t, "speed at 6 m", a[6].Speed, 5)
	near(t, "elapsed at 8 m", a[8].ElapsedS, 4)
	if a[8].Altitude != nil {
		t.Errorf("8 m takes the first segment's end: %+v", a[8])
	}
	near(t, "altitude at 9 m", a[9].Altitude, 12.5)
	near(t, "elapsed at 10 m", a[10].ElapsedS, 601)
	near(t, "altitude at 12 m", a[12].Altitude, 20)
	near(t, "b elapsed at 7 m", out[1][7].ElapsedS, 7)
	near(t, "b distance at 12 m", out[1][12].DistanceM, 12)
}

func TestAlignWithoutDistanceOrSamples(t *testing.T) {
	t.Parallel()
	a := []Sample{{Time: at(0), HR: new(100.0)}, {Time: at(1), HR: new(101.0)}}
	x, out := Align([][]Sample{a}, nil, AxisDistance, 3)
	if x[0] != 0 || x[2] != 0 {
		t.Errorf("axis without distance: %v", x)
	}
	for _, k := range out {
		for _, v := range k {
			none(t, "no distance", v)
		}
	}
	// One sample: the axis is a point and the sample is its value.
	_, out = Align([][]Sample{{a[0]}}, nil, AxisTime, 2)
	near(t, "single sample", out[0][1].HR, 100)
}
