package store_test

import (
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/iskyd/kudos/internal/store/db"
	"github.com/iskyd/kudos/internal/testutil"
)

func TestPlannedWorkouts(t *testing.T) {
	t.Parallel()
	pool := pg.NewDB(t)
	alice, bob := testutil.TwoUsers(t, pool)
	q := db.New(pool)
	ctx := t.Context()
	on := func(s string) time.Time {
		d, _ := time.Parse(time.DateOnly, s)
		return d
	}
	plan, err := q.CreatePlan(ctx, alice.ID)
	check(t, err)
	steps := []byte(`[{"kind": "recovery", "durationType": "time", "duration": 1800}]`)
	add := func(userID, planID int64, day, title string) (db.PlannedWorkout, error) {
		return q.CreatePlannedWorkout(ctx, db.CreatePlannedWorkoutParams{
			UserID: userID, PlanID: planID, Day: on(day), Sport: "running", Title: title, Steps: steps,
		})
	}
	long, err := add(alice.ID, plan.ID, "2026-11-15", "Long run")
	check(t, err)
	_, err = add(alice.ID, plan.ID, "2026-11-12", "Easy run")
	check(t, err)
	_, err = add(alice.ID, plan.ID, "2026-12-01", "Later")
	check(t, err)

	got, err := q.ListPlannedWorkoutsBetween(ctx, db.ListPlannedWorkoutsBetweenParams{UserID: alice.ID, FromDay: on("2026-11-09"), ToDay: on("2026-11-15"), MaxRows: 10})
	check(t, err)
	if len(got) != 2 || got[0].Title != "Easy run" || got[1].Title != "Long run" {
		t.Fatalf("between %+v", got)
	}
	one, err := q.GetPlannedWorkout(ctx, db.GetPlannedWorkoutParams{UserID: alice.ID, ID: long.ID})
	check(t, err)
	if one.PlanID != plan.ID || string(one.Steps) != `[{"kind": "recovery", "duration": 1800, "durationType": "time"}]` {
		t.Errorf("get %+v %s", one, one.Steps)
	}

	// Stored steps are always an array.
	if _, err := q.CreatePlannedWorkout(ctx, db.CreatePlannedWorkoutParams{
		UserID: alice.ID, PlanID: plan.ID, Day: on("2026-11-12"), Sport: "running", Title: "x", Steps: []byte(`{"kind": "recovery"}`),
	}); err == nil {
		t.Error("steps that are not an array were stored")
	}

	// Bob neither reads alice's workouts nor adds to her plan.
	if _, err := q.GetPlannedWorkout(ctx, db.GetPlannedWorkoutParams{UserID: bob.ID, ID: long.ID}); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("bob reads alice's workout: %v", err)
	}
	if rows, err := q.ListPlannedWorkoutsBetween(ctx, db.ListPlannedWorkoutsBetweenParams{UserID: bob.ID, FromDay: on("2026-01-01"), ToDay: on("2026-12-31"), MaxRows: 10}); err != nil || len(rows) != 0 {
		t.Errorf("bob lists %+v, %v", rows, err)
	}
	if _, err := add(bob.ID, plan.ID, "2026-11-12", "Into alice's plan"); err == nil {
		t.Error("bob added a workout to alice's plan")
	}
}

func TestPlannedWorkoutWritesAreIsolated(t *testing.T) {
	t.Parallel()
	pool := pg.NewDB(t)
	alice, bob := testutil.TwoUsers(t, pool)
	q := db.New(pool)
	ctx := t.Context()
	plan, err := q.CreatePlan(ctx, alice.ID)
	check(t, err)
	steps := []byte(`[{"kind": "recovery", "durationType": "time", "duration": 1800}]`)
	w, err := q.CreatePlannedWorkout(ctx, db.CreatePlannedWorkoutParams{
		UserID: alice.ID, PlanID: plan.ID, Day: time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC), Sport: "running", Title: "Long run", Steps: steps,
	})
	check(t, err)
	update := db.UpdatePlannedWorkoutParams{ID: w.ID, Day: w.Day, Sport: "cycling", Title: "Ride", Steps: steps}

	update.UserID = bob.ID
	if n, err := q.UpdatePlannedWorkout(ctx, update); err != nil || n != 0 {
		t.Errorf("bob updated alice's workout: %d, %v", n, err)
	}
	if n, err := q.DeletePlannedWorkout(ctx, db.DeletePlannedWorkoutParams{UserID: bob.ID, ID: w.ID}); err != nil || n != 0 {
		t.Errorf("bob deleted alice's workout: %d, %v", n, err)
	}
	update.UserID = alice.ID
	if n, err := q.UpdatePlannedWorkout(ctx, update); err != nil || n != 1 {
		t.Fatalf("update: %d, %v", n, err)
	}
	got, err := q.GetPlannedWorkout(ctx, db.GetPlannedWorkoutParams{UserID: alice.ID, ID: w.ID})
	if err != nil || got.Title != "Ride" || got.Sport != "cycling" {
		t.Errorf("after update %+v, %v", got, err)
	}
	if n, err := q.DeletePlannedWorkout(ctx, db.DeletePlannedWorkoutParams{UserID: alice.ID, ID: w.ID}); err != nil || n != 1 {
		t.Errorf("delete: %d, %v", n, err)
	}
}

func TestPlanProposals(t *testing.T) {
	t.Parallel()
	pool := pg.NewDB(t)
	alice, bob := testutil.TwoUsers(t, pool)
	q := db.New(pool)
	ctx := t.Context()
	from, to := time.Date(2026, 11, 13, 0, 0, 0, 0, time.UTC), time.Date(2026, 11, 26, 0, 0, 0, 0, time.UTC)
	create := func(userID int64) db.PlanProposal {
		t.Helper()
		p, err := q.CreatePlanProposal(ctx, db.CreatePlanProposalParams{UserID: userID, FromDay: from, ToDay: to})
		check(t, err)
		return p
	}
	old := create(alice.ID)
	if old.Status != "generating" {
		t.Fatalf("created %+v", old)
	}
	// One generating proposal per user.
	if _, err := q.CreatePlanProposal(ctx, db.CreatePlanProposalParams{UserID: alice.ID, FromDay: from, ToDay: to}); err == nil {
		t.Fatal("a second generating proposal")
	}
	bobs := create(bob.ID) // bob's does not count against alice

	// Expiring and failing touch only the user's own.
	check(t, q.ExpirePlanProposals(ctx, db.ExpirePlanProposalsParams{UserID: bob.ID, Before: time.Now().Add(time.Hour)}))
	if p, err := q.GetPlanProposal(ctx, db.GetPlanProposalParams{UserID: alice.ID, ID: old.ID}); err != nil || p.Status != "generating" {
		t.Errorf("bob's expiry reached alice: %+v, %v", p, err)
	}
	if p, err := q.GetPlanProposal(ctx, db.GetPlanProposalParams{UserID: bob.ID, ID: bobs.ID}); err != nil || p.Status != "failed" || *p.Failure != "timeout" {
		t.Errorf("bob's after expiry: %+v, %v", p, err)
	}
	invalid := "invalid_output"
	check(t, q.FailPlanProposal(ctx, db.FailPlanProposalParams{UserID: bob.ID, ID: old.ID, Failure: &invalid}))
	version := "plan.v2"
	if n, err := q.FillPlanProposal(ctx, db.FillPlanProposalParams{UserID: bob.ID, ID: old.ID, Status: "open", PromptVersion: &version}); err != nil || n != 0 {
		t.Errorf("bob filled alice's proposal: %d, %v", n, err)
	}
	if n, err := q.FillPlanProposal(ctx, db.FillPlanProposalParams{UserID: alice.ID, ID: old.ID, Status: "open", PromptVersion: &version}); err != nil || n != 1 {
		t.Fatalf("fill: %d, %v", n, err)
	}

	// Items join only the user's own proposals.
	after := []byte(`{"day": "2026-11-13", "sport": "running", "title": "Easy", "steps": []}`)
	check(t, q.CreatePlanProposalItem(ctx, db.CreatePlanProposalItemParams{UserID: alice.ID, ProposalID: old.ID, Action: "add", After: after}))
	if err := q.CreatePlanProposalItem(ctx, db.CreatePlanProposalItemParams{UserID: bob.ID, ProposalID: old.ID, Action: "add", After: after}); err == nil {
		t.Error("bob added an item to alice's proposal")
	}
	// An item's action decides what it holds.
	if err := q.CreatePlanProposalItem(ctx, db.CreatePlanProposalItemParams{UserID: alice.ID, ProposalID: old.ID, Action: "remove", After: after}); err == nil {
		t.Error("a removal without a workout and before was stored")
	}
	items, err := q.ListPlanProposalItems(ctx, db.ListPlanProposalItemsParams{UserID: alice.ID, ProposalID: old.ID, MaxRows: 10})
	check(t, err)
	if len(items) != 1 || items[0].Status != "pending" {
		t.Fatalf("items %+v", items)
	}
	if rows, err := q.ListPlanProposalItems(ctx, db.ListPlanProposalItemsParams{UserID: bob.ID, ProposalID: old.ID, MaxRows: 10}); err != nil || len(rows) != 0 {
		t.Errorf("bob lists %+v, %v", rows, err)
	}
	if n, err := q.DecidePlanProposalItem(ctx, db.DecidePlanProposalItemParams{UserID: bob.ID, ProposalID: old.ID, ID: items[0].ID, Status: "approved"}); err != nil || n != 0 {
		t.Errorf("bob decided alice's item: %d, %v", n, err)
	}
	if n, err := q.CountPendingPlanProposalItems(ctx, db.CountPendingPlanProposalItemsParams{UserID: bob.ID, ProposalID: old.ID}); err != nil || n != 0 {
		t.Errorf("bob counts %d, %v", n, err)
	}
	if n, err := q.CountPendingPlanProposalItems(ctx, db.CountPendingPlanProposalItemsParams{UserID: alice.ID, ProposalID: old.ID}); err != nil || n != 1 {
		t.Errorf("alice counts %d, %v", n, err)
	}
	if n, err := q.DecidePlanProposalItem(ctx, db.DecidePlanProposalItemParams{UserID: alice.ID, ProposalID: old.ID, ID: items[0].ID, Status: "approved"}); err != nil || n != 1 {
		t.Errorf("decide: %d, %v", n, err)
	}
	if n, err := q.DecidePlanProposalItem(ctx, db.DecidePlanProposalItemParams{UserID: alice.ID, ProposalID: old.ID, ID: items[0].ID, Status: "rejected"}); err != nil || n != 0 {
		t.Errorf("decided twice: %d, %v", n, err)
	}

	// Plans link only within the user; status and superseding stay with the user.
	bobPlan, err := q.CreatePlan(ctx, bob.ID)
	check(t, err)
	if err := q.SetPlanProposalPlan(ctx, db.SetPlanProposalPlanParams{UserID: alice.ID, ID: old.ID, PlanID: &bobPlan.ID}); err == nil {
		t.Error("alice's proposal links bob's plan")
	}
	check(t, q.SetPlanProposalStatus(ctx, db.SetPlanProposalStatusParams{UserID: bob.ID, ID: old.ID, Status: "closed"}))
	check(t, q.SupersedePlanProposals(ctx, db.SupersedePlanProposalsParams{UserID: bob.ID, ID: 0}))
	if p, err := q.LockPlanProposal(ctx, db.LockPlanProposalParams{UserID: alice.ID, ID: old.ID}); err != nil || p.Status != "open" {
		t.Errorf("bob changed alice's status: %+v, %v", p, err)
	}
	newer := create(alice.ID)
	check(t, q.SupersedePlanProposals(ctx, db.SupersedePlanProposalsParams{UserID: alice.ID, ID: newer.ID}))
	if p, err := q.GetPlanProposal(ctx, db.GetPlanProposalParams{UserID: alice.ID, ID: old.ID}); err != nil || p.Status != "superseded" {
		t.Errorf("after superseding %+v, %v", p, err)
	}
	if p, err := q.GetLatestPlanProposal(ctx, alice.ID); err != nil || p.ID != newer.ID {
		t.Errorf("latest %+v, %v", p, err)
	}
	for _, read := range []func() error{
		func() error {
			_, err := q.GetPlanProposal(ctx, db.GetPlanProposalParams{UserID: bob.ID, ID: newer.ID})
			return err
		},
		func() error {
			_, err := q.LockPlanProposal(ctx, db.LockPlanProposalParams{UserID: bob.ID, ID: newer.ID})
			return err
		},
	} {
		if err := read(); !errors.Is(err, pgx.ErrNoRows) {
			t.Errorf("bob reads alice's proposal: %v", err)
		}
	}
	if p, err := q.GetLatestPlanProposal(ctx, bob.ID); err != nil || p.ID != bobs.ID {
		t.Errorf("bob's latest %+v, %v", p, err)
	}
}

func TestPlannedWorkoutLinks(t *testing.T) {
	t.Parallel()
	pool := pg.NewDB(t)
	alice, bob := testutil.TwoUsers(t, pool)
	q := db.New(pool)
	ctx := t.Context()
	day := time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)
	steps := []byte(`[{"kind": "recovery", "durationType": "time", "duration": 1800}]`)
	workout := func(userID int64) db.PlannedWorkout {
		t.Helper()
		plan, err := q.CreatePlan(ctx, userID)
		check(t, err)
		w, err := q.CreatePlannedWorkout(ctx, db.CreatePlannedWorkoutParams{UserID: userID, PlanID: plan.ID, Day: day, Sport: "running", Title: "Easy", Steps: steps})
		check(t, err)
		return w
	}
	activity := func(userID int64, source string) int64 {
		t.Helper()
		id, err := q.UpsertActivity(ctx, db.UpsertActivityParams{UserID: userID, Source: "manual", SourceActivityID: source, Sport: "running", Subtype: "outdoor", StartTime: day.Add(7 * time.Hour)})
		check(t, err)
		return id
	}
	first, second, bobs := workout(alice.ID), workout(alice.ID), workout(bob.ID)
	run, bobRun := activity(alice.ID, "a"), activity(bob.ID, "b")

	link, err := q.PutPlannedWorkoutLink(ctx, db.PutPlannedWorkoutLinkParams{UserID: alice.ID, PlannedWorkoutID: first.ID, ActivityID: &run})
	if err != nil || *link.ActivityID != run {
		t.Fatalf("link %+v, %v", link, err)
	}
	// An activity completes one planned workout; unlinking stores no activity.
	if _, err := q.PutPlannedWorkoutLink(ctx, db.PutPlannedWorkoutLinkParams{UserID: alice.ID, PlannedWorkoutID: second.ID, ActivityID: &run}); err == nil {
		t.Error("one activity linked twice")
	}
	check(t, func() error {
		_, err := q.PutPlannedWorkoutLink(ctx, db.PutPlannedWorkoutLinkParams{UserID: alice.ID, PlannedWorkoutID: second.ID})
		return err
	}())
	// Nobody links across users, either way.
	for name, p := range map[string]db.PutPlannedWorkoutLinkParams{
		"bob's activity":   {UserID: alice.ID, PlannedWorkoutID: first.ID, ActivityID: &bobRun},
		"alice's workout":  {UserID: bob.ID, PlannedWorkoutID: first.ID, ActivityID: &bobRun},
		"alice's activity": {UserID: bob.ID, PlannedWorkoutID: bobs.ID, ActivityID: &run},
	} {
		if l, err := q.PutPlannedWorkoutLink(ctx, p); err == nil {
			t.Errorf("%s: linked %+v", name, l)
		}
	}
	got, err := q.ListPlannedWorkoutLinks(ctx, db.ListPlannedWorkoutLinksParams{UserID: alice.ID, PlannedWorkoutIds: []int64{second.ID}, ActivityIds: []int64{run}, MaxRows: 10})
	check(t, err)
	if len(got) != 2 || got[0].PlannedWorkoutID != first.ID || *got[0].ActivityID != run || got[1].ActivityID != nil {
		t.Errorf("links %+v", got)
	}
	if rows, err := q.ListPlannedWorkoutLinks(ctx, db.ListPlannedWorkoutLinksParams{UserID: bob.ID, PlannedWorkoutIds: []int64{first.ID, second.ID}, ActivityIds: []int64{run}, MaxRows: 10}); err != nil || len(rows) != 0 {
		t.Errorf("bob lists %+v, %v", rows, err)
	}
	if n, err := q.DeletePlannedWorkoutLink(ctx, db.DeletePlannedWorkoutLinkParams{UserID: bob.ID, PlannedWorkoutID: first.ID}); err != nil || n != 0 {
		t.Errorf("bob unset alice's link: %d, %v", n, err)
	}
	if n, err := q.DeletePlannedWorkoutLink(ctx, db.DeletePlannedWorkoutLinkParams{UserID: alice.ID, PlannedWorkoutID: first.ID}); err != nil || n != 1 {
		t.Errorf("unset: %d, %v", n, err)
	}

	// Recorded workout names stay with their user.
	check(t, q.UpsertRecordedWorkout(ctx, db.UpsertRecordedWorkoutParams{UserID: alice.ID, ActivityID: run, Name: new("Easy"), Steps: []byte(`[]`)}))
	if names, err := q.ListRecordedWorkoutNames(ctx, db.ListRecordedWorkoutNamesParams{UserID: alice.ID, ActivityIds: []int64{run, bobRun}, MaxRows: 10}); err != nil || len(names) != 1 || *names[0].Name != "Easy" {
		t.Errorf("names %+v, %v", names, err)
	}
	if names, err := q.ListRecordedWorkoutNames(ctx, db.ListRecordedWorkoutNamesParams{UserID: bob.ID, ActivityIds: []int64{run}, MaxRows: 10}); err != nil || len(names) != 0 {
		t.Errorf("bob reads %+v, %v", names, err)
	}

	// The setting is per user.
	check(t, q.SetAutoMatchWorkouts(ctx, db.SetAutoMatchWorkoutsParams{ID: alice.ID, AutoMatch: false}))
	if a, err := q.GetAutoMatchWorkouts(ctx, alice.ID); err != nil || a {
		t.Errorf("alice's setting %v, %v", a, err)
	}
	if b, err := q.GetAutoMatchWorkouts(ctx, bob.ID); err != nil || !b {
		t.Errorf("bob's setting %v, %v", b, err)
	}
}
