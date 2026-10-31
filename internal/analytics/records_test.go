package analytics

import (
	"math"
	"slices"
	"testing"
)

// distanceSamples records distance every step seconds, covering metres per
// second for each of the given seconds in turn.
func distanceSamples(step float64, speeds ...float64) []Sample {
	out := []Sample{{Time: at(0), Distance: new(0.0)}}
	d, t := 0.0, 0.0
	for _, v := range speeds {
		t += step
		d += v * step
		out = append(out, Sample{Time: at(t), Distance: new(d)})
	}
	return out
}

func effortMap(es []Effort) map[EffortName]float64 {
	m := map[EffortName]float64{}
	for _, e := range es {
		m[e.Name] = e.Value
	}
	return m
}

func sameEfforts(t *testing.T, name string, got []Effort, want map[EffortName]float64) {
	t.Helper()
	g := effortMap(got)
	if len(g) != len(want) {
		t.Errorf("%s: %v, want %v", name, g, want)
		return
	}
	for k, w := range want {
		if v, ok := g[k]; !ok || math.Abs(v-w) > 1e-6 {
			t.Errorf("%s: %s = %v, want %v", name, k, v, w)
		}
	}
}

func TestBestRunEfforts(t *testing.T) {
	t.Parallel()
	// 1500 s at 4 m/s (6 km), then 1500 s at 5 m/s (7.5 km): 13.5 km.
	// 1K and 5K fit in the fast half: 200 s and 1000 s. The best 10K takes the
	// whole fast half and 2.5 km at 4 m/s: 1500 + 625 s. No half marathon.
	segs := [][]Sample{distanceSamples(1, append(repeat(4, 1500), repeat(5, 1500)...)...)}
	got := BestEfforts(EffortInput{Sport: "running", DistanceM: new(13500.0), Segments: segs})
	sameEfforts(t, "run", got, map[EffortName]float64{
		LongestRun: 13500, Fastest1K: 200, Fastest5K: 1000, Fastest10K: 2125,
	})
	// A run's power is running power: never a cycling power effort.
	segs[0][0].Power = new(400.0)
	if got := effortMap(BestEfforts(EffortInput{Sport: "trail_running", Segments: segs})); got[Power5s] != 0 || got[Fastest1K] != 200 {
		t.Errorf("trail run: %v", got)
	}
}

func TestFastestDistance(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name   string
		segs   [][]Sample
		metres float64
		want   float64
		ok     bool
	}{
		// Samples every 7 s at 2.5 m/s: the start is interpolated between samples.
		{"interpolated", [][]Sample{distanceSamples(7, repeat(2.5, 100)...)}, 1000, 400, true},
		{"too short", [][]Sample{distanceSamples(1, repeat(3, 300)...)}, 1000, 0, false},
		// 600 m in each part of a merged activity is no kilometre.
		{"split across segments", [][]Sample{distanceSamples(1, repeat(3, 200)...), distanceSamples(1, repeat(3, 200)...)}, 1000, 0, false},
		// 500 m, a 60 s stop, 500 m, at 5 m/s: the stop counts.
		{"pause counts", [][]Sample{distanceSamples(1, append(append(repeat(5, 100), repeat(0, 60)...), repeat(5, 100)...)...)}, 1000, 260, true},
		// A GPS dip back is held at the furthest distance so far; samples
		// without distance are skipped.
		{"dip", [][]Sample{{
			{Time: at(0), Distance: new(0.0)}, {Time: at(100), Distance: new(600.0)}, {Time: at(110), Distance: new(590.0)},
			{Time: at(200), HR: new(150.0)}, {Time: at(300), Distance: new(1200.0)},
		}}, 1000, 300 - 100.0/3, true},
	} {
		got, ok := fastestDistance(tt.segs, tt.metres)
		if ok != tt.ok || (ok && math.Abs(got-tt.want) > 1e-9) {
			t.Errorf("%s: %v %v, want %v %v", tt.name, got, ok, tt.want, tt.ok)
		}
	}
}

func TestBestPowerEfforts(t *testing.T) {
	t.Parallel()
	// 60 s at 100 W, 10 s at 300 W, 60 s at 100 W, recorded every 2 s.
	watts := slices.Concat(repeat(100, 30), repeat(300, 5), repeat(100, 30))
	ride := [][]Sample{powerSamples(2, watts...)}
	got := BestEfforts(EffortInput{Sport: "cycling", DistanceM: new(1000.0), Segments: ride})
	sameEfforts(t, "ride", got, map[EffortName]float64{
		// A minute around the burst: 10 s at 300 W and 50 s at 100 W.
		LongestRide: 1000, Power5s: 300, Power1m: (300*10 + 100*50) / 60.0,
	})

	// A virtual ride sets power efforts, never a distance one.
	got = BestEfforts(EffortInput{Sport: "cycling", Virtual: true, DistanceM: new(30000.0), Segments: ride})
	if m := effortMap(got); len(m) != 2 || m[LongestRide] != 0 || m[Power5s] != 300 {
		t.Errorf("virtual ride: %v", m)
	}

	// A pause splits 80 s at 200 W into two 40 s runs: no full minute.
	paused := powerSamples(1, repeat(200, 40)...)[:40]
	resumed := powerSamples(1, repeat(200, 40)...)
	for i := range resumed {
		resumed[i].Time = resumed[i].Time.Add(MaxGap + 60e9)
	}
	got = BestEfforts(EffortInput{Sport: "cycling", Segments: [][]Sample{append(paused, resumed...)}})
	sameEfforts(t, "paused", got, map[EffortName]float64{Power5s: 200})

	// A ride at 0 W (a power meter while coasting) sets no power effort.
	got = BestEfforts(EffortInput{Sport: "cycling", Segments: [][]Sample{powerSamples(1, repeat(0, 100)...)}})
	sameEfforts(t, "no power", got, map[EffortName]float64{})
}

func TestBestEffortsOfOtherSports(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		in   EffortInput
		want map[EffortName]float64
	}{
		{EffortInput{Sport: "pool_swimming", DistanceM: new(2000.0)}, map[EffortName]float64{LongestSwim: 2000}},
		{EffortInput{Sport: "open_water_swimming", DistanceM: new(1500.0)}, map[EffortName]float64{LongestSwim: 1500}},
		{EffortInput{Sport: "hiking", DistanceM: new(12000.0)}, map[EffortName]float64{LongestHike: 12000}},
		{EffortInput{Sport: "hiking", DistanceM: new(0.0)}, map[EffortName]float64{}},
		{EffortInput{Sport: "strength", DistanceM: new(10.0)}, map[EffortName]float64{}},
		{EffortInput{Sport: "running", Virtual: true, DistanceM: new(5000.0), Segments: [][]Sample{distanceSamples(1, repeat(5, 1000)...)}}, map[EffortName]float64{}},
		{EffortInput{Sport: "running"}, map[EffortName]float64{}},
	} {
		sameEfforts(t, tt.in.Sport, BestEfforts(tt.in), tt.want)
	}
}

func TestEffortOrderAndDirection(t *testing.T) {
	t.Parallel()
	all := Efforts()
	if len(all) != 16 || all[0] != Fastest1K || all[15] != Power60m {
		t.Errorf("efforts: %v", all)
	}
	for _, e := range all {
		if want := e[:7] == "fastest"; e.LowerIsBetter() != want {
			t.Errorf("%s: lower is better %v", e, e.LowerIsBetter())
		}
	}
}
