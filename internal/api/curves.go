package api

import (
	"context"
	"fmt"
	"math"

	"github.com/iskyd/kudos/internal/analytics"
	"github.com/iskyd/kudos/internal/store/db"
)

// maxCurveRows bounds the curve points read: one per curve and duration.
const maxCurveRows = 100

// GetCurves returns the power and pace curves of a period, made non-increasing
// in duration, with the thresholds they imply.
func (s *Server) GetCurves(ctx context.Context, req GetCurvesRequestObject) (GetCurvesResponseObject, error) {
	p := db.ListBestCurvePointsParams{UserID: sessionFrom(ctx).userID, MaxRows: maxCurveRows}
	if req.Params.From != nil {
		p.FromDay = &req.Params.From.Time
	}
	if req.Params.To != nil {
		p.ToDay = &req.Params.To.Time
	}
	if p.FromDay != nil && p.ToDay != nil && p.ToDay.Before(*p.FromDay) {
		return nil, &Error{Kind: KindInvalidRequest, Detail: "to must not be before from"}
	}
	rows, err := s.q.ListBestCurvePoints(ctx, p)
	if err != nil {
		return nil, fmt.Errorf("listing curve points: %w", err)
	}
	byKind := map[analytics.CurveKind][]db.ListBestCurvePointsRow{} // rows come by kind, then duration
	for _, r := range rows {
		byKind[analytics.CurveKind(r.Kind)] = append(byKind[analytics.CurveKind(r.Kind)], r)
	}
	var all []analytics.CurvePoint
	curve := func(kind analytics.CurveKind) []CurvePoint {
		rs := byKind[kind]
		values := make([]float64, len(rs))
		for i, r := range rs {
			values[i] = r.Value
		}
		out := make([]CurvePoint, len(rs))
		for i, from := range analytics.Envelope(values) {
			src := rs[from]
			out[i] = CurvePoint{
				DurationS: int(rs[i].DurationS), Value: src.Value, ActivityId: src.ActivityID,
				StartTime: src.StartTime, UtcOffsetS: int(src.UtcOffsetS),
			}
			all = append(all, analytics.CurvePoint{Kind: kind, DurationS: out[i].DurationS, Value: src.Value})
		}
		return out
	}
	out := GetCurves200JSONResponse{Power: curve(analytics.PowerCurve), Pace: curve(analytics.PaceCurve)}
	ftp, speed := analytics.Estimates(all)
	if ftp != nil {
		out.FtpW = new(int(math.Round(*ftp)))
	}
	out.ThresholdSpeedMS = speed
	return out, nil
}
