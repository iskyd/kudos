package proposals

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/iskyd/kudos/internal/llm"
	"github.com/iskyd/kudos/internal/llm/reports"
	"github.com/iskyd/kudos/internal/plans"
	"github.com/iskyd/kudos/internal/store/db"
)

const (
	// contextTokens matches chat and reports: small local models need the room.
	contextTokens = 6000
	// generateTimeout allows for slow local models writing two weeks of
	// workouts, twice; it stays under River's rescue of stuck jobs.
	generateTimeout = 20 * time.Minute
	maxAttempts     = 3
	maxCurrent      = 100 // planned workouts on a proposal's days
	scheduleEvery   = 15 * time.Minute
	candidatePage   = 500
)

// Deps are what the workers need. Context and Client come from the API server,
// which is built after the job client: cmd/server sets them before starting it.
type Deps struct {
	Pool    *pgxpool.Pool
	Logger  *slog.Logger
	Context func(ctx context.Context, userID int64, today time.Time) (llm.AthleteContext, error)
	Client  func(ctx context.Context, userID int64) (llm.Client, error)
	Now     func() time.Time // nil for time.Now
}

func (d *Deps) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now()
}

// Register adds the workers.
func (d *Deps) Register(w *river.Workers) {
	river.AddWorker(w, &GenerateWorker{Deps: d})
	river.AddWorker(w, &ScheduleWorker{Deps: d})
}

// GenerateArgs fills in a generating proposal.
type GenerateArgs struct {
	UserID     int64 `json:"user_id"`
	ProposalID int64 `json:"proposal_id"`
}

// Kind implements river.JobArgs.
func (GenerateArgs) Kind() string { return "plan_proposal" }

// InsertOpts implements river.JobArgsWithInsertOpts.
func (GenerateArgs) InsertOpts() river.InsertOpts { return river.InsertOpts{MaxAttempts: maxAttempts} }

// GenerateWorker works GenerateArgs.
type GenerateWorker struct {
	river.WorkerDefaults[GenerateArgs]
	*Deps
}

// Timeout implements river.Worker.
func (*GenerateWorker) Timeout(*river.Job[GenerateArgs]) time.Duration { return generateTimeout }

// Work asks the model for the proposal's days, validates its plan (retrying
// once with the errors) and stores the changes it makes to the current plan.
// Output that stays invalid fails the proposal at once; provider errors fail
// it after the last attempt. Nothing unvalidated is stored (invariant 33).
func (w *GenerateWorker) Work(ctx context.Context, job *river.Job[GenerateArgs]) error {
	a := job.Args
	q := db.New(w.Pool)
	p, err := q.GetPlanProposal(ctx, db.GetPlanProposalParams{UserID: a.UserID, ID: a.ProposalID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("reading the plan proposal: %w", err)
	}
	if p.Status != "generating" {
		return nil
	}
	client, err := w.Client(ctx, a.UserID)
	if errors.Is(err, llm.ErrNotConfigured) {
		return w.fail(ctx, p, FailedNotConfigured)
	}
	if err != nil {
		return err
	}
	mode, today := llm.ModePlan, p.FromDay.AddDate(0, 0, -1)
	if p.Kind == KindAdjustment {
		mode, today = llm.ModeAdjust, p.FromDay
	}
	c, err := w.Context(ctx, a.UserID, today)
	if err != nil {
		return err
	}
	current, err := currentWorkouts(ctx, q, p)
	if err != nil {
		return err
	}
	ids := make([]int64, len(current))
	for i, c := range current {
		ids[i] = c.ID
	}
	athlete := llm.BuildContext(c, contextTokens)
	prompt, err := llm.Render(mode, llm.PromptInput{Today: today, Context: athlete, Schema: string(plans.DraftSchema)})
	if err != nil {
		return fmt.Errorf("rendering the %s prompt: %w", mode, err)
	}
	var draft plans.Draft
	_, err = llm.Structured(ctx, client, llm.Request{System: prompt.System, Messages: []llm.Message{{Role: "user", Content: prompt.User}}},
		func(doc []byte) error {
			var err error
			draft, err = plans.ValidateDraft(doc, p.FromDay, p.ToDay, ids, athlete)
			return err
		})
	if errors.Is(err, llm.ErrInvalidOutput) {
		w.Logger.WarnContext(ctx, "plan proposal invalid", "user_id", a.UserID, "proposal_id", p.ID)
		return w.fail(ctx, p, FailedInvalidOutput)
	}
	if err != nil {
		w.Logger.WarnContext(ctx, "plan proposal failed", "user_id", a.UserID, "proposal_id", p.ID, "attempt", job.Attempt)
		if job.Attempt >= job.MaxAttempts {
			return w.fail(ctx, p, FailedProvider)
		}
		return fmt.Errorf("planning: %w", err)
	}
	return inTx(ctx, w.Pool, func(tx pgx.Tx) error { return fill(ctx, db.New(tx), p, prompt.Version, draft) })
}

// fill stores the draft's changes to the current plan with its reasons and
// opens the proposal, superseding the open one; with no change it closes at once.
func fill(ctx context.Context, q *db.Queries, p db.PlanProposal, version string, draft plans.Draft) error {
	n, err := q.FillPlanProposal(ctx, db.FillPlanProposalParams{
		UserID: p.UserID, ID: p.ID, Status: "open", PromptVersion: &version, Rationale: &draft.Rationale, Refs: draft.References,
	})
	if err != nil {
		return fmt.Errorf("opening the plan proposal: %w", err)
	}
	if n == 0 {
		return nil // expired meanwhile
	}
	if err := q.SupersedePlanProposals(ctx, db.SupersedePlanProposalsParams{UserID: p.UserID, ID: p.ID}); err != nil {
		return fmt.Errorf("superseding plan proposals: %w", err)
	}
	// Read after superseding: decisions on the old proposal are committed by now.
	current, err := currentWorkouts(ctx, q, p)
	if err != nil {
		return err
	}
	changes, err := plans.Diff(current, draft)
	if err != nil {
		return fmt.Errorf("comparing the plans: %w", err)
	}
	for _, c := range changes {
		before, err := marshal(c.Before)
		if err != nil {
			return err
		}
		after, err := marshal(c.After)
		if err != nil {
			return err
		}
		var workoutID *int64
		if c.WorkoutID != 0 {
			workoutID = &c.WorkoutID
		}
		if err := q.CreatePlanProposalItem(ctx, db.CreatePlanProposalItemParams{
			UserID: p.UserID, ProposalID: p.ID, Action: c.Action, WorkoutID: workoutID, Before: before, After: after,
		}); err != nil {
			return fmt.Errorf("storing a proposal item: %w", err)
		}
	}
	if len(changes) == 0 {
		if err := q.SetPlanProposalStatus(ctx, db.SetPlanProposalStatusParams{UserID: p.UserID, ID: p.ID, Status: "closed"}); err != nil {
			return fmt.Errorf("closing the plan proposal: %w", err)
		}
	}
	return nil
}

func marshal(w *plans.Workout) ([]byte, error) {
	if w == nil {
		return nil, nil
	}
	b, err := json.Marshal(w)
	if err != nil {
		return nil, fmt.Errorf("encoding a workout: %w", err)
	}
	return b, nil
}

// currentWorkouts are the planned workouts on the proposal's days.
func currentWorkouts(ctx context.Context, q *db.Queries, p db.PlanProposal) ([]plans.Current, error) {
	rows, err := q.ListPlannedWorkoutsBetween(ctx, db.ListPlannedWorkoutsBetweenParams{
		UserID: p.UserID, FromDay: p.FromDay, ToDay: p.ToDay, MaxRows: maxCurrent,
	})
	if err != nil {
		return nil, fmt.Errorf("reading the current plan: %w", err)
	}
	current := make([]plans.Current, len(rows))
	for i, r := range rows {
		current[i] = plans.Current{ID: r.ID, Workout: plans.Workout{
			Day: r.Day.Format(time.DateOnly), Sport: r.Sport, Title: r.Title, Description: r.Description, Steps: r.Steps,
		}}
	}
	return current, nil
}

func (w *GenerateWorker) fail(ctx context.Context, p db.PlanProposal, failure string) error {
	if err := db.New(w.Pool).FailPlanProposal(ctx, db.FailPlanProposalParams{UserID: p.UserID, ID: p.ID, Failure: &failure}); err != nil {
		return fmt.Errorf("failing the plan proposal: %w", err)
	}
	return nil
}

// WeeklyDue returns the Monday c is due a new proposal at now: on Monday
// mornings (as reports.Morning) when it has none since that Monday began.
func WeeklyDue(c db.ListPlanCandidatesRow, now time.Time) *time.Time {
	today, ok := reports.Morning(c.Timezone, c.GarminConnected, c.LastSyncAt, now)
	if !ok || today.Weekday() != time.Monday {
		return nil
	}
	loc, err := time.LoadLocation(c.Timezone)
	if err != nil {
		loc = time.UTC // validated when set; this is only a guard
	}
	if c.LastProposal.Before(time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, loc)) {
		return &today
	}
	return nil
}

// AdjustDue returns the day c is due a check of its coming week at now: once a
// day, after that day's daily insight was written, while no other proposal of
// c is open or being generated, so a check never buries one waiting for the
// user.
func AdjustDue(c db.ListPlanCandidatesRow, now time.Time) *time.Time {
	today, ok := reports.Morning(c.Timezone, c.GarminConnected, c.LastSyncAt, now)
	if !ok || c.PendingProposal || c.LastInsight.Before(today) {
		return nil
	}
	loc, err := time.LoadLocation(c.Timezone)
	if err != nil {
		loc = time.UTC // validated when set; this is only a guard
	}
	if c.LastAdjustment.Before(time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, loc)) {
		return &today
	}
	return nil
}

// ScheduleArgs queues the plans and adjustments users are due.
type ScheduleArgs struct{}

// Kind implements river.JobArgs.
func (ScheduleArgs) Kind() string { return "plan_proposal_schedule" }

// SchedulePeriodic runs the scheduler every 15 minutes, starting at boot.
func SchedulePeriodic() *river.PeriodicJob {
	return river.NewPeriodicJob(river.PeriodicInterval(scheduleEvery),
		func() (river.JobArgs, *river.InsertOpts) { return ScheduleArgs{}, nil },
		&river.PeriodicJobOpts{RunOnStart: true})
}

// ScheduleWorker works ScheduleArgs.
type ScheduleWorker struct {
	river.WorkerDefaults[ScheduleArgs]
	*Deps
}

// Work regenerates the plan of every user with one on Monday mornings, and
// otherwise checks the coming week of those with workouts planned in it after
// their daily insight. A user whose proposal is generating already is skipped.
func (w *ScheduleWorker) Work(ctx context.Context, _ *river.Job[ScheduleArgs]) error {
	jobs := river.ClientFromContext[pgx.Tx](ctx)
	q := db.New(w.Pool)
	now := w.now()
	var after int64
	for {
		users, err := q.ListPlanCandidates(ctx, db.ListPlanCandidatesParams{AfterID: after, MaxRows: candidatePage})
		if err != nil {
			return fmt.Errorf("listing plan candidates: %w", err)
		}
		for _, u := range users {
			kind, today, err := w.due(ctx, q, u, now)
			if err != nil {
				return err
			}
			if today == nil {
				continue
			}
			err = inTx(ctx, w.Pool, func(tx pgx.Tx) error {
				_, err := start(ctx, tx, jobs, u.UserID, kind, *today)
				return err
			})
			if err != nil && !errors.Is(err, ErrGenerating) {
				return fmt.Errorf("queueing the %s of user %d: %w", kind, u.UserID, err)
			}
		}
		if len(users) < candidatePage {
			return nil
		}
		after = users[len(users)-1].UserID
	}
}

// due is what u is due at now, if anything: a plan, else an adjustment when
// workouts are planned in the coming week.
func (w *ScheduleWorker) due(ctx context.Context, q *db.Queries, u db.ListPlanCandidatesRow, now time.Time) (string, *time.Time, error) {
	if today := WeeklyDue(u, now); today != nil {
		return KindPlan, today, nil
	}
	today := AdjustDue(u, now)
	if today == nil {
		return "", nil, nil
	}
	from, to := days(KindAdjustment, *today)
	planned, err := q.ListPlannedWorkoutsBetween(ctx, db.ListPlannedWorkoutsBetweenParams{UserID: u.UserID, FromDay: from, ToDay: to, MaxRows: 1})
	if err != nil {
		return "", nil, fmt.Errorf("reading the coming week of user %d: %w", u.UserID, err)
	}
	if len(planned) == 0 {
		return "", nil, nil
	}
	return KindAdjustment, today, nil
}
