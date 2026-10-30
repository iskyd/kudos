package analytics

// LTTB picks at most threshold points of the series (xs[i], ys[i]) with the
// Largest-Triangle-Three-Buckets algorithm and returns their indices in order.
// It keeps the first and last points and, within each bucket, the point that
// best preserves the series' shape, so spikes and extremes survive. It is for
// display only: stored samples stay at full resolution.
//
// xs must be ascending and as long as ys. A series of threshold points or
// fewer is returned whole; a threshold below 3 keeps only the endpoints.
func LTTB(xs, ys []float64, threshold int) []int {
	n := len(ys)
	if n <= threshold || n <= 2 {
		all := make([]int, n)
		for i := range all {
			all[i] = i
		}
		return all
	}
	if threshold < 3 {
		return []int{0, n - 1}
	}

	out := make([]int, 0, threshold)
	out = append(out, 0)
	// The points between the endpoints are split into threshold-2 buckets.
	every := float64(n-2) / float64(threshold-2)
	a := 0
	for b := range threshold - 2 {
		// The average of the next bucket (or the last point) is the triangle's third vertex.
		nextStart := int(float64(b+1)*every) + 1
		nextEnd := min(int(float64(b+2)*every)+1, n)
		if b == threshold-3 {
			nextStart, nextEnd = n-1, n
		}
		var avgX, avgY float64
		for j := nextStart; j < nextEnd; j++ {
			avgX += xs[j]
			avgY += ys[j]
		}
		count := float64(nextEnd - nextStart)
		avgX, avgY = avgX/count, avgY/count

		start, end := int(float64(b)*every)+1, int(float64(b+1)*every)+1
		best, bestArea := start, -1.0
		for j := start; j < end; j++ {
			// Twice the triangle's area; the factor does not change the maximum.
			area := (xs[a]-avgX)*(ys[j]-ys[a]) - (xs[a]-xs[j])*(avgY-ys[a])
			if area < 0 {
				area = -area
			}
			if area > bestArea {
				best, bestArea = j, area
			}
		}
		out = append(out, best)
		a = best
	}
	return append(out, n-1)
}
