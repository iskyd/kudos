package importer

import "time"

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
	Laps    []Lap
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
