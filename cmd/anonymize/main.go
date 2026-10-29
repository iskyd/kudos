// Command anonymize writes an anonymized copy of a FIT file, or of a Garmin Connect
// GPX or TCX export, for the test corpus.
//
//	anonymize in.fit|in.gpx|in.tcx out
//
// Every longitude is shifted so the first position lands on a neutral meridian
// in the Atlantic. Latitudes are kept, so every distance between positions stays
// exact. Device serial numbers become 1, 2, 3… in order of first appearance.
// Text in fields the FIT profile does not describe is blanked: Garmin keeps the
// owner's name, sensor serials and the activity's city there. Everything else is
// copied as decoded, including unknown messages and developer fields. GPX and TCX
// get the same longitude shift; their track names and device serial are replaced.
package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
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

type stats struct {
	longitudes int // profiled longitude fields shifted
	unprofiled int // unprofiled fields shifted because they look like a longitude
	serials    int // serial numbers replaced
	texts      int // unprofiled text values blanked
}

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: anonymize in.fit|in.gpx|in.tcx out")
		os.Exit(2)
	}
	st, err := run(os.Args[1], os.Args[2])
	if err != nil {
		fmt.Fprintln(os.Stderr, "anonymize:", err)
		os.Exit(1)
	}
	fmt.Printf("shifted %d longitudes (%d in unprofiled fields), replaced %d serial numbers, blanked %d texts\n",
		st.longitudes+st.unprofiled, st.unprofiled, st.serials, st.texts)
}

func run(inPath, outPath string) (stats, error) {
	in, err := os.ReadFile(inPath) //nolint:gosec // G703: the path is the operator's own command-line argument
	if err != nil {
		return stats{}, fmt.Errorf("reading input: %w", err)
	}
	var (
		out []byte
		st  stats
	)
	switch strings.ToLower(filepath.Ext(inPath)) {
	case ".gpx", ".tcx":
		out, st, err = anonymizeXML(in)
	default:
		var buf bytes.Buffer
		st, err = anonymize(bytes.NewReader(in), &buf)
		out = buf.Bytes()
	}
	if err != nil {
		return st, err
	}
	if err := os.WriteFile(outPath, out, 0o600); err != nil { //nolint:gosec // G703: the path is the operator's own command-line argument
		return st, fmt.Errorf("writing output: %w", err)
	}
	return st, nil
}

// anonymize copies every FIT file chained in r to w, anonymized.
func anonymize(r io.Reader, w io.Writer) (stats, error) {
	// Without component expansion the decoded fields are exactly the stored ones.
	dec := decoder.New(r, decoder.WithNoComponentExpansion())
	// All 16 local message types, like the watch, so definitions are not rewritten before every message.
	enc := encoder.New(w, encoder.WithHeaderOption(encoder.HeaderOptionNormal, 15))
	var st stats
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

func anonymizeFIT(fit *proto.FIT, st *stats) {
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
				st.longitudes++
			} else if v, ok := unprofiledInt32(*f, lonBox); ok && hasLat {
				f.Value = proto.Int32(v + shift)
				st.unprofiled++
			} else if f.Name == "serial_number" && f.Value.Type() == proto.TypeUint32 && f.Value.Uint32() != basetype.Uint32zInvalid {
				old := f.Value.Uint32()
				if serials[old] == 0 {
					nextSerial++
					serials[old] = nextSerial
				}
				f.Value = proto.Uint32(serials[old])
				st.serials++
			} else if f.Name == factory.NameUnknown && blankText(f) {
				st.texts++
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
