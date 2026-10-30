package analytics

import (
	"math"
	"reflect"
	"testing"
)

func seq(n int) []float64 {
	xs := make([]float64, n)
	for i := range xs {
		xs[i] = float64(i)
	}
	return xs
}

func TestLTTBSmallInputs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		ys        []float64
		threshold int
		want      []int
	}{
		{"empty", nil, 10, []int{}},
		{"one point", []float64{5}, 1, []int{0}},
		{"two points", []float64{5, 6}, 1, []int{0, 1}},
		{"fits", []float64{1, 2, 3}, 3, []int{0, 1, 2}},
		{"fits loosely", []float64{1, 2, 3}, 100, []int{0, 1, 2}},
		{"threshold 2 keeps the endpoints", []float64{1, 9, 2, 3}, 2, []int{0, 3}},
		{"threshold 0 keeps the endpoints", []float64{1, 9, 2, 3}, 0, []int{0, 3}},
		// One bucket (1..3) against the last point: the spike at 2 makes the largest triangle.
		{"one bucket", []float64{0, 1, 10, 1, 0}, 3, []int{0, 2, 4}},
		// Buckets [1,2] and [3,4]: 9 at 2 and -9 at 3 are kept.
		{"two buckets", []float64{0, 1, 9, -9, 1, 0}, 4, []int{0, 2, 3, 5}},
	}
	for _, tt := range tests {
		if got := LTTB(seq(len(tt.ys)), tt.ys, tt.threshold); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%s: %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestLTTBKeepsEndpointsAndExtremes(t *testing.T) {
	t.Parallel()
	// An hour of smooth heart rate at uneven sampling with one spike and one dip.
	n := 3600
	xs, ys := make([]float64, n), make([]float64, n)
	for i := range n {
		xs[i] = float64(i) * 1.5
		ys[i] = 140 + 10*math.Sin(float64(i)/300)
	}
	ys[1234], ys[2711] = 199, 61

	got := LTTB(xs, ys, 200)
	if len(got) != 200 || got[0] != 0 || got[len(got)-1] != n-1 {
		t.Fatalf("%d points from %d to %d", len(got), got[0], got[len(got)-1])
	}
	kept := map[int]bool{}
	for i, idx := range got {
		if i > 0 && idx <= got[i-1] {
			t.Fatalf("indices not ascending at %d: %v", i, got[i-1:i+1])
		}
		kept[idx] = true
	}
	if !kept[1234] || !kept[2711] {
		t.Errorf("spike kept %v, dip kept %v", kept[1234], kept[2711])
	}
}
