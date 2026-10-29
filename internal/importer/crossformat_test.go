package importer_test

import (
	"compress/gzip"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/iskyd/kudos/internal/importer"
	"github.com/iskyd/kudos/internal/importer/fit"
	"github.com/iskyd/kudos/internal/importer/gpx"
	"github.com/iskyd/kudos/internal/importer/tcx"
)

// Garmin Connect's GPX and TCX exports of the FIT corpus activities, anonymized with
// the same shift, must decode to the same samples as the FIT file.
func TestExportsMatchFIT(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"half_marathon", "intervals", "bike", "zwift", "pool_swim"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ref := decode(t, "fit", name, fit.Decode)[0]
			byTime := map[time.Time]importer.Sample{}
			for _, s := range ref.Samples {
				byTime[s.Time] = s
			}

			for _, format := range []string{"gpx", "tcx"} {
				dec := gpx.Decode
				if format == "tcx" {
					dec = tcx.Decode
				}
				if name == "pool_swim" && format == "gpx" {
					// Garmin's GPX of a pool swim has no points.
					if _, err := dec(open(t, format, name)); !errors.Is(err, gpx.ErrNoTrack) {
						t.Errorf("pool_swim.gpx error = %v, want ErrNoTrack", err)
					}
					continue
				}
				a := decode(t, format, name, dec)[0]
				if !a.StartTime.Equal(ref.StartTime) || len(a.Samples) != len(ref.Samples) {
					t.Fatalf("%s: start %v with %d samples, FIT %v with %d", format, a.StartTime, len(a.Samples), ref.StartTime, len(ref.Samples))
				}
				for i, s := range a.Samples {
					r, ok := byTime[s.Time]
					if !ok {
						t.Fatalf("%s sample %d at %v has no FIT sample", format, i, s.Time)
					}
					check := func(field string, got, want *float64, tol float64) {
						t.Helper()
						if (got == nil) != (want == nil) || (got != nil && math.Abs(*got-*want) > tol) {
							t.Fatalf("%s sample %d %s = %v, FIT %v", format, i, field, val(got), val(want))
						}
					}
					check("lat", s.Lat, r.Lat, 0)
					check("lon", s.Lon, r.Lon, 0)
					check("altitude", s.Altitude, r.Altitude, 1e-4) // float32 in FIT
					check("hr", s.HR, r.HR, 0)
					// Exports drop the fractional cadence: steps/min differ by under 2.
					check("cadence", s.Cadence, r.Cadence, 1.99)
					if format == "tcx" {
						check("power", s.Power, r.Power, 0)
						check("speed", s.Speed, r.Speed, 2e-3) // rounded to mm/s in float32
						// The pool swim's first point gets a distance of 0 where FIT records none.
						if r.Distance != nil {
							check("distance", s.Distance, r.Distance, 1e-3)
						}
					}
				}
				if format == "tcx" {
					if len(a.Laps) != len(ref.Laps) {
						t.Errorf("tcx: %d laps, FIT %d", len(a.Laps), len(ref.Laps))
					}
					for _, c := range []struct {
						field     string
						got, want *float64
						tol       float64
					}{
						{"timer", a.TimerTime, ref.TimerTime, 1e-3},
						{"distance", a.Distance, ref.Distance, 0.02},
						{"max hr", a.MaxHR, ref.MaxHR, 0},
						{"max speed", a.MaxSpeed, ref.MaxSpeed, 1e-3},
						{"max power", a.MaxPower, ref.MaxPower, 0},
					} {
						if (c.got == nil) != (c.want == nil) || (c.got != nil && math.Abs(*c.got-*c.want) > c.tol) {
							t.Errorf("tcx %s = %v, FIT %v", c.field, val(c.got), val(c.want))
						}
					}
				}
			}
		})
	}
}

func TestZwiftGPXIsVirtual(t *testing.T) {
	t.Parallel()
	a := decode(t, "gpx", "zwift", gpx.Decode)[0]
	if a.Sport != importer.SportCycling || a.Subtype != importer.SubtypeVirtual {
		t.Errorf("zwift.gpx = %s/%s, want cycling/virtual", a.Sport, a.Subtype)
	}
}

// open returns the corpus file; GPX and TCX exports are gzipped to stay small.
func open(t *testing.T, format, name string) io.Reader {
	t.Helper()
	path := filepath.Join("../../testdata", format, name+"."+format)
	if format != "fit" {
		path += ".gz"
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	if format == "fit" {
		return f
	}
	z, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	return z
}

func decode(t *testing.T, format, name string, dec func(io.Reader) ([]importer.Activity, error)) []importer.Activity {
	t.Helper()
	acts, err := dec(open(t, format, name))
	if err != nil {
		t.Fatalf("%s.%s: %v", name, format, err)
	}
	return acts
}

func val(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}
