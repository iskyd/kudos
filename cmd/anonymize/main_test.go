package main

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/muktihari/fit/decoder"
	"github.com/muktihari/fit/encoder"
	"github.com/muktihari/fit/kit/datetime"
	"github.com/muktihari/fit/kit/semicircles"
	"github.com/muktihari/fit/profile/basetype"
	"github.com/muktihari/fit/profile/factory"
	"github.com/muktihari/fit/profile/mesgdef"
	"github.com/muktihari/fit/profile/typedef"
	"github.com/muktihari/fit/profile/untyped/fieldnum"
	"github.com/muktihari/fit/profile/untyped/mesgnum"
	"github.com/muktihari/fit/proto"
)

const proprietaryMesg typedef.MesgNum = 0xFF01

var (
	homeLat  = semicircles.ToSemicircles(45.4642)
	homeLong = semicircles.ToSemicircles(9.19)
	start    = time.Date(2026, 10, 15, 7, 0, 0, 0, time.UTC)
)

func field(mesg typedef.MesgNum, num byte, v any) proto.Field {
	return factory.CreateField(mesg, num).WithValue(v)
}

// unknownField is a field the profile does not describe, as the decoder returns it.
func unknownField(num byte, v any) proto.Field {
	f := factory.CreateField(proprietaryMesg, num).WithValue(v)
	f.BaseType = basetype.Sint32
	if _, ok := v.(string); ok {
		f.BaseType = basetype.String
	}
	return f
}

func record(i int32, lat, long int32) proto.Message {
	return proto.Message{
		Num: mesgnum.Record,
		Fields: []proto.Field{
			field(mesgnum.Record, fieldnum.RecordTimestamp, datetime.ToUint32(start.Add(time.Duration(i)*time.Second))),
			field(mesgnum.Record, fieldnum.RecordPositionLat, lat),
			field(mesgnum.Record, fieldnum.RecordPositionLong, long),
			field(mesgnum.Record, fieldnum.RecordHeartRate, uint8(140+i)),
			field(mesgnum.Record, fieldnum.RecordDistance, uint32(i*300)),
		},
		DeveloperFields: []proto.DeveloperField{{Num: 0, DeveloperDataIndex: 0, Value: proto.Uint16(uint16(200 + i))}},
	}
}

// sample builds a FIT activity covering every kind of data anonymize must handle.
func sample(t *testing.T) []byte {
	t.Helper()
	ts := datetime.ToUint32(start)
	fit := proto.FIT{FileHeader: proto.FileHeader{ProtocolVersion: proto.V2}, Messages: []proto.Message{
		{Num: mesgnum.FileId, Fields: []proto.Field{
			field(mesgnum.FileId, fieldnum.FileIdType, uint8(typedef.FileActivity)),
			field(mesgnum.FileId, fieldnum.FileIdManufacturer, uint16(typedef.ManufacturerGarmin)),
			field(mesgnum.FileId, fieldnum.FileIdSerialNumber, uint32(3999999999)),
			field(mesgnum.FileId, fieldnum.FileIdTimeCreated, ts),
		}},
		mesgdef.NewDeveloperDataId(nil).SetDeveloperDataIndex(0).SetApplicationId(make([]byte, 16)).ToMesg(nil),
		mesgdef.NewFieldDescription(nil).SetDeveloperDataIndex(0).SetFieldDefinitionNumber(0).
			SetFitBaseTypeId(basetype.Uint16).SetFieldName([]string{"target_power"}).SetUnits([]string{"watts"}).ToMesg(nil),
		{Num: mesgnum.DeviceInfo, Fields: []proto.Field{
			field(mesgnum.DeviceInfo, fieldnum.DeviceInfoTimestamp, ts),
			field(mesgnum.DeviceInfo, fieldnum.DeviceInfoSerialNumber, uint32(3999999999)),
		}},
		{Num: mesgnum.DeviceInfo, Fields: []proto.Field{
			field(mesgnum.DeviceInfo, fieldnum.DeviceInfoTimestamp, ts),
			field(mesgnum.DeviceInfo, fieldnum.DeviceInfoSerialNumber, uint32(1234)),
		}},
		record(0, homeLat, homeLong),
		record(1, homeLat+1000, homeLong+2000),
		record(2, basetype.Sint32Invalid, basetype.Sint32Invalid), // no GPS fix
		record(3, homeLat+3000, homeLong-500),
		// A proprietary message hiding the home position next to an unrelated value.
		{Num: proprietaryMesg, Fields: []proto.Field{
			unknownField(0, homeLat), unknownField(1, homeLong), unknownField(2, int32(42)),
		}},
		// A longitude-like value with no latitude beside it is left alone; unprofiled text is blanked.
		{Num: proprietaryMesg, Fields: []proto.Field{unknownField(1, homeLong), unknownField(3, "Lodi, IT")}},
		{Num: mesgnum.Length, Fields: []proto.Field{
			field(mesgnum.Length, fieldnum.LengthTimestamp, ts),
			field(mesgnum.Length, fieldnum.LengthStartTime, ts),
			field(mesgnum.Length, fieldnum.LengthTotalStrokes, uint16(18)),
		}},
		{Num: mesgnum.Set, Fields: []proto.Field{
			field(mesgnum.Set, fieldnum.SetTimestamp, ts),
			field(mesgnum.Set, fieldnum.SetRepetitions, uint16(10)),
		}},
		{Num: mesgnum.Lap, Fields: []proto.Field{
			field(mesgnum.Lap, fieldnum.LapTimestamp, ts),
			field(mesgnum.Lap, fieldnum.LapStartPositionLat, homeLat),
			field(mesgnum.Lap, fieldnum.LapStartPositionLong, homeLong),
			field(mesgnum.Lap, fieldnum.LapEndPositionLat, homeLat+3000),
			field(mesgnum.Lap, fieldnum.LapEndPositionLong, homeLong-500),
		}},
		{Num: mesgnum.Session, Fields: []proto.Field{
			field(mesgnum.Session, fieldnum.SessionTimestamp, ts),
			field(mesgnum.Session, fieldnum.SessionNecLat, homeLat+3000),
			field(mesgnum.Session, fieldnum.SessionNecLong, homeLong+2000),
			field(mesgnum.Session, fieldnum.SessionSwcLat, homeLat),
			field(mesgnum.Session, fieldnum.SessionSwcLong, homeLong-500),
		}},
	}}
	var buf bytes.Buffer
	if err := encoder.New(&buf).Encode(&fit); err != nil {
		t.Fatalf("encoding sample: %v", err)
	}
	return buf.Bytes()
}

func decode(t *testing.T, b []byte) *proto.FIT {
	t.Helper()
	fit, err := decoder.New(bytes.NewReader(b), decoder.WithNoComponentExpansion()).Decode()
	if err != nil {
		t.Fatalf("decoding: %v", err)
	}
	return fit
}

func TestAnonymize(t *testing.T) {
	t.Parallel()
	in := sample(t)
	var out bytes.Buffer
	st, err := anonymize(bytes.NewReader(in), &out)
	if err != nil {
		t.Fatalf("anonymize: %v", err)
	}
	if want := (stats{longitudes: 7, unprofiled: 1, serials: 3, texts: 1}); st != want {
		t.Errorf("stats = %+v, want %+v", st, want)
	}

	shift := semicircles.ToSemicircles(neutralLong) - homeLong
	before, after := decode(t, in), decode(t, out.Bytes())
	if len(before.Messages) != len(after.Messages) {
		t.Fatalf("got %d messages, want %d", len(after.Messages), len(before.Messages))
	}
	for i, want := range before.Messages {
		got := after.Messages[i]
		if got.Num != want.Num || len(got.Fields) != len(want.Fields) {
			t.Fatalf("message %d: got num %d with %d fields, want num %d with %d", i, got.Num, len(got.Fields), want.Num, len(want.Fields))
		}
		if !reflect.DeepEqual(values(got.DeveloperFields), values(want.DeveloperFields)) {
			t.Errorf("message %d: developer fields changed", i)
		}
		for j, wf := range want.Fields {
			gf := got.Fields[j]
			wantValue := wf.Value.Any()
			switch {
			case wf.Units == "semicircles" && !isLat(wf) && wf.Value.Int32() != basetype.Sint32Invalid,
				i == 9 && wf.Num == 1: // the longitude hidden in the proprietary message
				wantValue = wf.Value.Int32() + shift
			case i == 10 && wf.Num == 3:
				wantValue = ""
			case wf.Name == "serial_number":
				wantValue = map[uint32]uint32{3999999999: 1, 1234: 2}[wf.Value.Uint32()]
			}
			if gf.Num != wf.Num || !reflect.DeepEqual(gf.Value.Any(), wantValue) {
				t.Errorf("message %d (%d) field %s: got %v, want %v", i, want.Num, wf.Name, gf.Value.Any(), wantValue)
			}
		}
	}

	if got, want := after.Messages[5].Fields[2].Value.Int32(), semicircles.ToSemicircles(neutralLong); got != want {
		t.Errorf("first longitude = %d, want %d (%v°)", got, want, neutralLong)
	}
}

func values(fields []proto.DeveloperField) []any {
	out := make([]any, len(fields))
	for i, f := range fields {
		out[i] = []any{f.Num, f.DeveloperDataIndex, f.Value.Any()}
	}
	return out
}

func TestAnonymizeWithoutPositions(t *testing.T) {
	t.Parallel()
	fit := proto.FIT{Messages: []proto.Message{{Num: mesgnum.FileId, Fields: []proto.Field{
		field(mesgnum.FileId, fieldnum.FileIdType, uint8(typedef.FileActivity)),
		field(mesgnum.FileId, fieldnum.FileIdSerialNumber, uint32(77)),
	}}}}
	var in, out bytes.Buffer
	if err := encoder.New(&in).Encode(&fit); err != nil {
		t.Fatal(err)
	}
	st, err := anonymize(&in, &out)
	if err != nil {
		t.Fatalf("anonymize: %v", err)
	}
	if want := (stats{serials: 1}); st != want {
		t.Errorf("stats = %+v, want %+v", st, want)
	}
}

func TestRun(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	inPath, outPath := filepath.Join(dir, "in.fit"), filepath.Join(dir, "out.fit")
	if err := os.WriteFile(inPath, sample(t), 0o600); err != nil {
		t.Fatal(err)
	}
	garbage := filepath.Join(dir, "garbage.fit")
	if err := os.WriteFile(garbage, []byte("not a fit file at all"), 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		in, out string
		wantErr bool
	}{
		{name: "ok", in: inPath, out: outPath},
		{name: "missing input", in: filepath.Join(dir, "nope.fit"), out: outPath, wantErr: true},
		{name: "not a FIT file", in: garbage, out: outPath, wantErr: true},
		{name: "unwritable output", in: inPath, out: filepath.Join(dir, "no", "such", "dir.fit"), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := run(tt.in, tt.out)
			if (err != nil) != tt.wantErr {
				t.Fatalf("run() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
	// The "ok" case must have written a decodable file; later cases fail before writing.
	b, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	decode(t, b)
}
