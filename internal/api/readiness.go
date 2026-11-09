package api

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/iskyd/kudos/internal/analytics"
	"github.com/iskyd/kudos/internal/store/db"
)

// maxConditions bounds the health log entries read for one day's cap.
const maxConditions = 100

// GetReadiness scores a day from daily metrics, form, the check-in and the
// health log.
func (s *Server) GetReadiness(ctx context.Context, req GetReadinessRequestObject) (GetReadinessResponseObject, error) {
	r, err := s.readiness(ctx, sessionFrom(ctx).userID, req.Params.Day.Time)
	if err != nil {
		return nil, err
	}
	out := GetReadiness200JSONResponse{
		Day: req.Params.Day, Status: CollectingBaseline, BaselineDays: r.BaselineDays,
		RequiredDays: analytics.ReadinessMinBaselineDays, Components: []ReadinessComponent{},
	}
	if !r.Ready {
		return out, nil
	}
	out.Status = Ready
	out.Score, out.UncappedScore = new(int(r.Score)), new(int(r.Uncapped))
	for _, c := range r.Components {
		out.Components = append(out.Components, ReadinessComponent{
			Name: ReadinessComponentName(c.Name), Score: c.Score, Share: c.Share, Value: c.Value, Baseline: c.Baseline,
		})
	}
	if c := r.Cap; c != nil {
		out.Cap = &ReadinessCap{EntryId: c.ID, Kind: HealthLogKind(c.Kind), Max: int(math.Round(c.Max))}
	}
	return out, nil
}

// readiness scores the user's day.
func (s *Server) readiness(ctx context.Context, userID int64, day time.Time) (analytics.Readiness, error) {
	metrics, err := s.q.ListDailyMetrics(ctx, db.ListDailyMetricsParams{
		UserID: userID, After: day.AddDate(0, 0, -analytics.ReadinessBaselineDays-1), Until: day,
		MaxRows: analytics.ReadinessBaselineDays + 1,
	})
	if err != nil {
		return analytics.Readiness{}, fmt.Errorf("listing daily metrics: %w", err)
	}
	in := analytics.ReadinessInput{Day: day, Health: make([]analytics.HealthDay, len(metrics))}
	for i, m := range metrics {
		in.Health[i] = analytics.HealthDay{
			Day: m.Day, HRV: f64(m.HrvMs), RestingHR: i16f(m.RestingHrBpm), SleepS: i32f(m.SleepS), SleepScore: i16f(m.SleepScore),
		}
	}
	fitness, err := s.fitnessDays(ctx, userID, day, day)
	if err != nil {
		return analytics.Readiness{}, err
	}
	in.TSB = fitness[0].TSB
	switch w, err := s.q.GetWellnessDay(ctx, db.GetWellnessDayParams{UserID: userID, Day: day}); {
	case err == nil:
		in.CheckIn = &analytics.CheckIn{
			Soreness: i16f(w.Soreness), SleepQuality: i16f(w.SleepQuality), Stress: i16f(w.Stress), Motivation: i16f(w.Motivation),
		}
	case !errors.Is(err, pgx.ErrNoRows):
		return analytics.Readiness{}, fmt.Errorf("getting check-in: %w", err)
	}
	entries, err := s.q.ListHealthLogOn(ctx, db.ListHealthLogOnParams{UserID: userID, Day: day, MaxRows: maxConditions})
	if err != nil {
		return analytics.Readiness{}, fmt.Errorf("listing health log: %w", err)
	}
	for _, e := range entries {
		in.Conditions = append(in.Conditions, analytics.Condition{
			ID: e.ID, Kind: analytics.ConditionKind(e.Kind), Start: e.StartDate, End: e.EndDate,
		})
	}

	return analytics.ComputeReadiness(in), nil
}

func i32f(v *int32) *float64 {
	if v == nil {
		return nil
	}
	return new(float64(*v))
}
