package importer

import (
	"math"
	"testing"
	"time"
)

func TestLengthSWOLF(t *testing.T) {
	t.Parallel()
	f := func(v float64) *float64 { return &v }
	tests := []struct {
		name   string
		length Length
		want   *float64
	}{
		{name: "active", length: Length{Active: true, TimerTime: f(72.75), Strokes: f(31)}, want: f(103.75)},
		{name: "idle", length: Length{Active: false, TimerTime: f(45.375)}, want: nil},
		{name: "no strokes", length: Length{Active: true, TimerTime: f(70)}, want: nil},
		{name: "no time", length: Length{Active: true, Strokes: f(30)}, want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.length.SWOLF()
			if (got == nil) != (tt.want == nil) || (got != nil && *got != *tt.want) {
				t.Errorf("SWOLF() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSportInStrides(t *testing.T) {
	t.Parallel()
	for sport, want := range map[Sport]bool{
		SportRunning: true, SportTrailRunning: true, SportHiking: true,
		SportCycling: false, SportPoolSwimming: false, SportOther: false,
	} {
		if got := sport.InStrides(); got != want {
			t.Errorf("%s.InStrides() = %v, want %v", sport, got, want)
		}
	}
}

func TestFinite(t *testing.T) {
	t.Parallel()
	f := func(v float64) *float64 { return &v }
	for _, v := range []*float64{nil, f(math.NaN()), f(math.Inf(1)), f(math.Inf(-1))} {
		if Finite(v) != nil {
			t.Errorf("Finite(%v) is not nil", v)
		}
	}
	if got := Finite(f(1.5)); got == nil || *got != 1.5 {
		t.Errorf("Finite(1.5) = %v", got)
	}
}

func TestPlausibleTime(t *testing.T) {
	t.Parallel()
	for in, want := range map[time.Time]bool{
		time.Date(2026, 10, 29, 7, 0, 0, 0, time.UTC):                     true,
		time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC):                       true,
		time.Date(1969, 12, 31, 23, 59, 59, 0, time.UTC):                  false,
		time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC):                       false,
		time.Date(0, 1, 1, 0, 0, 0, 0, time.FixedZone("", 3600)):          false,
		time.Date(9999, 12, 31, 23, 0, 0, 0, time.FixedZone("", -2*3600)): false,
	} {
		if got := PlausibleTime(in); got != want {
			t.Errorf("PlausibleTime(%v) = %v, want %v", in, got, want)
		}
	}
}
