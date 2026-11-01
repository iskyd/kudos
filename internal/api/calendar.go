package api

import (
	"context"
	"fmt"
	"math"
	"slices"

	"github.com/iskyd/kudos/internal/importer/ingest"
	"github.com/iskyd/kudos/internal/store/db"
)

const (
	// maxCalendarDays is six weeks, the most a month view shows.
	maxCalendarDays = 42
	// maxCalendarActivities bounds one range's activities, far above real use.
	maxCalendarActivities = 500
)

// GetCalendar returns the range's activities, check-ins and injury or illness entries.
func (s *Server) GetCalendar(ctx context.Context, req GetCalendarRequestObject) (GetCalendarResponseObject, error) {
	from, to := req.Params.From.Time, req.Params.To.Time
	switch days := int(to.Sub(from).Hours()/24) + 1; {
	case days < 1:
		return nil, &Error{Kind: KindInvalidRequest, Detail: "to must not be before from"}
	case days > maxCalendarDays:
		return nil, &Error{Kind: KindInvalidRequest, Detail: fmt.Sprintf("at most %d days", maxCalendarDays)}
	}
	userID := sessionFrom(ctx).userID
	rows, err := s.q.ListActivities(ctx, db.ListActivitiesParams{
		UserID: userID, Sort: "date", FromDay: &from, ToDay: &to,
		BeforeKey: math.MaxFloat64, BeforeID: math.MaxInt64, MaxRows: maxCalendarActivities,
	})
	if err != nil {
		return nil, fmt.Errorf("listing activities: %w", err)
	}
	wellness, err := s.q.ListWellnessDaysBetween(ctx, db.ListWellnessDaysBetweenParams{
		UserID: userID, FromDay: from, ToDay: to, MaxRows: maxCalendarDays,
	})
	if err != nil {
		return nil, fmt.Errorf("listing wellness days: %w", err)
	}
	log, err := s.q.ListHealthLogBetween(ctx, db.ListHealthLogBetweenParams{
		UserID: userID, FromDay: from, ToDay: to, MaxRows: maxPageSize,
	})
	if err != nil {
		return nil, fmt.Errorf("listing health log: %w", err)
	}

	out := GetCalendar200JSONResponse{
		Activities: make([]ActivitySummary, 0, len(rows)), Wellness: make([]WellnessDay, len(wellness)), HealthLog: make([]HealthLogEntry, len(log)),
	}
	slices.Reverse(rows) // newest first from the list query
	for _, r := range rows {
		a, err := ingest.Effective(r.Activity)
		if err != nil {
			return nil, err
		}
		out.Activities = append(out.Activities, activitySummary(a))
	}
	for i, d := range wellness {
		out.Wellness[i] = wellnessDay(d)
	}
	for i, e := range log {
		out.HealthLog[i] = healthLogEntry(e)
	}
	return out, nil
}
