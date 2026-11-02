package llm

import (
	"flag"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/iskyd/kudos/internal/analytics"
)

var update = flag.Bool("update", false, "rewrite golden files")

func date(s string) time.Time {
	d, _ := time.Parse(time.DateOnly, s) // the tests' literals are valid
	return d
}

func localTime(s string) time.Time {
	t, _ := time.Parse("2006-01-02 15:04", s) // the tests' literals are valid
	return t
}

// sample is a synthetic athlete with every section filled.
func sample() AthleteContext {
	return AthleteContext{
		Today:  date("2026-11-02"),
		Sports: []string{"running", "cycling"},
		Thresholds: []Threshold{
			{Sport: "running", From: date("2026-10-15"), LTHR: new(172), ThresholdSpeedMS: new(1000 / 274.0)},
			{Sport: "cycling", From: date("2026-10-20"), FTP: new(128), LTHR: new(165)},
			{Sport: "general", From: date("2026-01-01"), MaxHR: new(190), RestingHR: new(48)},
			{Sport: "swimming", From: date("2026-09-01"), CSSMS: new(200 / 190.0)},
			{Sport: "empty", From: date("2026-09-01")},
		},
		HealthLog: []HealthEntry{
			{Kind: "injury", Description: "Sore left knee\nafter the long run", Start: date("2026-09-01"), End: new(date("2026-09-20"))},
			{Kind: "illness", Description: "Cold", Start: date("2026-10-28")},
			{Kind: "period", Start: date("2026-10-12"), End: new(date("2026-10-16"))},
		},
		Goals: []GoalProgress{
			{Period: "year", Start: date("2026-01-01"), Sport: "running", Metric: "distance", Target: 2e6, Value: 1.5e6, Projected: 1.8e6, Status: "behind"},
			{Period: "month", Start: date("2026-11-01"), Metric: "duration", Target: 36000, Value: 5400, Projected: 81000, Status: "ahead"},
			{Period: "week", Start: date("2026-11-02"), Sport: "cycling", Metric: "ascent", Target: 1500, Value: 0, Projected: 0, Status: "behind"},
		},
		Readiness: &analytics.Readiness{
			Ready: true, Score: 58, Uncapped: 67, BaselineDays: 30,
			Components: []analytics.Component{{Name: analytics.ReadinessHRV, Score: 89.3}, {Name: analytics.ReadinessRestingHR, Score: 25}},
			Cap:        &analytics.Cap{ID: 2, Kind: analytics.ConditionIllness, Max: 60},
		},
		CheckIns: []CheckIn{
			{Day: date("2026-11-01"), Soreness: new(2), SleepQuality: new(4), Note: "Legs heavy"},
			{Day: date("2026-11-02"), Stress: new(3), Motivation: new(5)},
		},
		Load: []LoadWeek{
			{Start: date("2026-10-19"), CTL: 41.04, ATL: 48.2, TSB: -7.16},
			{Start: date("2026-10-26"), CTL: 43.5, ATL: 39.75, TSB: 3.75},
		},
		Activities: []ActivitySummary{
			{ID: 11, Start: localTime("2026-10-30 18:05"), Sport: "cycling", DurationS: 3600, DistanceM: new(30000.0), AvgPowerW: new(140), Load: new(55.4)},
			{ID: 12, Start: localTime("2026-11-01 07:30"), Sport: "running", DurationS: 3000, DistanceM: new(10000.0), AvgHR: new(150), Load: new(64.6)},
			{ID: 10, Start: localTime("2026-10-29 06:45"), Sport: "strength", DurationS: 2700},
			{ID: 9, Start: localTime("2026-10-28 19:00"), Sport: "trail_running", DurationS: 5400, DistanceM: new(14000.0), AvgHR: new(145)},
		},
		Health: []HealthDay{
			{Day: date("2026-11-01"), HRV: new(58.4), RestingHR: new(49), SleepS: new(7*3600 + 1800.0)},
			{Day: date("2026-11-02"), HRV: new(55.0), RestingHR: new(51)},
			{Day: date("2026-10-31")},
		},
		Records: []Record{
			{Label: "Fastest 5K", Value: "22:01", ActivityID: 5, Day: date("2026-10-25")},
			{Label: "Best 20 min power", Value: "134 W", ActivityID: 6, Day: date("2026-10-20")},
		},
	}
}

func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "context", name+".golden")
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("%s differs from %s (run with -update after checking):\n%s", name, path, got)
	}
}

func TestBuildContextGolden(t *testing.T) {
	t.Parallel()
	full := BuildContext(sample(), 0)
	golden(t, "full", full)
	// A budget of about half: lower sections are cut item by item, then dropped.
	trimmed := BuildContext(sample(), tokens(full)/2)
	golden(t, "trimmed", trimmed)
	if tokens(trimmed) > tokens(full)/2 {
		t.Errorf("trimmed is %d tokens, over the budget of %d", tokens(trimmed), tokens(full)/2)
	}
}

func TestBuildContextKeepsTheHealthLogAtAnyBudget(t *testing.T) {
	t.Parallel()
	full := BuildContext(sample(), 0)
	var injuries []string
	for line := range strings.Lines(full) {
		if strings.HasPrefix(line, "- Injury") || strings.HasPrefix(line, "- Illness") || strings.HasPrefix(line, "- Menstrual") {
			injuries = append(injuries, line)
		}
	}
	if len(injuries) != 3 {
		t.Fatalf("full context has %d health log lines", len(injuries))
	}
	for budget := 1; budget <= tokens(full)+10; budget++ {
		got := BuildContext(sample(), budget)
		for _, line := range injuries {
			if !strings.Contains(got, line) {
				t.Fatalf("budget %d drops %q:\n%s", budget, line, got)
			}
		}
	}
	// With no entries the section still says so.
	c := sample()
	c.HealthLog = nil
	if got := BuildContext(c, 1); !strings.Contains(got, "## Injury and illness history\n- None recorded.\n") {
		t.Errorf("empty health log:\n%s", got)
	}
}

func TestBuildContextIsDeterministic(t *testing.T) {
	t.Parallel()
	a, b := sample(), sample()
	// The same items in another order render the same bytes.
	slices.Reverse(b.Activities)
	slices.Reverse(b.HealthLog)
	slices.Reverse(b.Goals)
	slices.Reverse(b.Thresholds)
	slices.Reverse(b.Sports)
	slices.Reverse(b.CheckIns)
	slices.Reverse(b.Load)
	slices.Reverse(b.Health)
	slices.Reverse(b.Records)
	for _, budget := range []int{0, 300, 150} {
		if x, y := BuildContext(a, budget), BuildContext(b, budget); x != y {
			t.Errorf("budget %d: outputs differ:\n%s\n---\n%s", budget, x, y)
		}
		if x, y := BuildContext(a, budget), BuildContext(a, budget); x != y {
			t.Errorf("budget %d: two runs differ", budget)
		}
	}
}

func TestBuildContextTrimsLowestPriorityFirst(t *testing.T) {
	t.Parallel()
	full := BuildContext(sample(), 0)
	headings := []string{"## Athlete profile", "## Injury and illness history", "## Goals", "## Readiness and check-ins",
		"## Training load, last 6 weeks", "## Recent activities", "## Health trends", "## Personal records"}
	last := -1
	for _, h := range headings {
		i := strings.Index(full, h)
		if i <= last {
			t.Fatalf("%q out of order", h)
		}
		last = i
	}
	// As the budget shrinks, a section never appears without every section
	// before it, and some budgets keep part of a section.
	cut := false
	for budget := 1; budget <= tokens(full); budget += 7 {
		got := BuildContext(sample(), budget)
		cut = cut || strings.Contains(got, "more left out for length")
		seenGap := false
		for _, h := range headings {
			has := strings.Contains(got, h)
			if has && seenGap && h != "## Injury and illness history" {
				t.Fatalf("budget %d keeps %q after dropping an earlier section:\n%s", budget, h, got)
			}
			if !has {
				seenGap = true
			}
		}
	}
	if !cut {
		t.Error("no budget keeps part of a section")
	}
	// Every item carries a reference.
	for line := range strings.Lines(full) {
		if strings.HasPrefix(line, "- ") && !strings.HasPrefix(line, "- Sports:") && !strings.Contains(line, "[act:") && !strings.Contains(line, "[day:") {
			t.Errorf("no reference: %q", line)
		}
	}
}

func TestBuildContextEdges(t *testing.T) {
	t.Parallel()
	// Readiness still collecting, nothing else.
	got := BuildContext(AthleteContext{Today: date("2026-11-02"), Readiness: &analytics.Readiness{BaselineDays: 5}}, 0)
	want := "# Athlete context, 2026-11-02\n\n" +
		"## Injury and illness history\n- None recorded.\n\n" +
		"## Readiness and check-ins\n- Readiness 2026-11-02 [day:2026-11-02]: not shown yet, 5 of 14 baseline days\n"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	for in, want := range map[float64]string{0: "–", -1: "–", 1000 / 300.0: "5:00"} {
		if got := pace(in, 1000); got != want {
			t.Errorf("pace(%v) = %q", in, got)
		}
	}
	if got := metricValue("ascent", 1234.6); got != "1235 m" {
		t.Errorf("ascent = %q", got)
	}
	if got := healthLogItems([]HealthEntry{{Kind: "other", Start: date("2026-01-01")}}); got[0] != "- other, since 2026-01-01 (ongoing) [day:2026-01-01]" {
		t.Errorf("unknown kind = %q", got[0])
	}
}
