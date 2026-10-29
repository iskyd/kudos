// Package anonymize makes copies of activity files safe to publish as test data.
//
// Every longitude is shifted so the first position lands on a neutral meridian
// in the Atlantic. Latitudes are kept, so every distance between positions stays
// exact. FIT device serial numbers become 1, 2, 3… in order of first appearance,
// and text in fields the FIT profile does not describe is blanked: Garmin keeps
// the owner's name, sensor serials and the activity's city there. Everything else
// is copied as decoded, including unknown messages and developer fields. GPX and
// TCX get the same longitude shift; their track names and device serial are replaced.
package anonymize

import (
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/muktihari/fit/decoder"
	"github.com/muktihari/fit/encoder"
	"github.com/muktihari/fit/kit/semicircles"
	"github.com/muktihari/fit/profile/basetype"
	"github.com/muktihari/fit/profile/factory"
	"github.com/muktihari/fit/proto"
)

// neutralLong is where the first position lands, in degrees: open ocean at most latitudes.
const neutralLong = -30.0

// unprofiledMargin widens the track's bounding box when looking for positions
// hidden in fields the FIT profile does not describe (Garmin's proprietary messages).
var unprofiledMargin = int64(semicircles.ToSemicircles(0.05))

// Stats counts what was changed.
type Stats struct {
	Longitudes int // profiled longitude fields shifted
	Unprofiled int // unprofiled fields shifted because they look like a longitude
	Serials    int // serial numbers replaced
	Texts      int // unprofiled text values blanked
}

// FIT copies every FIT file chained in r to w, anonymized.
func FIT(r io.Reader, w io.Writer) (Stats, error) {
	// Without component expansion the decoded fields are exactly the stored ones.
	dec := decoder.New(r, decoder.WithNoComponentExpansion())
	// All 16 local message types, like the watch, so definitions are not rewritten before every message.
	enc := encoder.New(w, encoder.WithHeaderOption(encoder.HeaderOptionNormal, 15))
	var st Stats
	for dec.Next() {
		fit, err := dec.Decode()
		if err != nil {
			return st, fmt.Errorf("decoding: %w", err)
		}
		anonymizeFIT(fit, &st)
		if err := enc.Encode(fit); err != nil {
			return st, fmt.Errorf("encoding: %w", err)
		}
	}
	return st, nil
}

func anonymizeFIT(fit *proto.FIT, st *Stats) {
	var (
		shift          int32
		found          bool
		latBox, lonBox box
	)
	for i := range fit.Messages {
		for _, f := range fit.Messages[i].Fields {
			v, ok := semicircle(f)
			switch {
			case !ok:
			case isLat(f):
				latBox.add(v)
			default:
				lonBox.add(v)
				if !found {
					shift, found = semicircles.ToSemicircles(neutralLong)-v, true
				}
			}
		}
	}

	serials := map[uint32]uint32{}
	var nextSerial uint32
	for i := range fit.Messages {
		fields := fit.Messages[i].Fields
		hasLat := false
		for j := range fields {
			if _, ok := unprofiledInt32(fields[j], latBox); ok {
				hasLat = true
			}
		}
		for j := range fields {
			f := &fields[j]
			if v, ok := semicircle(*f); ok && !isLat(*f) {
				f.Value = proto.Int32(v + shift) // int32 wraps around ±180°, as longitudes should
				st.Longitudes++
			} else if v, ok := unprofiledInt32(*f, lonBox); ok && hasLat {
				f.Value = proto.Int32(v + shift)
				st.Unprofiled++
			} else if f.Name == "serial_number" && f.Value.Type() == proto.TypeUint32 && f.Value.Uint32() != basetype.Uint32zInvalid {
				old := f.Value.Uint32()
				if serials[old] == 0 {
					nextSerial++
					serials[old] = nextSerial
				}
				f.Value = proto.Uint32(serials[old])
				st.Serials++
			} else if f.Name == factory.NameUnknown && blankText(f) {
				st.Texts++
			}
		}
	}
}

// blankText empties a text value and reports whether there was any text.
func blankText(f *proto.Field) bool {
	switch v := f.Value.Any().(type) {
	case string:
		f.Value = proto.String("")
		return v != ""
	case []string:
		f.Value = proto.SliceString(make([]string, len(v)))
		return slices.ContainsFunc(v, func(s string) bool { return s != "" })
	}
	return false
}

// semicircle returns the value of a valid profiled position field.
func semicircle(f proto.Field) (int32, bool) {
	if f.Units != "semicircles" || f.Value.Type() != proto.TypeInt32 {
		return 0, false
	}
	v := f.Value.Int32()
	return v, v != basetype.Sint32Invalid
}

func isLat(f proto.Field) bool { return strings.HasSuffix(f.Name, "_lat") }

// unprofiledInt32 returns the value of a field the profile does not describe when it falls inside b.
func unprofiledInt32(f proto.Field, b box) (int32, bool) {
	if f.Name != factory.NameUnknown || f.Value.Type() != proto.TypeInt32 {
		return 0, false
	}
	v := f.Value.Int32()
	// int64, so the margin cannot wrap around ±180°.
	return v, b.ok && int64(v) >= int64(b.lo)-unprofiledMargin && int64(v) <= int64(b.hi)+unprofiledMargin
}

type box struct {
	lo, hi int32
	ok     bool
}

func (b *box) add(v int32) {
	if !b.ok {
		*b = box{lo: v, hi: v, ok: true}
		return
	}
	b.lo, b.hi = min(b.lo, v), max(b.hi, v)
}
