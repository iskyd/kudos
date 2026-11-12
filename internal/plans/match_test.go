package plans

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/iskyd/kudos/internal/analytics"
)

func TestTotals(t *testing.T) {
	t.Parallel()
	d, m, err := Totals(json.RawMessage(`[` + warmup + `,` + repeat(5, interval, recovery) + `,` + cooldown + `]`))
	if err != nil || d != 600+5*90 || m != 5*1000 {
		t.Errorf("totals %v s, %v m, %v", d, m, err)
	}
	if _, _, err := Totals(json.RawMessage(`{`)); err == nil {
		t.Error("no error for unreadable steps")
	}
}

func ptr(n int64) *int64 { return &n }

func TestMatch(t *testing.T) {
	t.Parallel()
	const day = "2026-11-10"
	for _, tc := range []struct {
		name    string
		planned []Planned
		done    []Done
		links   []Link
		auto    bool
		want    map[int64]Completion
	}{
		{
			name:    "the recorded workout's name first",
			planned: []Planned{{ID: 1, Day: day, Sport: "running", Title: "5 × 1 km", DurationS: 2400}, {ID: 2, Day: day, Sport: "running", Title: "Easy", DurationS: 2400}},
			done:    []Done{{ID: 10, Day: day, Sport: "running", WorkoutName: " 5 × 1 KM", DurationS: 3600}, {ID: 11, Day: day, Sport: "running", DurationS: 2400}},
			auto:    true,
			want:    map[int64]Completion{1: {ActivityID: 10}, 2: {ActivityID: 11}},
		},
		{
			name:    "a name on another day does not match",
			planned: []Planned{{ID: 1, Day: day, Sport: "cycling", Title: "Sweet spot"}},
			done:    []Done{{ID: 10, Day: "2026-11-11", Sport: "cycling", WorkoutName: "Sweet spot"}},
			auto:    true,
			want:    map[int64]Completion{},
		},
		{
			name: "ambiguous same-day workouts pair by the closest duration",
			planned: []Planned{
				{ID: 1, Day: day, Sport: "running", Title: "Easy", DurationS: 1800},
				{ID: 2, Day: day, Sport: "running", Title: "Long", DurationS: 5400},
				{ID: 3, Day: day, Sport: "cycling", Title: "Spin", DurationS: 3600},
			},
			done: []Done{
				{ID: 10, Day: day, Sport: "running", DurationS: 5100},
				{ID: 11, Day: day, Sport: "trail_running", DurationS: 2100},
			},
			auto: true,
			want: map[int64]Completion{1: {ActivityID: 11}, 2: {ActivityID: 10}},
		},
		{
			name:    "distance when the plan has no time",
			planned: []Planned{{ID: 1, Day: day, Sport: "pool_swimming", Title: "Swim", DistanceM: 2000}},
			done:    []Done{{ID: 10, Day: day, Sport: "open_water_swimming", DistanceM: 3000}, {ID: 11, Day: day, Sport: "pool_swimming", DistanceM: 2100}},
			auto:    true,
			want:    map[int64]Completion{1: {ActivityID: 11}},
		},
		{
			name:    "no match: another sport, another day",
			planned: []Planned{{ID: 1, Day: day, Sport: "running", Title: "Easy", DurationS: 1800}, {ID: 2, Day: day, Sport: "hiking", Title: "Hike"}},
			done:    []Done{{ID: 10, Day: day, Sport: "cycling", DurationS: 1800}, {ID: 11, Day: day, Sport: "other"}, {ID: 12, Day: "2026-11-09", Sport: "running"}},
			auto:    true,
			want:    map[int64]Completion{},
		},
		{
			name: "the user's links win",
			planned: []Planned{
				{ID: 1, Day: day, Sport: "running", Title: "Easy", DurationS: 1800},
				{ID: 2, Day: day, Sport: "running", Title: "Strides", DurationS: 1800},
				{ID: 3, Day: day, Sport: "running", Title: "Shakeout", DurationS: 1800},
			},
			done: []Done{{ID: 10, Day: day, Sport: "running", DurationS: 1800}, {ID: 11, Day: day, Sport: "running", DurationS: 600}},
			// 1 is linked to the worse fit, 2 unlinked, and 99 (another day, not listed) is taken.
			links: []Link{{PlannedID: 1, ActivityID: ptr(11)}, {PlannedID: 2}, {PlannedID: 7, ActivityID: ptr(99)}},
			auto:  true,
			want:  map[int64]Completion{1: {ActivityID: 11, Manual: true}, 3: {ActivityID: 10}},
		},
		{
			name:    "automatic matching off keeps only links",
			planned: []Planned{{ID: 1, Day: day, Sport: "running", Title: "Easy"}, {ID: 2, Day: day, Sport: "running", Title: "Long"}},
			done:    []Done{{ID: 10, Day: day, Sport: "running"}, {ID: 11, Day: day, Sport: "running"}},
			links:   []Link{{PlannedID: 2, ActivityID: ptr(11)}},
			want:    map[int64]Completion{2: {ActivityID: 11, Manual: true}},
		},
	} {
		got := Match(tc.planned, tc.done, tc.links, tc.auto)
		if len(got) != len(tc.want) {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
			continue
		}
		for id, w := range tc.want {
			if got[id] != w {
				t.Errorf("%s: planned %d = %+v, want %+v", tc.name, id, got[id], w)
			}
		}
	}
}

func f(v float64) *float64 { return &v }

func TestPlannedLoad(t *testing.T) {
	t.Parallel()
	runner := analytics.Thresholds{ThresholdSpeed: f(4), LTHR: f(170)}
	cyclist := analytics.Thresholds{FTP: f(250)}
	swimmer := analytics.Thresholds{CSS: f(1.25)}
	general := analytics.Thresholds{RestingHR: f(50)}
	for _, tc := range []struct {
		name, sport, steps string
		own                analytics.Thresholds
		want               float64 // -1 when it cannot be estimated
	}{
		// An hour at threshold pace scores 100.
		{"pace range", "running", `[{"kind": "interval", "durationType": "time", "duration": 3600, "target": {"metric": "pace", "low": 3.6, "high": 4.4}}]`, runner, 100},
		// 30 min at 225 W (IF 0.9): 40.5; then 2 × 10 min in Coggan zone 4, 227.5–265 W, mid 246.25 W (IF 0.985): 2 × 16.170417.
		{"power range and zone", "cycling", `[
			{"kind": "warmup", "durationType": "time", "duration": 1800, "target": {"metric": "power", "low": 200, "high": 250}},
			{"kind": "repeat", "count": 2, "steps": [{"kind": "interval", "durationType": "time", "duration": 600, "target": {"metric": "power", "zone": 4}}]}
		]`, cyclist, 40.5 + 2*16.17041667},
		// hrTSS with the general resting HR: (145 − 50) / (170 − 50) = 0.791667, squared, an hour.
		{"heart rate", "trail_running", `[{"kind": "interval", "durationType": "time", "duration": 3600, "target": {"metric": "heart_rate", "low": 140, "high": 150}}]`, runner, 62.67361111},
		// 400 m at CSS takes 320 s: 320/3600 × 100.
		{"swim distance at pace", "pool_swimming", `[{"kind": "interval", "durationType": "distance", "duration": 400, "target": {"metric": "pace", "low": 1.25, "high": 1.25}}]`, swimmer, 8.88888889},
		// Friel pace zone 1 is open below: its one bound, 4 × 100/130 m/s (IF 0.769231), for 30 min.
		{"open first zone", "running", `[{"kind": "recovery", "durationType": "time", "duration": 1800, "target": {"metric": "pace", "zone": 1}}]`, runner, 0.5 * 100 * (100.0 / 130) * (100.0 / 130)},
		{"a step without target", "running", `[{"kind": "warmup", "durationType": "time", "duration": 600}]`, runner, -1},
		{"an open step", "running", `[{"kind": "cooldown", "durationType": "open", "target": {"metric": "pace", "zone": 1}}]`, runner, -1},
		{"distance at a heart rate", "running", `[{"kind": "interval", "durationType": "distance", "duration": 1000, "target": {"metric": "heart_rate", "zone": 2}}]`, runner, -1},
		{"no such zone", "running", `[{"kind": "interval", "durationType": "time", "duration": 60, "target": {"metric": "pace", "zone": 7}}]`, analytics.Thresholds{}, -1},
		{"an FTP never scores a swim", "pool_swimming", `[{"kind": "interval", "durationType": "time", "duration": 60, "target": {"metric": "power", "low": 200, "high": 200}}]`, cyclist, -1},
		{"no threshold", "running", `[{"kind": "interval", "durationType": "time", "duration": 60, "target": {"metric": "pace", "low": 4, "high": 4}}]`, analytics.Thresholds{}, -1},
		{"a range without high", "running", `[{"kind": "interval", "durationType": "time", "duration": 60, "target": {"metric": "pace", "low": 4}}]`, runner, -1},
		{"a repeat with a bad step", "running", `[{"kind": "repeat", "count": 2, "steps": [{"kind": "interval", "durationType": "time", "duration": 60}]}]`, runner, -1},
	} {
		got, ok, err := PlannedLoad(json.RawMessage(tc.steps), tc.sport, tc.own, general)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if tc.want < 0 {
			if ok {
				t.Errorf("%s: estimated %v", tc.name, got)
			}
			continue
		}
		if !ok || math.Abs(got-tc.want) > 1e-6 {
			t.Errorf("%s: %v (%v), want %v", tc.name, got, ok, tc.want)
		}
	}
	if _, _, err := PlannedLoad(json.RawMessage(`[`), "running", runner, general); err == nil {
		t.Error("no error for unreadable steps")
	}
}

func TestSummarize(t *testing.T) {
	t.Parallel()
	got := Summarize([]Outcome{
		{PlannedS: 3600, PlannedLoad: f(60), Done: true, ActualS: 3300, ActualLoad: f(66), StepsWithin: 4, StepsCompared: 5},
		{PlannedS: 1800, PlannedLoad: f(30), Done: true, ActualS: 2100, StepsWithin: 1, StepsCompared: 1}, // no actual load
		{Done: true, ActualS: 4000, ActualLoad: f(80)},                                                    // nothing planned to compare
		{PlannedS: 2700, PlannedLoad: f(45)},                                                              // missed
	})
	want := Adherence{Planned: 4, Completed: 3, PlannedS: 5400, ActualS: 5400, PlannedLoad: 60, ActualLoad: 66, LoadCompared: 1, StepsWithin: 5, StepsCompared: 6}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}
