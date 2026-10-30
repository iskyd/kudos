package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/iskyd/kudos/internal/analytics"
	"github.com/iskyd/kudos/internal/importer"
	"github.com/iskyd/kudos/internal/importer/ingest"
	"github.com/iskyd/kudos/internal/store/db"
)

const (
	// maxExecutionLaps bounds the laps read to group by step.
	// shortcut: laps past the first 1000 are not grouped; page the read if a workout ever has more.
	maxExecutionLaps = 1000
	// maxPowerIntervals bounds the intervals returned; compliance still covers the whole workout.
	maxPowerIntervals = 1000
)

// GetActivityExecution compares a structured workout's execution with its targets.
func (s *Server) GetActivityExecution(ctx context.Context, req GetActivityExecutionRequestObject) (GetActivityExecutionResponseObject, error) {
	a, err := s.activity(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	steps, err := s.executeSteps(ctx, a.ID)
	if err != nil {
		return nil, err
	}
	if steps != nil {
		return GetActivityExecution200JSONResponse(WorkoutExecution{Kind: Steps, Steps: steps}), nil
	}
	out, err := s.executeTargetPower(ctx, a)
	if err != nil {
		return nil, err
	}
	return GetActivityExecution200JSONResponse(out), nil
}

// executeSteps groups the laps by workout step; nil when no lap ran a step.
func (s *Server) executeSteps(ctx context.Context, id int64) (*[]StepExecution, error) {
	userID := sessionFrom(ctx).userID
	w, err := s.q.GetRecordedWorkout(ctx, db.GetRecordedWorkoutParams{UserID: userID, ActivityID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading the recorded workout: %w", err)
	}
	var steps []importer.WorkoutStep
	if err := json.Unmarshal(w.Steps, &steps); err != nil {
		return nil, fmt.Errorf("decoding workout steps: %w", err)
	}
	rows, err := s.q.ListLaps(ctx, db.ListLapsParams{UserID: userID, ActivityID: id, After: -1, MaxRows: maxExecutionLaps})
	if err != nil {
		return nil, fmt.Errorf("listing laps: %w", err)
	}
	laps := make([]analytics.LapActual, len(rows))
	ran := false
	for i, l := range rows {
		laps[i] = analytics.LapActual{
			TimerS: l.TimerTimeS, DistanceM: l.DistanceM, AvgSpeed: f64(l.AvgSpeedMS),
			AvgHR: i16f(l.AvgHrBpm), AvgPower: i16f(l.AvgPowerW), AvgCadence: f64(l.AvgCadence),
		}
		if l.WorkoutStepIndex != nil {
			laps[i].StepIndex, ran = new(int(*l.WorkoutStepIndex)), true
		}
	}
	if !ran {
		return nil, nil
	}
	runs := analytics.Execute(steps, laps)
	out := make([]StepExecution, len(runs))
	for i, r := range runs {
		out[i] = StepExecution{
			StepIndex: r.StepIndex, Laps: r.Laps, TimerTimeS: r.TimerS, DistanceM: r.DistanceM,
			AvgSpeedMS: r.AvgSpeed, AvgHrBpm: r.AvgHR, AvgPowerW: r.AvgPower, AvgCadence: r.AvgCadence, Actual: r.Actual,
		}
		if r.Result != "" {
			out[i].Result = new(StepExecutionResult(r.Result))
		}
	}
	return &out, nil
}

// executeTargetPower measures power against the per-second target of the
// activity's edited view; kind none when no sample carries a target.
func (s *Server) executeTargetPower(ctx context.Context, a db.Activity) (WorkoutExecution, error) {
	v, err := s.ingest.View(ctx, a)
	if err != nil {
		return WorkoutExecution{}, fmt.Errorf("reading edits: %w", err)
	}
	rows, err := s.ingest.Samples(ctx, sessionFrom(ctx).userID, v, nil, maxDecimatedSamples)
	if err != nil {
		return WorkoutExecution{}, fmt.Errorf("listing streams: %w", err)
	}
	samples := make([]analytics.TargetSample, len(rows))
	targeted := false
	for i, r := range rows {
		samples[i] = analytics.TargetSample{Time: r.Time, Power: i16f(r.PowerW)}
		if r.Extras == nil {
			continue
		}
		var extras struct {
			Target *float64 `json:"target_power_w"`
		}
		if err := json.Unmarshal(r.Extras, &extras); err != nil {
			return WorkoutExecution{}, fmt.Errorf("decoding extras: %w", err)
		}
		samples[i].Target = extras.Target
		targeted = targeted || extras.Target != nil
	}
	if !targeted {
		return WorkoutExecution{Kind: None}, nil
	}
	if a, err = ingest.Effective(a); err != nil {
		return WorkoutExecution{}, err
	}
	found, within, total := analytics.PowerCompliance(samples)
	found = found[:min(len(found), maxPowerIntervals)]
	intervals := make([]PowerInterval, len(found))
	for i, iv := range found {
		intervals[i] = PowerInterval{
			StartS: iv.Start.Sub(a.StartTime).Seconds(), DurationS: iv.DurationS,
			TargetW: iv.TargetW, AvgPowerW: iv.AvgPowerW, WithinS: iv.WithinS,
		}
	}
	return WorkoutExecution{
		Kind: TargetPower, Intervals: &intervals,
		WithinS: &within, TotalS: &total, ToleranceW: new(analytics.PowerTolerance),
	}, nil
}
