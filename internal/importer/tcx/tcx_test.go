package tcx

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

const fixtures = "../../../testdata/tcx"

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
	b, err := os.ReadFile(filepath.Join(fixtures, name+".tcx"))
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
	if want := time.Date(2026, 5, 3, 7, 0, 0, 0, time.UTC); !a.StartTime.Equal(want) || a.StartTime.Location() != time.UTC {
		t.Errorf("start = %v, want %v", a.StartTime, want)
	}
	if a.Device.Product != 4315 || a.Device.Serial != 1000000001 {
		t.Errorf("device = %+v", a.Device)
	}

	// Summary: lap totals and maxima, elapsed from the last trackpoint.
	assertNear(t, "timer", a.TimerTime, 420)
	assertNear(t, "elapsed", a.ElapsedTime, 420)
	assertNear(t, "distance", a.Distance, 1200)
	assertNear(t, "calories", a.Calories, 85)
	assertNear(t, "max hr", a.MaxHR, 155)
	assertNear(t, "max speed", a.MaxSpeed, 3.6)
	assertNear(t, "max power", a.MaxPower, 300)
	assertNear(t, "max cadence", a.MaxCadence, 176)
	if a.AvgHR != nil || a.AvgSpeed != nil {
		t.Error("activity averages filled; they are left to analytics")
	}

	if len(a.Laps) != 2 {
		t.Fatalf("got %d laps, want 2", len(a.Laps))
	}
	l := a.Laps[0]
	if l.Intensity != "active" || a.Laps[1].Intensity != "rest" {
		t.Errorf("intensity = %q/%q", l.Intensity, a.Laps[1].Intensity)
	}
	assertNear(t, "lap avg speed", l.AvgSpeed, 3.33)
	assertNear(t, "lap avg hr", l.AvgHR, 140)
	assertNear(t, "lap avg cadence (strides doubled)", l.AvgCadence, 170)
	assertNear(t, "lap avg power", l.AvgPower, 255)

	// The second lap's first trackpoint repeats the first lap's last: dropped.
	if len(a.Samples) != 3 {
		t.Fatalf("got %d samples, want 3", len(a.Samples))
	}
	s := a.Samples[0]
	assertNear(t, "lat", s.Lat, 45)
	assertNear(t, "lon", s.Lon, -30)
	assertNear(t, "altitude", s.Altitude, 50)
	assertNear(t, "hr", s.HR, 130)
	assertNear(t, "speed", s.Speed, 3.2)
	assertNear(t, "power", s.Power, 250)
	assertNear(t, "cadence (strides doubled)", s.Cadence, 168)
	assertNear(t, "sample distance", a.Samples[2].Distance, 1200)
	if a.Samples[2].Cadence != nil || a.Samples[2].Speed != nil {
		t.Error("missing extension values decoded as values")
	}
}

func TestDecodeMissingHRAndElevation(t *testing.T) {
	t.Parallel()
	a := decodeFile(t, "ride_no_hr_no_ele")[0]
	if a.Sport != importer.SportCycling {
		t.Errorf("sport = %s", a.Sport)
	}
	for i, s := range a.Samples {
		if s.HR != nil || s.Altitude != nil {
			t.Errorf("sample %d: missing values decoded as %v/%v", i, s.HR, s.Altitude)
		}
	}
	if a.MaxHR != nil || a.Laps[0].AvgHR != nil {
		t.Error("missing heart rate decoded as a value")
	}
	// Cycling cadence is rpm as recorded.
	assertNear(t, "cadence", a.Samples[1].Cadence, 90)
	assertNear(t, "lap avg cadence", a.Laps[0].AvgCadence, 88)
	assertNear(t, "max cadence", a.MaxCadence, 95)
	assertNear(t, "power", a.Samples[1].Power, 220)
}

func TestDecodeFallbacksAndInvalidValues(t *testing.T) {
	t.Parallel()
	const doc = `<TrainingCenterDatabase><Activities>
		<Activity Sport="Other">
			<Lap StartTime="2026-05-05T08:00:00+02:00"><TotalTimeSeconds>NaN</TotalTimeSeconds><Track>
				<Trackpoint><Time>2026-05-05T06:00:00Z</Time>
					<Position><LatitudeDegrees>45</LatitudeDegrees><LongitudeDegrees>200</LongitudeDegrees></Position></Trackpoint>
				<Trackpoint><Position><LatitudeDegrees>45</LatitudeDegrees><LongitudeDegrees>-30</LongitudeDegrees></Position></Trackpoint>
				<Trackpoint><Time>2026-05-05T05:59:00Z</Time></Trackpoint>
			</Track></Lap>
		</Activity>
		<Activity Sport="Running"></Activity>
	</Activities></TrainingCenterDatabase>`
	acts, err := Decode(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	if len(acts) != 1 {
		t.Fatalf("got %d activities, want 1 (one without a start time is skipped)", len(acts))
	}
	a := acts[0]
	if a.Sport != importer.SportOther {
		t.Errorf("sport = %s", a.Sport)
	}
	if want := time.Date(2026, 5, 5, 6, 0, 0, 0, time.UTC); !a.StartTime.Equal(want) || a.StartTime.Location() != time.UTC {
		t.Errorf("start = %v, want the first lap's %v", a.StartTime, want)
	}
	if a.TimerTime != nil {
		t.Error("NaN timer time kept")
	}
	if len(a.Samples) != 1 || a.Samples[0].Lat != nil {
		t.Errorf("samples = %+v, want one without a position", a.Samples)
	}
	assertNear(t, "elapsed", a.ElapsedTime, 0)
}

func TestDecodeErrors(t *testing.T) {
	t.Parallel()
	if _, err := Decode(strings.NewReader("not xml")); err == nil {
		t.Error("garbage decoded without error")
	}
	for _, doc := range []string{`<TrainingCenterDatabase/>`, `<TrainingCenterDatabase><Activities><Activity/></Activities></TrainingCenterDatabase>`} {
		if _, err := Decode(strings.NewReader(doc)); !errors.Is(err, ErrNoActivity) {
			t.Errorf("Decode(%q) error = %v, want ErrNoActivity", doc, err)
		}
	}
}

func TestHelpers(t *testing.T) {
	t.Parallel()
	f := func(v float64) *float64 { return &v }
	tests := []struct {
		name string
		got  *float64
		want *float64
	}{
		{"add nil nil", add(nil, nil), nil},
		{"add nil b", add(nil, f(2)), f(2)},
		{"add a nil", add(f(1), nil), f(1)},
		{"add a b", add(f(1), f(2)), f(3)},
		{"max nil b", maxOf(nil, f(2)), f(2)},
		{"max a nil", maxOf(f(1), nil), f(1)},
		{"max a<b", maxOf(f(1), f(2)), f(2)},
		{"max a>b", maxOf(f(3), f(2)), f(3)},
		{"first a", first(f(1), f(2)), f(1)},
		{"first b", first(nil, f(2)), f(2)},
		{"cadence strides", cadence(f(85.5), true), f(171)},
		{"cadence rpm", cadence(f(85.5), false), f(85.5)},
		{"cadence nil", cadence(nil, true), nil},
		{"cadence NaN", cadence(f(math.NaN()), false), nil},
	}
	for _, tt := range tests {
		if (tt.got == nil) != (tt.want == nil) || (tt.got != nil && *tt.got != *tt.want) {
			t.Errorf("%s = %v, want %v", tt.name, deref(tt.got), deref(tt.want))
		}
	}
}

func FuzzDecodeTCX(f *testing.F) {
	for _, name := range names {
		b, err := os.ReadFile(filepath.Join(fixtures, name+".tcx"))
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
	if got == nil || math.Abs(*got-want) > 1e-9 {
		t.Errorf("%s = %v, want %v", name, deref(got), want)
	}
}

func deref(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}

func TestDecodeDropsImplausibleTimes(t *testing.T) {
	t.Parallel()
	// Valid xs:dateTime, but year -1 and 10000 in UTC: unstorable and unencodable.
	const doc = `<TrainingCenterDatabase><Activities>
		<Activity Sport="Running"><Id>0000-01-01T00:00:00+01:00</Id>
			<Lap StartTime="2026-05-05T06:00:00Z"><Track>
				<Trackpoint><Time>2026-05-05T06:00:00Z</Time></Trackpoint>
				<Trackpoint><Time>9999-12-31T23:00:00-02:00</Time></Trackpoint>
			</Track></Lap>
			<Lap StartTime="9999-12-31T23:00:00-02:00"></Lap>
		</Activity>
		<Activity Sport="Biking"><Lap StartTime="0000-01-01T00:00:00+01:00"></Lap></Activity>
	</Activities></TrainingCenterDatabase>`
	acts, err := Decode(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	if len(acts) != 1 {
		t.Fatalf("got %d activities, want 1 (the other has no plausible start)", len(acts))
	}
	a := acts[0]
	if want := time.Date(2026, 5, 5, 6, 0, 0, 0, time.UTC); !a.StartTime.Equal(want) {
		t.Errorf("start = %v, want the first lap's %v", a.StartTime, want)
	}
	if len(a.Laps) != 1 || len(a.Samples) != 1 {
		t.Errorf("got %d laps and %d samples, want 1 and 1", len(a.Laps), len(a.Samples))
	}
	if _, err := json.Marshal(acts); err != nil {
		t.Fatal(err)
	}
}
