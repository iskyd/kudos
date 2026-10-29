package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/muktihari/fit/kit/semicircles"
)

func TestAnonymizeXML(t *testing.T) {
	t.Parallel()
	// 9.4997017085552215576171875° is a whole number of semicircles, as Garmin writes them.
	first := semicircles.ToDegrees(semicircles.ToSemicircles(9.4997017085552215576171875))
	second := semicircles.ToDegrees(semicircles.ToSemicircles(first) + 1000)
	tests := []struct {
		name, in, want string
		st             stats
	}{
		{
			name: "gpx",
			in:   `<trk><name>Lodi - Half Marathon</name><trkpt lat="45.3" lon="9.4997017085552215576171875"/><trkpt lat="45.3" lon="` + ftoa(second) + `"/></trk>`,
			want: `<trk><name></name><trkpt lat="45.3" lon="` + ftoa(semicircles.ToDegrees(semicircles.ToSemicircles(neutralLong))) +
				`"/><trkpt lat="45.3" lon="` + ftoa(semicircles.ToDegrees(semicircles.ToSemicircles(neutralLong)+1000)) + `"/></trk>`,
			st: stats{longitudes: 2, texts: 1},
		},
		{
			name: "tcx",
			in:   `<Position><LatitudeDegrees>45.3</LatitudeDegrees><LongitudeDegrees>` + ftoa(first) + `</LongitudeDegrees></Position><Creator><Name>Forerunner 255</Name><UnitId>3456789012</UnitId></Creator>`,
			want: `<Position><LatitudeDegrees>45.3</LatitudeDegrees><LongitudeDegrees>` + ftoa(semicircles.ToDegrees(semicircles.ToSemicircles(neutralLong))) +
				`</LongitudeDegrees></Position><Creator><Name>Forerunner 255</Name><UnitId>1</UnitId></Creator>`,
			st: stats{longitudes: 1, serials: 1},
		},
		{name: "no positions", in: `<trk><name></name><trkseg/></trk>`, want: `<trk><name></name><trkseg/></trk>`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, st, err := anonymizeXML([]byte(tt.in))
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tt.want {
				t.Errorf("got  %s\nwant %s", got, tt.want)
			}
			if st != tt.st {
				t.Errorf("stats = %+v, want %+v", st, tt.st)
			}
		})
	}
	if _, _, err := anonymizeXML([]byte(`<trkpt lon="east"/>`)); err == nil {
		t.Error("unparsable longitude accepted")
	}
}

func TestRunXML(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	in, out := filepath.Join(dir, "in.GPX"), filepath.Join(dir, "out.gpx")
	if err := os.WriteFile(in, []byte(`<trkpt lat="1" lon="2"/>`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := run(in, out); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), `lon="2"`) {
		t.Errorf("longitude not shifted: %s", b)
	}
	bad := filepath.Join(dir, "bad.tcx")
	if err := os.WriteFile(bad, []byte(`<LongitudeDegrees>x</LongitudeDegrees>`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := run(bad, out); err == nil {
		t.Error("bad tcx accepted")
	}
}

func ftoa(f float64) string { return strconv.FormatFloat(f, 'g', -1, 64) }
