package api

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/iskyd/kudos/internal/importer/ingest"
	"github.com/iskyd/kudos/internal/plans"
	"github.com/iskyd/kudos/internal/store/db"
)

const (
	// maxPlannedWorkouts bounds one range's planned workouts, far above real use.
	maxPlannedWorkouts = 200
	// maxMatchActivities bounds the activities matched against a range's plan.
	// shortcut: past 500 activities in a range the rest go unmatched; page the read if one appears.
	maxMatchActivities = 500
)

var errPlannedWorkoutNotFound = &Error{Kind: KindNotFound, Detail: "no such planned workout"}

// plannedWorkout converts a stored workout with its completion; its steps were
// validated before they were stored.
func plannedWorkout(w db.PlannedWorkout, m matched) (PlannedWorkout, error) {
	out := PlannedWorkout{
		Id: w.ID, PlanId: w.PlanID, Day: openapi_types.Date{Time: w.Day}, Sport: ActivitySport(w.Sport),
		Title: w.Title, Description: w.Description, Link: PlannedWorkoutLinkAuto,
	}
	if m.manual[w.ID] {
		out.Link = PlannedWorkoutLinkManual
	}
	if c, ok := m.done[w.ID]; ok {
		out.ActivityId = &c.ActivityID
	}
	if err := json.Unmarshal(w.Steps, &out.Steps); err != nil {
		return PlannedWorkout{}, fmt.Errorf("reading the steps of planned workout %d: %w", w.ID, err)
	}
	return out, nil
}

// matched is planned workouts with what completed them.
type matched struct {
	done       map[int64]plans.Completion
	manual     map[int64]bool        // workouts whose link the user set
	activities map[int64]db.Activity // the days' activities, effective, by ID
}

// match pairs the user's planned workouts with the activities of their days
// (plans.Match), the user's links first.
func (s *Server) match(ctx context.Context, userID int64, rows []db.PlannedWorkout) (matched, error) {
	m := matched{done: map[int64]plans.Completion{}, manual: map[int64]bool{}, activities: map[int64]db.Activity{}}
	if len(rows) == 0 {
		return m, nil
	}
	from := slices.MinFunc(rows, func(a, b db.PlannedWorkout) int { return a.Day.Compare(b.Day) }).Day
	to := slices.MaxFunc(rows, func(a, b db.PlannedWorkout) int { return a.Day.Compare(b.Day) }).Day
	acts, err := s.q.ListActivities(ctx, db.ListActivitiesParams{
		UserID: userID, Sort: "date", FromDay: &from, ToDay: &to,
		BeforeKey: math.MaxFloat64, BeforeID: math.MaxInt64, MaxRows: maxMatchActivities,
	})
	if err != nil {
		return matched{}, fmt.Errorf("listing activities to match: %w", err)
	}
	done := make([]plans.Done, 0, len(acts))
	actIDs := make([]int64, 0, len(acts))
	for _, row := range acts {
		a, err := ingest.Effective(row.Activity)
		if err != nil {
			return matched{}, err
		}
		m.activities[a.ID] = a
		actIDs = append(actIDs, a.ID)
		d := plans.Done{ID: a.ID, Day: localDay(a.StartTime, a.UtcOffsetS).Format(time.DateOnly), Sport: a.Sport}
		if t := cmp.Or(a.TimerTimeS, a.ElapsedTimeS); t != nil {
			d.DurationS = *t
		}
		if a.DistanceM != nil {
			d.DistanceM = *a.DistanceM
		}
		done = append(done, d)
	}
	names, err := s.q.ListRecordedWorkoutNames(ctx, db.ListRecordedWorkoutNamesParams{UserID: userID, ActivityIds: actIDs, MaxRows: maxMatchActivities})
	if err != nil {
		return matched{}, fmt.Errorf("listing recorded workout names: %w", err)
	}
	for _, n := range names {
		if i := slices.IndexFunc(done, func(d plans.Done) bool { return d.ID == n.ActivityID }); i >= 0 {
			done[i].WorkoutName = deref(n.Name)
		}
	}

	planned := make([]plans.Planned, len(rows))
	ids := make([]int64, len(rows))
	for i, w := range rows {
		ids[i] = w.ID
		planned[i] = plans.Planned{ID: w.ID, Day: w.Day.Format(time.DateOnly), Sport: w.Sport, Title: w.Title}
		if planned[i].DurationS, planned[i].DistanceM, err = plans.Totals(w.Steps); err != nil {
			return matched{}, err
		}
	}
	stored, err := s.q.ListPlannedWorkoutLinks(ctx, db.ListPlannedWorkoutLinksParams{
		UserID: userID, PlannedWorkoutIds: ids, ActivityIds: actIDs, MaxRows: maxPlannedWorkouts + maxMatchActivities,
	})
	if err != nil {
		return matched{}, fmt.Errorf("listing planned workout links: %w", err)
	}
	links := make([]plans.Link, len(stored))
	for i, l := range stored {
		links[i] = plans.Link{PlannedID: l.PlannedWorkoutID, ActivityID: l.ActivityID}
		m.manual[l.PlannedWorkoutID] = true
	}
	auto, err := s.q.GetAutoMatchWorkouts(ctx, userID)
	if err != nil {
		return matched{}, fmt.Errorf("reading plan settings: %w", err)
	}
	m.done = plans.Match(planned, done, links, auto)
	return m, nil
}

// plannedWorkoutsBetween lists the user's workouts planned from from to to.
func (s *Server) plannedWorkoutsBetween(ctx context.Context, userID int64, from, to openapi_types.Date) ([]PlannedWorkout, error) {
	rows, err := s.q.ListPlannedWorkoutsBetween(ctx, db.ListPlannedWorkoutsBetweenParams{
		UserID: userID, FromDay: from.Time, ToDay: to.Time, MaxRows: maxPlannedWorkouts,
	})
	if err != nil {
		return nil, fmt.Errorf("listing planned workouts: %w", err)
	}
	m, err := s.match(ctx, userID, rows)
	if err != nil {
		return nil, err
	}
	out := make([]PlannedWorkout, len(rows))
	for i, r := range rows {
		if out[i], err = plannedWorkout(r, m); err != nil {
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

// onePlannedWorkout returns a planned workout with its completion.
func (s *Server) onePlannedWorkout(ctx context.Context, id int64) (PlannedWorkout, error) {
	userID := sessionFrom(ctx).userID
	w, err := s.q.GetPlannedWorkout(ctx, db.GetPlannedWorkoutParams{UserID: userID, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return PlannedWorkout{}, errPlannedWorkoutNotFound
	}
	if err != nil {
		return PlannedWorkout{}, fmt.Errorf("getting planned workout: %w", err)
	}
	// Matching runs over the whole day: the day's other workouts take their activities.
	day := openapi_types.Date{Time: w.Day}
	all, err := s.plannedWorkoutsBetween(ctx, userID, day, day)
	if err != nil {
		return PlannedWorkout{}, err
	}
	for _, p := range all {
		if p.Id == id {
			return p, nil
		}
	}
	return PlannedWorkout{}, errPlannedWorkoutNotFound // deleted meanwhile
}

// GetPlannedWorkout returns one planned workout with its steps.
func (s *Server) GetPlannedWorkout(ctx context.Context, req GetPlannedWorkoutRequestObject) (GetPlannedWorkoutResponseObject, error) {
	out, err := s.onePlannedWorkout(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	return GetPlannedWorkout200JSONResponse(out), nil
}

// LinkPlannedWorkout sets the activity that completed a planned workout, or none.
func (s *Server) LinkPlannedWorkout(ctx context.Context, req LinkPlannedWorkoutRequestObject) (LinkPlannedWorkoutResponseObject, error) {
	userID := sessionFrom(ctx).userID
	if _, err := s.q.GetPlannedWorkout(ctx, db.GetPlannedWorkoutParams{UserID: userID, ID: req.Id}); errors.Is(err, pgx.ErrNoRows) {
		return nil, errPlannedWorkoutNotFound
	} else if err != nil {
		return nil, fmt.Errorf("getting planned workout: %w", err)
	}
	if id := req.Body.ActivityId; id != nil {
		if _, err := s.activity(ctx, *id); err != nil {
			return nil, err
		}
	}
	_, err := s.q.PutPlannedWorkoutLink(ctx, db.PutPlannedWorkoutLinkParams{UserID: userID, PlannedWorkoutID: req.Id, ActivityID: req.Body.ActivityId})
	if pgErr := (*pgconn.PgError)(nil); errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
		return nil, &Error{Kind: KindActivityLinked, Detail: "the activity already completes another planned workout"}
	}
	if err != nil {
		return nil, fmt.Errorf("linking a planned workout: %w", err)
	}
	out, err := s.onePlannedWorkout(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	return LinkPlannedWorkout200JSONResponse(out), nil
}

// UnsetPlannedWorkoutLink drops the user's link: automatic matching decides again.
func (s *Server) UnsetPlannedWorkoutLink(ctx context.Context, req UnsetPlannedWorkoutLinkRequestObject) (UnsetPlannedWorkoutLinkResponseObject, error) {
	if _, err := s.q.DeletePlannedWorkoutLink(ctx, db.DeletePlannedWorkoutLinkParams{UserID: sessionFrom(ctx).userID, PlannedWorkoutID: req.Id}); err != nil {
		return nil, fmt.Errorf("unlinking a planned workout: %w", err)
	}
	out, err := s.onePlannedWorkout(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	return UnsetPlannedWorkoutLink200JSONResponse(out), nil
}

// GetPlanSettings returns the user's plan settings.
func (s *Server) GetPlanSettings(ctx context.Context, _ GetPlanSettingsRequestObject) (GetPlanSettingsResponseObject, error) {
	auto, err := s.q.GetAutoMatchWorkouts(ctx, sessionFrom(ctx).userID)
	if err != nil {
		return nil, fmt.Errorf("reading plan settings: %w", err)
	}
	return GetPlanSettings200JSONResponse{AutoMatch: auto}, nil
}

// PutPlanSettings changes the user's plan settings.
func (s *Server) PutPlanSettings(ctx context.Context, req PutPlanSettingsRequestObject) (PutPlanSettingsResponseObject, error) {
	if err := s.q.SetAutoMatchWorkouts(ctx, db.SetAutoMatchWorkoutsParams{ID: sessionFrom(ctx).userID, AutoMatch: req.Body.AutoMatch}); err != nil {
		return nil, fmt.Errorf("saving plan settings: %w", err)
	}
	return PutPlanSettings200JSONResponse{AutoMatch: req.Body.AutoMatch}, nil
}
