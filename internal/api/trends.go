package api

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/iskyd/kudos/internal/store/db"
)

// trendCursor is the position after a page's last activity.
type trendCursor struct {
	Start time.Time `json:"s"`
	ID    int64     `json:"i"`
}

// ListActivityTrends returns one page of runs' or pool swims' analytics, newest first.
func (s *Server) ListActivityTrends(ctx context.Context, req ListActivityTrendsRequestObject) (ListActivityTrendsResponseObject, error) {
	p := req.Params
	invalid := func(detail string) (ListActivityTrendsResponseObject, error) {
		return nil, &Error{Kind: KindInvalidRequest, Detail: detail}
	}
	if !p.Kind.Valid() {
		return invalid("kind must be running or swimming")
	}
	limit := defaultPageSize
	if p.Limit != nil {
		limit = *p.Limit
	}
	if limit < 1 || limit > maxPageSize {
		return invalid(fmt.Sprintf("limit must be 1 to %d", maxPageSize))
	}
	args := db.ListActivityTrendsParams{
		UserID: sessionFrom(ctx).userID, Kind: string(p.Kind),
		BeforeTime: time.Date(9999, 12, 31, 0, 0, 0, 0, time.UTC), BeforeID: math.MaxInt64,
		MaxRows: int32(limit + 1), // one extra row tells whether another page follows
	}
	if p.From != nil {
		args.FromDay = &p.From.Time
	}
	if p.To != nil {
		args.ToDay = &p.To.Time
	}
	if p.From != nil && p.To != nil && p.To.Before(p.From.Time) {
		return invalid("to must not be before from")
	}
	if p.Cursor != nil {
		var c trendCursor
		if err := s.cursors.Decode(*p.Cursor, &c); err != nil {
			return nil, err
		}
		args.BeforeTime, args.BeforeID = c.Start, c.ID
	}
	rows, err := s.q.ListActivityTrends(ctx, args)
	if err != nil {
		return nil, fmt.Errorf("listing activity trends: %w", err)
	}
	page := ActivityTrendPage{Items: []ActivityTrend{}}
	if len(rows) > limit {
		rows = rows[:limit]
		last := rows[limit-1]
		next, err := s.cursors.Encode(trendCursor{Start: last.StartTime, ID: last.ID})
		if err != nil {
			return nil, err
		}
		page.NextCursor = &next
	}
	for _, r := range rows {
		page.Items = append(page.Items, ActivityTrend{
			ActivityId: r.ID, StartTime: r.StartTime, UtcOffsetS: int(r.UtcOffsetS), Sport: ActivitySport(r.Sport),
			AvgSpeedMS: r.AvgSpeedMS, AvgHrBpm: r.AvgHrBpm, CadenceSpm: r.AvgCadence,
			EfficiencyMPerBeat: r.EfficiencyMPerBeat, DecouplingPct: r.DecouplingPct,
			VerticalOscillationM: r.AvgVerticalOscillationM, StanceTimeS: r.AvgStanceTimeS, StepLengthM: r.AvgStepLengthM,
			Swolf: r.AvgSwolf, PoolLengthM: f64(r.PoolLengthM),
		})
	}
	return ListActivityTrends200JSONResponse(page), nil
}
