package analytics

import (
	"sort"
	"time"
)

// Axis is what two activities are aligned on.
type Axis string

const (
	// AxisTime aligns on seconds from each activity's first sample.
	AxisTime Axis = "time"
	// AxisDistance aligns on metres covered, summed over segments.
	AxisDistance Axis = "distance"
)

// Aligned is one activity's values at one point of the common axis; every field
// is nil where the activity has no value there (past its end, in a pause, or
// a channel it did not record).
type Aligned struct {
	ElapsedS, DistanceM                 *float64
	HR, Power, Cadence, Speed, Altitude *float64
}

// Align resamples two activities, each given as segments of samples like
// Summarize takes, onto one evenly spaced axis from 0 to the longer activity's
// end, so their channels can be overlaid. Values are linearly interpolated
// between neighbouring samples, which absorbs different sampling rates; no
// value is made up across a pause (a gap over MaxGap) or between segments.
// points must be at least 2.
func Align(a, b [][]Sample, axis Axis, points int) (x []float64, out [2][]Aligned) {
	tracks := [2][]point{track(a, axis), track(b, axis)}
	end := 0.0
	for _, t := range tracks {
		if len(t) > 0 {
			end = max(end, t[len(t)-1].x)
		}
	}
	x = make([]float64, points)
	for i := range x {
		x[i] = end * float64(i) / float64(points-1)
	}
	for k, t := range tracks {
		out[k] = make([]Aligned, points)
		for i, g := range x {
			out[k][i] = valueAt(t, g)
		}
	}
	return x, out
}

// point is a sample placed on the axis, with its elapsed time and distance so far.
type point struct {
	x, elapsed float64
	distance   *float64
	seg        int
	s          *Sample
}

// track places an activity's samples on the axis, in order. On the distance
// axis, samples without a distance are left out and the distance never goes
// backwards; each segment continues from the previous one's total.
func track(segments [][]Sample, axis Axis) []point {
	var out []point
	var start time.Time
	offset := 0.0
	for si, seg := range segments {
		var first *float64
		total := 0.0
		for i := range seg {
			s := &seg[i]
			if start.IsZero() {
				start = s.Time
			}
			p := point{elapsed: s.Time.Sub(start).Seconds(), seg: si, s: s}
			if s.Distance != nil {
				if first == nil {
					first = s.Distance
				}
				total = max(total, *s.Distance-*first)
				p.distance = new(offset + total)
			}
			switch {
			case axis == AxisTime:
				p.x = p.elapsed
			case p.distance == nil:
				continue
			default:
				p.x = *p.distance
			}
			out = append(out, p)
		}
		offset += total
	}
	return out
}

// valueAt interpolates the track at g.
func valueAt(t []point, g float64) Aligned {
	j := sort.Search(len(t), func(i int) bool { return t[i].x >= g })
	switch {
	case j == len(t):
		return Aligned{}
	case t[j].x == g:
		return aligned(t[j], t[j], 0)
	case j == 0:
		return Aligned{}
	}
	p, q := t[j-1], t[j]
	if p.seg != q.seg || q.s.Time.Sub(p.s.Time) > MaxGap {
		return Aligned{}
	}
	return aligned(p, q, (g-p.x)/(q.x-p.x))
}

// aligned is the point a fraction f of the way from p to q.
func aligned(p, q point, f float64) Aligned {
	return Aligned{
		ElapsedS:  lerp(&p.elapsed, &q.elapsed, f),
		DistanceM: lerp(p.distance, q.distance, f),
		HR:        lerp(p.s.HR, q.s.HR, f),
		Power:     lerp(p.s.Power, q.s.Power, f),
		Cadence:   lerp(p.s.Cadence, q.s.Cadence, f),
		Speed:     lerp(p.s.Speed, q.s.Speed, f),
		Altitude:  lerp(p.s.Altitude, q.s.Altitude, f),
	}
}

// lerp is nil unless both ends have a value.
func lerp(a, b *float64, f float64) *float64 {
	if a == nil || b == nil {
		return nil
	}
	return new(*a + (*b-*a)*f)
}
