package analytics

import (
	"math"
	"time"
)

// ReadinessComponent names one input of the readiness score.
type ReadinessComponent string

// Readiness components.
const (
	ReadinessHRV       ReadinessComponent = "hrv"
	ReadinessRestingHR ReadinessComponent = "resting_hr"
	ReadinessSleep     ReadinessComponent = "sleep"
	ReadinessForm      ReadinessComponent = "form"
	ReadinessCheckIn   ReadinessComponent = "check_in"
)

// ConditionKind is a health log kind that caps readiness.
type ConditionKind string

// Capping conditions.
const (
	ConditionIllness ConditionKind = "illness"
	ConditionInjury  ConditionKind = "injury"
)

const (
	// ReadinessMinBaselineDays of HRV and resting HR in the baseline window are
	// needed before a score is shown.
	ReadinessMinBaselineDays = 14
	// ReadinessBaselineDays before the day form the long baseline.
	ReadinessBaselineDays = 60
	// readinessTrendDays up to and including the day form the short baseline.
	readinessTrendDays = 7

	// Default weights; shares are renormalized over the components present.
	weightHRV       = 0.30
	weightRestingHR = 0.20
	weightSleep     = 0.20
	weightForm      = 0.15
	weightCheckIn   = 0.15

	// A component at its baseline scores 75; one standard deviation moves it 25.
	neutralScore = 75
	scorePerSD   = 25
	// Floors keep a very steady baseline from turning noise into big swings.
	minLnHRVSD     = 0.05 // about 5% of HRV
	minRestingHRSD = 1    // bpm
	// formPerTSB moves the form score per point of TSB: −50 scores 0, +17 scores 100.
	formPerTSB = 1.5
	sleepNeedS = 8 * 3600

	illnessCap = 30
	injuryCap  = 50
)

// HealthDay holds one day's overnight values; nil means not recorded.
type HealthDay struct {
	Day                                time.Time
	HRV, RestingHR, SleepS, SleepScore *float64 // ms, bpm, s, 0–100
}

// CheckIn holds the day's 1–5 scores: soreness and stress 5 = worst, sleep
// quality and motivation 5 = best. Nil means not answered.
type CheckIn struct {
	Soreness, SleepQuality, Stress, Motivation *float64
}

// Condition is a health log entry; End nil means ongoing. Kinds other than
// illness and injury never cap.
type Condition struct {
	ID    int64
	Kind  ConditionKind
	Start time.Time
	End   *time.Time
}

// ReadinessInput holds everything the score reads. Days are UTC midnights.
type ReadinessInput struct {
	Day time.Time
	// Health days in any order; those outside the baseline window and the day
	// itself are ignored.
	Health     []HealthDay
	TSB        float64
	CheckIn    *CheckIn
	Conditions []Condition
}

// Component is one scored input; Share is its weight among those present.
type Component struct {
	Name            ReadinessComponent
	Score, Share    float64 // 0–100, 0–1
	Value, Baseline *float64
}

// Cap is the condition limiting the score.
type Cap struct {
	ID   int64
	Kind ConditionKind
	Max  float64
}

// Readiness is the day's score with its breakdown. Until Ready, only
// BaselineDays is set.
type Readiness struct {
	Ready           bool
	BaselineDays    int
	Score, Uncapped float64 // 0–100, rounded
	Components      []Component
	Cap             *Cap
}

// ComputeReadiness scores a day from HRV against its 60-day and 7-day
// baselines, resting HR against its 60-day baseline, sleep, form and the
// check-in, capped by active illness or injury.
func ComputeReadiness(in ReadinessInput) Readiness {
	var today *HealthDay
	var baseHRV, baseRHR, trendHRV []float64
	baselineDays := 0
	for i := range in.Health {
		h := &in.Health[i]
		ago := int(math.Round(in.Day.Sub(h.Day).Hours() / 24))
		if ago == 0 {
			today = h
		}
		if ago >= 0 && ago < readinessTrendDays && h.HRV != nil {
			trendHRV = append(trendHRV, math.Log(*h.HRV))
		}
		if ago < 1 || ago > ReadinessBaselineDays {
			continue
		}
		if h.HRV != nil {
			baseHRV = append(baseHRV, math.Log(*h.HRV))
		}
		if h.RestingHR != nil {
			baseRHR = append(baseRHR, *h.RestingHR)
		}
		if h.HRV != nil && h.RestingHR != nil {
			baselineDays++
		}
	}
	r := Readiness{BaselineDays: baselineDays}
	if baselineDays < ReadinessMinBaselineDays {
		return r
	}
	r.Ready = true
	if today == nil {
		today = &HealthDay{}
	}

	type part struct {
		c Component
		w float64
	}
	var parts []part
	add := func(name ReadinessComponent, w, score float64, value, baseline *float64) {
		parts = append(parts, part{Component{Name: name, Score: clamp100(score), Value: value, Baseline: baseline}, w})
	}

	if len(trendHRV) > 0 {
		mean, sd := meanSD(baseHRV, minLnHRVSD)
		z := (meanOf(trendHRV) - mean) / sd
		if today.HRV != nil {
			z = (z + (math.Log(*today.HRV)-mean)/sd) / 2
		}
		add(ReadinessHRV, weightHRV, neutralScore+scorePerSD*z, today.HRV, new(math.Exp(mean)))
	}
	if today.RestingHR != nil {
		mean, sd := meanSD(baseRHR, minRestingHRSD)
		add(ReadinessRestingHR, weightRestingHR, neutralScore-scorePerSD*(*today.RestingHR-mean)/sd, today.RestingHR, new(mean))
	}
	var sleep []float64
	if today.SleepS != nil {
		sleep = append(sleep, 100**today.SleepS/sleepNeedS)
	}
	if today.SleepScore != nil {
		sleep = append(sleep, *today.SleepScore)
	}
	if len(sleep) > 0 {
		add(ReadinessSleep, weightSleep, meanOf(sleep), today.SleepS, nil)
	}
	add(ReadinessForm, weightForm, neutralScore+formPerTSB*in.TSB, new(in.TSB), nil)
	if c := in.CheckIn; c != nil {
		var scores []float64
		for _, v := range []*float64{c.Soreness, c.Stress} {
			if v != nil {
				scores = append(scores, (5-*v)/4*100)
			}
		}
		for _, v := range []*float64{c.SleepQuality, c.Motivation} {
			if v != nil {
				scores = append(scores, (*v-1)/4*100)
			}
		}
		if len(scores) > 0 {
			add(ReadinessCheckIn, weightCheckIn, meanOf(scores), nil, nil)
		}
	}

	var total, weights float64
	for _, p := range parts {
		weights += p.w
	}
	for _, p := range parts {
		p.c.Share = p.w / weights
		total += p.c.Score * p.c.Share
		r.Components = append(r.Components, p.c)
	}
	r.Uncapped = math.Round(total)
	r.Score = r.Uncapped
	for _, c := range in.Conditions {
		if c.Start.After(in.Day) || (c.End != nil && c.End.Before(in.Day)) {
			continue
		}
		var limit float64
		switch c.Kind {
		case ConditionIllness:
			limit = illnessCap
		case ConditionInjury:
			limit = injuryCap
		default:
			continue
		}
		if r.Cap == nil || limit < r.Cap.Max || (limit == r.Cap.Max && c.ID < r.Cap.ID) {
			r.Cap = &Cap{ID: c.ID, Kind: c.Kind, Max: limit}
		}
	}
	if r.Cap != nil {
		r.Score = math.Min(r.Score, r.Cap.Max)
	}
	return r
}

func clamp100(v float64) float64 { return math.Max(0, math.Min(100, v)) }

func meanOf(vs []float64) float64 {
	var s float64
	for _, v := range vs {
		s += v
	}
	return s / float64(len(vs))
}

// meanSD returns the mean and the sample standard deviation, at least minSD.
func meanSD(vs []float64, minSD float64) (mean, sd float64) {
	mean = meanOf(vs)
	var ss float64
	for _, v := range vs {
		ss += (v - mean) * (v - mean)
	}
	if len(vs) > 1 {
		sd = math.Sqrt(ss / float64(len(vs)-1))
	}
	return mean, math.Max(sd, minSD)
}
