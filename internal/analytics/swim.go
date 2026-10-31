package analytics

import (
	"math"

	"github.com/iskyd/kudos/internal/importer"
)

// CSS is critical swim speed (m/s) from the fastest 400 m and 200 m in seconds
// (Swim Smooth's CSS test, after Wakayoshi's critical speed): the 200 m between
// them over the time between them. None unless the 400 m is slower.
func CSS(t200, t400 float64) *float64 {
	if t200 <= 0 || t400 <= t200 {
		return nil
	}
	return new(200 / (t400 - t200))
}

// AvgSWOLF is the mean SWOLF of the active lengths that have one, as watches
// show it; none without such lengths.
func AvgSWOLF(lengths []importer.Length) *float64 {
	var sum float64
	n := 0
	for _, l := range lengths {
		if v := l.SWOLF(); v != nil {
			sum += *v
			n++
		}
	}
	if n == 0 {
		return nil
	}
	return new(sum / float64(n))
}

// fastestLengths is the shortest time over metres of consecutive active
// lengths of a pool, an idle length (rest) breaking the run; none when metres is
// not a whole number of lengths. A length's time is its timer time, else its
// elapsed time; a length with neither breaks the run.
func fastestLengths(lengths []importer.Length, pool, metres float64) (seconds float64, ok bool) {
	if pool <= 0 {
		return 0, false
	}
	n := int(math.Round(metres / pool))
	if n == 0 || math.Abs(float64(n)*pool-metres) > 0.5 {
		return 0, false
	}
	best := math.Inf(1)
	var window []float64
	var sum float64
	for _, l := range lengths {
		t := l.TimerTime
		if t == nil {
			t = l.ElapsedTime
		}
		if !l.Active || t == nil {
			window, sum = nil, 0
			continue
		}
		window = append(window, *t)
		sum += *t
		if len(window) > n {
			sum -= window[0]
			window = window[1:]
		}
		if len(window) == n {
			best = math.Min(best, sum)
		}
	}
	return best, !math.IsInf(best, 1)
}
