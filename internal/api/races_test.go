package api

import (
	"fmt"
	"net/http"
	"testing"
	"time"
)

func raceIn(name, day string) RaceInput {
	d, _ := time.Parse(time.DateOnly, day)
	return RaceInput{Name: name, Day: on(d), Sport: ActivitySportRunning, DistanceM: 42195, GoalTimeS: new(11700.0), Priority: RaceInputPriorityA}
}

func TestRaces(t *testing.T) {
	t.Parallel()
	pool := pg.NewDB(t)
	alice, bob := chatUser(t, pool, "alice"), chatUser(t, pool, "bob")
	create := func(in RaceInput) Race {
		t.Helper()
		return decodeJSON[Race](t, alice.do(http.MethodPost, "/api/races", in, nil), http.StatusCreated)
	}
	in := raceIn("  City Marathon ", "2026-12-06")
	marathon := create(in)
	if marathon.Name != "City Marathon" || marathon.Day.Format(time.DateOnly) != "2026-12-06" || *marathon.GoalTimeS != 11700 ||
		marathon.Priority != RacePriorityA || marathon.Sport != ActivitySportRunning {
		t.Fatalf("created %+v", marathon)
	}
	tenK := raceIn("Park 10K", "2026-11-15")
	tenK.GoalTimeS, tenK.DistanceM, tenK.Priority = nil, 10000, RaceInputPriorityC
	create(tenK)
	create(raceIn("Spring Half", "2026-04-12"))

	page := decodeJSON[RacePage](t, alice.do(http.MethodGet, "/api/races?limit=2", nil, nil), http.StatusOK)
	if len(page.Items) != 2 || page.Items[0].Name != "City Marathon" || page.Items[1].GoalTimeS != nil || page.NextCursor == nil {
		t.Fatalf("first page %+v", page)
	}
	rest := decodeJSON[RacePage](t, alice.do(http.MethodGet, "/api/races?limit=2&cursor="+*page.NextCursor, nil, nil), http.StatusOK)
	if len(rest.Items) != 1 || rest.Items[0].Name != "Spring Half" || rest.NextCursor != nil {
		t.Errorf("second page %+v", rest)
	}

	in.Name, in.Priority = "City Marathon (moved)", RaceInputPriorityB
	updated := decodeJSON[Race](t, alice.do(http.MethodPut, fmt.Sprintf("/api/races/%d", marathon.Id), in, nil), http.StatusOK)
	if updated.Id != marathon.Id || updated.Name != "City Marathon (moved)" || updated.Priority != RacePriorityB {
		t.Errorf("updated %+v", updated)
	}

	// The calendar shows the races in its range.
	cal := decodeJSON[Calendar](t, alice.do(http.MethodGet, "/api/calendar?from=2026-11-09&to=2026-12-13", nil, nil), http.StatusOK)
	if len(cal.Races) != 2 || cal.Races[0].Name != "Park 10K" || cal.Races[1].Name != "City Marathon (moved)" {
		t.Errorf("calendar races %+v", cal.Races)
	}

	// Bob sees and changes none of them.
	if other := decodeJSON[RacePage](t, bob.do(http.MethodGet, "/api/races", nil, nil), http.StatusOK); len(other.Items) != 0 {
		t.Errorf("bob lists %+v", other)
	}
	if cal := decodeJSON[Calendar](t, bob.do(http.MethodGet, "/api/calendar?from=2026-11-09&to=2026-12-13", nil, nil), http.StatusOK); len(cal.Races) != 0 {
		t.Errorf("bob's calendar %+v", cal.Races)
	}
	wantProblem(t, bob.do(http.MethodPut, fmt.Sprintf("/api/races/%d", marathon.Id), in, nil), KindNotFound)
	wantProblem(t, bob.do(http.MethodDelete, fmt.Sprintf("/api/races/%d", marathon.Id), nil, nil), KindNotFound)

	if rec := alice.do(http.MethodDelete, fmt.Sprintf("/api/races/%d", marathon.Id), nil, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body)
	}
	wantProblem(t, alice.do(http.MethodDelete, fmt.Sprintf("/api/races/%d", marathon.Id), nil, nil), KindNotFound)
}

func TestRaceValidation(t *testing.T) {
	t.Parallel()
	c := chatUser(t, pg.NewDB(t), "alice")
	bad := map[string]func(*RaceInput){
		"blank name":    func(r *RaceInput) { r.Name = "  " },
		"long name":     func(r *RaceInput) { r.Name = string(make([]rune, maxRaceName+1)) },
		"unknown sport": func(r *RaceInput) { r.Sport = "quidditch" },
		"zero distance": func(r *RaceInput) { r.DistanceM = 0 },
		"zero goal":     func(r *RaceInput) { r.GoalTimeS = new(0.0) },
		"priority":      func(r *RaceInput) { r.Priority = "D" },
	}
	for name, change := range bad {
		t.Run(name, func(t *testing.T) {
			in := raceIn("Race", "2026-12-06")
			change(&in)
			wantProblem(t, c.do(http.MethodPost, "/api/races", in, nil), KindInvalidRequest)
			wantProblem(t, c.do(http.MethodPut, "/api/races/1", in, nil), KindInvalidRequest)
		})
	}
	wantProblem(t, c.do(http.MethodPut, "/api/races/999", raceIn("Race", "2026-12-06"), nil), KindNotFound)
	for _, path := range []string{"/api/races?limit=0", "/api/races?cursor=junk"} {
		wantProblem(t, c.do(http.MethodGet, path, nil, nil), KindInvalidRequest)
	}
}
