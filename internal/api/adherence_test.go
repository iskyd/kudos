package api

import (
	"testing"
	"time"

	"github.com/iskyd/kudos/internal/llm"
	"github.com/iskyd/kudos/internal/store/db"
)

func TestPlanContextAdherence(t *testing.T) {
	t.Parallel()
	c, q, me := loggedIn(t, "UTC")
	ctx := t.Context()
	id := uploadCorpus(t, c, "intervals.fit")
	a, err := q.GetActivity(ctx, db.GetActivityParams{UserID: me.Id, ID: id})
	if err != nil {
		t.Fatal(err)
	}
	rec, err := q.GetRecordedWorkout(ctx, db.GetRecordedWorkoutParams{UserID: me.Id, ActivityID: id})
	if err != nil || rec.Name == nil {
		t.Fatalf("recorded workout %+v, %v", rec, err)
	}
	day := localDay(a.StartTime, a.UtcOffsetS)
	on := func(d time.Time) string { return d.Format(time.DateOnly) }
	// Planned under the name the watch recorded, and as another sport, so only the name can match it.
	named := planWorkout(t, q, me.Id, on(day), *rec.Name)
	if _, err := q.UpdatePlannedWorkout(ctx, db.UpdatePlannedWorkoutParams{
		UserID: me.Id, ID: named.ID, Day: day, Sport: "cycling", Title: *rec.Name, Steps: named.Steps,
	}); err != nil {
		t.Fatal(err)
	}
	missed := planWorkout(t, q, me.Id, on(day.AddDate(0, 0, -1)), "Easy")
	later := planWorkout(t, q, me.Id, on(day.AddDate(0, 0, 3)), "Tomorrow")

	// The steps the execution compared with a target (T3.8): every rep ran faster than planned.
	exec := execution(t, c, id)
	compared, within := 0, 0
	for _, st := range *exec.Steps {
		if st.Result != nil && *st.Result != Skipped {
			compared++
			if *st.Result == Within {
				within++
			}
		}
	}
	if compared < 10 {
		t.Fatalf("the execution compared %d steps", compared)
	}

	// The day after: the named workout done with its steps, the day before missed, later still to come.
	srv := newServer(q, nil, nil, GarminDeps{}, nil, true, "", cheapArgon())
	plan, adh, err := srv.planContext(ctx, me.Id, day.AddDate(0, 0, 1))
	if err != nil {
		t.Fatal(err)
	}
	if len(plan) != 3 || plan[0].ID != missed.ID || plan[0].ActivityID != nil || plan[1].ID != named.ID || *plan[1].ActivityID != id ||
		plan[2].ID != later.ID || plan[2].ActivityID != nil {
		t.Fatalf("plan %+v", plan)
	}
	// Recent execution: every rep ran faster than its target.
	if st := plan[1].Steps; st == nil || st.Within != within || st.Above < 10 || st.Within+st.Above+st.Below != compared || plan[0].Steps != nil {
		t.Errorf("steps %+v", plan[1].Steps)
	}
	want := llmAdherence(day.AddDate(0, 0, 1-adherenceDays), day, 2, 1, 1050, *a.TimerTimeS, within, compared)
	if *adh != want {
		t.Errorf("adherence %+v, want %+v", *adh, want)
	}

	// Linked to a workout planned on another day, the activity still counts.
	if _, err := q.PutPlannedWorkoutLink(ctx, db.PutPlannedWorkoutLinkParams{UserID: me.Id, PlannedWorkoutID: missed.ID, ActivityID: &id}); err != nil {
		t.Fatal(err)
	}
	_, adh, err = srv.planContext(ctx, me.Id, day.AddDate(0, 0, 1))
	if err != nil {
		t.Fatal(err)
	}
	if adh.Completed != 1 || adh.Planned != 2 || adh.StepsCompared != compared {
		t.Errorf("after linking: %+v", *adh)
	}
	// Nothing planned before today: no adherence.
	if _, adh, err := srv.planContext(ctx, me.Id, day.AddDate(0, 0, -5)); err != nil || adh != nil {
		t.Errorf("before any plan: %+v, %v", adh, err)
	}
}

func llmAdherence(from, to time.Time, planned, completed int, plannedS, actualS float64, within, compared int) llm.Adherence {
	return llm.Adherence{
		From: from, To: to, Planned: planned, Completed: completed, PlannedS: plannedS, ActualS: actualS,
		StepsWithin: within, StepsCompared: compared,
	}
}
