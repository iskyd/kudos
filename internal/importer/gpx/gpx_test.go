package gpx

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/iskyd/kudos/internal/importer"
)

var update = flag.Bool("update", false, "rewrite golden files")

const fixtures = "../../../testdata/gpx"

var names = []string{"run", "ride_no_hr_no_ele"}

func TestDecodeGolden(t *testing.T) {
	t.Parallel()
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := json.MarshalIndent(decodeFile(t, name), "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			got = append(got, '\n')
			path := filepath.Join("testdata", name+".golden.json")
			if *update {
				if err := os.WriteFile(path, got, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("reading golden file (run with -update to create it): %v", err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("%s differs from the golden file; run go test -update and review the diff", name)
			}
		})
	}
}

func decodeFile(t *testing.T, name string) []importer.Activity {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(fixtures, name+".gpx"))
	if err != nil {
		t.Fatal(err)
	}
	acts, err := Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	return acts
}

func TestDecodeRun(t *testing.T) {
	t.Parallel()
	acts := decodeFile(t, "run")
	if len(acts) != 1 {
		t.Fatalf("got %d activities, want 1", len(acts))
	}
	a := acts[0]
	if a.Sport != importer.SportRunning || a.Subtype != importer.SubtypeOutdoor {
		t.Errorf("sport = %s/%s", a.Sport, a.Subtype)
	}
	if want := time.Date(2026, 5, 1, 6, 0, 0, 0, time.UTC); !a.StartTime.Equal(want) || a.StartTime.Location() != time.UTC {
		t.Errorf("start = %v, want %v", a.StartTime, want)
	}
	if a.UTCOffset != nil {
		t.Errorf("offset = %v, want nil (caller falls back)", *a.UTCOffset)
	}
	// The repeated timestamp is dropped; segments are joined.
	if len(a.Samples) != 3 {
		t.Fatalf("got %d samples, want 3", len(a.Samples))
	}
	// 0.0002° of latitude on a 6371008.8 m sphere.
	wantDist := 0.0002 * math.Pi / 180 * 6371008.8
	assertNear(t, "distance", a.Distance, wantDist)
	assertNear(t, "last sample distance", a.Samples[2].Distance, wantDist)
	assertNear(t, "elapsed", a.ElapsedTime, 9)
	s := a.Samples[1]
	assertNear(t, "altitude", s.Altitude, 100.5)
	assertNear(t, "hr", s.HR, 125)
	assertNear(t, "cadence (strides doubled)", s.Cadence, 170)
	assertNear(t, "temperature", s.Temperature, 18)
	if a.Samples[2].Temperature != nil {
		t.Error("missing temperature decoded as a value")
	}
}

func TestDecodeMissingHRAndElevation(t *testing.T) {
	t.Parallel()
	a := decodeFile(t, "ride_no_hr_no_ele")[0]
	if a.Sport != importer.SportCycling {
		t.Errorf("sport = %s", a.Sport)
	}
	if want := time.Date(2026, 5, 2, 14, 0, 0, 0, time.UTC); !a.StartTime.Equal(want) {
		t.Errorf("start = %v, want %v (converted to UTC)", a.StartTime, want)
	}
	for i, s := range a.Samples {
		if s.HR != nil || s.Altitude != nil || s.Cadence != nil {
			t.Errorf("sample %d: missing values decoded as %v/%v/%v", i, s.HR, s.Altitude, s.Cadence)
		}
	}
	assertNear(t, "power", a.Samples[1].Power, 210)
	// 0.002° of longitude at 45°N.
	assertNear(t, "distance", a.Distance, 0.002*math.Pi/180*6371008.8*math.Cos(math.Pi/4))
}

func TestDecodeDropsInvalidValues(t *testing.T) {
	t.Parallel()
	const doc = `<gpx><trk><trkseg>
		<trkpt lat="95" lon="-30"><ele>NaN</ele><time>2026-05-01T06:00:00Z</time></trkpt>
		<trkpt lat="45" lon="-30"><time>2026-05-01T05:59:59Z</time></trkpt>
		<trkpt lat="45" lon="-30"></trkpt>
		<trkpt lat="45" lon="-30"><ele>+Inf</ele><time>2026-05-01T06:00:01Z</time></trkpt>
	</trkseg></trk></gpx>`
	acts, err := Decode(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	a := acts[0]
	if a.Sport != importer.SportOther {
		t.Errorf("sport = %s, want other", a.Sport)
	}
	if len(a.Samples) != 2 {
		t.Fatalf("got %d samples, want 2 (out-of-order and untimed dropped)", len(a.Samples))
	}
	if s := a.Samples[0]; s.Lat != nil || s.Lon != nil || s.Altitude != nil || s.Distance != nil {
		t.Errorf("invalid position or NaN kept: %+v", s)
	}
	if a.Samples[1].Altitude != nil {
		t.Error("infinite altitude kept")
	}
	if a.Distance != nil {
		t.Errorf("distance = %v, want nil with a single position", *a.Distance)
	}
}

func TestDecodeErrors(t *testing.T) {
	t.Parallel()
	if _, err := Decode(strings.NewReader("not xml")); err == nil {
		t.Error("garbage decoded without error")
	}
	for _, doc := range []string{`<gpx/>`, `<gpx><trk><trkseg><trkpt lat="1" lon="1"/></trkseg></trk></gpx>`} {
		if _, err := Decode(strings.NewReader(doc)); !errors.Is(err, ErrNoTrack) {
			t.Errorf("Decode(%q) error = %v, want ErrNoTrack", doc, err)
		}
	}
}

func TestSportOf(t *testing.T) {
	t.Parallel()
	type st struct {
		sport   importer.Sport
		subtype importer.Subtype
	}
	out, virtual := importer.SubtypeOutdoor, importer.SubtypeVirtual
	for in, want := range map[string]st{
		"running": {importer.SportRunning, out}, "Run": {importer.SportRunning, out},
		"virtual_run": {importer.SportRunning, virtual}, "trail_running": {importer.SportTrailRunning, out},
		"cycling": {importer.SportCycling, out}, "Biking": {importer.SportCycling, out},
		"virtual_ride": {importer.SportCycling, virtual}, "hiking": {importer.SportHiking, out},
		"open_water_swimming": {importer.SportOpenWaterSwimming, out},
		"walking":             {importer.SportOther, out}, "": {importer.SportOther, out},
	} {
		if sport, subtype := sportOf(in); sport != want.sport || subtype != want.subtype {
			t.Errorf("sportOf(%q) = %s/%s, want %s/%s", in, sport, subtype, want.sport, want.subtype)
		}
	}
}

func FuzzDecodeGPX(f *testing.F) {
	for _, name := range names {
		b, err := os.ReadFile(filepath.Join(fixtures, name+".gpx"))
		if err != nil {
			f.Fatal(err)
		}
		f.Add(b)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		acts, err := Decode(bytes.NewReader(data))
		if err != nil {
			return
		}
		for _, a := range acts {
			for i := 1; i < len(a.Samples); i++ {
				if !a.Samples[i].Time.After(a.Samples[i-1].Time) {
					t.Fatalf("samples not strictly increasing at %d", i)
				}
			}
			// NaN or Inf would make the activity unstorable.
			if _, err := json.Marshal(a); err != nil {
				t.Fatal(err)
			}
		}
	})
}

func assertNear(t *testing.T, name string, got *float64, want float64) {
	t.Helper()
	if got == nil || math.Abs(*got-want) > 1e-6 { // the 45°N arc and parallel differ by under 1 µm
		t.Errorf("%s = %v, want %v", name, deref(got), want)
	}
}

func deref(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}
