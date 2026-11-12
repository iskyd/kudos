package plans

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

var (
	from = time.Date(2026, 11, 13, 0, 0, 0, 0, time.UTC)
	to   = time.Date(2026, 11, 26, 0, 0, 0, 0, time.UTC)
)

// draft builds a PlanDraft document.
func draft(keep string, workouts ...string) string {
	return `{"keep": [` + keep + `], "workouts": [` + strings.Join(workouts, ",") + `]}`
}

func on(day, steps string) string { return strings.Replace(workout(steps), "2026-11-12", day, 1) }

func TestValidateDraftAccepts(t *testing.T) {
	t.Parallel()
	d, err := ValidateDraft([]byte(draft("7", on("2026-11-13", recovery), on("2026-11-26", warmup+","+repeat(5, interval, recovery)))), from, to, []int64{7, 8})
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Keep) != 1 || d.Keep[0] != 7 || len(d.Workouts) != 2 || d.Workouts[1].Day != "2026-11-26" || d.Workouts[0].Title != "Intervals" {
		t.Errorf("draft = %+v", d)
	}
	if _, err := ValidateDraft([]byte(draft("")), from, to, nil); err != nil {
		t.Errorf("an empty plan: %v", err)
	}
}

func TestValidateDraftRejects(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct{ doc, want string }{
		"not JSON":        {`{"keep": [`, "not JSON"},
		"no keep":         {`{"workouts": []}`, "does not match the schema"},
		"bad workout":     {draft("", strings.Replace(on("2026-11-13", recovery), "running", "quidditch", 1)), "does not match the schema"},
		"before the plan": {draft("", on("2026-11-12", recovery)), "workouts/0: day 2026-11-12 is not between 2026-11-13 and 2026-11-26"},
		"after the plan":  {draft("", on("2026-11-13", recovery), on("2026-11-27", recovery)), "workouts/1: day 2026-11-27"},
		"bad step":        {draft("", on("2026-11-13", recovery), on("2026-11-14", `{"kind": "interval", "durationType": "distance"}`)), "workouts/1/steps/0: a distance step needs a duration"},
		"unknown keep":    {draft("7, 9"), "keep/1: 9 is not a workout of the current plan"},
	} {
		_, err := ValidateDraft([]byte(tc.doc), from, to, []int64{7})
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", name, err, tc.want)
		}
	}
}

func w(day, sport, title, steps string) Workout {
	return Workout{Day: day, Sport: sport, Title: title, Steps: json.RawMessage(steps)}
}

func TestDiff(t *testing.T) {
	t.Parallel()
	easy := `[{"kind": "recovery", "durationType": "time", "duration": 2700}]`
	// The same steps as stored: jsonb reorders keys and drops spaces.
	stored := `[{"duration":2700,"durationType":"time","kind":"recovery"}]`
	empty := ""
	current := []Current{
		{ID: 1, Workout: w("2026-11-13", "running", "Easy", stored)},    // kept
		{ID: 2, Workout: w("2026-11-14", "running", "Easy", stored)},    // replaced by an identical one: no change
		{ID: 3, Workout: w("2026-11-15", "running", "Easy", stored)},    // changed
		{ID: 4, Workout: w("2026-11-16", "cycling", "Ride", stored)},    // removed
		{ID: 5, Workout: w("2026-11-17", "running", "Easy", stored)},    // first of two: changed
		{ID: 6, Workout: w("2026-11-17", "running", "Strides", stored)}, // second of two: removed
		{ID: 8, Workout: w("2026-11-19", "strength", "Core", stored)},   // another sport that day: removed
	}
	same := w("2026-11-14", "running", "Easy", easy)
	same.Description = &empty
	got, err := Diff(current, Draft{Keep: []int64{1}, Workouts: []Workout{
		w("2026-11-18", "running", "Long", easy), // added
		same,
		w("2026-11-15", "running", "Tempo", easy),
		w("2026-11-17", "running", "Hills", easy),
		w("2026-11-19", "running", "Easy", easy), // added
	}})
	if err != nil {
		t.Fatal(err)
	}
	type row struct {
		action, day, title string
		id                 int64
	}
	var rows []row
	for _, c := range got {
		r := row{action: c.Action, id: c.WorkoutID}
		switch c.Action {
		case "add":
			r.day, r.title = c.After.Day, c.After.Title
			if c.Before != nil || c.WorkoutID != 0 {
				t.Errorf("an addition has a before or an ID: %+v", c)
			}
		case "change":
			r.day, r.title = c.After.Day, c.Before.Title+" → "+c.After.Title
		case "remove":
			r.day, r.title = c.Before.Day, c.Before.Title
			if c.After != nil {
				t.Errorf("a removal has an after: %+v", c)
			}
		}
		rows = append(rows, r)
	}
	want := []row{
		{"change", "2026-11-15", "Easy → Tempo", 3},
		{"remove", "2026-11-16", "Ride", 4},
		{"change", "2026-11-17", "Easy → Hills", 5},
		{"remove", "2026-11-17", "Strides", 6},
		{"add", "2026-11-18", "Long", 0},
		{"add", "2026-11-19", "Easy", 0},
		{"remove", "2026-11-19", "Core", 8},
	}
	if len(rows) != len(want) {
		t.Fatalf("changes = %+v, want %+v", rows, want)
	}
	for i := range want {
		if rows[i] != want[i] {
			t.Errorf("change %d = %+v, want %+v", i, rows[i], want[i])
		}
	}
}

func TestDiffRejectsUnreadableSteps(t *testing.T) {
	t.Parallel()
	_, err := Diff([]Current{{ID: 1, Workout: w("2026-11-13", "running", "Easy", `[`)}},
		Draft{Workouts: []Workout{w("2026-11-13", "running", "Easy", `[]`)}})
	if err == nil {
		t.Error("no error for unreadable stored steps")
	}
}
