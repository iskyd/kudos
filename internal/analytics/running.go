package analytics

import "math"

// EfficiencyFactor is Friel's efficiency factor, speed over heart rate, in SI
// units: metres per heartbeat (TrainingPeaks shows yards per minute per bpm,
// the same ratio in other units). None without speed or heart rate.
func EfficiencyFactor(speed, hr *float64) *float64 {
	if speed == nil || hr == nil || *speed <= 0 || *hr <= 0 {
		return nil
	}
	return new(*speed / (*hr / 60))
}

// MinDecouplingS is the shortest moving time aerobic decoupling is computed for.
const MinDecouplingS = 20 * 60

// Decoupling is aerobic decoupling (Friel's Pa:HR): how much the efficiency
// factor falls from the first half of the moving time to the second, in percent
// of the first; positive when heart rate drifts up at the same pace. Speed and
// heart rate are time-weighted like Summarize, over the time both are recorded.
// None under MinDecouplingS of such time.
func Decoupling(segments [][]Sample) *float64 {
	type span struct{ w, speed, hr float64 }
	var spans []span
	var total float64
	for _, seg := range segments {
		for i := 0; i+1 < len(seg); i++ {
			s := seg[i]
			gap := seg[i+1].Time.Sub(s.Time)
			if gap > MaxGap || s.Speed == nil || s.HR == nil {
				continue
			}
			spans = append(spans, span{gap.Seconds(), *s.Speed, *s.HR})
			total += gap.Seconds()
		}
	}
	if total < MinDecouplingS {
		return nil
	}
	var halves [2]struct{ speed, hr float64 }
	var done float64
	for _, s := range spans {
		// A span across the middle is shared between the halves.
		first := math.Max(0, math.Min(s.w, total/2-done))
		for h, w := range [2]float64{first, s.w - first} {
			halves[h].speed += s.speed * w
			halves[h].hr += s.hr * w
		}
		done += s.w
	}
	// Both halves weigh total/2, so the weighted sums' ratio is the means'.
	ef := func(h int) float64 { return halves[h].speed / halves[h].hr }
	if halves[0].hr <= 0 || halves[1].hr <= 0 || ef(0) <= 0 {
		return nil
	}
	return new((ef(0) - ef(1)) / ef(0) * 100)
}

// VDOT is Daniels and Gilbert's VO2max estimate (ml/kg/min) from a race of
// metres in seconds (Daniels' Running Formula): the oxygen cost of the race
// pace over the fraction of VO2max that can be held for its duration.
//
//	VO2  = −4.60 + 0.182258·v + 0.000104·v²   (v in m/min)
//	%max = 0.8 + 0.1894393·e^(−0.012778·t) + 0.2989558·e^(−0.1932605·t)   (t in min)
func VDOT(metres, seconds float64) float64 {
	t := seconds / 60
	v := metres / t
	vo2 := -4.60 + 0.182258*v + 0.000104*v*v
	pct := 0.8 + 0.1894393*math.Exp(-0.012778*t) + 0.2989558*math.Exp(-0.1932605*t)
	return vo2 / pct
}

// MinVDOTDurationS is the shortest effort VO2max is estimated from: Daniels'
// tables start at 1500 m, about 4 to 8 minutes of racing.
const MinVDOTDurationS = 5 * 60

// VO2max estimates VO2max as the best VDOT among a pace curve's points of
// MinVDOTDurationS or longer, each read as a race of its speed and duration;
// none without such points. Training efforts are not races, so it is a lower
// bound that harder efforts raise.
func VO2max(curve []CurvePoint) *float64 {
	var best *float64
	for _, p := range curve {
		if p.Kind != PaceCurve || p.DurationS < MinVDOTDurationS || p.Value <= 0 {
			continue
		}
		t := float64(p.DurationS)
		if v := VDOT(p.Value*t, t); best == nil || v > *best {
			best = &v
		}
	}
	return best
}

// RaceDistances are the distances races are predicted for, in metres: 5K, 10K,
// half marathon and marathon.
func RaceDistances() []float64 { return []float64{5000, 10000, 21097.5, 42195} }

// PredictRace is the time in seconds to race metres at a VDOT: the time whose
// VDOT matches (Daniels' equivalent performances), found by bisection between
// 10 m/s and 0.5 m/s, over which VDOT falls as time grows.
func PredictRace(vdot, metres float64) float64 {
	lo, hi := metres/10, metres/0.5
	for range 100 {
		mid := (lo + hi) / 2
		if VDOT(metres, mid) > vdot {
			lo = mid
		} else {
			hi = mid
		}
	}
	return (lo + hi) / 2
}
