package api

import (
	"context"
	"fmt"

	"github.com/iskyd/kudos/internal/analytics"
)

const defaultComparisonPoints = 1000

// GetActivityComparison aligns two of the user's activities on time or distance.
func (s *Server) GetActivityComparison(ctx context.Context, req GetActivityComparisonRequestObject) (GetActivityComparisonResponseObject, error) {
	p := req.Params
	axis := ComparisonAxisTime
	if p.Axis != nil {
		axis = *p.Axis
	}
	if !axis.Valid() {
		return nil, &Error{Kind: KindInvalidRequest, Detail: "axis must be time or distance"}
	}
	points := defaultComparisonPoints
	if p.Points != nil {
		points = *p.Points
	}
	if points < 2 || points > 5000 {
		return nil, &Error{Kind: KindInvalidRequest, Detail: "points must be 2 to 5000"}
	}
	var segs [2][][]analytics.Sample
	for i, id := range []int64{req.Id, p.With} {
		a, err := s.activity(ctx, id)
		if err != nil {
			return nil, err
		}
		v, err := s.ingest.View(ctx, a)
		if err != nil {
			return nil, fmt.Errorf("reading edits: %w", err)
		}
		if segs[i], err = s.ingest.Segments(ctx, sessionFrom(ctx).userID, v, maxDecimatedSamples); err != nil {
			return nil, fmt.Errorf("reading samples: %w", err)
		}
	}
	x, out := analytics.Align(segs[0], segs[1], analytics.Axis(axis), points)
	return GetActivityComparison200JSONResponse(ActivityComparison{
		Axis: axis, X: x, A: compared(out[0]), B: compared(out[1]),
	}), nil
}

// compared lays aligned values out as arrays; channels with no value are omitted.
func compared(vs []analytics.Aligned) ComparedStreams {
	var out ComparedStreams
	for _, ch := range []struct {
		read func(analytics.Aligned) *float64
		set  **[]*float64
	}{
		{func(v analytics.Aligned) *float64 { return v.ElapsedS }, &out.ElapsedS},
		{func(v analytics.Aligned) *float64 { return v.DistanceM }, &out.DistanceM},
		{func(v analytics.Aligned) *float64 { return v.HR }, &out.HrBpm},
		{func(v analytics.Aligned) *float64 { return v.Power }, &out.PowerW},
		{func(v analytics.Aligned) *float64 { return v.Cadence }, &out.Cadence},
		{func(v analytics.Aligned) *float64 { return v.Speed }, &out.SpeedMS},
		{func(v analytics.Aligned) *float64 { return v.Altitude }, &out.AltitudeM},
	} {
		col := make([]*float64, len(vs))
		recorded := false
		for i, v := range vs {
			col[i] = ch.read(v)
			recorded = recorded || col[i] != nil
		}
		if recorded {
			*ch.set = &col
		}
	}
	return out
}
