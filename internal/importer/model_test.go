package importer

import "testing"

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
