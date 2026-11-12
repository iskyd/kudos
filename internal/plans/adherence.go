package plans

import (
	"encoding/json"
	"fmt"

	"github.com/iskyd/kudos/internal/analytics"
)

// PlannedLoad estimates a workout's load with the formulas analytics.Load
// uses for activities, step by step: each step's target, a range's midpoint
// or its zone's in the sport's zones, gives its intensity against the sport's
// threshold (power TSS from FTP, rTSS from threshold pace, swim TSS from CSS,
// hrTSS from LTHR), and its duration, or its distance at the target pace, gives
// its hours. ok is false when a step has no target or duration it can use: a
// workout is estimated whole or not at all.
func PlannedLoad(steps json.RawMessage, sport string, own, general analytics.Thresholds) (load float64, ok bool, err error) {
	var ss []step
	if err := json.Unmarshal(steps, &ss); err != nil {
		return 0, false, fmt.Errorf("reading steps: %w", err)
	}
	zs := analytics.ZoneSportOf(sport)
	load, ok = stepsLoad(ss, zs, own, general, analytics.ZonesFor(zs, own, general))
	return load, ok, nil
}

func stepsLoad(steps []step, sport analytics.ZoneSport, own, general analytics.Thresholds, zones analytics.Zones) (float64, bool) {
	total := 0.0
	for _, s := range steps {
		if s.Kind == "repeat" {
			if s.Count == nil {
				return 0, false
			}
			l, ok := stepsLoad(s.Steps, sport, own, general, zones)
			if !ok {
				return 0, false
			}
			total += l * float64(*s.Count)
			continue
		}
		l, ok := stepLoad(s, sport, own, general, zones)
		if !ok {
			return 0, false
		}
		total += l
	}
	return total, true
}

func stepLoad(s step, sport analytics.ZoneSport, own, general analytics.Thresholds, zones analytics.Zones) (float64, bool) {
	if s.Target == nil || s.Duration == nil || s.Target.Metric == "" {
		return 0, false
	}
	var zs []analytics.Zone
	switch s.Target.Metric {
	case "heart_rate":
		zs = zones.HR
	case "pace":
		zs = zones.Pace
	case "power":
		zs = zones.Power
	}
	v, ok := targetValue(s, zs)
	if !ok {
		return 0, false
	}
	seconds := *s.Duration
	if s.DurationType == "distance" {
		if s.Target.Metric != "pace" {
			return 0, false
		}
		seconds = *s.Duration / v
	}
	h := seconds / 3600
	switch s.Target.Metric {
	case "power":
		if ftp := own.FTP; ftp != nil && *ftp > 0 && (sport == analytics.ZoneCycling || sport == analytics.ZoneRunning) {
			return h * sq(v / *ftp) * 100, true
		}
	case "pace":
		if thr := own.ThresholdSpeed; sport == analytics.ZoneRunning && thr != nil && *thr > 0 {
			return h * sq(v / *thr) * 100, true
		}
		if css := own.CSS; sport == analytics.ZoneSwimming && css != nil && *css > 0 {
			i := v / *css
			return h * i * i * i * 100, true
		}
	case "heart_rate":
		lthr := own.LTHR
		if lthr == nil {
			lthr = general.LTHR
		}
		rest := 0.0
		if r := own.RestingHR; r != nil {
			rest = *r
		} else if r := general.RestingHR; r != nil {
			rest = *r
		}
		if lthr != nil && *lthr > rest {
			return h * sq(max(0, (v-rest)/(*lthr-rest))) * 100, true
		}
	}
	return 0, false
}

// targetValue is a step's target as one number: a range's midpoint, or the
// midpoint of its zone (the one bound of an open first or last zone).
func targetValue(s step, zones []analytics.Zone) (float64, bool) {
	t := s.Target
	if t.Zone == nil {
		if t.Low == nil || t.High == nil {
			return 0, false
		}
		return (*t.Low + *t.High) / 2, true
	}
	if *t.Zone < 1 || *t.Zone > len(zones) {
		return 0, false
	}
	z := zones[*t.Zone-1]
	switch {
	case z.Low != nil && z.High != nil:
		return (*z.Low + *z.High) / 2, true
	case z.Low != nil:
		return *z.Low, true
	case z.High != nil:
		return *z.High, true
	}
	return 0, false
}

func sq(x float64) float64 { return x * x }

// Outcome is how one past planned workout went.
type Outcome struct {
	PlannedS    float64  // of its time steps; 0 when none
	PlannedLoad *float64 // nil when it cannot be estimated
	Done        bool
	ActualS     float64  // the activity's timer time
	ActualLoad  *float64 // nil when the activity has none
	// The activity's structured workout steps within their target, of those compared.
	StepsWithin, StepsCompared int
}

// Adherence sums how a span of planned workouts went. Duration and load
// compare only completed workouts with both sides known, so a missed workout
// counts once, in the completion rate.
type Adherence struct {
	Planned, Completed         int
	PlannedS, ActualS          float64 // completed workouts with a planned time
	PlannedLoad, ActualLoad    float64 // completed workouts with both loads
	LoadCompared               int
	StepsWithin, StepsCompared int
}

// Summarize sums outcomes.
func Summarize(outcomes []Outcome) Adherence {
	var a Adherence
	for _, o := range outcomes {
		a.Planned++
		if !o.Done {
			continue
		}
		a.Completed++
		if o.PlannedS > 0 {
			a.PlannedS += o.PlannedS
			a.ActualS += o.ActualS
		}
		if o.PlannedLoad != nil && o.ActualLoad != nil {
			a.PlannedLoad += *o.PlannedLoad
			a.ActualLoad += *o.ActualLoad
			a.LoadCompared++
		}
		a.StepsWithin += o.StepsWithin
		a.StepsCompared += o.StepsCompared
	}
	return a
}
