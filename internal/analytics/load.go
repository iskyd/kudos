package analytics

import (
	"math"
	"time"
)

// LoadMethod names how an activity's load was computed.
type LoadMethod string

// Load methods, in the order they are tried (invariant 21).
const (
	LoadPowerTSS   LoadMethod = "power_tss"
	LoadPaceRTSS   LoadMethod = "pace_rtss"
	LoadSwimTSS    LoadMethod = "swim_tss"
	LoadHRTSS      LoadMethod = "hr_tss"
	LoadSessionRPE LoadMethod = "session_rpe"
)

// LoadInput is what an activity's load is computed from.
type LoadInput struct {
	Sport     ZoneSport
	DurationS float64    // timer time
	Segments  [][]Sample // for normalized power
	AvgSpeed  *float64   // m/s, distance over timer time
	AvgHR     *float64   // time-weighted, bpm
	RPE       *float64   // session RPE, 0–10
	Own       Thresholds // the sport's zone version
	General   Thresholds // the general zone version
}

// Load gives an activity's load with the first method its data allows; ok is
// false when none applies. Every TSS variant scores an hour at threshold 100:
//   - power TSS: hours × (NP / FTP)² × 100, for running or cycling with the
//     sport's own FTP, so a running FTP never scores a ride;
//   - rTSS: hours × (speed / threshold speed)² × 100, without grade adjustment;
//   - swim TSS: hours × (speed / CSS)³ × 100;
//   - hrTSS: hours × ((HR − rest) / (LTHR − rest))² × 100, with the sport's
//     LTHR, else the general one, and resting HR 0 when none is set;
//   - session RPE: RPE × hours × 10, so RPE 10 for an hour scores 100.
func Load(in LoadInput) (load float64, method LoadMethod, ok bool) {
	h := in.DurationS / 3600
	if h <= 0 {
		return 0, "", false
	}
	if ftp := in.Own.FTP; ftp != nil && *ftp > 0 && (in.Sport == ZoneCycling || in.Sport == ZoneRunning) {
		if np, ok := NormalizedPower(in.Segments); ok {
			intensity := np / *ftp
			return h * intensity * intensity * 100, LoadPowerTSS, true
		}
	}
	if thr := in.Own.ThresholdSpeed; in.Sport == ZoneRunning && thr != nil && *thr > 0 && in.AvgSpeed != nil {
		intensity := *in.AvgSpeed / *thr
		return h * intensity * intensity * 100, LoadPaceRTSS, true
	}
	if css := in.Own.CSS; in.Sport == ZoneSwimming && css != nil && *css > 0 && in.AvgSpeed != nil {
		intensity := *in.AvgSpeed / *css
		return h * intensity * intensity * intensity * 100, LoadSwimTSS, true
	}
	if lthr := first(in.Own.LTHR, in.General.LTHR); lthr != nil && in.AvgHR != nil {
		rest := 0.0
		if r := first(in.Own.RestingHR, in.General.RestingHR); r != nil {
			rest = *r
		}
		if *lthr > rest {
			intensity := math.Max(0, (*in.AvgHR-rest)/(*lthr-rest))
			return h * intensity * intensity * 100, LoadHRTSS, true
		}
	}
	if in.RPE != nil {
		return *in.RPE * h * 10, LoadSessionRPE, true
	}
	return 0, "", false
}

func first(vs ...*float64) *float64 {
	for _, v := range vs {
		if v != nil {
			return v
		}
	}
	return nil
}

// npWindow is normalized power's rolling average, in seconds.
const npWindow = 30

// NormalizedPower is Coggan's normalized power: the fourth-power mean of the
// 30-second rolling average of power, sampled every second. Each sample's power
// holds until the next one, so irregular recording weighs by time; a pause (a
// gap over MaxGap), a sample without power or a new segment restarts the
// window. ok is false without one full window.
func NormalizedPower(segments [][]Sample) (np float64, ok bool) {
	var sum4 float64
	n := 0
	for _, seg := range segments {
		var buf [npWindow]float64
		var wsum float64
		k, filled, j := 0, 0, 0
		for t := 0; len(seg) > 1; t++ {
			at := seg[0].Time.Add(time.Duration(t) * time.Second)
			for j+1 < len(seg) && !seg[j+1].Time.After(at) {
				j++
			}
			if j+1 == len(seg) {
				break
			}
			p := seg[j].Power
			if p == nil || seg[j+1].Time.Sub(seg[j].Time) > MaxGap {
				filled, wsum = 0, 0
				continue
			}
			if filled == npWindow {
				wsum -= buf[k]
			}
			buf[k] = *p
			wsum += *p
			k = (k + 1) % npWindow
			filled = min(filled+1, npWindow)
			if filled == npWindow {
				sum4 += math.Pow(wsum/npWindow, 4)
				n++
			}
		}
	}
	if n == 0 {
		return 0, false
	}
	return math.Pow(sum4/float64(n), 0.25), true
}

// DayLoad is the summed load of one local day.
type DayLoad struct {
	Day  time.Time
	Load float64
}

// FitnessDay is one day of the fitness (CTL, 42-day), fatigue (ATL, 7-day) and
// form (TSB) curves.
type FitnessDay struct {
	Day                 time.Time
	Load, CTL, ATL, TSB float64
}

// Fitness runs the fitness and fatigue recurrences day by day, starting at zero
// on the earlier of from and the first loaded day, and returns the days from
// from to to. Days are UTC midnights; loads are in ascending order and days
// without one count as zero. Form is the previous day's fitness minus fatigue.
func Fitness(loads []DayLoad, from, to time.Time) []FitnessDay {
	start := from
	if len(loads) > 0 && loads[0].Day.Before(start) {
		start = loads[0].Day
	}
	var out []FitnessDay
	var ctl, atl float64
	i := 0
	for d := start; !d.After(to); d = d.AddDate(0, 0, 1) {
		var l float64
		for ; i < len(loads) && !loads[i].Day.After(d); i++ {
			if loads[i].Day.Equal(d) {
				l += loads[i].Load
			}
		}
		tsb := ctl - atl
		ctl += (l - ctl) / 42
		atl += (l - atl) / 7
		if !d.Before(from) {
			out = append(out, FitnessDay{Day: d, Load: l, CTL: ctl, ATL: atl, TSB: tsb})
		}
	}
	return out
}
