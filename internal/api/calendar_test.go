package api

import (
	"net/http"
	"slices"
	"testing"
)

func calendar(t *testing.T, c *apiClient, from, to string) Calendar {
	t.Helper()
	return decodeJSON[Calendar](t, c.do(http.MethodGet, "/api/calendar?from="+from+"&to="+to, nil, nil), http.StatusOK)
}

func TestCalendarPlacesActivitiesByLocalDay(t *testing.T) {
	t.Parallel()
	// Summer time ends on 25 October 2026 at 03:00 in Berlin: 00:30 that day is
	// 22:30 UTC on the 24th (+02:00), 23:30 is 22:30 UTC on the 25th (+01:00).
	c, _, _ := loggedIn(t, "Europe/Berlin")
	eve := run(t, c, "2026-10-24T23:30", 5000, 1500)
	early := run(t, c, "2026-10-25T00:30", 6000, 1800)
	late := run(t, c, "2026-10-25T23:30", 7000, 2100)
	next := run(t, c, "2026-10-26T00:30", 8000, 2400)

	ids := func(cal Calendar) []int64 {
		out := []int64{}
		for _, a := range cal.Activities {
			out = append(out, a.Id)
		}
		return out
	}
	for _, tc := range []struct {
		from, to string
		want     []int64
	}{
		{"2026-10-24", "2026-10-24", []int64{eve}},
		{"2026-10-25", "2026-10-25", []int64{early, late}},
		{"2026-10-26", "2026-10-26", []int64{next}},
		// Oldest first across the range.
		{"2026-10-19", "2026-11-01", []int64{eve, early, late, next}},
	} {
		if got := ids(calendar(t, c, tc.from, tc.to)); !slices.Equal(got, tc.want) {
			t.Errorf("%s to %s: %v, want %v", tc.from, tc.to, got, tc.want)
		}
	}
	cal := calendar(t, c, "2026-10-25", "2026-10-25")
	if cal.Activities[0].UtcOffsetS != 7200 || cal.Activities[1].UtcOffsetS != 3600 {
		t.Errorf("offsets: %d, %d", cal.Activities[0].UtcOffsetS, cal.Activities[1].UtcOffsetS)
	}
}

func TestCalendarWellnessAndHealthLog(t *testing.T) {
	t.Parallel()
	c, _, _ := loggedIn(t, "UTC")
	for _, d := range []string{"2026-10-04", "2026-10-05", "2026-10-11", "2026-10-12"} {
		decodeJSON[WellnessDay](t, c.do(http.MethodPut, "/api/wellness/"+d, WellnessDayInput{Soreness: new(2)}, nil), http.StatusOK)
	}
	logEntry := func(in HealthLogInput) HealthLogEntry {
		t.Helper()
		return decodeJSON[HealthLogEntry](t, c.do(http.MethodPost, "/api/health-log", in, nil), http.StatusCreated)
	}
	knee := logEntry(HealthLogInput{Kind: Injury, Description: new("Sore knee"), StartDate: day("2026-10-01"), EndDate: new(day("2026-10-05"))})
	logEntry(HealthLogInput{Kind: Injury, Description: new("Old"), StartDate: day("2026-09-01"), EndDate: new(day("2026-10-04"))})
	cold := logEntry(HealthLogInput{Kind: Illness, Description: new("Cold"), StartDate: day("2026-10-08")})
	logEntry(HealthLogInput{Kind: Period, StartDate: day("2026-10-06")})

	cal := calendar(t, c, "2026-10-05", "2026-10-11")
	if len(cal.Wellness) != 2 || cal.Wellness[0].Day != day("2026-10-05") || cal.Wellness[1].Day != day("2026-10-11") {
		t.Errorf("wellness: %+v", cal.Wellness)
	}
	if len(cal.HealthLog) != 2 || cal.HealthLog[0].Id != knee.Id || cal.HealthLog[1].Id != cold.Id {
		t.Errorf("health log: %+v", cal.HealthLog)
	}
	if len(cal.Activities) != 0 {
		t.Errorf("activities: %+v", cal.Activities)
	}
}

func TestCalendarValidationAndIsolation(t *testing.T) {
	t.Parallel()
	c, _, _ := loggedIn(t, "UTC")
	wantProblem(t, c.do(http.MethodGet, "/api/calendar?from=2026-10-02&to=2026-10-01", nil, nil), KindInvalidRequest)
	wantProblem(t, c.do(http.MethodGet, "/api/calendar?from=2026-10-01&to=2026-11-12", nil, nil), KindInvalidRequest)
	if cal := calendar(t, c, "2026-10-01", "2026-11-11"); len(cal.Activities) != 0 { // 42 days
		t.Errorf("empty calendar: %+v", cal)
	}

	run(t, c, "2026-10-01T07:00", 5000, 1500)
	decodeJSON[WellnessDay](t, c.do(http.MethodPut, "/api/wellness/2026-10-01", WellnessDayInput{Soreness: new(2)}, nil), http.StatusOK)
	decodeJSON[HealthLogEntry](t, c.do(http.MethodPost, "/api/health-log", HealthLogInput{Kind: Illness, Description: new("Cold"), StartDate: day("2026-10-01")}, nil), http.StatusCreated)

	bob := &apiClient{t: t, h: c.h}
	bob.session(bob.do(http.MethodPost, "/api/auth/register", register("bob"), nil), http.StatusCreated)
	if cal := calendar(t, bob, "2026-10-01", "2026-10-01"); len(cal.Activities)+len(cal.Wellness)+len(cal.HealthLog) != 0 {
		t.Errorf("bob sees %+v", cal)
	}
	if cal := calendar(t, c, "2026-10-01", "2026-10-01"); len(cal.Activities) != 1 || len(cal.Wellness) != 1 || len(cal.HealthLog) != 1 {
		t.Errorf("alice sees %+v", cal)
	}
}
