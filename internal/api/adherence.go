package api

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/iskyd/kudos/internal/analytics"
	"github.com/iskyd/kudos/internal/importer/ingest"
	"github.com/iskyd/kudos/internal/llm"
	"github.com/iskyd/kudos/internal/plans"
	"github.com/iskyd/kudos/internal/store/db"
)

// adherenceDays is how far back the context sums adherence: the two weeks
// before today, as long as a plan.
const adherenceDays = llm.PlanDays

// planContext loads the planned workouts from two weeks back through the days
// a new plan covers, with what completed them, and sums the adherence of those
// before today.
func (s *Server) planContext(ctx context.Context, userID int64, today time.Time) ([]llm.PlannedSummary, *llm.Adherence, error) {
	from := today.AddDate(0, 0, -adherenceDays)
	rows, err := s.q.ListPlannedWorkoutsBetween(ctx, db.ListPlannedWorkoutsBetweenParams{
		UserID: userID, FromDay: from, ToDay: today.AddDate(0, 0, llm.PlanDays), MaxRows: maxContextPlanned,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("loading the plan for the coach: %w", err)
	}
	m, err := s.match(ctx, userID, rows)
	if err != nil {
		return nil, nil, err
	}
	var plan []llm.PlannedSummary
	var outcomes []plans.Outcome
	for _, w := range rows {
		p := llm.PlannedSummary{ID: w.ID, Day: w.Day, Sport: w.Sport, Title: w.Title}
		if c, ok := m.done[w.ID]; ok {
			p.ActivityID = &c.ActivityID
		}
		plan = append(plan, p)
		if w.Day.Before(today) {
			o, err := s.outcome(ctx, userID, w, p.ActivityID, m)
			if err != nil {
				return nil, nil, err
			}
			outcomes = append(outcomes, o)
		}
	}
	if len(outcomes) == 0 {
		return plan, nil, nil
	}
	a := plans.Summarize(outcomes)
	return plan, &llm.Adherence{
		From: from, To: today.AddDate(0, 0, -1), Planned: a.Planned, Completed: a.Completed,
		PlannedS: a.PlannedS, ActualS: a.ActualS, PlannedLoad: a.PlannedLoad, ActualLoad: a.ActualLoad,
		LoadCompared: a.LoadCompared, StepsWithin: a.StepsWithin, StepsCompared: a.StepsCompared,
	}, nil
}

// outcome is how a past planned workout went: the activity that completed it
// (activityID, nil when missed) against the plan, with the zones valid on its day.
func (s *Server) outcome(ctx context.Context, userID int64, w db.PlannedWorkout, activityID *int64, m matched) (plans.Outcome, error) {
	var o plans.Outcome
	var err error
	if o.PlannedS, _, err = plans.Totals(w.Steps); err != nil {
		return plans.Outcome{}, err
	}
	zones, err := s.q.ListZoneSetsAt(ctx, db.ListZoneSetsAtParams{UserID: userID, Day: w.Day})
	if err != nil {
		return plans.Outcome{}, fmt.Errorf("reading zones for adherence: %w", err)
	}
	var own, general analytics.Thresholds
	for _, z := range zones {
		switch {
		case analytics.ZoneSport(z.Sport) == analytics.ZoneSportOf(w.Sport):
			own = ingest.ZoneThresholds(z)
		case z.Sport == string(analytics.ZoneGeneral):
			general = ingest.ZoneThresholds(z)
		}
	}
	if load, ok, err := plans.PlannedLoad(w.Steps, w.Sport, own, general); err != nil {
		return plans.Outcome{}, err
	} else if ok {
		o.PlannedLoad = &load
	}
	if activityID == nil {
		return o, nil
	}
	o.Done = true
	a, ok := m.activities[*activityID]
	if !ok { // linked by the user to an activity on another day
		row, err := s.q.GetActivity(ctx, db.GetActivityParams{UserID: userID, ID: *activityID})
		if err != nil {
			return plans.Outcome{}, fmt.Errorf("reading a linked activity: %w", err)
		}
		if a, err = ingest.Effective(row); err != nil {
			return plans.Outcome{}, err
		}
	}
	if t := cmp.Or(a.TimerTimeS, a.ElapsedTimeS); t != nil {
		o.ActualS = *t
	}
	switch d, err := s.q.GetActivityDerived(ctx, db.GetActivityDerivedParams{UserID: userID, ActivityID: a.ID}); {
	case err == nil:
		o.ActualLoad = d.Load
	case !errors.Is(err, pgx.ErrNoRows):
		return plans.Outcome{}, fmt.Errorf("reading activity load: %w", err)
	}
	steps, err := s.executeSteps(ctx, userID, a.ID)
	if err != nil {
		return plans.Outcome{}, err
	}
	if steps != nil {
		for _, st := range *steps {
			if st.Result != nil && *st.Result != Skipped {
				o.StepsCompared++
				if *st.Result == Within {
					o.StepsWithin++
				}
			}
		}
	}
	return o, nil
}
