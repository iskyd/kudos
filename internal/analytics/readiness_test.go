package analytics

import (
	"math"
	"reflect"
	"slices"
	"testing"
	"time"
)

var readinessDay = time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC)

func daysAgo(n int) time.Time { return readinessDay.AddDate(0, 0, -n) }

// steadyBaseline has HRV 60 ms and resting HR 50 bpm on the days 1 to n before
// the day.
func steadyBaseline(n int) []HealthDay {
	out := make([]HealthDay, n)
	for i := range out {
		out[i] = HealthDay{Day: daysAgo(i + 1), HRV: new(60.0), RestingHR: new(50.0)}
	}
	return out
}

// fullInput has every component. With constant baselines the SD floors apply:
//
//	HRV: ln(today/60) = 0.05, one floor SD, so z(today) = 1 and z(7-day) = 1/7;
//	     score 75 + 25·(1 + 1/7)/2 = 89.2857
//	resting HR: 52 against 50, two floor SDs: 75 − 50 = 25
//	sleep: 7 h of 8 is 87.5, with sleep score 80: 83.75
//	form: TSB −10: 75 − 15 = 60
//	check-in: soreness 2 → 75, stress 4 → 25, sleep quality 5 → 100, motivation 3 → 50: 62.5
//	total: 0.3·89.2857 + 0.2·25 + 0.2·83.75 + 0.15·60 + 0.15·62.5 = 66.91 → 67
func fullInput() ReadinessInput {
	today := HealthDay{Day: readinessDay, HRV: new(60 * math.Exp(0.05)), RestingHR: new(52.0), SleepS: new(7 * 3600.0), SleepScore: new(80.0)}
	return ReadinessInput{
		Day:     readinessDay,
		Health:  append(steadyBaseline(20), today),
		TSB:     -10,
		CheckIn: &CheckIn{Soreness: new(2.0), Stress: new(4.0), SleepQuality: new(5.0), Motivation: new(3.0)},
	}
}

func TestReadinessComponents(t *testing.T) {
	t.Parallel()
	got := ComputeReadiness(fullInput())
	if !got.Ready || got.BaselineDays != 20 || got.Score != 67 || got.Uncapped != 67 || got.Cap != nil {
		t.Fatalf("readiness: %+v", got)
	}
	want := []struct {
		name            ReadinessComponent
		score, share    float64
		value, baseline *float64
	}{
		{ReadinessHRV, 75 + 25*(1+1.0/7)/2, 0.3, new(60 * math.Exp(0.05)), new(60.0)},
		{ReadinessRestingHR, 25, 0.2, new(52.0), new(50.0)},
		{ReadinessSleep, 83.75, 0.2, new(7 * 3600.0), nil},
		{ReadinessForm, 60, 0.15, new(-10.0), nil},
		{ReadinessCheckIn, 62.5, 0.15, nil, nil},
	}
	if len(got.Components) != len(want) {
		t.Fatalf("components: %+v", got.Components)
	}
	same := func(a, b *float64) bool { return (a == nil) == (b == nil) && (a == nil || math.Abs(*a-*b) < 1e-9) }
	for i, w := range want {
		c := got.Components[i]
		if c.Name != w.name || math.Abs(c.Score-w.score) > 1e-9 || math.Abs(c.Share-w.share) > 1e-9 ||
			!same(c.Value, w.value) || !same(c.Baseline, w.baseline) {
			t.Errorf("%s: %+v", w.name, c)
		}
	}
}

func TestReadinessBaselineGate(t *testing.T) {
	t.Parallel()
	health := steadyBaseline(13)
	health = append(health,
		// Neither the day itself, a day without resting HR nor one beyond 60 days counts.
		HealthDay{Day: readinessDay, HRV: new(60.0), RestingHR: new(50.0)},
		HealthDay{Day: daysAgo(14), HRV: new(60.0)},
		HealthDay{Day: daysAgo(61), HRV: new(60.0), RestingHR: new(50.0)},
	)
	got := ComputeReadiness(ReadinessInput{Day: readinessDay, Health: health})
	if !reflect.DeepEqual(got, Readiness{BaselineDays: 13}) {
		t.Errorf("13 days: %+v", got)
	}
	health = append(health, HealthDay{Day: daysAgo(60), HRV: new(60.0), RestingHR: new(50.0)})
	if got := ComputeReadiness(ReadinessInput{Day: readinessDay, Health: health}); !got.Ready || got.BaselineDays != 14 {
		t.Errorf("14 days: %+v", got)
	}
}

func TestReadinessRenormalizesMissingComponents(t *testing.T) {
	t.Parallel()
	// Nothing recorded today and no check-in: HRV from the 7-day trend alone
	// (at baseline, 75) and form (TSB 0, 75) share the weight 0.30 : 0.15.
	got := ComputeReadiness(ReadinessInput{Day: readinessDay, Health: steadyBaseline(14)})
	if got.Score != 75 || len(got.Components) != 2 {
		t.Fatalf("readiness: %+v", got)
	}
	if c := got.Components[0]; c.Name != ReadinessHRV || c.Value != nil || math.Abs(c.Share-2.0/3) > 1e-9 {
		t.Errorf("hrv: %+v", c)
	}
	if c := got.Components[1]; c.Name != ReadinessForm || math.Abs(c.Share-1.0/3) > 1e-9 {
		t.Errorf("form: %+v", c)
	}

	// An empty check-in adds nothing; sleep from duration alone.
	in := ReadinessInput{
		Day:     readinessDay,
		Health:  append(steadyBaseline(14), HealthDay{Day: readinessDay, SleepS: new(4 * 3600.0)}),
		CheckIn: &CheckIn{},
	}
	got = ComputeReadiness(in)
	if len(got.Components) != 3 || got.Components[1].Name != ReadinessSleep || got.Components[1].Score != 50 {
		t.Errorf("sleep only: %+v", got.Components)
	}
}

func TestReadinessClampsScores(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		tsb, want float64
	}{{100, 100}, {-100, 0}, {10, 90}} {
		got := ComputeReadiness(ReadinessInput{Day: readinessDay, Health: steadyBaseline(14), TSB: tt.tsb})
		if form := got.Components[1]; form.Score != tt.want {
			t.Errorf("TSB %v: form %v, want %v", tt.tsb, form.Score, tt.want)
		}
	}
}

func TestReadinessCaps(t *testing.T) {
	t.Parallel()
	yesterday := daysAgo(1)
	illness := Condition{ID: 5, Kind: ConditionIllness, Start: daysAgo(3)}
	injury := Condition{ID: 2, Kind: ConditionInjury, Start: daysAgo(10), End: &readinessDay}
	for _, tt := range []struct {
		name       string
		conditions []Condition
		tsb        float64
		worst      bool // worst check-in answers
		want       float64
		cap        *Cap
	}{
		{"none", nil, -10, false, 67, nil},
		{"ongoing illness", []Condition{illness}, -10, false, 30, &Cap{5, ConditionIllness, 30}},
		{"injury ending today", []Condition{injury}, -10, false, 50, &Cap{2, ConditionInjury, 50}},
		{"illness beats injury", []Condition{injury, illness}, -10, false, 30, &Cap{5, ConditionIllness, 30}},
		{"same cap, lowest ID", []Condition{{ID: 9, Kind: ConditionInjury, Start: yesterday}, injury}, -10, false, 50, &Cap{2, ConditionInjury, 50}},
		// Below the cap the score stands, but the cap still shows: form and
		// check-in at 0 leave 0.3·89.2857 + 0.2·25 + 0.2·83.75 = 48.54.
		{"cap not binding", []Condition{injury}, -50, true, 49, &Cap{2, ConditionInjury, 50}},
		{"ended yesterday", []Condition{{ID: 1, Kind: ConditionInjury, Start: daysAgo(5), End: &yesterday}}, -10, false, 67, nil},
		{"starts tomorrow", []Condition{{ID: 1, Kind: ConditionIllness, Start: readinessDay.AddDate(0, 0, 1)}}, -10, false, 67, nil},
		{"period", []Condition{{ID: 1, Kind: "period", Start: yesterday}}, -10, false, 67, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			in := fullInput()
			in.TSB = tt.tsb
			in.Conditions = tt.conditions
			if tt.worst {
				in.CheckIn = &CheckIn{Soreness: new(5.0), Stress: new(5.0), SleepQuality: new(1.0), Motivation: new(1.0)}
			}
			got := ComputeReadiness(in)
			if got.Score != tt.want || !reflect.DeepEqual(got.Cap, tt.cap) {
				t.Errorf("score %v cap %+v, want %v %+v", got.Score, got.Cap, tt.want, tt.cap)
			}
		})
	}
}

func TestReadinessIsDeterministic(t *testing.T) {
	t.Parallel()
	in := fullInput()
	want := ComputeReadiness(in)
	slices.Reverse(in.Health)
	for range 3 {
		if got := ComputeReadiness(in); !reflect.DeepEqual(got, want) {
			t.Fatalf("got %+v, want %+v", got, want)
		}
	}
}
