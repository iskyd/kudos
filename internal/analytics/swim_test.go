package analytics

import (
	"math"
	"testing"

	"github.com/iskyd/kudos/internal/importer"
)

func TestCSS(t *testing.T) {
	t.Parallel()
	// 400 m in 6:00 and 200 m in 2:50: 200 m in 190 s, 1:35/100 m.
	near(t, "CSS", CSS(170, 360), 200.0/190)
	for _, c := range [][2]float64{{0, 360}, {170, 170}, {200, 150}} {
		if got := CSS(c[0], c[1]); got != nil {
			t.Errorf("CSS(%v, %v) = %v, want none", c[0], c[1], *got)
		}
	}
}

// swum is an active length of seconds with strokes; rest is an idle one.
func swum(seconds, strokes float64) importer.Length {
	return importer.Length{Active: true, TimerTime: new(seconds), Strokes: new(strokes)}
}

func rest(seconds float64) importer.Length {
	return importer.Length{TimerTime: new(seconds)}
}

func TestAvgSWOLF(t *testing.T) {
	t.Parallel()
	// (50+20) and (55+22); the rest and the length without strokes have none.
	lengths := []importer.Length{swum(50, 20), rest(30), {Active: true, TimerTime: new(40.0)}, swum(55, 22)}
	near(t, "SWOLF", AvgSWOLF(lengths), (70+77)/2.0)
	if got := AvgSWOLF([]importer.Length{rest(30)}); got != nil {
		t.Errorf("no active lengths: %v", *got)
	}
}

func TestFastestLengths(t *testing.T) {
	t.Parallel()
	// 25 m lengths: a 200 m is 8 lengths. The fastest 8 consecutive run
	// together after the rest; an elapsed time stands in for a missing timer time.
	lengths := []importer.Length{swum(20, 0), swum(20, 0), rest(60)}
	for range 7 {
		lengths = append(lengths, swum(22, 0))
	}
	lengths = append(lengths, importer.Length{Active: true, ElapsedTime: new(19.0)}, swum(30, 0))
	tests := []struct {
		name         string
		lengths      []importer.Length
		pool, metres float64
		want         float64
		ok           bool
	}{
		{"after the rest", lengths, 25, 200, 7*22 + 19, true},
		{"too few in a row", lengths, 25, 400, 0, false},
		// 50 m: 4 lengths; 200 m in a 33⅓ m pool is 6.
		{"long course", lengths, 50, 200, 22*3 + 19, true},
		{"third pool", lengths, 100.0 / 3, 200, 22*5 + 19, true},
		// 200 m is 8.75 lengths of 25 yd: not a whole number.
		{"yards", lengths, 22.86, 200, 0, false},
		{"no pool", lengths, 0, 200, 0, false},
		{"no time", []importer.Length{swum(20, 0), {Active: true}, swum(20, 0)}, 100, 200, 0, false},
	}
	for _, tt := range tests {
		got, ok := fastestLengths(tt.lengths, tt.pool, tt.metres)
		if ok != tt.ok || (ok && math.Abs(got-tt.want) > 1e-9) {
			t.Errorf("%s: %v %v, want %v %v", tt.name, got, ok, tt.want, tt.ok)
		}
	}
}

func TestSwimEfforts(t *testing.T) {
	t.Parallel()
	var lengths []importer.Length
	for range 16 {
		lengths = append(lengths, swum(25, 0))
	}
	got := BestEfforts(EffortInput{Sport: "pool_swimming", DistanceM: new(800.0), PoolLengthM: new(50.0), Lengths: lengths})
	sameEfforts(t, "pool", got, map[EffortName]float64{LongestSwim: 800, FastestSwim200: 100, FastestSwim400: 200})
	// Without a pool length the lengths say nothing.
	got = BestEfforts(EffortInput{Sport: "pool_swimming", DistanceM: new(800.0), Lengths: lengths})
	sameEfforts(t, "no pool length", got, map[EffortName]float64{LongestSwim: 800})

	// Open water: 1.25 m/s from GPS distance; a virtual swim sets nothing.
	water := [][]Sample{distanceSamples(1, repeat(1.25, 400)...)}
	got = BestEfforts(EffortInput{Sport: "open_water_swimming", DistanceM: new(500.0), Segments: water})
	sameEfforts(t, "open water", got, map[EffortName]float64{LongestSwim: 500, FastestSwim200: 160, FastestSwim400: 320})
	got = BestEfforts(EffortInput{Sport: "open_water_swimming", Virtual: true, DistanceM: new(500.0), Segments: water})
	sameEfforts(t, "virtual", got, map[EffortName]float64{})
}
