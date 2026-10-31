package analytics

import (
	"math"
	"sort"
)

// CurveKind names a duration curve.
type CurveKind string

// Curves: best mean power (W) for rides and best mean speed (m/s) for runs over
// each duration.
const (
	PowerCurve CurveKind = "power"
	PaceCurve  CurveKind = "pace"
)

// The durations the estimates read: FTP is 95% of the best 20-minute power
// (Allen and Coggan's 20-minute test) and threshold pace the best 30-minute
// pace (Friel's 30-minute time trial).
const (
	FTPDurationS           = 1200
	ThresholdPaceDurationS = 1800
)

// CurveDurations lists a curve's durations in seconds, shortest first. Pace
// starts at a minute: shorter windows are GPS noise.
func CurveDurations(kind CurveKind) []int {
	long := []int{60, 120, 300, 600, 1200, 1800, 2700, 3600, 5400, 7200, 10800, 14400, 18000}
	if kind == PowerCurve {
		return append([]int{1, 5, 10, 15, 30}, long...)
	}
	return long
}

// CurvePoint is the best value over one duration.
type CurvePoint struct {
	Kind      CurveKind
	DurationS int
	Value     float64
}

// Curves returns an activity's curve points: best mean power over each duration
// for rides (virtual ones too) and best mean speed for runs (not virtual ones,
// invariant 18). Running power is never part of a power curve.
func Curves(in EffortInput) []CurvePoint {
	var out []CurvePoint
	switch {
	case in.Sport == "cycling":
		runs := powerSeconds(in.Segments)
		for _, d := range CurveDurations(PowerCurve) {
			if w, ok := bestMeanPower(runs, d); ok {
				out = append(out, CurvePoint{PowerCurve, d, w})
			}
		}
	case (in.Sport == "running" || in.Sport == "trail_running") && !in.Virtual:
		for _, d := range CurveDurations(PaceCurve) {
			if v, ok := bestMeanSpeed(in.Segments, float64(d)); ok {
				out = append(out, CurvePoint{PaceCurve, d, v})
			}
		}
	}
	return out
}

// bestMeanSpeed is the furthest distance covered in seconds within one segment,
// pauses included, over seconds; none when it is no distance. Distance is linear between samples, so the
// best window starts or ends at a sample; both are tried.
func bestMeanSpeed(segments [][]Sample, seconds float64) (speed float64, ok bool) {
	best := math.Inf(-1)
	for _, seg := range segments {
		ts, ds := distanceTrack(seg)
		if len(ts) == 0 || ts[len(ts)-1] < seconds {
			continue
		}
		end := ts[len(ts)-1]
		for i, t := range ts {
			if t+seconds <= end {
				best = math.Max(best, distanceAt(ts, ds, t+seconds)-ds[i])
			}
			if t >= seconds {
				best = math.Max(best, ds[i]-distanceAt(ts, ds, t-seconds))
			}
		}
	}
	return best / seconds, best > 0
}

// distanceTrack returns a segment's samples with distance, as seconds from its
// start and the distance so far, a dip held at the furthest point (noise, not
// running back).
func distanceTrack(seg []Sample) (ts, ds []float64) {
	for _, s := range seg {
		if s.Distance == nil {
			continue
		}
		d := *s.Distance
		if len(ds) > 0 {
			d = math.Max(d, ds[len(ds)-1])
		}
		ts = append(ts, s.Time.Sub(seg[0].Time).Seconds())
		ds = append(ds, d)
	}
	return ts, ds
}

// distanceAt interpolates the distance at t, which lies within the track.
func distanceAt(ts, ds []float64, t float64) float64 {
	j := sort.SearchFloat64s(ts, t)
	if ts[j] == t {
		return ds[j]
	}
	return ds[j-1] + (t-ts[j-1])/(ts[j]-ts[j-1])*(ds[j]-ds[j-1])
}

// Envelope makes a curve non-increasing in duration, as a curve should read:
// each point takes the best value at its duration or any longer one. values
// are ordered by duration; it returns, for each, the index whose value it takes.
func Envelope(values []float64) []int {
	from := make([]int, len(values))
	for i := len(values) - 1; i >= 0; i-- {
		from[i] = i
		if i+1 < len(values) && values[from[i+1]] > values[i] {
			from[i] = from[i+1]
		}
	}
	return from
}

// Estimates reads the thresholds a curve implies, as its envelope reads: FTP
// from the best power over 20 minutes or longer, threshold speed from the best
// speed over 30 minutes or longer; nil when the curve is too short.
func Estimates(curve []CurvePoint) (ftp, thresholdSpeed *float64) {
	var power, speed float64
	for _, p := range curve {
		switch {
		case p.Kind == PowerCurve && p.DurationS >= FTPDurationS:
			power = math.Max(power, p.Value)
		case p.Kind == PaceCurve && p.DurationS >= ThresholdPaceDurationS:
			speed = math.Max(speed, p.Value)
		}
	}
	if w := EstimateFTP(power); w > 0 {
		ftp = &w
	}
	if speed > 0 {
		thresholdSpeed = new(EstimateThresholdSpeed(speed))
	}
	return ftp, thresholdSpeed
}

// EstimateFTP is 95% of the best 20-minute power, rounded down to a watt.
func EstimateFTP(power20m float64) float64 {
	return math.Floor(0.95 * power20m)
}

// EstimateThresholdSpeed is the best 30-minute speed with its pace rounded up
// to a whole second per kilometre, so the estimate never flatters and noise
// below a second per kilometre is no change.
func EstimateThresholdSpeed(speed30m float64) float64 {
	return 1000 / math.Ceil(1000/speed30m)
}
