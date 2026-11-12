package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/iskyd/kudos/internal/plans/proposals"
	"github.com/iskyd/kudos/internal/store/db"
)

// maxProposalItems bounds a proposal's items, above any the planner makes.
const maxProposalItems = 500

// CreatePlanProposal starts generating a plan for the 14 days from tomorrow.
func (s *Server) CreatePlanProposal(ctx context.Context, _ CreatePlanProposalRequestObject) (CreatePlanProposalResponseObject, error) {
	sess := sessionFrom(ctx)
	if _, err := s.q.GetLLMProvider(ctx, sess.userID); errors.Is(err, pgx.ErrNoRows) {
		return nil, &Error{Kind: KindLLMNotConfigured, Detail: "set up an AI coach provider in Settings first"}
	} else if err != nil {
		return nil, fmt.Errorf("reading the LLM provider: %w", err)
	}
	p, err := s.proposals.Start(ctx, sess.userID, localToday(sess.timezone))
	if errors.Is(err, proposals.ErrGenerating) {
		return nil, &Error{Kind: KindProposalGenerating, Detail: "wait for the plan being generated"}
	}
	if err != nil {
		return nil, fmt.Errorf("starting a plan proposal: %w", err)
	}
	out, err := s.planProposal(ctx, p)
	if err != nil {
		return nil, err
	}
	return CreatePlanProposal202JSONResponse(out), nil
}

// GetLatestPlanProposal returns the user's most recent proposal.
func (s *Server) GetLatestPlanProposal(ctx context.Context, _ GetLatestPlanProposalRequestObject) (GetLatestPlanProposalResponseObject, error) {
	p, err := s.q.GetLatestPlanProposal(ctx, sessionFrom(ctx).userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, &Error{Kind: KindNotFound, Detail: "no plan proposal yet"}
	}
	if err != nil {
		return nil, fmt.Errorf("reading the latest plan proposal: %w", err)
	}
	out, err := s.planProposal(ctx, p)
	if err != nil {
		return nil, err
	}
	return GetLatestPlanProposal200JSONResponse(out), nil
}

// DecidePlanProposal approves and rejects items of an open proposal.
func (s *Server) DecidePlanProposal(ctx context.Context, req DecidePlanProposalRequestObject) (DecidePlanProposalResponseObject, error) {
	userID := sessionFrom(ctx).userID
	err := s.proposals.Decide(ctx, userID, req.Id, req.Body.Approve, req.Body.Reject)
	switch {
	case errors.Is(err, proposals.ErrNotFound):
		return nil, &Error{Kind: KindNotFound, Detail: "no such plan proposal"}
	case errors.Is(err, proposals.ErrClosed):
		return nil, &Error{Kind: KindProposalClosed, Detail: "the proposal is no longer open"}
	case errors.Is(err, proposals.ErrBadItems):
		return nil, &Error{Kind: KindInvalidRequest, Detail: err.Error()} // names item IDs only
	case err != nil:
		return nil, fmt.Errorf("deciding a plan proposal: %w", err)
	}
	p, err := s.q.GetPlanProposal(ctx, db.GetPlanProposalParams{UserID: userID, ID: req.Id})
	if err != nil {
		return nil, fmt.Errorf("reading the plan proposal: %w", err)
	}
	out, err := s.planProposal(ctx, p)
	if err != nil {
		return nil, err
	}
	return DecidePlanProposal200JSONResponse(out), nil
}

// planProposal converts a stored proposal with its items; their workouts were
// validated before they were stored.
func (s *Server) planProposal(ctx context.Context, p db.PlanProposal) (PlanProposal, error) {
	out := PlanProposal{
		Id: p.ID, Status: PlanProposalStatus(p.Status), Failure: (*PlanProposalFailure)(p.Failure), CreatedAt: p.CreatedAt,
		From: openapi_types.Date{Time: p.FromDay}, To: openapi_types.Date{Time: p.ToDay}, Items: []PlanProposalItem{},
	}
	items, err := s.q.ListPlanProposalItems(ctx, db.ListPlanProposalItemsParams{UserID: p.UserID, ProposalID: p.ID, MaxRows: maxProposalItems})
	if err != nil {
		return PlanProposal{}, fmt.Errorf("listing proposal items: %w", err)
	}
	for _, it := range items {
		item := PlanProposalItem{
			Id: it.ID, Action: PlanProposalItemAction(it.Action), Status: PlanProposalItemStatus(it.Status), WorkoutId: it.WorkoutID,
		}
		if item.Before, err = workoutInput(it.Before); err != nil {
			return PlanProposal{}, fmt.Errorf("reading proposal item %d: %w", it.ID, err)
		}
		if item.After, err = workoutInput(it.After); err != nil {
			return PlanProposal{}, fmt.Errorf("reading proposal item %d: %w", it.ID, err)
		}
		out.Items = append(out.Items, item)
	}
	return out, nil
}

// workoutInput decodes a stored workout; nil stays nil.
func workoutInput(doc []byte) (*PlannedWorkoutInput, error) {
	if doc == nil {
		return nil, nil
	}
	var w PlannedWorkoutInput
	if err := json.Unmarshal(doc, &w); err != nil {
		return nil, fmt.Errorf("decoding a workout: %w", err)
	}
	return &w, nil
}
