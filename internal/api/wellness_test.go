package api

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func putDay(t *testing.T, c *apiClient, day string, in WellnessDayInput) WellnessDay {
	t.Helper()
	return decodeJSON[WellnessDay](t, c.do(http.MethodPut, "/api/wellness/"+day, in, nil), http.StatusOK)
}

func TestWellnessDays(t *testing.T) {
	t.Parallel()
	c, _, _ := loggedIn(t, "UTC")
	putDay(t, c, "2026-10-01", WellnessDayInput{Soreness: new(2), SleepQuality: new(4), Stress: new(3), Motivation: new(5), Note: new("  Legs heavy  ")})
	putDay(t, c, "2026-10-02", WellnessDayInput{WeightKg: new(70.5), BodyFatPct: new(14.0), HydrationL: new(2.5)})
	// Putting a day again replaces it: the note goes, a blank note is none.
	got := putDay(t, c, "2026-10-01", WellnessDayInput{Soreness: new(1), Note: new("   ")})
	if *got.Soreness != 1 || got.Note != nil || got.Motivation != nil || got.Day.String() != "2026-10-01" {
		t.Errorf("replaced: %+v", got)
	}

	page := decodeJSON[WellnessDayPage](t, c.do(http.MethodGet, "/api/wellness?limit=1", nil, nil), http.StatusOK)
	if len(page.Items) != 1 || page.Items[0].Day.String() != "2026-10-02" || *page.Items[0].WeightKg != 70.5 || page.NextCursor == nil {
		t.Fatalf("first page: %+v", page)
	}
	page = decodeJSON[WellnessDayPage](t, c.do(http.MethodGet, "/api/wellness?limit=1&cursor="+*page.NextCursor, nil, nil), http.StatusOK)
	if len(page.Items) != 1 || page.Items[0].Day.String() != "2026-10-01" || page.NextCursor != nil {
		t.Errorf("second page: %+v", page)
	}

	for name, in := range map[string]WellnessDayInput{
		"empty":      {Note: new(" ")},
		"score":      {Stress: new(6)},
		"zero score": {Motivation: new(0)},
		"weight":     {WeightKg: new(0.0)},
		"body fat":   {BodyFatPct: new(101.0)},
		"hydration":  {HydrationL: new(-1.0)},
		"long note":  {Note: new(strings.Repeat("a", 2001))},
	} {
		rec := c.do(http.MethodPut, "/api/wellness/2026-10-03", in, nil)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d", name, rec.Code)
		}
	}
	wantProblem(t, c.do(http.MethodGet, "/api/wellness?limit=0", nil, nil), KindInvalidRequest)
	wantProblem(t, c.do(http.MethodGet, "/api/wellness?cursor=bogus", nil, nil), KindInvalidRequest)
	wantProblem(t, c.do(http.MethodPut, "/api/wellness/yesterday", WellnessDayInput{Stress: new(1)}, nil), KindInvalidRequest)

	// Bob neither sees nor deletes Alice's days; a client without a session gets nothing.
	bob := &apiClient{t: t, h: c.h}
	bob.session(bob.do(http.MethodPost, "/api/auth/register", register("bob"), nil), http.StatusCreated)
	wantProblem(t, bob.do(http.MethodDelete, "/api/wellness/2026-10-01", nil, nil), KindNotFound)
	if page := decodeJSON[WellnessDayPage](t, bob.do(http.MethodGet, "/api/wellness", nil, nil), http.StatusOK); len(page.Items) != 0 {
		t.Errorf("bob lists %+v", page.Items)
	}
	wantProblem(t, (&apiClient{t: t, h: c.h}).do(http.MethodGet, "/api/wellness", nil, nil), KindUnauthorized)

	if rec := c.do(http.MethodDelete, "/api/wellness/2026-10-01", nil, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("delete: %d", rec.Code)
	}
	wantProblem(t, c.do(http.MethodDelete, "/api/wellness/2026-10-01", nil, nil), KindNotFound)
}

func TestHealthLog(t *testing.T) {
	t.Parallel()
	c, _, _ := loggedIn(t, "UTC")
	on := func(s string) *Day {
		d, err := time.Parse(time.DateOnly, s)
		if err != nil {
			t.Fatal(err)
		}
		return &Day{Time: d}
	}
	create := func(in HealthLogInput) HealthLogEntry {
		t.Helper()
		return decodeJSON[HealthLogEntry](t, c.do(http.MethodPost, "/api/health-log", in, nil), http.StatusCreated)
	}
	knee := create(HealthLogInput{Kind: Injury, Description: new(" Sore left knee "), StartDate: *on("2026-09-01"), EndDate: on("2026-09-20")})
	if *knee.Description != "Sore left knee" || knee.EndDate.String() != "2026-09-20" {
		t.Errorf("created: %+v", knee)
	}
	cold := create(HealthLogInput{Kind: Illness, Description: new("Cold"), StartDate: *on("2026-10-10")})
	period := create(HealthLogInput{Kind: Period, StartDate: *on("2026-10-12")})

	page := decodeJSON[HealthLogPage](t, c.do(http.MethodGet, "/api/health-log?limit=2", nil, nil), http.StatusOK)
	if len(page.Items) != 2 || page.Items[0].Id != period.Id || page.Items[1].Id != cold.Id || page.NextCursor == nil {
		t.Fatalf("first page: %+v", page)
	}
	page = decodeJSON[HealthLogPage](t, c.do(http.MethodGet, "/api/health-log?limit=2&cursor="+*page.NextCursor, nil, nil), http.StatusOK)
	if len(page.Items) != 1 || page.Items[0].Id != knee.Id || page.NextCursor != nil {
		t.Errorf("second page: %+v", page)
	}
	page = decodeJSON[HealthLogPage](t, c.do(http.MethodGet, "/api/health-log?kind=illness", nil, nil), http.StatusOK)
	if len(page.Items) != 1 || page.Items[0].Id != cold.Id {
		t.Errorf("illnesses: %+v", page)
	}

	path := fmt.Sprintf("/api/health-log/%d", cold.Id)
	healed := decodeJSON[HealthLogEntry](t, c.do(http.MethodPut, path, HealthLogInput{Kind: Illness, Description: new("Cold"), StartDate: *on("2026-10-10"), EndDate: on("2026-10-14")}, nil), http.StatusOK)
	if healed.EndDate.String() != "2026-10-14" {
		t.Errorf("updated: %+v", healed)
	}

	for name, in := range map[string]HealthLogInput{
		"kind":           {Kind: "sprain", Description: new("x"), StartDate: *on("2026-10-01")},
		"no description": {Kind: Injury, Description: new("  "), StartDate: *on("2026-10-01")},
		"long":           {Kind: Illness, Description: new(strings.Repeat("a", 2001)), StartDate: *on("2026-10-01")},
		"end first":      {Kind: Period, StartDate: *on("2026-10-02"), EndDate: on("2026-10-01")},
	} {
		wantProblem(t, c.do(http.MethodPost, "/api/health-log", in, nil), KindInvalidRequest)
		if name == "kind" {
			wantProblem(t, c.do(http.MethodPut, path, in, nil), KindInvalidRequest)
		}
	}
	wantProblem(t, c.do(http.MethodGet, "/api/health-log?kind=sprain", nil, nil), KindInvalidRequest)
	wantProblem(t, c.do(http.MethodGet, "/api/health-log?limit=101", nil, nil), KindInvalidRequest)
	wantProblem(t, c.do(http.MethodGet, "/api/health-log?cursor=bogus", nil, nil), KindInvalidRequest)

	bob := &apiClient{t: t, h: c.h}
	bob.session(bob.do(http.MethodPost, "/api/auth/register", register("bob"), nil), http.StatusCreated)
	wantProblem(t, bob.do(http.MethodPut, path, HealthLogInput{Kind: Illness, Description: new("Bob's now"), StartDate: *on("2026-10-10")}, nil), KindNotFound)
	wantProblem(t, bob.do(http.MethodDelete, path, nil, nil), KindNotFound)
	if page := decodeJSON[HealthLogPage](t, bob.do(http.MethodGet, "/api/health-log", nil, nil), http.StatusOK); len(page.Items) != 0 {
		t.Errorf("bob lists %+v", page.Items)
	}
	wantProblem(t, (&apiClient{t: t, h: c.h}).do(http.MethodPost, "/api/health-log", HealthLogInput{Kind: Period, StartDate: *on("2026-10-01")}, nil), KindUnauthorized)

	if rec := c.do(http.MethodDelete, path, nil, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("delete: %d", rec.Code)
	}
	wantProblem(t, c.do(http.MethodDelete, path, nil, nil), KindNotFound)
}
