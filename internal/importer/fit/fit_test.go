package fit

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/muktihari/fit/encoder"
	"github.com/muktihari/fit/kit/semicircles"
	"github.com/muktihari/fit/profile/basetype"
	"github.com/muktihari/fit/profile/mesgdef"
	"github.com/muktihari/fit/profile/typedef"
	"github.com/muktihari/fit/proto"

	"github.com/iskyd/kudos/internal/importer"
)

var update = flag.Bool("update", false, "rewrite golden files")

const corpus = "../../../testdata/fit"

// golden is what a golden file holds: the full activity with every 300th sample,
// plus a hash of all samples so any change to any sample shows up.
type golden struct {
	Activity      importer.Activity
	SampleCount   int
	SamplesSHA256 string
}

func TestDecodeGolden(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"half_marathon", "intervals", "zwift", "bike", "hike", "pool_swim", "strength"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			acts := decodeFile(t, filepath.Join(corpus, name+".fit"))
			var out []golden
			for _, a := range acts {
				all, err := json.Marshal(a.Samples)
				if err != nil {
					t.Fatal(err)
				}
				sum := sha256.Sum256(all)
				g := golden{Activity: a, SampleCount: len(a.Samples), SamplesSHA256: hex.EncodeToString(sum[:])}
				g.Activity.Samples = nil
				for i := 0; i < len(a.Samples); i += 300 {
					g.Activity.Samples = append(g.Activity.Samples, a.Samples[i])
				}
				out = append(out, g)
			}
			got, err := json.MarshalIndent(out, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join("testdata", name+".golden.json")
			if *update {
				if err := os.WriteFile(path, append(got, '\n'), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("reading golden file (run with -update to create it): %v", err)
			}
			if !bytes.Equal(append(got, '\n'), want) {
				t.Errorf("%s differs from the golden file; run go test -update and review the diff", name)
			}
		})
	}
}

func decodeFile(t *testing.T, path string) []importer.Activity {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	acts, err := Decode(f)
	if err != nil {
		t.Fatalf("Decode(%s): %v", path, err)
	}
	return acts
}

func TestDecodeSamples(t *testing.T) {
	t.Parallel()
	hm := decodeFile(t, filepath.Join(corpus, "half_marathon.fit"))
	zw := decodeFile(t, filepath.Join(corpus, "zwift.fit"))
	if len(hm) != 1 || len(zw) != 1 {
		t.Fatalf("sessions: half marathon %d, zwift %d; want 1 each", len(hm), len(zw))
	}
	h, z := hm[0], zw[0]

	if h.Sport != importer.SportRunning || h.Subtype != importer.SubtypeOutdoor || h.UTCOffset == nil || *h.UTCOffset != time.Hour {
		t.Errorf("half marathon: sport %s/%s offset %v; want running/outdoor +1h (25 October, after summer time ended)", h.Sport, h.Subtype, h.UTCOffset)
	}
	// Spec: Zwift writes the FIT epoch as local time, so the offset must fall back.
	if z.Sport != importer.SportCycling || z.Subtype != importer.SubtypeVirtual || z.UTCOffset != nil {
		t.Errorf("zwift: sport %s/%s offset %v; want cycling/virtual, no offset", z.Sport, z.Subtype, z.UTCOffset)
	}
	// Zwift reports timer 1 s longer than elapsed; decoding must not reject it.
	if *z.TimerTime != *z.ElapsedTime+1 {
		t.Errorf("zwift timer %v, elapsed %v", *z.TimerTime, *z.ElapsedTime)
	}
	// Session cadence is recorded as 84 + 103/128 strides per minute.
	if want := (84 + 103.0/128) * 2; *h.AvgCadence != want {
		t.Errorf("half marathon avg cadence = %v, want %v steps/min", *h.AvgCadence, want)
	}
	// The hike records about 53 strides/min while moving: doubled to steps.
	hk := decodeFile(t, filepath.Join(corpus, "hike.fit"))[0]
	if hk.Sport != importer.SportHiking || hk.Subtype != importer.SubtypeOutdoor || *hk.AvgCadence != 2*(20+63.0/128) {
		t.Errorf("hike: sport %s/%s avg cadence %v; want hiking/outdoor (20 + 63/128) × 2 steps/min", hk.Sport, hk.Subtype, *hk.AvgCadence)
	}
	for _, s := range h.Samples {
		if s.Temperature != nil {
			t.Fatalf("half marathon sample at %v has temperature %v; the watch leaves it unset (127)", s.Time, *s.Temperature)
		}
	}
}

func TestCadence(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name            string
		whole, fraction uint8
		strides         bool
		want            *float64
	}{
		{name: "running doubles strides with fraction", whole: 84, fraction: 64, strides: true, want: ptr(169.0)},
		{name: "running without fraction", whole: 90, fraction: basetype.Uint8Invalid, strides: true, want: ptr(180.0)},
		{name: "cycling rpm as is", whole: 61, fraction: basetype.Uint8Invalid, want: ptr(61.0)},
		{name: "cycling with fraction", whole: 61, fraction: 32, want: ptr(61.25)},
		{name: "invalid", whole: basetype.Uint8Invalid, fraction: 64, strides: true, want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assertPtr(t, cadence(tt.whole, tt.fraction, tt.strides), tt.want)
		})
	}
}

func TestSentinelsAreDropped(t *testing.T) {
	t.Parallel()
	// A record with every field unset carries the sentinels 255, 65535, 4294967295, 127 and 0x7FFFFFFF.
	rec := mesgdef.NewRecord(nil).SetTimestamp(time.Unix(1_700_000_000, 0))
	s := sample(rec, true)
	for name, v := range map[string]*float64{
		"lat": s.Lat, "lon": s.Lon, "altitude": s.Altitude, "hr": s.HR, "power": s.Power, "cadence": s.Cadence,
		"speed": s.Speed, "distance": s.Distance, "temperature": s.Temperature,
	} {
		if v != nil {
			t.Errorf("%s = %v, want nil", name, *v)
		}
	}
	if s.Extras != nil {
		t.Errorf("extras = %v, want nil", s.Extras)
	}

	sum := summary(totals{
		elapsed: math.NaN(), timer: math.NaN(), distance: math.NaN(), avgSpeed: math.NaN(), maxSpeed: math.NaN(),
		ascent: basetype.Uint16Invalid, descent: basetype.Uint16Invalid, avgPower: basetype.Uint16Invalid, maxPower: basetype.Uint16Invalid,
		calories: basetype.Uint16Invalid, avgHR: basetype.Uint8Invalid, maxHR: basetype.Uint8Invalid, avgCadence: basetype.Uint8Invalid,
		avgFraction: basetype.Uint8Invalid, maxCadence: basetype.Uint8Invalid, maxFraction: basetype.Uint8Invalid, temperature: basetype.Sint8Invalid,
	}, false)
	if sum != (importer.Summary{}) {
		t.Errorf("summary of invalid totals = %+v, want all nil", sum)
	}
}

func TestSampleUnits(t *testing.T) {
	t.Parallel()
	rec := mesgdef.NewRecord(nil).SetTimestamp(time.Date(2026, 10, 25, 8, 35, 15, 0, time.FixedZone("CET", 3600))).
		SetPositionLat(semicircles.ToSemicircles(45.31)).SetPositionLong(semicircles.ToSemicircles(-30)).
		SetEnhancedAltitude((70 + 500) * 5).SetEnhancedSpeed(3387).SetDistance(3273).SetHeartRate(150).SetPower(337).
		SetCadence(84).SetFractionalCadence(64).SetTemperature(-3).
		SetVerticalOscillation(1132).SetStepLength(11770).SetStanceTime(2240)
	s := sample(rec, true)

	if s.Time.Location() != time.UTC || !s.Time.Equal(time.Date(2026, 10, 25, 7, 35, 15, 0, time.UTC)) {
		t.Errorf("time = %v, want 07:35:15 UTC", s.Time)
	}
	for name, c := range map[string]struct {
		got  *float64
		want float64
	}{
		"lat": {s.Lat, 45.31}, "lon": {s.Lon, -30}, "altitude": {s.Altitude, 70}, "speed": {s.Speed, 3.387},
		"distance": {s.Distance, 32.73}, "hr": {s.HR, 150}, "power": {s.Power, 337}, "cadence": {s.Cadence, 169},
		"temperature":            {s.Temperature, -3},
		"vertical_oscillation_m": {ptr(s.Extras["vertical_oscillation_m"]), 0.1132},
		"step_length_m":          {ptr(s.Extras["step_length_m"]), 1.177},
		"stance_time_s":          {ptr(s.Extras["stance_time_s"]), 0.224},
	} {
		// Semicircles resolve to about 8e-8 degrees.
		if c.got == nil || math.Abs(*c.got-c.want) > 1e-7 {
			t.Errorf("%s = %v, want %v", name, deref(c.got), c.want)
		}
	}
}

func TestLocalOffset(t *testing.T) {
	t.Parallel()
	ts := time.Date(2026, 10, 15, 16, 40, 13, 0, time.UTC)
	fitEpoch := time.Date(1989, 12, 31, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name  string
		local time.Time
		want  *time.Duration
	}{
		{name: "summer time", local: ts.Add(2 * time.Hour), want: ptr(2 * time.Hour)},
		{name: "west", local: ts.Add(-5 * time.Hour), want: ptr(-5 * time.Hour)},
		{name: "quarter hour zone", local: ts.Add(5*time.Hour + 45*time.Minute), want: ptr(5*time.Hour + 45*time.Minute)},
		{name: "furthest east", local: ts.Add(14 * time.Hour), want: ptr(14 * time.Hour)},
		{name: "zwift fit epoch", local: fitEpoch, want: nil},
		{name: "beyond any zone", local: ts.Add(15 * time.Hour), want: nil},
		{name: "not a quarter hour", local: ts.Add(7 * time.Minute), want: nil},
		{name: "missing", local: time.Time{}, want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			a := mesgdef.NewActivity(nil).SetTimestamp(ts).SetLocalTimestamp(tt.local)
			assertPtr(t, localOffset(a), tt.want)
		})
	}
	if localOffset(nil) != nil {
		t.Error("no activity message: want nil offset")
	}
}

func TestSportOf(t *testing.T) {
	t.Parallel()
	tests := []struct {
		sport       typedef.Sport
		sub         typedef.SubSport
		wantSport   importer.Sport
		wantSubtype importer.Subtype
	}{
		{typedef.SportRunning, typedef.SubSportGeneric, importer.SportRunning, importer.SubtypeOutdoor},
		{typedef.SportRunning, typedef.SubSportTrail, importer.SportTrailRunning, importer.SubtypeOutdoor},
		{typedef.SportRunning, typedef.SubSportTreadmill, importer.SportRunning, importer.SubtypeTreadmill},
		{typedef.SportRunning, typedef.SubSportIndoorRunning, importer.SportRunning, importer.SubtypeIndoor},
		{typedef.SportRunning, typedef.SubSportVirtualActivity, importer.SportRunning, importer.SubtypeVirtual},
		{typedef.SportCycling, typedef.SubSportVirtualActivity, importer.SportCycling, importer.SubtypeVirtual},
		{typedef.SportCycling, typedef.SubSportIndoorCycling, importer.SportCycling, importer.SubtypeIndoor},
		{typedef.SportCycling, typedef.SubSportRoad, importer.SportCycling, importer.SubtypeOutdoor},
		{typedef.SportEBiking, typedef.SubSportGeneric, importer.SportCycling, importer.SubtypeOutdoor},
		{typedef.SportHiking, typedef.SubSportGeneric, importer.SportHiking, importer.SubtypeOutdoor},
		{typedef.SportSwimming, typedef.SubSportLapSwimming, importer.SportPoolSwimming, importer.SubtypeIndoor},
		{typedef.SportSwimming, typedef.SubSportOpenWater, importer.SportOpenWaterSwimming, importer.SubtypeOutdoor},
		{typedef.SportTraining, typedef.SubSportStrengthTraining, importer.SportStrength, importer.SubtypeIndoor},
		{typedef.SportTraining, typedef.SubSportYoga, importer.SportOther, importer.SubtypeOutdoor},
		{typedef.SportTransition, typedef.SubSportGeneric, importer.SportTransition, importer.SubtypeOutdoor},
		{typedef.SportWalking, typedef.SubSportIndoorWalking, importer.SportOther, importer.SubtypeIndoor},
	}
	for _, tt := range tests {
		t.Run(tt.sport.String()+"/"+tt.sub.String(), func(t *testing.T) {
			t.Parallel()
			sport, subtype := sportOf(tt.sport, tt.sub)
			if sport != tt.wantSport || subtype != tt.wantSubtype {
				t.Errorf("sportOf = %s/%s, want %s/%s", sport, subtype, tt.wantSport, tt.wantSubtype)
			}
		})
	}
}

func TestIntensity(t *testing.T) {
	t.Parallel()
	if got := intensity(typedef.IntensityWarmup); got != "warmup" {
		t.Errorf("warmup = %q", got)
	}
	if got := intensity(typedef.IntensityInvalid); got != "" {
		t.Errorf("invalid = %q, want empty", got)
	}
}

// encode builds a FIT file from messages.
func encode(t testing.TB, mesgs ...proto.Message) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := encoder.New(&buf).Encode(&proto.FIT{Messages: mesgs}); err != nil {
		t.Fatalf("encoding: %v", err)
	}
	return buf.Bytes()
}

// twoSessions is a run then a ride, with a repeated record timestamp in the run.
func twoSessions(t testing.TB) []byte {
	t0 := time.Date(2026, 10, 15, 7, 0, 0, 0, time.UTC)
	at := func(s int) time.Time { return t0.Add(time.Duration(s) * time.Second) }
	rec := func(s int, hr uint8) proto.Message {
		return mesgdef.NewRecord(nil).SetTimestamp(at(s)).SetHeartRate(hr).ToMesg(nil)
	}
	return encode(t,
		mesgdef.NewFileId(nil).SetType(typedef.FileActivity).SetManufacturer(typedef.ManufacturerGarmin).SetProduct(3990).SetSerialNumber(7).SetTimeCreated(t0).ToMesg(nil),
		rec(0, 100), rec(1, 101), rec(1, 199), rec(2, 102),
		mesgdef.NewLap(nil).SetTimestamp(at(2)).SetStartTime(at(0)).SetIntensity(typedef.IntensityActive).ToMesg(nil),
		mesgdef.NewSession(nil).SetTimestamp(at(2)).SetStartTime(at(0)).SetSport(typedef.SportRunning).SetSubSport(typedef.SubSportGeneric).ToMesg(nil),
		rec(10, 120), rec(11, 121),
		mesgdef.NewLap(nil).SetTimestamp(at(11)).SetStartTime(at(10)).ToMesg(nil),
		mesgdef.NewSession(nil).SetTimestamp(at(11)).SetStartTime(at(10)).SetSport(typedef.SportCycling).SetSubSport(typedef.SubSportRoad).ToMesg(nil),
		mesgdef.NewActivity(nil).SetTimestamp(at(11)).SetLocalTimestamp(at(11).Add(2*time.Hour)).SetNumSessions(2).ToMesg(nil),
	)
}

func TestDecodeSplitsSessions(t *testing.T) {
	t.Parallel()
	acts, err := Decode(bytes.NewReader(twoSessions(t)))
	if err != nil {
		t.Fatal(err)
	}
	if len(acts) != 2 {
		t.Fatalf("got %d activities, want 2", len(acts))
	}
	run, ride := acts[0], acts[1]
	if run.Sport != importer.SportRunning || ride.Sport != importer.SportCycling {
		t.Errorf("sports = %s, %s", run.Sport, ride.Sport)
	}
	if len(run.Laps) != 1 || run.Laps[0].Intensity != "active" || len(ride.Laps) != 1 || ride.Laps[0].Intensity != "" {
		t.Errorf("laps = %+v / %+v", run.Laps, ride.Laps)
	}
	// The repeated second keeps its first record.
	var hrs []float64
	for _, s := range run.Samples {
		hrs = append(hrs, *s.HR)
	}
	if len(hrs) != 3 || hrs[1] != 101 || len(ride.Samples) != 2 {
		t.Errorf("run HR %v, ride samples %d; want [100 101 102] and 2", hrs, len(ride.Samples))
	}
	want := importer.Device{Manufacturer: "garmin", Product: 3990, Serial: 7}
	if run.Device != want || ride.UTCOffset == nil || *ride.UTCOffset != 2*time.Hour {
		t.Errorf("device %+v offset %v; want %+v, +2h", run.Device, ride.UTCOffset, want)
	}
}

func TestDecodeErrors(t *testing.T) {
	t.Parallel()
	noSession := encode(t, mesgdef.NewFileId(nil).SetType(typedef.FileActivity).ToMesg(nil))
	if _, err := Decode(bytes.NewReader(noSession)); !errors.Is(err, ErrNoSession) {
		t.Errorf("file without session: error = %v, want ErrNoSession", err)
	}
	if _, err := Decode(bytes.NewReader([]byte("not a fit file"))); err == nil || errors.Is(err, ErrNoSession) {
		t.Errorf("garbage: error = %v, want a decoding error", err)
	}
}

func FuzzDecodeActivity(f *testing.F) {
	f.Add(twoSessions(f))
	for _, name := range []string{"half_marathon", "intervals", "zwift", "bike", "hike", "pool_swim", "strength"} {
		b, err := os.ReadFile(filepath.Join(corpus, name+".fit"))
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
		}
	})
}

func deref(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}

func assertPtr[T comparable](t *testing.T, got, want *T) {
	t.Helper()
	switch {
	case got == nil && want == nil:
	case got == nil || want == nil:
		t.Errorf("got %v, want %v", got, want)
	case *got != *want:
		t.Errorf("got %v, want %v", *got, *want)
	}
}

func TestInStrides(t *testing.T) {
	t.Parallel()
	for sport, want := range map[typedef.Sport]bool{
		typedef.SportRunning: true, typedef.SportHiking: true, typedef.SportCycling: false, typedef.SportWalking: false,
	} {
		if got := inStrides(sport); got != want {
			t.Errorf("inStrides(%s) = %v, want %v", sport, got, want)
		}
	}
}
