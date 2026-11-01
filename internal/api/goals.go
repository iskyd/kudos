package api

import (
	"context"
	"fmt"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/iskyd/kudos/internal/analytics"
	"github.com/iskyd/kudos/internal/store/db"
)

var errGoalNotFound = &Error{Kind: KindNotFound, Detail: "no such goal"}

// goalCursor is the position after a page's last (oldest) goal.
type goalCursor struct {
	ID int64 `json:"i"`
}

// periodBounds returns the first and last days of the week (from Monday),
// month or year holding day.
func periodBounds(period string, day time.Time) (first, last time.Time) {
	y, m, d := day.Date()
	switch period {
	case "week":
		first = time.Date(y, m, d-(int(day.Weekday())+6)%7, 0, 0, 0, 0, time.UTC)
		return first, first.AddDate(0, 0, 6)
	case "month":
		first = time.Date(y, m, 1, 0, 0, 0, 0, time.UTC)
		return first, first.AddDate(0, 1, -1)
	default:
		first = time.Date(y, 1, 1, 0, 0, 0, 0, time.UTC)
		return first, first.AddDate(1, 0, -1)
	}
}

// goalProgress sums the goal's volume so far and projects it as of today.
func (s *Server) goalProgress(ctx context.Context, g db.Goal, today time.Time) (Goal, error) {
	first, last := periodBounds(g.Period, g.StartDay)
	rows, err := s.q.ListVolume(ctx, db.ListVolumeParams{
		UserID: g.UserID, Period: g.Period, FromDay: first, ToDay: last, Sport: g.Sport, MaxRows: 1,
	})
	if err != nil {
		return Goal{}, fmt.Errorf("summing goal volume: %w", err)
	}
	var value float64
	if len(rows) > 0 {
		value = map[string]float64{"distance": rows[0].DistanceM, "duration": rows[0].DurationS, "ascent": rows[0].AscentM}[g.Metric]
	}
	p := analytics.ProjectGoal(g.Target, value, first, last, today)
	out := Goal{
		Id: g.ID, Period: GoalPeriod(g.Period), Start: openapi_types.Date{Time: first}, End: openapi_types.Date{Time: last},
		Sport: (*ActivitySport)(g.Sport), Metric: GoalMetric(g.Metric), Target: g.Target,
		Value: value, Projected: p.Projected, Status: GoalStatus(p.Status),
	}
	if p.Finish != nil {
		out.ProjectedFinish = &openapi_types.Date{Time: *p.Finish}
	}
	return out, nil
}

// ListGoals returns one page of the user's goals, newest first, with progress.
func (s *Server) ListGoals(ctx context.Context, req ListGoalsRequestObject) (ListGoalsResponseObject, error) {
	limit := defaultPageSize
	if req.Params.Limit != nil {
		limit = *req.Params.Limit
	}
	if limit < 1 || limit > maxPageSize {
		return nil, &Error{Kind: KindInvalidRequest, Detail: fmt.Sprintf("limit must be 1 to %d", maxPageSize)}
	}
	var before *int64
	if req.Params.Cursor != nil {
		var c goalCursor
		if err := s.cursors.Decode(*req.Params.Cursor, &c); err != nil {
			return nil, err
		}
		before = &c.ID
	}
	sess := sessionFrom(ctx)
	rows, err := s.q.ListGoals(ctx, db.ListGoalsParams{
		UserID: sess.userID, Before: before,
		MaxRows: int32(limit + 1), // one extra row tells whether another page follows
	})
	if err != nil {
		return nil, fmt.Errorf("listing goals: %w", err)
	}
	page := GoalPage{Items: []Goal{}}
	if len(rows) > limit {
		rows = rows[:limit]
		next, err := s.cursors.Encode(goalCursor{ID: rows[limit-1].ID})
		if err != nil {
			return nil, err
		}
		page.NextCursor = &next
	}
	today := localToday(sess.timezone)
	for _, g := range rows {
		goal, err := s.goalProgress(ctx, g, today)
		if err != nil {
			return nil, err
		}
		page.Items = append(page.Items, goal)
	}
	return ListGoals200JSONResponse(page), nil
}

// CreateGoal adds a volume goal for the period holding the given day.
func (s *Server) CreateGoal(ctx context.Context, req CreateGoalRequestObject) (CreateGoalResponseObject, error) {
	b := req.Body
	invalid := func(detail string) (CreateGoalResponseObject, error) {
		return nil, &Error{Kind: KindInvalidRequest, Detail: detail}
	}
	switch {
	case !b.Period.Valid():
		return invalid("period must be week, month or year")
	case !b.Metric.Valid():
		return invalid("metric must be distance, duration or ascent")
	case b.Sport != nil && !b.Sport.Valid():
		return invalid("unknown sport")
	case !(b.Target > 0):
		return invalid("target must be positive")
	}
	sess := sessionFrom(ctx)
	first, _ := periodBounds(string(b.Period), b.Start.Time)
	g, err := s.q.CreateGoal(ctx, db.CreateGoalParams{
		UserID: sess.userID, Period: string(b.Period), StartDay: first, Sport: (*string)(b.Sport), Metric: string(b.Metric), Target: b.Target,
	})
	if err != nil {
		return nil, fmt.Errorf("creating goal: %w", err)
	}
	goal, err := s.goalProgress(ctx, g, localToday(sess.timezone))
	if err != nil {
		return nil, err
	}
	return CreateGoal201JSONResponse(goal), nil
}

// DeleteGoal deletes a goal.
func (s *Server) DeleteGoal(ctx context.Context, req DeleteGoalRequestObject) (DeleteGoalResponseObject, error) {
	n, err := s.q.DeleteGoal(ctx, db.DeleteGoalParams{UserID: sessionFrom(ctx).userID, ID: req.Id})
	if err != nil {
		return nil, fmt.Errorf("deleting goal: %w", err)
	}
	if n == 0 {
		return nil, errGoalNotFound
	}
	return DeleteGoal204Response{}, nil
}
