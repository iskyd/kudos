package analytics

// ZoneSport groups activity sports that share thresholds.
type ZoneSport string

// Zone sports; general holds max and resting HR for heart rate reserve zones in any sport.
const (
	ZoneRunning  ZoneSport = "running"
	ZoneCycling  ZoneSport = "cycling"
	ZoneSwimming ZoneSport = "swimming"
	ZoneGeneral  ZoneSport = "general"
)

// ZoneSportOf maps an activity's sport to its thresholds; hiking and the rest use general.
func ZoneSportOf(sport string) ZoneSport {
	switch sport {
	case "running", "trail_running":
		return ZoneRunning
	case "cycling":
		return ZoneCycling
	case "pool_swimming", "open_water_swimming":
		return ZoneSwimming
	default:
		return ZoneGeneral
	}
}

// Thresholds are one zone version's inputs in SI units; nil when not set.
type Thresholds struct {
	LTHR           *float64 // lactate threshold HR, bpm
	ThresholdSpeed *float64 // running threshold pace, m/s
	FTP            *float64 // cycling functional threshold power, W
	CSS            *float64 // critical swim speed, m/s
	MaxHR          *float64 // bpm
	RestingHR      *float64 // bpm
}

// Zone is a range [Low, High) in the metric's SI unit (bpm, m/s or W); Low is
// nil for the first zone and High for the last. Speed zones run slow to fast.
type Zone struct {
	Name      string
	Low, High *float64
}

// Zones are the zones one version gives a sport, with the model each comes from;
// a metric without a model is nil.
type Zones struct {
	HRModel, PaceModel, PowerModel string
	HR, Pace, Power                []Zone
}

// Model names.
const (
	ModelFrielRunHR   = "friel_run_hr"
	ModelFrielBikeHR  = "friel_bike_hr"
	ModelHRR          = "heart_rate_reserve"
	ModelFrielRunPace = "friel_run_pace"
	ModelCSS          = "css"
	ModelCoggan       = "coggan_power"
)

var (
	frielNames  = []string{"Z1", "Z2", "Z3", "Z4", "Z5a", "Z5b", "Z5c"}
	cogganNames = []string{"Active recovery", "Endurance", "Tempo", "Threshold", "VO2max", "Anaerobic", "Neuromuscular"}
	cssNames    = []string{"Recovery", "Easy", "Steady", "Threshold", "Fast"}
	hrrNames    = []string{"Z1", "Z2", "Z3", "Z4", "Z5"}
)

// The published tables give whole percentages per zone ("Z2 85–89%"), so each
// zone starts where the next whole percentage does: Z2 is [85%, 90%).
var (
	// Friel, running heart rate: % of LTHR.
	frielRunHR = []float64{85, 90, 95, 100, 103, 107}
	// Friel, cycling heart rate: % of LTHR.
	frielBikeHR = []float64{81, 90, 94, 100, 103, 107}
	// Coggan power levels: % of FTP.
	coggan = []float64{56, 76, 91, 106, 121, 151}
	// Friel, running pace: % of threshold pace time, slow to fast (Z1 slower than 129%).
	frielRunPace = []float64{130, 114, 106, 101, 97, 90}
	// Heart rate reserve (Karvonen): % of max − resting, added to resting.
	hrr = []float64{60, 70, 80, 90}
	// CSS: seconds per 100 m added to CSS pace, slow to fast (Swim Smooth's Easy is CSS +6–10 s).
	cssOffsets = []float64{10, 6, 3, -2}
)

// ZonesFor gives a sport's zones from its own version (own) and the general
// version (general), either of which may be empty. Heart rate uses Friel from
// LTHR where the sport has a model for it, otherwise heart rate reserve from
// the sport's own max and resting HR, otherwise from the general version's.
// A running threshold is never used for cycling: each sport reads only its own.
func ZonesFor(sport ZoneSport, own, general Thresholds) Zones {
	var z Zones
	switch {
	case sport == ZoneRunning && own.LTHR != nil:
		z.HRModel, z.HR = ModelFrielRunHR, ofThreshold(frielNames, frielRunHR, *own.LTHR)
	case sport == ZoneCycling && own.LTHR != nil:
		z.HRModel, z.HR = ModelFrielBikeHR, ofThreshold(frielNames, frielBikeHR, *own.LTHR)
	case own.MaxHR != nil && own.RestingHR != nil:
		z.HRModel, z.HR = ModelHRR, reserve(*own.MaxHR, *own.RestingHR)
	case general.MaxHR != nil && general.RestingHR != nil:
		z.HRModel, z.HR = ModelHRR, reserve(*general.MaxHR, *general.RestingHR)
	}
	switch {
	case sport == ZoneRunning && own.ThresholdSpeed != nil:
		bounds := make([]float64, len(frielRunPace))
		for i, pct := range frielRunPace {
			bounds[i] = *own.ThresholdSpeed * 100 / pct
		}
		z.PaceModel, z.Pace = ModelFrielRunPace, zones(frielNames, bounds)
	case sport == ZoneSwimming && own.CSS != nil:
		pace := 100 / *own.CSS // s per 100 m
		bounds := make([]float64, len(cssOffsets))
		for i, off := range cssOffsets {
			bounds[i] = 100 / (pace + off)
		}
		z.PaceModel, z.Pace = ModelCSS, zones(cssNames, bounds)
	}
	if sport == ZoneCycling && own.FTP != nil {
		z.PowerModel, z.Power = ModelCoggan, ofThreshold(cogganNames, coggan, *own.FTP)
	}
	return z
}

func ofThreshold(names []string, pcts []float64, threshold float64) []Zone {
	bounds := make([]float64, len(pcts))
	for i, pct := range pcts {
		bounds[i] = threshold * pct / 100
	}
	return zones(names, bounds)
}

func reserve(maxHR, restingHR float64) []Zone {
	bounds := make([]float64, len(hrr))
	for i, pct := range hrr {
		bounds[i] = restingHR + (maxHR-restingHR)*pct/100
	}
	return zones(hrrNames, bounds)
}

// TimeInZones sums, per zone, the time the channel read by value spent in it.
// Like Summarize, a sample weighs the time to the next sample of its segment,
// unless that gap is a pause (over MaxGap), so smart recording's irregular gaps
// count for what they last. Samples without the channel count nowhere. Nil without zones.
func TimeInZones(segments [][]Sample, zones []Zone, value func(Sample) *float64) []float64 {
	if zones == nil {
		return nil
	}
	out := make([]float64, len(zones))
	for _, seg := range segments {
		for i := 0; i+1 < len(seg); i++ {
			v := value(seg[i])
			gap := seg[i+1].Time.Sub(seg[i].Time)
			if v == nil || gap > MaxGap {
				continue
			}
			out[zoneOf(zones, *v)] += gap.Seconds()
		}
	}
	return out
}

// zoneOf is the index of the zone [Low, High) holding v; only the last zone is open above.
func zoneOf(zones []Zone, v float64) int {
	for i, z := range zones[:len(zones)-1] {
		if v < *z.High {
			return i
		}
	}
	return len(zones) - 1
}

// zones names the ranges between ascending bounds, open at both ends.
func zones(names []string, bounds []float64) []Zone {
	out := make([]Zone, len(names))
	for i := range names {
		out[i].Name = names[i]
		if i > 0 {
			out[i].Low = &bounds[i-1]
		}
		if i < len(bounds) {
			out[i].High = &bounds[i]
		}
	}
	return out
}
