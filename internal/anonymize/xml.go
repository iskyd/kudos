package anonymize

import (
	"fmt"
	"regexp"
	"strconv"

	"github.com/muktihari/fit/kit/semicircles"
)

// Garmin Connect writes these exports in a fixed layout, so plain patterns find every value.
var (
	longitude = regexp.MustCompile(`(lon="|<LongitudeDegrees>)([^"<]+)`)
	// GPX track names often hold the city the activity started in.
	gpxName = regexp.MustCompile(`<name>[^<]*</name>`)
	unitID  = regexp.MustCompile(`<UnitId>[^<]*</UnitId>`)
)

// XML anonymizes a Garmin Connect GPX or TCX export like FIT does a
// FIT file. The shift is computed in semicircles from the first position, so the
// positions equal those in the anonymized FIT of the same activity.
func XML(in []byte) ([]byte, Stats, error) {
	var (
		st    Stats
		shift int32
		err   error
	)
	out := longitude.ReplaceAllFunc(in, func(m []byte) []byte {
		sub := longitude.FindSubmatch(m)
		deg, perr := strconv.ParseFloat(string(sub[2]), 64)
		if perr != nil {
			err = fmt.Errorf("parsing longitude %q: %w", sub[2], perr)
			return m
		}
		v := semicircles.ToSemicircles(deg)
		if st.Longitudes == 0 {
			shift = semicircles.ToSemicircles(neutralLong) - v
		}
		st.Longitudes++
		// int32 wraps around ±180°, as longitudes should.
		return []byte(string(sub[1]) + strconv.FormatFloat(semicircles.ToDegrees(v+shift), 'g', -1, 64))
	})
	if err != nil {
		return nil, st, err
	}
	out = gpxName.ReplaceAllFunc(out, func(m []byte) []byte {
		if string(m) != "<name></name>" {
			st.Texts++
		}
		return []byte("<name></name>")
	})
	out = unitID.ReplaceAllFunc(out, func([]byte) []byte {
		st.Serials++
		return []byte("<UnitId>1</UnitId>")
	})
	return out, st, nil
}
