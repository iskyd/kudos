package plans

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/iskyd/kudos/internal/llm"
)

// DraftSchema is PlanDraft from api/openapi.yaml as a JSON Schema (see
// generate.go): what the LLM replies when it plans.
//
//go:embed draft.schema.json
var DraftSchema []byte

// Workout is a PlannedWorkoutInput of api/openapi.yaml.
type Workout struct {
	Day         string          `json:"day"` // local, YYYY-MM-DD
	Sport       string          `json:"sport"`
	Title       string          `json:"title"`
	Description *string         `json:"description,omitempty"`
	Steps       json.RawMessage `json:"steps"`
}

// Draft is a PlanDraft: the current workouts the model keeps, and its others.
type Draft struct {
	Keep     []int64   `json:"keep"`
	Workouts []Workout `json:"workouts"`
}

// Current is a planned workout already on one of the planned days.
type Current struct {
	ID      int64
	Workout Workout
}

// ValidateDraft checks the model's plan for the local days from to to against
// the schema, then the rules it cannot say: each workout's steps, as in
// ValidateWorkout, a day in the window, and kept IDs among current. Errors name
// the place (workouts/2/steps/0) so the model can correct it.
func ValidateDraft(doc []byte, from, to time.Time, current []int64) (Draft, error) {
	s, err := llm.CompileSchema("plan-draft", DraftSchema)
	if err != nil {
		return Draft{}, err
	}
	if err := s.Validate(doc); err != nil {
		return Draft{}, err
	}
	var d Draft
	var steps struct {
		Workouts []struct {
			Steps []step `json:"steps"`
		} `json:"workouts"`
	}
	if err := json.Unmarshal(doc, &d); err != nil {
		return Draft{}, fmt.Errorf("reading the plan: %w", err)
	}
	if err := json.Unmarshal(doc, &steps); err != nil {
		return Draft{}, fmt.Errorf("reading the plan: %w", err)
	}
	for i, w := range d.Workouts {
		at := fmt.Sprintf("workouts/%d", i)
		day, err := time.Parse(time.DateOnly, w.Day)
		if err != nil || day.Before(from) || day.After(to) {
			return Draft{}, fmt.Errorf("%s: day %s is not between %s and %s", at, w.Day, from.Format(time.DateOnly), to.Format(time.DateOnly))
		}
		if err := checkWorkout(steps.Workouts[i].Steps, at+"/steps"); err != nil {
			return Draft{}, err
		}
	}
	for i, id := range d.Keep {
		if !slices.Contains(current, id) {
			return Draft{}, fmt.Errorf("keep/%d: %d is not a workout of the current plan in these days", i, id)
		}
	}
	return d, nil
}

// Change is a proposed change to the plan: an addition (After only), a change
// (Before and After) or a removal (Before only) of the workout WorkoutID.
type Change struct {
	Action    string // add, change or remove
	WorkoutID int64  // 0 for an addition
	Before    *Workout
	After     *Workout
}

// Diff turns a draft into changes to the current workouts of its days. A
// current workout the draft does not keep is replaced, in order, by a draft
// workout of the same day and sport (a change, or nothing when they are the
// same); one left unreplaced is removed, and a draft workout left over is
// added. Changes come by day, then as the draft and current list them.
func Diff(current []Current, d Draft) ([]Change, error) {
	type slot struct{ day, sport string }
	open := map[slot][]Current{}
	for _, c := range current {
		if !slices.Contains(d.Keep, c.ID) {
			s := slot{c.Workout.Day, c.Workout.Sport}
			open[s] = append(open[s], c)
		}
	}
	var changes []Change
	for _, w := range d.Workouts {
		s := slot{w.Day, w.Sport}
		if len(open[s]) == 0 {
			changes = append(changes, Change{Action: "add", After: &w})
			continue
		}
		c := open[s][0]
		open[s] = open[s][1:]
		same, err := equal(c.Workout, w)
		if err != nil {
			return nil, err
		}
		if !same {
			changes = append(changes, Change{Action: "change", WorkoutID: c.ID, Before: &c.Workout, After: &w})
		}
	}
	for _, c := range current { // in their order, not the map's
		s := slot{c.Workout.Day, c.Workout.Sport}
		if slices.ContainsFunc(open[s], func(o Current) bool { return o.ID == c.ID }) {
			changes = append(changes, Change{Action: "remove", WorkoutID: c.ID, Before: &c.Workout})
		}
	}
	slices.SortStableFunc(changes, func(a, b Change) int { return strings.Compare(a.day(), b.day()) })
	return changes, nil
}

func (c Change) day() string {
	if c.After != nil {
		return c.After.Day
	}
	return c.Before.Day
}

// equal compares workouts as JSON values: stored steps come back from jsonb
// with their keys reordered, and an empty description is none.
func equal(a, b Workout) (bool, error) {
	ca, err := canonical(a)
	if err != nil {
		return false, err
	}
	cb, err := canonical(b)
	if err != nil {
		return false, err
	}
	return bytes.Equal(ca, cb), nil
}

func canonical(w Workout) ([]byte, error) {
	if w.Description != nil && *w.Description == "" {
		w.Description = nil
	}
	var steps any
	if err := json.Unmarshal(w.Steps, &steps); err != nil {
		return nil, fmt.Errorf("reading steps: %w", err)
	}
	// The outer Steps shadows Workout's: the decoded value encodes with sorted keys.
	b, err := json.Marshal(struct {
		Workout
		Steps any `json:"steps"`
	}{w, steps})
	if err != nil {
		return nil, fmt.Errorf("encoding workout: %w", err)
	}
	return b, nil
}
