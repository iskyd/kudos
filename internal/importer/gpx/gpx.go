// Package gpx parses GPX 1.1 tracks, including Garmin's TrackPointExtension, into the
// internal activity model.
package gpx

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"time"

	"github.com/iskyd/kudos/internal/importer"
)

// ErrNoTrack is returned when the file has no timestamped track points.
var ErrNoTrack = errors.New("gpx has no timestamped track points")

// Element names without a namespace match any namespace, so gpxtpx:, ns3: and
// unprefixed extensions all decode.
type file struct {
	Tracks []struct {
		Type     string `xml:"type"`
		Segments []struct {
			Points []point `xml:"trkpt"`
		} `xml:"trkseg"`
	} `xml:"trk"`
}

type point struct {
	Lat         *float64   `xml:"lat,attr"`
	Lon         *float64   `xml:"lon,attr"`
	Ele         *float64   `xml:"ele"`
	Time        *time.Time `xml:"time"`
	HR          *float64   `xml:"extensions>TrackPointExtension>hr"`
	Cadence     *float64   `xml:"extensions>TrackPointExtension>cad"`
	Temperature *float64   `xml:"extensions>TrackPointExtension>atemp"`
	Power       *float64   `xml:"extensions>power"`
}

// Decode parses a GPX file into one activity holding every track's points. GPX
// records no summary, so only elapsed time and the GPS distance are filled in.
func Decode(r io.Reader) ([]importer.Activity, error) {
	var f file
	if err := xml.NewDecoder(r).Decode(&f); err != nil {
		return nil, fmt.Errorf("decoding gpx: %w", err)
	}
	act := importer.Activity{Sport: importer.SportOther, Subtype: importer.SubtypeOutdoor}
	if len(f.Tracks) > 0 {
		act.Sport, act.Subtype = sportOf(f.Tracks[0].Type)
	}
	distance := 0.0
	var prev *importer.Sample
	for _, trk := range f.Tracks {
		for _, seg := range trk.Segments {
			for _, p := range seg.Points {
				// Samples are keyed by time: keep the first of any repeated or out-of-order timestamp.
				if p.Time == nil || (prev != nil && !p.Time.After(prev.Time)) {
					continue
				}
				s := sample(p, act.Sport.InStrides())
				if s.Lat != nil {
					if prev != nil && prev.Lat != nil {
						distance += haversine(*prev.Lat, *prev.Lon, *s.Lat, *s.Lon)
					}
					s.Distance = ptr(distance)
				}
				act.Samples = append(act.Samples, s)
				prev = &act.Samples[len(act.Samples)-1]
			}
		}
	}
	if len(act.Samples) == 0 {
		return nil, ErrNoTrack
	}
	act.StartTime = act.Samples[0].Time
	act.ElapsedTime = ptr(prev.Time.Sub(act.StartTime).Seconds())
	if distance > 0 {
		act.Distance = ptr(distance)
	}
	return []importer.Activity{act}, nil
}

func sample(p point, strides bool) importer.Sample {
	s := importer.Sample{
		Time:        p.Time.UTC(),
		Altitude:    importer.Finite(p.Ele),
		HR:          importer.Finite(p.HR),
		Cadence:     importer.Finite(p.Cadence),
		Power:       importer.Finite(p.Power),
		Temperature: importer.Finite(p.Temperature),
	}
	if lat, lon := importer.Finite(p.Lat), importer.Finite(p.Lon); lat != nil && lon != nil &&
		math.Abs(*lat) <= 90 && math.Abs(*lon) <= 180 {
		s.Lat, s.Lon = lat, lon
	}
	if s.Cadence != nil && strides {
		s.Cadence = ptr(*s.Cadence * 2)
	}
	return s
}

// sportOf maps the free-text track type that Garmin and Strava write. Garmin
// Connect marks virtual activities (virtual_ride, virtual_run) only here.
func sportOf(t string) (importer.Sport, importer.Subtype) {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "running", "run", "street_running":
		return importer.SportRunning, importer.SubtypeOutdoor
	case "virtual_run":
		return importer.SportRunning, importer.SubtypeVirtual
	case "trail_running":
		return importer.SportTrailRunning, importer.SubtypeOutdoor
	case "cycling", "biking", "ride", "road_biking", "mountain_biking", "gravel_cycling":
		return importer.SportCycling, importer.SubtypeOutdoor
	case "virtual_ride":
		return importer.SportCycling, importer.SubtypeVirtual
	case "hiking", "hike":
		return importer.SportHiking, importer.SubtypeOutdoor
	case "open_water_swimming", "swimming", "swim":
		return importer.SportOpenWaterSwimming, importer.SubtypeOutdoor
	default:
		return importer.SportOther, importer.SubtypeOutdoor
	}
}

// haversine returns the great-circle distance in metres between two points in degrees.
func haversine(lat1, lon1, lat2, lon2 float64) float64 {
	const earthRadius = 6371008.8 // m, mean radius
	rad := math.Pi / 180
	dLat, dLon := (lat2-lat1)*rad, (lon2-lon1)*rad
	a := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(lat1*rad)*math.Cos(lat2*rad)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return 2 * earthRadius * math.Asin(math.Sqrt(a))
}

func ptr[T any](v T) *T { return &v }
