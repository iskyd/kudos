// Package proposals turns the LLM's plans into proposals and applies the
// changes the user approves: nothing changes the plan without approval
// (invariant 34).
package proposals

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/iskyd/kudos/internal/jobs"
	"github.com/iskyd/kudos/internal/llm"
	"github.com/iskyd/kudos/internal/plans"
	"github.com/iskyd/kudos/internal/store/db"
)

var (
	// ErrGenerating means another proposal of the user is being generated.
	ErrGenerating = errors.New("a plan proposal is being generated")
	// ErrNotFound means the user has no such proposal.
	ErrNotFound = errors.New("no such plan proposal")
	// ErrClosed means the proposal is not open for decisions.
	ErrClosed = errors.New("the plan proposal is not open")
	// ErrBadItems means a decision names an item that is not pending in the proposal.
	ErrBadItems = errors.New("invalid proposal items")
)

const (
	// staleAfter is when a proposal still generating has lost its job: well
	// past every attempt's timeout.
	staleAfter = time.Hour
	// maxItems bounds a proposal: a draft's workouts plus the current ones it removes.
	maxItems = 500
)

// The failures a proposal records.
const (
	FailedInvalidOutput = "invalid_output"
	FailedProvider      = "provider_error"
	FailedNotConfigured = "not_configured"
)

// Service starts proposals and applies decisions.
type Service struct {
	pool *pgxpool.Pool
	jobs *jobs.Client
}

// NewService returns a service enqueueing on jobClient.
func NewService(pool *pgxpool.Pool, jobClient *jobs.Client) *Service {
	return &Service{pool: pool, jobs: jobClient}
}

// Start creates a proposal for the days after today and queues its generation.
func (s *Service) Start(ctx context.Context, userID int64, today time.Time) (db.PlanProposal, error) {
	var p db.PlanProposal
	err := inTx(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		p, err = start(ctx, tx, s.jobs, userID, today)
		return err
	})
	return p, err
}

// start creates a proposal and its job in tx.
func start(ctx context.Context, tx pgx.Tx, jobClient *jobs.Client, userID int64, today time.Time) (db.PlanProposal, error) {
	q := db.New(tx)
	if err := q.ExpirePlanProposals(ctx, db.ExpirePlanProposalsParams{UserID: userID, Before: time.Now().Add(-staleAfter)}); err != nil {
		return db.PlanProposal{}, fmt.Errorf("expiring plan proposals: %w", err)
	}
	p, err := q.CreatePlanProposal(ctx, db.CreatePlanProposalParams{
		UserID: userID, FromDay: today.AddDate(0, 0, 1), ToDay: today.AddDate(0, 0, llm.PlanDays),
	})
	if pgErr := (*pgconn.PgError)(nil); errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return db.PlanProposal{}, ErrGenerating
	}
	if err != nil {
		return db.PlanProposal{}, fmt.Errorf("creating a plan proposal: %w", err)
	}
	if _, err := jobClient.InsertTx(ctx, tx, GenerateArgs{UserID: userID, ProposalID: p.ID}, nil); err != nil {
		return db.PlanProposal{}, fmt.Errorf("queueing the plan proposal: %w", err)
	}
	return p, nil
}

// Decide approves and rejects pending items of an open proposal, all or none:
// approved items change the planned workouts, rejected ones change nothing.
// The proposal closes once no item is pending.
func (s *Service) Decide(ctx context.Context, userID, proposalID int64, approve, reject []int64) error {
	ids := slices.Concat(approve, reject)
	for i, id := range ids {
		if slices.Contains(ids[:i], id) {
			return fmt.Errorf("%w: item %d is named twice", ErrBadItems, id)
		}
	}
	return inTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := db.New(tx)
		p, err := q.LockPlanProposal(ctx, db.LockPlanProposalParams{UserID: userID, ID: proposalID})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("reading the plan proposal: %w", err)
		}
		if p.Status != "open" {
			return ErrClosed
		}
		items, err := q.ListPlanProposalItems(ctx, db.ListPlanProposalItemsParams{UserID: userID, ProposalID: p.ID, MaxRows: maxItems})
		if err != nil {
			return fmt.Errorf("reading the proposal items: %w", err)
		}
		for _, id := range ids {
			status := "rejected"
			if slices.Contains(approve, id) {
				status = "approved"
			}
			n, err := q.DecidePlanProposalItem(ctx, db.DecidePlanProposalItemParams{UserID: userID, ProposalID: p.ID, ID: id, Status: status})
			if err != nil {
				return fmt.Errorf("deciding item %d: %w", id, err)
			}
			if n == 0 {
				return fmt.Errorf("%w: item %d is not pending in this proposal", ErrBadItems, id)
			}
		}
		for _, id := range approve {
			i := slices.IndexFunc(items, func(it db.PlanProposalItem) bool { return it.ID == id })
			if err := apply(ctx, q, &p, items[i]); err != nil {
				return err
			}
		}
		pending, err := q.CountPendingPlanProposalItems(ctx, db.CountPendingPlanProposalItemsParams{UserID: userID, ProposalID: p.ID})
		if err != nil {
			return fmt.Errorf("counting pending items: %w", err)
		}
		if pending == 0 {
			if err := q.SetPlanProposalStatus(ctx, db.SetPlanProposalStatusParams{UserID: userID, ID: p.ID, Status: "closed"}); err != nil {
				return fmt.Errorf("closing the plan proposal: %w", err)
			}
		}
		return nil
	})
}

// apply makes an approved item's change. Additions join the proposal's plan,
// created on its first approved addition.
func apply(ctx context.Context, q *db.Queries, p *db.PlanProposal, it db.PlanProposalItem) error {
	var after plans.Workout
	if it.After != nil {
		if err := json.Unmarshal(it.After, &after); err != nil {
			return fmt.Errorf("reading item %d: %w", it.ID, err)
		}
	}
	day, _ := time.Parse(time.DateOnly, after.Day) // validated before it was stored; empty for a removal
	var n int64
	var err error
	switch it.Action {
	case "add":
		if p.PlanID == nil {
			plan, err := q.CreatePlan(ctx, p.UserID)
			if err != nil {
				return fmt.Errorf("creating the plan: %w", err)
			}
			if err := q.SetPlanProposalPlan(ctx, db.SetPlanProposalPlanParams{UserID: p.UserID, ID: p.ID, PlanID: &plan.ID}); err != nil {
				return fmt.Errorf("linking the plan: %w", err)
			}
			p.PlanID = &plan.ID
		}
		_, err = q.CreatePlannedWorkout(ctx, db.CreatePlannedWorkoutParams{
			UserID: p.UserID, PlanID: *p.PlanID, Day: day, Sport: after.Sport, Title: after.Title, Description: after.Description, Steps: after.Steps,
		})
		n = 1
	case "change":
		n, err = q.UpdatePlannedWorkout(ctx, db.UpdatePlannedWorkoutParams{
			UserID: p.UserID, ID: *it.WorkoutID, Day: day, Sport: after.Sport, Title: after.Title, Description: after.Description, Steps: after.Steps,
		})
	case "remove":
		n, err = q.DeletePlannedWorkout(ctx, db.DeletePlannedWorkoutParams{UserID: p.UserID, ID: *it.WorkoutID})
	}
	if err != nil {
		return fmt.Errorf("applying item %d: %w", it.ID, err)
	}
	if n == 0 {
		return fmt.Errorf("%w: item %d's planned workout is gone", ErrBadItems, it.ID)
	}
	return nil
}

func inTx(ctx context.Context, pool *pgxpool.Pool, fn func(pgx.Tx) error) error {
	var fnErr error
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		fnErr = fn(tx)
		return fnErr
	})
	if fnErr != nil {
		return fnErr
	}
	if err != nil {
		return fmt.Errorf("in transaction: %w", err)
	}
	return nil
}
