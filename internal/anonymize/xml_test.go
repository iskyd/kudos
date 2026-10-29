package anonymize

import (
	"strconv"
	"testing"

	"github.com/muktihari/fit/kit/semicircles"
)

func TestAnonymizeXML(t *testing.T) {
	t.Parallel()
	// A whole number of semicircles, as Garmin writes them.
	first := semicircles.ToDegrees(semicircles.ToSemicircles(12.5))
	second := semicircles.ToDegrees(semicircles.ToSemicircles(first) + 1000)
	tests := []struct {
		name, in, want string
		st             Stats
	}{
		{
			name: "gpx",
			in:   `<trk><name>Springfield - Half Marathon</name><trkpt lat="45.3" lon="` + ftoa(first) + `"/><trkpt lat="45.3" lon="` + ftoa(second) + `"/></trk>`,
			want: `<trk><name></name><trkpt lat="45.3" lon="` + ftoa(semicircles.ToDegrees(semicircles.ToSemicircles(neutralLong))) +
				`"/><trkpt lat="45.3" lon="` + ftoa(semicircles.ToDegrees(semicircles.ToSemicircles(neutralLong)+1000)) + `"/></trk>`,
			st: Stats{Longitudes: 2, Texts: 1},
		},
		{
			name: "tcx",
			in:   `<Position><LatitudeDegrees>45.3</LatitudeDegrees><LongitudeDegrees>` + ftoa(first) + `</LongitudeDegrees></Position><Creator><Name>Forerunner 255</Name><UnitId>3456789012</UnitId></Creator>`,
			want: `<Position><LatitudeDegrees>45.3</LatitudeDegrees><LongitudeDegrees>` + ftoa(semicircles.ToDegrees(semicircles.ToSemicircles(neutralLong))) +
				`</LongitudeDegrees></Position><Creator><Name>Forerunner 255</Name><UnitId>1</UnitId></Creator>`,
			st: Stats{Longitudes: 1, Serials: 1},
		},
		{name: "no positions", in: `<trk><name></name><trkseg/></trk>`, want: `<trk><name></name><trkseg/></trk>`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, st, err := XML([]byte(tt.in))
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
	if _, _, err := XML([]byte(`<trkpt lon="east"/>`)); err == nil {
		t.Error("unparsable longitude accepted")
	}
}

func ftoa(f float64) string { return strconv.FormatFloat(f, 'g', -1, 64) }
