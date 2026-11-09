package llm

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/iskyd/kudos/internal/analytics"
)

// AthleteContext is everything the context builder summarizes. Values are SI;
// the builder renders them in display units so the model never converts or
// computes anything (invariant 19). Days are dates at UTC midnight; activity
// starts are local times.
type AthleteContext struct {
	Today      time.Time
	Sports     []string
	Thresholds []Threshold // the version valid today, per sport
	HealthLog  []HealthEntry
	Goals      []GoalProgress
	Readiness  *analytics.Readiness // today's; nil when unknown
	CheckIns   []CheckIn
	Load       []LoadWeek
	Activities []ActivitySummary
	Health     []HealthDay
	Records    []Record
}

// Threshold is a sport's zone version.
type Threshold struct {
	Sport            string
	From             time.Time
	LTHR, MaxHR      *int
	RestingHR, FTP   *int
	ThresholdSpeedMS *float64
	CSSMS            *float64
}

// HealthEntry is an injury, illness or menstrual period; End is nil while ongoing.
type HealthEntry struct {
	Kind        string
	Description string
	Start       time.Time
	End         *time.Time
}

// GoalProgress is a volume goal with its progress.
type GoalProgress struct {
	Period                   string // week, month or year
	Start                    time.Time
	Sport                    string // empty for all sports
	Metric                   string // distance, duration or ascent
	Target, Value, Projected float64
	Status                   string
}

// CheckIn is a day's scores (1–5) and note.
type CheckIn struct {
	Day                                        time.Time
	Soreness, SleepQuality, Stress, Motivation *int
	Note                                       string
}

// LoadWeek is fitness, fatigue and form at the end of a week starting Monday.
type LoadWeek struct {
	Start         time.Time
	CTL, ATL, TSB float64
}

// ActivitySummary is one activity's headline values.
type ActivitySummary struct {
	ID        int64
	Start     time.Time // local
	Sport     string
	DurationS float64
	DistanceM *float64
	AvgHR     *int
	AvgPowerW *int
	Load      *float64
}

// HealthDay is a day's recovery metrics.
type HealthDay struct {
	Day       time.Time
	HRV       *float64 // ms
	RestingHR *int
	SleepS    *float64
}

// Record is a personal best and the activity that set it.
type Record struct {
	Label      string // e.g. "Fastest 5K"
	Value      string // already in display units, e.g. "22:01"
	ActivityID int64
	Day        time.Time
}

// EffortRecord renders a best effort as a Record: fastest efforts are times,
// longest distances, the rest power.
func EffortRecord(e analytics.EffortName, value float64, activityID int64, d time.Time) Record {
	label := strings.ReplaceAll(string(e), "_", " ")
	r := Record{Label: strings.ToUpper(label[:1]) + label[1:], ActivityID: activityID, Day: d}
	switch {
	case e.LowerIsBetter():
		r.Value = duration(value)
	case strings.HasPrefix(string(e), "longest"):
		r.Value = km(value)
	default:
		r.Value = fmt.Sprintf("%.0f W", value)
	}
	return r
}

// RefActivity and RefDay are the references every item carries for the model
// to cite and the UI to link.
func RefActivity(id int64) string { return fmt.Sprintf("[act:%d]", id) }

// RefDay references a calendar day.
func RefDay(d time.Time) string { return "[day:" + d.Format(time.DateOnly) + "]" }

// section is a heading and its items, most important first.
type section struct {
	title string
	items []string
	keep  bool // never trimmed
	empty string
}

// BuildContext renders c in the spec's priority order within budget tokens
// (0 for no limit). Sections are cut from the end item by item; the health
// log is always kept whole. The same input always gives the same bytes.
func BuildContext(c AthleteContext, budget int) string {
	sections := []section{
		{title: "Athlete profile", items: profileItems(c)},
		{title: "Injury and illness history", items: healthLogItems(c.HealthLog), keep: true, empty: "- None recorded."},
		{title: "Goals", items: goalItems(c.Goals)},
		{title: "Readiness and check-ins", items: readinessItems(c)},
		{title: "Training load, last 6 weeks", items: loadItems(c.Load)},
		{title: "Recent activities", items: activityItems(c.Activities)},
		{title: "Health trends", items: healthItems(c.Health)},
		{title: "Personal records", items: recordItems(c.Records)},
	}
	head := "# Athlete context, " + c.Today.Format(time.DateOnly) + "\n"
	left := math.MaxInt
	if budget > 0 {
		left = budget - tokens(head)
	}
	// The kept sections are paid for first, so they survive any budget.
	rendered := make([]string, len(sections))
	for i, s := range sections {
		if s.keep {
			rendered[i] = s.render(len(s.items))
			left -= tokens(rendered[i])
		}
	}
	for i, s := range sections {
		if s.keep || len(s.items) == 0 {
			continue
		}
		n := len(s.items)
		for n > 0 && tokens(s.render(n)) > left {
			n--
		}
		if n > 0 {
			rendered[i] = s.render(n)
			left -= tokens(rendered[i])
		}
		// Once a section is cut, everything of lower priority goes too.
		if n < len(s.items) {
			break
		}
	}
	var b strings.Builder
	b.WriteString(head)
	for _, r := range rendered {
		if r != "" {
			b.WriteString("\n" + r)
		}
	}
	return b.String()
}

// render writes the heading and the first n items, noting any left out.
func (s section) render(n int) string {
	var b strings.Builder
	b.WriteString("## " + s.title + "\n")
	if len(s.items) == 0 {
		b.WriteString(s.empty + "\n")
	}
	for _, item := range s.items[:n] {
		b.WriteString(item + "\n")
	}
	if left := len(s.items) - n; left > 0 {
		fmt.Fprintf(&b, "- (%d more left out for length)\n", left)
	}
	return b.String()
}

// tokens estimates a text's token count.
// shortcut: four bytes per token, about right for English; count with the
// provider's tokenizer if models start overflowing.
func tokens(s string) int { return (len(s) + 3) / 4 }

func profileItems(c AthleteContext) []string {
	var items []string
	if len(c.Sports) > 0 {
		sports := slices.Sorted(slices.Values(c.Sports))
		items = append(items, "- Sports: "+strings.Join(sports, ", "))
	}
	ts := slices.Clone(c.Thresholds)
	slices.SortFunc(ts, func(a, b Threshold) int { return strings.Compare(a.Sport, b.Sport) })
	for _, t := range ts {
		var parts []string
		add := func(v *int, label, unit string) {
			if v != nil {
				parts = append(parts, fmt.Sprintf("%s %d %s", label, *v, unit))
			}
		}
		add(t.LTHR, "LTHR", "bpm")
		add(t.MaxHR, "max HR", "bpm")
		add(t.RestingHR, "resting HR", "bpm")
		add(t.FTP, "FTP", "W")
		if t.ThresholdSpeedMS != nil {
			parts = append(parts, "threshold pace "+pace(*t.ThresholdSpeedMS, 1000)+" /km")
		}
		if t.CSSMS != nil {
			parts = append(parts, "CSS "+pace(*t.CSSMS, 100)+" /100 m")
		}
		if len(parts) > 0 {
			items = append(items, fmt.Sprintf("- %s zones since %s %s: %s", t.Sport, day(t.From), RefDay(t.From), strings.Join(parts, ", ")))
		}
	}
	return items
}

func healthLogItems(log []HealthEntry) []string {
	es := slices.Clone(log)
	slices.SortStableFunc(es, func(a, b HealthEntry) int { return b.Start.Compare(a.Start) })
	items := make([]string, len(es))
	for i, e := range es {
		when := "since " + day(e.Start) + " (ongoing)"
		if e.End != nil {
			when = day(e.Start) + " to " + day(*e.End)
		}
		kind := map[string]string{"injury": "Injury", "illness": "Illness", "period": "Menstrual period"}[e.Kind]
		items[i] = fmt.Sprintf("- %s, %s %s", cmp.Or(kind, e.Kind), when, RefDay(e.Start))
		if e.Description != "" {
			items[i] += ": " + oneLine(e.Description)
		}
	}
	return items
}

func goalItems(goals []GoalProgress) []string {
	gs := slices.Clone(goals)
	slices.SortStableFunc(gs, func(a, b GoalProgress) int { return b.Start.Compare(a.Start) })
	items := make([]string, len(gs))
	for i, g := range gs {
		sport := cmp.Or(g.Sport, "all sports")
		period := map[string]string{"week": "week of " + day(g.Start), "month": g.Start.Format("January 2006"), "year": g.Start.Format("2006")}[g.Period]
		items[i] = fmt.Sprintf("- %s %s in %s %s: %s so far, %s; projected %s",
			metricValue(g.Metric, g.Target), sport, period, RefDay(g.Start), metricValue(g.Metric, g.Value), g.Status, metricValue(g.Metric, g.Projected))
	}
	return items
}

func readinessItems(c AthleteContext) []string {
	var items []string
	if r := c.Readiness; r != nil {
		if !r.Ready {
			items = append(items, fmt.Sprintf("- Readiness %s %s: not shown yet, %d of %d baseline days",
				day(c.Today), RefDay(c.Today), r.BaselineDays, analytics.ReadinessMinBaselineDays))
		} else {
			parts := make([]string, len(r.Components))
			for i, comp := range r.Components {
				parts[i] = fmt.Sprintf("%s %.0f", strings.ReplaceAll(string(comp.Name), "_", " "), comp.Score)
			}
			item := fmt.Sprintf("- Readiness %s %s: %.0f/100 (%s)", day(c.Today), RefDay(c.Today), r.Score, strings.Join(parts, ", "))
			if r.Cap != nil {
				item += fmt.Sprintf("; capped at %.0f by the current %s", r.Cap.Max, r.Cap.Kind)
			}
			items = append(items, item)
		}
	}
	cs := slices.Clone(c.CheckIns)
	slices.SortStableFunc(cs, func(a, b CheckIn) int { return b.Day.Compare(a.Day) })
	for _, ci := range cs {
		var parts []string
		for _, s := range []struct {
			label string
			v     *int
		}{{"soreness", ci.Soreness}, {"sleep quality", ci.SleepQuality}, {"stress", ci.Stress}, {"motivation", ci.Motivation}} {
			if s.v != nil {
				parts = append(parts, fmt.Sprintf("%s %d/5", s.label, *s.v))
			}
		}
		if ci.Note != "" {
			parts = append(parts, "note: "+oneLine(ci.Note))
		}
		items = append(items, fmt.Sprintf("- Check-in %s %s: %s", day(ci.Day), RefDay(ci.Day), strings.Join(parts, "; ")))
	}
	return items
}

func loadItems(weeks []LoadWeek) []string {
	ws := slices.Clone(weeks)
	slices.SortStableFunc(ws, func(a, b LoadWeek) int { return b.Start.Compare(a.Start) })
	items := make([]string, len(ws))
	for i, w := range ws {
		items[i] = fmt.Sprintf("- Week of %s %s: fitness %.1f, fatigue %.1f, form %.1f", day(w.Start), RefDay(w.Start), w.CTL, w.ATL, w.TSB)
	}
	return items
}

func activityItems(acts []ActivitySummary) []string {
	as := slices.Clone(acts)
	slices.SortStableFunc(as, func(a, b ActivitySummary) int {
		return cmp.Or(b.Start.Compare(a.Start), cmp.Compare(b.ID, a.ID))
	})
	items := make([]string, len(as))
	for i, a := range as {
		parts := []string{duration(a.DurationS)}
		if a.DistanceM != nil && *a.DistanceM > 0 {
			parts = append(parts, km(*a.DistanceM))
			if a.DurationS > 0 && (a.Sport == "running" || a.Sport == "trail_running" || a.Sport == "hiking") {
				parts = append(parts, pace(*a.DistanceM/a.DurationS, 1000)+" /km")
			}
		}
		if a.AvgHR != nil {
			parts = append(parts, fmt.Sprintf("avg HR %d bpm", *a.AvgHR))
		}
		if a.AvgPowerW != nil {
			parts = append(parts, fmt.Sprintf("avg power %d W", *a.AvgPowerW))
		}
		if a.Load != nil {
			parts = append(parts, fmt.Sprintf("load %.0f", *a.Load))
		}
		items[i] = fmt.Sprintf("- %s %s %s: %s", a.Start.Format("2006-01-02 15:04"), strings.ReplaceAll(a.Sport, "_", " "), RefActivity(a.ID), strings.Join(parts, ", "))
	}
	return items
}

func healthItems(days []HealthDay) []string {
	ds := slices.Clone(days)
	slices.SortStableFunc(ds, func(a, b HealthDay) int { return b.Day.Compare(a.Day) })
	var items []string
	for _, d := range ds {
		var parts []string
		if d.HRV != nil {
			parts = append(parts, fmt.Sprintf("HRV %.0f ms", *d.HRV))
		}
		if d.RestingHR != nil {
			parts = append(parts, fmt.Sprintf("resting HR %d bpm", *d.RestingHR))
		}
		if d.SleepS != nil {
			parts = append(parts, "sleep "+duration(*d.SleepS))
		}
		if len(parts) > 0 {
			items = append(items, fmt.Sprintf("- %s %s: %s", day(d.Day), RefDay(d.Day), strings.Join(parts, ", ")))
		}
	}
	return items
}

func recordItems(records []Record) []string {
	rs := slices.Clone(records)
	slices.SortStableFunc(rs, func(a, b Record) int { return strings.Compare(a.Label, b.Label) })
	items := make([]string, len(rs))
	for i, r := range rs {
		items[i] = fmt.Sprintf("- %s %s: %s on %s", r.Label, RefActivity(r.ActivityID), r.Value, day(r.Day))
	}
	return items
}

func day(t time.Time) string { return t.Format(time.DateOnly) }

// oneLine keeps user text from breaking the list: newlines become spaces.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

func duration(s float64) string {
	total := int(math.Round(s))
	h, m, sec := total/3600, total/60%60, total%60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, sec)
	}
	return fmt.Sprintf("%d:%02d", m, sec)
}

func km(m float64) string { return fmt.Sprintf("%.2f km", m/1000) }

// pace is the time per `per` metres at speed m/s, as m:ss.
func pace(speed, per float64) string {
	if speed <= 0 {
		return "–"
	}
	return duration(per / speed)
}

func metricValue(metric string, v float64) string {
	switch metric {
	case "distance":
		return km(v)
	case "duration":
		return duration(v)
	default:
		return fmt.Sprintf("%.0f m", v)
	}
}
