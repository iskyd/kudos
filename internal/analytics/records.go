package analytics

import (
	"math"
	"time"
)

// EffortName names a best effort; activities' best efforts make the records.
type EffortName string

// Best efforts. Fastest efforts are seconds (lower is better), longest are
// metres and power watts (higher is better).
const (
	Fastest1K           EffortName = "fastest_1k"
	Fastest5K           EffortName = "fastest_5k"
	Fastest10K          EffortName = "fastest_10k"
	FastestHalfMarathon EffortName = "fastest_half_marathon"
	FastestMarathon     EffortName = "fastest_marathon"
	LongestRun          EffortName = "longest_run"
	LongestRide         EffortName = "longest_ride"
	LongestSwim         EffortName = "longest_swim"
	LongestHike         EffortName = "longest_hike"
	Power5s             EffortName = "power_5s"
	Power1m             EffortName = "power_1m"
	Power5m             EffortName = "power_5m"
	Power20m            EffortName = "power_20m"
	Power60m            EffortName = "power_60m"
)

// Efforts lists every best effort in display order.
func Efforts() []EffortName {
	return []EffortName{
		Fastest1K, Fastest5K, Fastest10K, FastestHalfMarathon, FastestMarathon,
		LongestRun, LongestRide, LongestSwim, LongestHike,
		Power5s, Power1m, Power5m, Power20m, Power60m,
	}
}

// LowerIsBetter reports whether a smaller value beats a larger one.
func (e EffortName) LowerIsBetter() bool {
	switch e {
	case Fastest1K, Fastest5K, Fastest10K, FastestHalfMarathon, FastestMarathon:
		return true
	default:
		return false
	}
}

// EffortInput is an activity after its edits.
type EffortInput struct {
	Sport     string
	Virtual   bool
	DistanceM *float64 // the effective distance, nil when unknown
	Segments  [][]Sample
}

// Effort is an activity's best value for one effort.
type Effort struct {
	Name  EffortName
	Value float64
}

// BestEfforts returns an activity's best efforts: for runs the fastest time
// over each standard distance and the distance, for rides, swims and hikes the
// distance, and for rides the best mean power over each standard duration.
// Virtual activities get no distance efforts (invariant 18), only power.
func BestEfforts(in EffortInput) []Effort {
	var out []Effort
	var longest EffortName
	switch in.Sport {
	case "running", "trail_running":
		longest = LongestRun
	case "cycling":
		longest = LongestRide
	case "pool_swimming", "open_water_swimming":
		longest = LongestSwim
	case "hiking":
		longest = LongestHike
	}
	if !in.Virtual && longest != "" && in.DistanceM != nil && *in.DistanceM > 0 {
		out = append(out, Effort{longest, *in.DistanceM})
	}
	if !in.Virtual && longest == LongestRun {
		for _, d := range []struct {
			name   EffortName
			metres float64
		}{
			{Fastest1K, 1000}, {Fastest5K, 5000}, {Fastest10K, 10000},
			{FastestHalfMarathon, 21097.5}, {FastestMarathon, 42195},
		} {
			if s, ok := fastestDistance(in.Segments, d.metres); ok {
				out = append(out, Effort{d.name, s})
			}
		}
	}
	if in.Sport == "cycling" {
		runs := powerSeconds(in.Segments)
		for _, d := range []struct {
			name    EffortName
			seconds int
		}{
			{Power5s, 5}, {Power1m, 60}, {Power5m, 300}, {Power20m, 1200}, {Power60m, 3600},
		} {
			if w, ok := bestMeanPower(runs, d.seconds); ok {
				out = append(out, Effort{d.name, w})
			}
		}
	}
	return out
}

// fastestDistance is the shortest time to cover metres within one segment,
// pauses included: from any sample back to the point metres earlier, found by
// linear interpolation between samples.
func fastestDistance(segments [][]Sample, metres float64) (seconds float64, ok bool) {
	best := math.Inf(1)
	for _, seg := range segments {
		ts, ds := distanceTrack(seg)
		i := 0
		for j := range ds {
			target := ds[j] - metres
			if target < ds[0] {
				continue
			}
			for ds[i+1] <= target {
				i++
			}
			start := ts[i] + (target-ds[i])/(ds[i+1]-ds[i])*(ts[i+1]-ts[i])
			best = math.Min(best, ts[j]-start)
		}
	}
	return best, !math.IsInf(best, 1)
}

// bestMeanPower is the highest mean power over seconds within one run; none
// when it is 0 W.
func bestMeanPower(runs [][]float64, seconds int) (watts float64, ok bool) {
	best := math.Inf(-1)
	for _, run := range runs {
		if len(run) < seconds {
			continue
		}
		var sum float64
		for k, w := range run {
			sum += w
			if k >= seconds {
				sum -= run[k-seconds]
			}
			if k >= seconds-1 {
				best = math.Max(best, sum/float64(seconds))
			}
		}
	}
	return best, best > 0
}

// powerSeconds samples power every second, each sample's power holding until
// the next one, and returns the runs of consecutive seconds with power: a pause
// (a gap over MaxGap), a sample without power or a new segment ends a run.
func powerSeconds(segments [][]Sample) [][]float64 {
	var runs [][]float64
	for _, seg := range segments {
		var run []float64
		j := 0
		for t := 0; len(seg) > 1; t++ {
			at := seg[0].Time.Add(time.Duration(t) * time.Second)
			for j+1 < len(seg) && !seg[j+1].Time.After(at) {
				j++
			}
			if j+1 == len(seg) {
				break
			}
			if p := seg[j].Power; p != nil && seg[j+1].Time.Sub(seg[j].Time) <= MaxGap {
				run = append(run, *p)
				continue
			}
			if len(run) > 0 {
				runs = append(runs, run)
			}
			run = nil
		}
		if len(run) > 0 {
			runs = append(runs, run)
		}
	}
	return runs
}
