// Package fit decodes FIT activity files into the importer model.
package fit

import (
	"errors"
	"fmt"
	"io"
	"math"
	"time"

	"github.com/muktihari/fit/decoder"
	"github.com/muktihari/fit/profile/basetype"
	"github.com/muktihari/fit/profile/filedef"
	"github.com/muktihari/fit/profile/mesgdef"
	"github.com/muktihari/fit/profile/typedef"

	"github.com/iskyd/kudos/internal/importer"
)

// ErrNoSession means the file holds no session, so it is not an activity.
var ErrNoSession = errors.New("fit: no session in file")

// Decode parses a FIT activity file into one activity per session, in order: a
// multisport file yields its legs and transitions, which the caller links in a group.
// Unknown and proprietary messages are ignored.
func Decode(r io.Reader) ([]importer.Activity, error) {
	fit, err := decoder.New(r).Decode()
	if err != nil {
		return nil, fmt.Errorf("decoding fit: %w", err)
	}
	file := filedef.NewActivity(fit.Messages...)
	if len(file.Sessions) == 0 {
		return nil, ErrNoSession
	}

	device := importer.Device{Manufacturer: file.FileId.Manufacturer.String(), Serial: file.FileId.SerialNumber}
	if file.FileId.Product != basetype.Uint16Invalid {
		device.Product = file.FileId.Product
	}
	offset := localOffset(file.Activity)
	names := exerciseNames(file)
	dev := devFields(file)

	acts := make([]importer.Activity, len(file.Sessions))
	for i, s := range file.Sessions {
		sport, subtype := sportOf(s.Sport, s.SubSport)
		acts[i] = importer.Activity{
			Sport:     sport,
			Subtype:   subtype,
			StartTime: s.StartTime.UTC(),
			UTCOffset: offset,
			Device:    device,
			Summary: summary(totals{
				elapsed: s.TotalElapsedTimeScaled(), timer: s.TotalTimerTimeScaled(), distance: s.TotalDistanceScaled(),
				avgSpeed: s.EnhancedAvgSpeedScaled(), maxSpeed: s.EnhancedMaxSpeedScaled(),
				ascent: s.TotalAscent, descent: s.TotalDescent, avgPower: s.AvgPower, maxPower: s.MaxPower, calories: s.TotalCalories,
				avgHR: s.AvgHeartRate, maxHR: s.MaxHeartRate, avgCadence: s.AvgCadence, avgFraction: s.AvgFractionalCadence,
				maxCadence: s.MaxCadence, maxFraction: s.MaxFractionalCadence, temperature: s.AvgTemperature,
			}, inStrides(s.Sport)),
			PoolLength: scaled(s.PoolLengthScaled()),
			RPE:        rpe(s.WorkoutRpe),
			Feel:       num(s.WorkoutFeel, basetype.Uint8Invalid),
			Thresholds: thresholds(file, i),
		}
	}

	// Laps and records belong to the last session that started at or before them.
	for _, l := range file.Laps {
		i := sessionAt(file.Sessions, l.StartTime)
		acts[i].Laps = append(acts[i].Laps, importer.Lap{
			StartTime: l.StartTime.UTC(),
			Summary: summary(totals{
				elapsed: l.TotalElapsedTimeScaled(), timer: l.TotalTimerTimeScaled(), distance: l.TotalDistanceScaled(),
				avgSpeed: l.EnhancedAvgSpeedScaled(), maxSpeed: l.EnhancedMaxSpeedScaled(),
				ascent: l.TotalAscent, descent: l.TotalDescent, avgPower: l.AvgPower, maxPower: l.MaxPower, calories: l.TotalCalories,
				avgHR: l.AvgHeartRate, maxHR: l.MaxHeartRate, avgCadence: l.AvgCadence, avgFraction: l.AvgFractionalCadence,
				maxCadence: l.MaxCadence, maxFraction: l.MaxFractionalCadence, temperature: l.AvgTemperature,
			}, inStrides(file.Sessions[i].Sport)),
			Intensity:        intensity(l.Intensity),
			WorkoutStepIndex: messageIndex(l.WktStepIndex),
		})
	}
	for _, l := range file.Lengths {
		i := sessionAt(file.Sessions, l.StartTime)
		acts[i].Lengths = append(acts[i].Lengths, length(l))
	}
	for _, st := range strengthSets(file) {
		i := sessionAt(file.Sessions, st.StartTime)
		acts[i].Sets = append(acts[i].Sets, set(st, names))
	}
	for _, rec := range file.Records {
		i := sessionAt(file.Sessions, rec.Timestamp)
		// Samples are keyed by time: keep the first of any repeated or out-of-order timestamp.
		if n := len(acts[i].Samples); n > 0 && !rec.Timestamp.After(acts[i].Samples[n-1].Time) {
			continue
		}
		smp := sample(rec, inStrides(file.Sessions[i].Sport))
		addDevFields(&smp, rec.DeveloperFields, dev)
		acts[i].Samples = append(acts[i].Samples, smp)
	}

	// The workout goes to the sessions whose laps or sets ran its steps, or the first one.
	if w := workout(file); w != nil {
		attached := false
		for i := range acts {
			if usesSteps(acts[i]) {
				acts[i].Workout, attached = w, true
			}
		}
		if !attached {
			acts[0].Workout = w
		}
	}
	return acts, nil
}

func usesSteps(a importer.Activity) bool {
	for _, l := range a.Laps {
		if l.WorkoutStepIndex != nil {
			return true
		}
	}
	for _, s := range a.Sets {
		if s.WorkoutStepIndex != nil {
			return true
		}
	}
	return false
}

// rpe converts the recorded 0–100 scale to 0–10.
func rpe(v uint8) *float64 {
	if v == basetype.Uint8Invalid {
		return nil
	}
	return ptr(float64(v) / 10)
}

func sessionAt(sessions []*mesgdef.Session, t time.Time) int {
	i := 0
	for j, s := range sessions {
		if !s.StartTime.After(t) {
			i = j
		}
	}
	return i
}

// localOffset is the activity's local_timestamp − timestamp, or nil when that is not a
// real UTC offset (Zwift writes the FIT epoch, 1989-12-31, as local time).
func localOffset(a *mesgdef.Activity) *time.Duration {
	if a == nil || a.Timestamp.IsZero() || a.LocalTimestamp.IsZero() {
		return nil
	}
	d := a.LocalTimestamp.Sub(a.Timestamp)
	if d < -12*time.Hour || d > 14*time.Hour || d%(15*time.Minute) != 0 {
		return nil
	}
	return &d
}

func sportOf(sport typedef.Sport, sub typedef.SubSport) (importer.Sport, importer.Subtype) {
	subtype := importer.SubtypeOutdoor
	switch sub {
	case typedef.SubSportVirtualActivity:
		subtype = importer.SubtypeVirtual
	case typedef.SubSportTreadmill:
		subtype = importer.SubtypeTreadmill
	case typedef.SubSportIndoorRunning, typedef.SubSportIndoorCycling, typedef.SubSportSpin,
		typedef.SubSportLapSwimming, typedef.SubSportStrengthTraining, typedef.SubSportIndoorWalking:
		subtype = importer.SubtypeIndoor
	}

	switch sport {
	case typedef.SportRunning:
		if sub == typedef.SubSportTrail {
			return importer.SportTrailRunning, subtype
		}
		return importer.SportRunning, subtype
	case typedef.SportCycling, typedef.SportEBiking:
		return importer.SportCycling, subtype
	case typedef.SportHiking:
		return importer.SportHiking, subtype
	case typedef.SportSwimming:
		if sub == typedef.SubSportLapSwimming {
			return importer.SportPoolSwimming, subtype
		}
		return importer.SportOpenWaterSwimming, subtype
	case typedef.SportTraining:
		if sub == typedef.SubSportStrengthTraining {
			return importer.SportStrength, subtype
		}
	case typedef.SportTransition:
		return importer.SportTransition, subtype
	}
	return importer.SportOther, subtype
}

// totals are the raw values sessions and laps share.
type totals struct {
	elapsed, timer, distance, avgSpeed, maxSpeed  float64 // already scaled; NaN when invalid
	ascent, descent, avgPower, maxPower, calories uint16
	avgHR, maxHR, avgCadence, avgFraction         uint8
	maxCadence, maxFraction                       uint8
	temperature                                   int8
}

func summary(t totals, strides bool) importer.Summary {
	return importer.Summary{
		ElapsedTime:    scaled(t.elapsed),
		TimerTime:      scaled(t.timer),
		Distance:       scaled(t.distance),
		Ascent:         num(t.ascent, basetype.Uint16Invalid),
		Descent:        num(t.descent, basetype.Uint16Invalid),
		AvgSpeed:       scaled(t.avgSpeed),
		MaxSpeed:       scaled(t.maxSpeed),
		AvgHR:          num(t.avgHR, basetype.Uint8Invalid),
		MaxHR:          num(t.maxHR, basetype.Uint8Invalid),
		AvgPower:       num(t.avgPower, basetype.Uint16Invalid),
		MaxPower:       num(t.maxPower, basetype.Uint16Invalid),
		AvgCadence:     cadence(t.avgCadence, t.avgFraction, strides),
		MaxCadence:     cadence(t.maxCadence, t.maxFraction, strides),
		AvgTemperature: num(t.temperature, basetype.Sint8Invalid),
		Calories:       num(t.calories, basetype.Uint16Invalid),
	}
}

func sample(r *mesgdef.Record, strides bool) importer.Sample {
	s := importer.Sample{
		Time:        r.Timestamp.UTC(),
		Lat:         scaled(r.PositionLatDegrees()),
		Lon:         scaled(r.PositionLongDegrees()),
		Altitude:    scaled(r.EnhancedAltitudeScaled()),
		HR:          num(r.HeartRate, basetype.Uint8Invalid),
		Power:       num(r.Power, basetype.Uint16Invalid),
		Cadence:     cadence(r.Cadence, r.FractionalCadence, strides),
		Speed:       scaled(r.EnhancedSpeedScaled()),
		Distance:    scaled(r.DistanceScaled()),
		Temperature: num(r.Temperature, basetype.Sint8Invalid),
	}
	// Running dynamics are recorded in millimetres and milliseconds.
	for name, v := range map[string]float64{
		"vertical_oscillation_m": r.VerticalOscillationScaled() / 1000,
		"step_length_m":          r.StepLengthScaled() / 1000,
		"stance_time_s":          r.StanceTimeScaled() / 1000,
	} {
		if !math.IsNaN(v) {
			if s.Extras == nil {
				s.Extras = map[string]float64{}
			}
			s.Extras[name] = v
		}
	}
	return s
}

// inStrides reports whether the sport records cadence in strides per minute
// (confirmed for running and hiking samples); steps per minute are twice that.
func inStrides(s typedef.Sport) bool {
	return s == typedef.SportRunning || s == typedef.SportHiking
}

// cadence adds the fractional part and converts strides to steps per minute.
func cadence(whole, fraction uint8, strides bool) *float64 {
	if whole == basetype.Uint8Invalid {
		return nil
	}
	c := float64(whole)
	if fraction != basetype.Uint8Invalid {
		c += float64(fraction) / 128
	}
	if strides {
		c *= 2
	}
	return &c
}

func intensity(i typedef.Intensity) string {
	switch i {
	case typedef.IntensityActive, typedef.IntensityRest, typedef.IntensityWarmup, typedef.IntensityCooldown,
		typedef.IntensityRecovery, typedef.IntensityInterval, typedef.IntensityOther:
		return i.String()
	}
	return ""
}

// num returns v as a float, or nil when it is the type's invalid sentinel.
func num[T uint8 | uint16 | int8](v, invalid T) *float64 {
	if v == invalid {
		return nil
	}
	f := float64(v)
	return &f
}

// scaled returns f, or nil for the library's invalid value (NaN).
func scaled(f float64) *float64 {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return nil
	}
	return &f
}
