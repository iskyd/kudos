// Package tcx parses Garmin Training Center (TCX) activities, including the
// ActivityExtension v2 speed, power and run cadence, into the internal activity model.
package tcx

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"time"

	"github.com/iskyd/kudos/internal/importer"
)

// ErrNoActivity is returned when the file has no activity with a plausible start time.
var ErrNoActivity = errors.New("tcx has no activity")

// Element names without a namespace match any namespace, so ns3:, ax: and
// unprefixed extensions all decode.
type file struct {
	Activities []activity `xml:"Activities>Activity"`
}

type activity struct {
	Sport     string     `xml:"Sport,attr"`
	ID        *time.Time `xml:"Id"`
	Laps      []lap      `xml:"Lap"`
	ProductID uint16     `xml:"Creator>ProductID"`
	UnitID    uint32     `xml:"Creator>UnitId"`
}

type lap struct {
	StartTime   time.Time    `xml:"StartTime,attr"`
	TotalTime   *float64     `xml:"TotalTimeSeconds"`
	Distance    *float64     `xml:"DistanceMeters"`
	MaxSpeed    *float64     `xml:"MaximumSpeed"`
	Calories    *float64     `xml:"Calories"`
	AvgHR       *float64     `xml:"AverageHeartRateBpm>Value"`
	MaxHR       *float64     `xml:"MaximumHeartRateBpm>Value"`
	Intensity   string       `xml:"Intensity"`
	Cadence     *float64     `xml:"Cadence"`
	AvgSpeed    *float64     `xml:"Extensions>LX>AvgSpeed"`
	AvgRunCad   *float64     `xml:"Extensions>LX>AvgRunCadence"`
	MaxRunCad   *float64     `xml:"Extensions>LX>MaxRunCadence"`
	MaxBikeCad  *float64     `xml:"Extensions>LX>MaxBikeCadence"`
	AvgPower    *float64     `xml:"Extensions>LX>AvgWatts"`
	MaxPower    *float64     `xml:"Extensions>LX>MaxWatts"`
	Trackpoints []trackpoint `xml:"Track>Trackpoint"`
}

type trackpoint struct {
	Time       *time.Time `xml:"Time"`
	Lat        *float64   `xml:"Position>LatitudeDegrees"`
	Lon        *float64   `xml:"Position>LongitudeDegrees"`
	Altitude   *float64   `xml:"AltitudeMeters"`
	Distance   *float64   `xml:"DistanceMeters"`
	HR         *float64   `xml:"HeartRateBpm>Value"`
	Cadence    *float64   `xml:"Cadence"`
	Speed      *float64   `xml:"Extensions>TPX>Speed"`
	Power      *float64   `xml:"Extensions>TPX>Watts"`
	RunCadence *float64   `xml:"Extensions>TPX>RunCadence"`
}

// Decode parses a TCX file into one activity per Activity element. The activity
// summary is the laps' totals and maxima; averages are left to analytics.
func Decode(r io.Reader) ([]importer.Activity, error) {
	var f file
	if err := xml.NewDecoder(r).Decode(&f); err != nil {
		return nil, fmt.Errorf("decoding tcx: %w", err)
	}
	var acts []importer.Activity
	for _, a := range f.Activities {
		a.Laps = slices.DeleteFunc(a.Laps, func(l lap) bool { return !importer.PlausibleTime(l.StartTime) })
		start := a.ID
		if (start == nil || !importer.PlausibleTime(*start)) && len(a.Laps) > 0 {
			start = &a.Laps[0].StartTime
		}
		if start == nil || !importer.PlausibleTime(*start) {
			continue
		}
		act := importer.Activity{
			Sport:     sportOf(a.Sport),
			Subtype:   importer.SubtypeOutdoor,
			StartTime: start.UTC(),
			Device:    importer.Device{Product: a.ProductID, Serial: a.UnitID},
		}
		strides := act.Sport.InStrides()
		for _, l := range a.Laps {
			act.Laps = append(act.Laps, lapOf(l, strides))
			for _, tp := range l.Trackpoints {
				// Samples are keyed by time: keep the first of any repeated or out-of-order timestamp.
				if n := len(act.Samples); tp.Time == nil || !importer.PlausibleTime(*tp.Time) || (n > 0 && !tp.Time.After(act.Samples[n-1].Time)) {
					continue
				}
				act.Samples = append(act.Samples, sample(tp, strides))
			}
		}
		act.Summary = totals(act.Laps)
		if n := len(act.Samples); n > 0 {
			act.ElapsedTime = ptr(act.Samples[n-1].Time.Sub(act.StartTime).Seconds())
		}
		acts = append(acts, act)
	}
	if len(acts) == 0 {
		return nil, ErrNoActivity
	}
	return acts, nil
}

func lapOf(l lap, strides bool) importer.Lap {
	avgCad, maxCad := l.Cadence, l.MaxBikeCad
	if strides {
		avgCad, maxCad = first(l.AvgRunCad, l.Cadence), l.MaxRunCad
	}
	out := importer.Lap{
		StartTime: l.StartTime.UTC(),
		Summary: importer.Summary{
			TimerTime:  importer.Finite(l.TotalTime),
			Distance:   importer.Finite(l.Distance),
			AvgSpeed:   importer.Finite(l.AvgSpeed),
			MaxSpeed:   importer.Finite(l.MaxSpeed),
			AvgHR:      importer.Finite(l.AvgHR),
			MaxHR:      importer.Finite(l.MaxHR),
			AvgPower:   importer.Finite(l.AvgPower),
			MaxPower:   importer.Finite(l.MaxPower),
			AvgCadence: cadence(avgCad, strides),
			MaxCadence: cadence(maxCad, strides),
			Calories:   importer.Finite(l.Calories),
		},
	}
	switch l.Intensity {
	case "Active":
		out.Intensity = "active"
	case "Resting":
		out.Intensity = "rest"
	}
	return out
}

func sample(tp trackpoint, strides bool) importer.Sample {
	s := importer.Sample{
		Time:     tp.Time.UTC(),
		Altitude: importer.Finite(tp.Altitude),
		Distance: importer.Finite(tp.Distance),
		HR:       importer.Finite(tp.HR),
		Speed:    importer.Finite(tp.Speed),
		Power:    importer.Finite(tp.Power),
		Cadence:  cadence(first(tp.RunCadence, tp.Cadence), strides),
	}
	if lat, lon := importer.Finite(tp.Lat), importer.Finite(tp.Lon); lat != nil && lon != nil &&
		math.Abs(*lat) <= 90 && math.Abs(*lon) <= 180 {
		s.Lat, s.Lon = lat, lon
	}
	return s
}

// totals sums the laps' times, distances and calories and takes their maxima.
func totals(laps []importer.Lap) importer.Summary {
	var s importer.Summary
	for _, l := range laps {
		s.TimerTime = add(s.TimerTime, l.TimerTime)
		s.Distance = add(s.Distance, l.Distance)
		s.Calories = add(s.Calories, l.Calories)
		s.MaxSpeed = maxOf(s.MaxSpeed, l.MaxSpeed)
		s.MaxHR = maxOf(s.MaxHR, l.MaxHR)
		s.MaxPower = maxOf(s.MaxPower, l.MaxPower)
		s.MaxCadence = maxOf(s.MaxCadence, l.MaxCadence)
	}
	return s
}

// cadence converts the recorded value to steps/min for sports counted in strides.
func cadence(v *float64, strides bool) *float64 {
	v = importer.Finite(v)
	if v == nil || !strides {
		return v
	}
	return ptr(*v * 2)
}

func sportOf(s string) importer.Sport {
	switch s {
	case "Running":
		return importer.SportRunning
	case "Biking":
		return importer.SportCycling
	default:
		return importer.SportOther
	}
}

func add(a, b *float64) *float64 {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	return ptr(*a + *b)
}

func maxOf(a, b *float64) *float64 {
	if a == nil || (b != nil && *b > *a) {
		return b
	}
	return a
}

func first(a, b *float64) *float64 {
	if a != nil {
		return a
	}
	return b
}

func ptr[T any](v T) *T { return &v }
