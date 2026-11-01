package api

import (
	"context"
	"fmt"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/iskyd/kudos/internal/store/db"
)

// GetVolume returns volume per period and the range's time in zones.
func (s *Server) GetVolume(ctx context.Context, req GetVolumeRequestObject) (GetVolumeResponseObject, error) {
	p := req.Params
	invalid := func(detail string) (GetVolumeResponseObject, error) {
		return nil, &Error{Kind: KindInvalidRequest, Detail: detail}
	}
	if !p.Period.Valid() {
		return invalid("period must be week, month or year")
	}
	switch days := int(p.To.Sub(p.From.Time).Hours()/24) + 1; {
	case days < 1:
		return invalid("to must not be before from")
	case days > maxFitnessDays:
		return invalid(fmt.Sprintf("at most %d days", maxFitnessDays))
	}
	if p.Sport != nil && !p.Sport.Valid() {
		return invalid("unknown sport")
	}
	userID := sessionFrom(ctx).userID
	rows, err := s.q.ListVolume(ctx, db.ListVolumeParams{
		UserID: userID, Period: string(p.Period), FromDay: p.From.Time, ToDay: p.To.Time,
		Sport: (*string)(p.Sport), GearID: p.Gear,
		MaxRows: maxFitnessDays/7 + 2, // weeks are the most periods a range holds
	})
	if err != nil {
		return nil, fmt.Errorf("listing volume: %w", err)
	}
	zones, err := s.q.SumTimeInZones(ctx, db.SumTimeInZonesParams{
		UserID: userID, FromDay: p.From.Time, ToDay: p.To.Time, Sport: (*string)(p.Sport), GearID: p.Gear,
	})
	if err != nil {
		return nil, fmt.Errorf("summing time in zones: %w", err)
	}
	out := GetVolume200JSONResponse{Periods: make([]VolumePeriod, len(rows)), TimeInZones: []ZoneTime{}}
	for i, r := range rows {
		out.Periods[i] = VolumePeriod{
			Start: openapi_types.Date{Time: r.Start}, Activities: int(r.Activities),
			DistanceM: r.DistanceM, DurationS: r.DurationS, AscentM: r.AscentM, Load: r.Load,
		}
	}
	// Rows come by metric, model and zone: each metric and model's zones in order.
	for _, z := range zones {
		n := len(out.TimeInZones)
		if n == 0 || string(out.TimeInZones[n-1].Metric) != z.Metric || out.TimeInZones[n-1].Model != z.Model {
			out.TimeInZones = append(out.TimeInZones, ZoneTime{Metric: ZoneTimeMetric(z.Metric), Model: z.Model})
			n++
		}
		out.TimeInZones[n-1].Seconds = append(out.TimeInZones[n-1].Seconds, z.Seconds)
	}
	return out, nil
}
