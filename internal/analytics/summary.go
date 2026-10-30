package analytics

import (
	"math"
	"time"
)

// MaxGap is the longest gap between two samples that still counts as moving.
// A longer one is a pause: it counts toward elapsed time, not timer time or averages.
// Smart recording leaves gaps of up to about 6 s.
const MaxGap = 10 * time.Second

// Sample is one recorded sample in SI units; a nil channel was not recorded.
type Sample struct {
	Time                                             time.Time
	HR, Power, Cadence, Speed, Distance, Temperature *float64
}

// Summary holds an activity's summary values computed from its samples.
type Summary struct {
	Start, End       time.Time
	ElapsedS, TimerS float64
	// Nil when no sample recorded the channel.
	DistanceM, AvgSpeed, MaxSpeed, AvgHR, MaxHR, AvgPower, MaxPower *float64
	AvgCadence, MaxCadence, AvgTemperature                          *float64
}

// Summarize computes summary values from segments of samples, each segment in
// time order and the segments one after another (a merged activity has two).
// Averages are time-weighted: a sample weighs the time to the next sample of its
// segment, unless that gap is a pause (over MaxGap). ok is false without samples.
func Summarize(segments [][]Sample) (s Summary, ok bool) {
	var first, last *Sample
	var distance float64
	hasDistance := false
	channels := map[string]*channel{}
	ch := func(name string) *channel {
		if channels[name] == nil {
			channels[name] = &channel{max: math.Inf(-1)}
		}
		return channels[name]
	}
	for _, seg := range segments {
		var segFirst, segLast *float64
		for i := range seg {
			smp := &seg[i]
			if first == nil {
				first = smp
			}
			last = smp
			w := 0.0
			if i+1 < len(seg) {
				if gap := seg[i+1].Time.Sub(smp.Time); gap <= MaxGap {
					w = gap.Seconds()
				}
			}
			s.TimerS += w
			ch("hr").add(smp.HR, w)
			ch("power").add(smp.Power, w)
			ch("cadence").add(smp.Cadence, w)
			ch("speed").add(smp.Speed, w)
			ch("temperature").add(smp.Temperature, w)
			if smp.Distance != nil {
				if segFirst == nil {
					segFirst = smp.Distance
				}
				segLast = smp.Distance
			}
		}
		if segFirst != nil {
			distance += *segLast - *segFirst
			hasDistance = true
		}
	}
	if first == nil {
		return Summary{}, false
	}
	s.Start, s.End = first.Time, last.Time
	s.ElapsedS = last.Time.Sub(first.Time).Seconds()
	if hasDistance {
		s.DistanceM = &distance
	}
	s.AvgHR, s.MaxHR = ch("hr").avg(), ch("hr").maximum()
	s.AvgPower, s.MaxPower = ch("power").avg(), ch("power").maximum()
	s.AvgCadence, s.MaxCadence = ch("cadence").avg(), ch("cadence").maximum()
	s.AvgTemperature = ch("temperature").avg()
	s.AvgSpeed, s.MaxSpeed = ch("speed").avg(), ch("speed").maximum()
	if hasDistance && s.TimerS > 0 {
		s.AvgSpeed = new(distance / s.TimerS)
	}
	return s, true
}

// channel accumulates one channel's time-weighted sum and maximum.
type channel struct {
	sum, weight, plain float64
	n                  int
	max                float64
}

func (c *channel) add(v *float64, w float64) {
	if v == nil {
		return
	}
	c.sum += *v * w
	c.weight += w
	c.plain += *v
	c.n++
	c.max = math.Max(c.max, *v)
}

// avg is the time-weighted mean, or the plain mean when no sample carries weight
// (a single sample, or samples separated by pauses only).
func (c *channel) avg() *float64 {
	switch {
	case c.n == 0:
		return nil
	case c.weight > 0:
		return new(c.sum / c.weight)
	default:
		return new(c.plain / float64(c.n))
	}
}

func (c *channel) maximum() *float64 {
	if c.n == 0 {
		return nil
	}
	return new(c.max)
}
