package api

import (
	"context"
	"fmt"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/iskyd/kudos/internal/analytics"
	"github.com/iskyd/kudos/internal/store/db"
)

const (
	// maxFitnessDays bounds one request's range.
	maxFitnessDays = 3660
	// fitnessWarmUpDays of history before from feed the recurrences: (41/42)^365
	// leaves earlier loads under 0.02% of their weight.
	fitnessWarmUpDays = 365
)

// GetFitness returns daily load with fitness, fatigue and form.
func (s *Server) GetFitness(ctx context.Context, req GetFitnessRequestObject) (GetFitnessResponseObject, error) {
	from, to := req.Params.From.Time, req.Params.To.Time
	switch days := int(to.Sub(from).Hours()/24) + 1; {
	case days < 1:
		return nil, &Error{Kind: KindInvalidRequest, Detail: "to must not be before from"}
	case days > maxFitnessDays:
		return nil, &Error{Kind: KindInvalidRequest, Detail: fmt.Sprintf("at most %d days", maxFitnessDays)}
	}
	start := from.AddDate(0, 0, -fitnessWarmUpDays)
	rows, err := s.q.ListDailyLoads(ctx, db.ListDailyLoadsParams{
		UserID: sessionFrom(ctx).userID, FromDay: start, ToDay: to, MaxRows: maxFitnessDays + fitnessWarmUpDays,
	})
	if err != nil {
		return nil, fmt.Errorf("listing daily loads: %w", err)
	}
	loads := make([]analytics.DayLoad, len(rows))
	for i, r := range rows {
		loads[i] = analytics.DayLoad{Day: r.Day, Load: r.Load}
	}
	// Starting every curve a fixed warm-up before from keeps a day's values the
	// same whichever range asks for it.
	days := analytics.Fitness(loads, start, to)[fitnessWarmUpDays:]
	out := make(GetFitness200JSONResponse, len(days))
	for i, d := range days {
		out[i] = FitnessDay{Day: openapi_types.Date{Time: d.Day}, Load: d.Load, Ctl: d.CTL, Atl: d.ATL, Tsb: d.TSB}
	}
	return out, nil
}
