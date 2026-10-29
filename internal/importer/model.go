package importer

import (
	"math"
	"time"
)

// Sport values match the activities.sport check constraint.
type Sport string

// Sports.
const (
	SportRunning           Sport = "running"
	SportTrailRunning      Sport = "trail_running"
	SportCycling           Sport = "cycling"
	SportPoolSwimming      Sport = "pool_swimming"
	SportOpenWaterSwimming Sport = "open_water_swimming"
	SportHiking            Sport = "hiking"
	SportStrength          Sport = "strength"
	SportTransition        Sport = "transition"
	SportOther             Sport = "other"
)

// Subtype values match the activities.subtype check constraint.
type Subtype string

// Subtypes.
const (
	SubtypeOutdoor   Subtype = "outdoor"
	SubtypeIndoor    Subtype = "indoor"
	SubtypeVirtual   Subtype = "virtual"
	SubtypeTreadmill Subtype = "treadmill"
)

// Activity is one parsed activity. Values are SI units (energy in kcal); nil means not recorded.
type Activity struct {
	Sport     Sport
	Subtype   Subtype
	StartTime time.Time // UTC
	// UTCOffset is the local offset recorded in the file, or nil when the file has
	// no valid one: the caller falls back to source metadata, then the user's timezone.
	UTCOffset *time.Duration
	Device    Device
	Summary
	PoolLength *float64 // m, pool swims only
	RPE        *float64 // 0–10
	Feel       *float64 // 0–100, higher is better
	// Thresholds are those the device recorded as valid on the day; nil for third-party files.
	Thresholds *Thresholds
	// Workout is the structured workout run on the device, if any.
	Workout *Workout
	Laps    []Lap
	Lengths []Length
	Sets    []Set
	Samples []Sample
}

// Device identifies the recording device; zero values mean unknown.
type Device struct {
	Manufacturer string
	Product      uint16
	Serial       uint32
}

// Summary holds totals and averages as recorded by the device.
type Summary struct {
	ElapsedTime    *float64 // s
	TimerTime      *float64 // s
	Distance       *float64 // m
	Ascent         *float64 // m
	Descent        *float64 // m
	AvgSpeed       *float64 // m/s
	MaxSpeed       *float64 // m/s
	AvgHR          *float64 // bpm
	MaxHR          *float64 // bpm
	AvgPower       *float64 // W
	MaxPower       *float64 // W
	AvgCadence     *float64 // steps/min on foot, rpm otherwise
	MaxCadence     *float64
	AvgTemperature *float64 // °C
	Calories       *float64 // kcal
}

// Lap is a lap as recorded by the device.
type Lap struct {
	StartTime time.Time // UTC
	Summary
	// Intensity is active, rest, warmup, cooldown, recovery, interval or other; empty when not recorded.
	Intensity string
	// WorkoutStepIndex points into Workout.Steps.
	WorkoutStepIndex *int
}

// Length is one pool length.
type Length struct {
	StartTime   time.Time // UTC
	ElapsedTime *float64  // s
	TimerTime   *float64  // s
	// Active is false for idle (rest) lengths.
	Active bool
	// Stroke is freestyle, backstroke, breaststroke, butterfly, drill, mixed or im; empty when not recorded.
	Stroke   string
	Strokes  *float64
	AvgSpeed *float64 // m/s
}

// SWOLF is the length time in seconds plus its strokes, for active lengths.
// Watches leave it empty, so it is computed.
func (l Length) SWOLF() *float64 {
	if !l.Active || l.TimerTime == nil || l.Strokes == nil {
		return nil
	}
	v := *l.TimerTime + *l.Strokes
	return &v
}

// Set is one strength set.
type Set struct {
	StartTime time.Time // UTC
	Duration  *float64  // s
	// Active is false for rest sets.
	Active bool
	Reps   *float64
	// Weight is 0 when the user did not enter one.
	Weight *float64 // kg
	// ExerciseCategory and ExerciseNumber are the FIT exercise identifiers; ExerciseName
	// is resolved from the file's exercise titles, empty when it has none.
	ExerciseCategory *int
	ExerciseNumber   *int
	ExerciseName     string
	WorkoutStepIndex *int
}

// Thresholds are the device's thresholds and zone upper bounds, ascending.
type Thresholds struct {
	FTP         *float64 // W, for the activity's sport
	ThresholdHR *float64 // bpm
	MaxHR       *float64 // bpm
	RestingHR   *float64 // bpm
	HRZones     []float64
	PowerZones  []float64
}

// Workout is a structured workout as recorded by the device.
type Workout struct {
	Name  string
	Steps []WorkoutStep
}

// WorkoutStep is one step with its values converted to SI units.
type WorkoutStep struct {
	Index int
	Name  string `json:",omitempty"`
	// Intensity is active, rest, warmup, cooldown, recovery, interval or other; empty for repeats.
	Intensity string `json:",omitempty"`
	// DurationType is the FIT duration type, e.g. time, distance, reps, open, repeat_until_steps_cmplt.
	DurationType string
	// Duration is in s for time, m for distance, a count for reps, kcal for calories; nil otherwise.
	Duration *float64 `json:",omitempty"`
	// TargetType is speed (m/s), heart_rate (bpm), heart_rate_percent (of max HR),
	// power (W), power_percent (of FTP), cadence, or empty for no target.
	TargetType string   `json:",omitempty"`
	TargetLow  *float64 `json:",omitempty"`
	TargetHigh *float64 `json:",omitempty"`
	// TargetZone is set when the target is a zone number instead of a range.
	TargetZone *int `json:",omitempty"`
	// RepeatFrom and RepeatCount describe a repeat step: run steps RepeatFrom..Index-1 RepeatCount times.
	RepeatFrom       *int     `json:",omitempty"`
	RepeatCount      *int     `json:",omitempty"`
	ExerciseCategory *int     `json:",omitempty"`
	ExerciseNumber   *int     `json:",omitempty"`
	Weight           *float64 `json:",omitempty"` // kg
}

// Sample is one stream sample.
type Sample struct {
	Time        time.Time // UTC
	Lat         *float64  // degrees
	Lon         *float64  // degrees
	Altitude    *float64  // m
	HR          *float64  // bpm
	Power       *float64  // W
	Cadence     *float64  // steps/min on foot, rpm otherwise
	Speed       *float64  // m/s
	Distance    *float64  // m
	Temperature *float64  // °C
	// Extras holds sport-specific values keyed by name with a unit suffix, e.g. step_length_m.
	Extras map[string]float64
}

// InStrides reports whether the sport's device cadence counts strides (one foot)
// rather than steps, so steps/min is twice the recorded value.
func (s Sport) InStrides() bool {
	return s == SportRunning || s == SportTrailRunning || s == SportHiking
}

// Finite returns v, or nil when v is nil, NaN or infinite. Text formats can spell those.
func Finite(v *float64) *float64 {
	if v == nil || math.IsNaN(*v) || math.IsInf(*v, 0) {
		return nil
	}
	return v
}
