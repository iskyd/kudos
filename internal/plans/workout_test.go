package plans

import (
	"strings"
	"testing"
)

// workout wraps steps in a valid planned workout.
func workout(steps string) string {
	return `{"day": "2026-11-12", "sport": "running", "title": "Intervals", "steps": [` + steps + `]}`
}

const (
	warmup   = `{"kind": "warmup", "durationType": "time", "duration": 600, "target": {"metric": "heart_rate", "zone": 2}}`
	interval = `{"kind": "interval", "durationType": "distance", "duration": 1000, "target": {"metric": "pace", "low": 3.9, "high": 4.1}}`
	recovery = `{"kind": "recovery", "durationType": "time", "duration": 90}`
	cooldown = `{"kind": "cooldown", "durationType": "open", "note": "easy jog home"}`
)

func repeat(count int, steps ...string) string {
	return `{"kind": "repeat", "count": ` + string(rune('0'+count)) + `, "steps": [` + strings.Join(steps, ",") + `]}`
}

func TestValidateWorkoutAccepts(t *testing.T) {
	t.Parallel()
	for name, doc := range map[string]string{
		"steps and a repeat": workout(warmup + "," + repeat(5, interval, recovery) + "," + cooldown),
		"a repeat in a repeat": workout(warmup + "," +
			repeat(3, repeat(4, `{"kind": "interval", "durationType": "time", "duration": 30, "target": {"metric": "power", "low": 300, "high": 320}}`, recovery), recovery)),
		"no target":             workout(recovery),
		"description, no notes": `{"day": "2026-11-12", "sport": "cycling", "title": "Ride", "description": "Steady", "steps": [` + recovery + `]}`,
		"equal low and high":    workout(`{"kind": "interval", "durationType": "time", "duration": 60, "target": {"metric": "power", "low": 250, "high": 250}}`),
	} {
		if err := ValidateWorkout([]byte(doc)); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestValidateWorkoutRejects(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct{ doc, want string }{
		// Missing or malformed fields: the schema.
		"not JSON":         {`{"day":`, "not JSON"},
		"no title":         {`{"day": "2026-11-12", "sport": "running", "steps": [` + recovery + `]}`, "does not match the schema"},
		"no steps":         {workout(""), "does not match the schema"},
		"unknown sport":    {strings.Replace(workout(recovery), "running", "quidditch", 1), "does not match the schema"},
		"unknown field":    {workout(`{"kind": "recovery", "durationType": "time", "duration": 90, "pace": "easy"}`), "does not match the schema"},
		"unknown kind":     {workout(`{"kind": "sprint", "durationType": "time", "duration": 9}`), "does not match the schema"},
		"zero duration":    {workout(`{"kind": "interval", "durationType": "time", "duration": 0}`), "does not match the schema"},
		"no duration type": {workout(`{"kind": "interval", "duration": 60}`), "steps/0: durationType is required"},
		"no duration":      {workout(`{"kind": "interval", "durationType": "distance"}`), "steps/0: a distance step needs a duration"},
		"open with duration": {
			workout(`{"kind": "cooldown", "durationType": "open", "duration": 600}`), "steps/0: an open step has no duration",
		},
		// Bad targets.
		"zone 8":          {workout(`{"kind": "interval", "durationType": "time", "duration": 60, "target": {"metric": "heart_rate", "zone": 8}}`), "does not match the schema"},
		"unknown metric":  {workout(`{"kind": "interval", "durationType": "time", "duration": 60, "target": {"metric": "cadence", "zone": 2}}`), "does not match the schema"},
		"no metric":       {workout(`{"kind": "interval", "durationType": "time", "duration": 60, "target": {"zone": 2}}`), "does not match the schema"},
		"empty target":    {workout(`{"kind": "interval", "durationType": "time", "duration": 60, "target": {"metric": "power"}}`), "steps/0: a target needs a zone, or both low and high"},
		"low only":        {workout(`{"kind": "interval", "durationType": "time", "duration": 60, "target": {"metric": "power", "low": 200}}`), "steps/0: a target needs a zone, or both low and high"},
		"zone and range":  {workout(`{"kind": "interval", "durationType": "time", "duration": 60, "target": {"metric": "power", "zone": 3, "low": 200, "high": 220}}`), "not both"},
		"low above high":  {workout(`{"kind": "interval", "durationType": "time", "duration": 60, "target": {"metric": "power", "low": 300, "high": 200}}`), "0 < low <= high"},
		"zero low":        {workout(`{"kind": "interval", "durationType": "time", "duration": 60, "target": {"metric": "pace", "low": 0, "high": 4}}`), "0 < low <= high"},
		"negative values": {workout(`{"kind": "interval", "durationType": "time", "duration": 60, "target": {"metric": "pace", "low": -1, "high": 4}}`), "does not match the schema"},
		// Repeats.
		"repeat without count": {workout(`{"kind": "repeat", "steps": [` + interval + `]}`), "steps/0: a repeat needs count and steps"},
		"repeat without steps": {workout(`{"kind": "repeat", "count": 3}`), "steps/0: a repeat needs count and steps"},
		"repeat of one":        {workout(`{"kind": "repeat", "count": 1, "steps": [` + interval + `]}`), "does not match the schema"},
		"repeat with duration": {workout(`{"kind": "repeat", "count": 3, "durationType": "time", "duration": 60, "steps": [` + interval + `]}`), "steps/0: a repeat has only count and steps"},
		"steps in a step":      {workout(`{"kind": "interval", "durationType": "time", "duration": 60, "steps": [` + recovery + `]}`), "steps/0: only a repeat has count and steps"},
		"three levels": {
			workout(warmup + "," + repeat(2, repeat(2, repeat(2, interval)))), "steps/1/steps/0/steps/0: repeats nest at most 2 deep",
		},
		"bad step in a repeat": {
			workout(repeat(4, interval, `{"kind": "recovery", "durationType": "time"}`)), "steps/0/steps/1: a time step needs a duration",
		},
		"too many steps once written out": {
			workout(repeat(9, repeat(9, interval, recovery, interval))), "243 steps once its repeats are written out",
		},
	} {
		err := ValidateWorkout([]byte(tc.doc))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: got %v, want %q", name, err, tc.want)
		}
	}
}
