package api

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/iskyd/kudos/internal/store/db"
)

func TestCreatePlanProposal(t *testing.T) {
	t.Parallel()
	pool := pg.NewDB(t)
	alice, bob := chatUser(t, pool, "alice"), chatUser(t, pool, "bob")

	wantProblem(t, alice.do(http.MethodGet, "/api/plan-proposals/latest", nil, nil), KindNotFound)
	wantProblem(t, alice.do(http.MethodPost, "/api/plan-proposals", nil, nil), KindLLMNotConfigured)

	url := "http://ollama.example.test"
	setLLM(t, alice, LLMProviderInput{Provider: Ollama, BaseUrl: &url, Model: "m"})
	p := decodeJSON[PlanProposal](t, alice.do(http.MethodPost, "/api/plan-proposals", nil, nil), http.StatusAccepted)
	tomorrow := time.Now().In(time.UTC).AddDate(0, 0, 1).Format(time.DateOnly) // the test users' timezone is UTC
	if p.Status != PlanProposalStatusGenerating || p.From.Format(time.DateOnly) != tomorrow || p.To.Sub(p.From.Time) != 13*24*time.Hour || len(p.Items) != 0 {
		t.Errorf("created %+v", p)
	}
	wantProblem(t, alice.do(http.MethodPost, "/api/plan-proposals", nil, nil), KindProposalGenerating)
	if got := decodeJSON[PlanProposal](t, alice.do(http.MethodGet, "/api/plan-proposals/latest", nil, nil), http.StatusOK); got.Id != p.Id {
		t.Errorf("latest %+v", got)
	}
	wantProblem(t, bob.do(http.MethodGet, "/api/plan-proposals/latest", nil, nil), KindNotFound)
}

func TestDecidePlanProposal(t *testing.T) {
	t.Parallel()
	pool := pg.NewDB(t)
	alice, q := newAPIOn(t, pool, true, nil)
	me := alice.session(alice.do(http.MethodPost, "/api/auth/register", register("alice"), nil), http.StatusCreated)
	bob := chatUser(t, pool, "bob")
	ctx := t.Context()

	// An open proposal: change the 13th, add the 14th.
	current := planWorkout(t, q, me.Id, "2026-11-13", "Easy")
	stored, err := q.CreatePlanProposal(ctx, db.CreatePlanProposalParams{UserID: me.Id, FromDay: current.Day, ToDay: current.Day.AddDate(0, 0, 13)})
	if err != nil {
		t.Fatal(err)
	}
	version := "plan.v2"
	if _, err := q.FillPlanProposal(ctx, db.FillPlanProposalParams{UserID: me.Id, ID: stored.ID, Status: "open", PromptVersion: &version}); err != nil {
		t.Fatal(err)
	}
	workout := func(day, title string) []byte {
		return fmt.Appendf(nil, `{"day": %q, "sport": "running", "title": %q, "steps": %s}`, day, title, intervalSteps)
	}
	for _, it := range []db.CreatePlanProposalItemParams{
		{Action: "change", WorkoutID: &current.ID, Before: workout("2026-11-13", "Easy"), After: workout("2026-11-13", "Tempo")},
		{Action: "add", After: workout("2026-11-14", "Long")},
	} {
		it.UserID, it.ProposalID = me.Id, stored.ID
		if err := q.CreatePlanProposalItem(ctx, it); err != nil {
			t.Fatal(err)
		}
	}
	p := decodeJSON[PlanProposal](t, alice.do(http.MethodGet, "/api/plan-proposals/latest", nil, nil), http.StatusOK)
	if p.Status != PlanProposalStatusOpen || len(p.Items) != 2 {
		t.Fatalf("proposal %+v", p)
	}
	change, add := p.Items[0], p.Items[1]
	if change.Action != Change || *change.WorkoutId != current.ID || change.Before.Title != "Easy" || change.After.Title != "Tempo" || len(change.After.Steps) != 3 ||
		add.Action != Add || add.Before != nil || add.WorkoutId != nil || add.After.Day.Format(time.DateOnly) != "2026-11-14" {
		t.Fatalf("items %+v %+v", change, add)
	}
	path := fmt.Sprintf("/api/plan-proposals/%d/decisions", p.Id)

	// Bad decisions change nothing.
	wantProblem(t, alice.do(http.MethodPost, path, PlanDecisions{Approve: []int64{add.Id, 999999}, Reject: []int64{}}, nil), KindInvalidRequest)
	wantProblem(t, bob.do(http.MethodPost, path, PlanDecisions{Approve: []int64{add.Id}, Reject: []int64{}}, nil), KindNotFound)
	wantProblem(t, alice.do(http.MethodPost, "/api/plan-proposals/999999/decisions", PlanDecisions{Approve: []int64{}, Reject: []int64{}}, nil), KindNotFound)
	planned := func() []PlannedWorkout {
		return decodeJSON[[]PlannedWorkout](t, alice.do(http.MethodGet, "/api/planned-workouts?from=2026-11-13&to=2026-11-14", nil, nil), http.StatusOK)
	}
	if got := planned(); len(got) != 1 || got[0].Title != "Easy" {
		t.Fatalf("before deciding %+v", got)
	}

	// Approving the addition and rejecting the change applies only the addition.
	p = decodeJSON[PlanProposal](t, alice.do(http.MethodPost, path, PlanDecisions{Approve: []int64{add.Id}, Reject: []int64{change.Id}}, nil), http.StatusOK)
	if p.Status != PlanProposalStatusClosed || p.Items[0].Status != Rejected || p.Items[1].Status != Approved {
		t.Errorf("after deciding %+v", p)
	}
	if got := planned(); len(got) != 2 || got[0].Title != "Easy" || got[1].Title != "Long" {
		t.Errorf("planned %+v", got)
	}
	wantProblem(t, alice.do(http.MethodPost, path, PlanDecisions{Approve: []int64{}, Reject: []int64{change.Id}}, nil), KindProposalClosed)
}
