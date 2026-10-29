package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRun(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write := func(name, content string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	gpx := write("in.GPX", `<trkpt lat="1" lon="2"/>`)
	out := filepath.Join(dir, "out")

	tests := []struct {
		name    string
		in, out string
		wantErr bool
	}{
		{name: "fit", in: "../../testdata/fit/hike.fit", out: out},
		{name: "gpx", in: gpx, out: out},
		{name: "missing input", in: filepath.Join(dir, "nope.fit"), out: out, wantErr: true},
		{name: "not a FIT file", in: write("garbage.fit", "not a fit file at all"), out: out, wantErr: true},
		{name: "bad tcx", in: write("bad.tcx", "<LongitudeDegrees>x</LongitudeDegrees>"), out: out, wantErr: true},
		{name: "unwritable output", in: gpx, out: filepath.Join(dir, "no", "such", "dir"), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := run(tt.in, tt.out); (err != nil) != tt.wantErr {
				t.Fatalf("run() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
	// The gpx case ran last among the successes: its longitude moved.
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), `lon="2"`) {
		t.Errorf("longitude not shifted: %s", b)
	}
}
