package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/iskyd/kudos/internal/store/db"
)

// maxPlannedWorkouts bounds one range's planned workouts, far above real use.
const maxPlannedWorkouts = 200

var errPlannedWorkoutNotFound = &Error{Kind: KindNotFound, Detail: "no such planned workout"}

// plannedWorkout converts a stored workout; its steps were validated before
// they were stored.
func plannedWorkout(w db.PlannedWorkout) (PlannedWorkout, error) {
	out := PlannedWorkout{
		Id: w.ID, PlanId: w.PlanID, Day: openapi_types.Date{Time: w.Day}, Sport: ActivitySport(w.Sport),
		Title: w.Title, Description: w.Description,
	}
	if err := json.Unmarshal(w.Steps, &out.Steps); err != nil {
		return PlannedWorkout{}, fmt.Errorf("reading the steps of planned workout %d: %w", w.ID, err)
	}
	return out, nil
}

// plannedWorkoutsBetween lists the user's workouts planned from from to to.
func (s *Server) plannedWorkoutsBetween(ctx context.Context, userID int64, from, to openapi_types.Date) ([]PlannedWorkout, error) {
	rows, err := s.q.ListPlannedWorkoutsBetween(ctx, db.ListPlannedWorkoutsBetweenParams{
		UserID: userID, FromDay: from.Time, ToDay: to.Time, MaxRows: maxPlannedWorkouts,
	})
	if err != nil {
		return nil, fmt.Errorf("listing planned workouts: %w", err)
	}
	out := make([]PlannedWorkout, len(rows))
	for i, r := range rows {
		if out[i], err = plannedWorkout(r); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// ListPlannedWorkouts returns the workouts planned in a range of at most 42 days.
func (s *Server) ListPlannedWorkouts(ctx context.Context, req ListPlannedWorkoutsRequestObject) (ListPlannedWorkoutsResponseObject, error) {
	from, to := req.Params.From, req.Params.To
	switch days := int(to.Sub(from.Time).Hours()/24) + 1; {
	case days < 1:
		return nil, &Error{Kind: KindInvalidRequest, Detail: "to must not be before from"}
	case days > maxCalendarDays:
		return nil, &Error{Kind: KindInvalidRequest, Detail: fmt.Sprintf("at most %d days", maxCalendarDays)}
	}
	out, err := s.plannedWorkoutsBetween(ctx, sessionFrom(ctx).userID, from, to)
	if err != nil {
		return nil, err
	}
	return ListPlannedWorkouts200JSONResponse(out), nil
}

// GetPlannedWorkout returns one planned workout with its steps.
func (s *Server) GetPlannedWorkout(ctx context.Context, req GetPlannedWorkoutRequestObject) (GetPlannedWorkoutResponseObject, error) {
	w, err := s.q.GetPlannedWorkout(ctx, db.GetPlannedWorkoutParams{UserID: sessionFrom(ctx).userID, ID: req.Id})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errPlannedWorkoutNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("getting planned workout: %w", err)
	}
	out, err := plannedWorkout(w)
	if err != nil {
		return nil, err
	}
	return GetPlannedWorkout200JSONResponse(out), nil
}
