package api

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func createGear(t *testing.T, c *apiClient, in GearInput) Gear {
	t.Helper()
	return decodeJSON[Gear](t, c.do(http.MethodPost, "/api/gear", in, nil), http.StatusCreated)
}

func run(t *testing.T, c *apiClient, start string, distanceM, durationS float64) int64 {
	t.Helper()
	body := ManualActivityRequest{Sport: SportRunning, LocalStartTime: start, DurationS: durationS, DistanceM: &distanceM}
	return decodeJSON[CreatedActivity](t, c.do(http.MethodPost, "/api/activities", body, nil), http.StatusCreated).Id
}

func setGear(t *testing.T, c *apiClient, activityID int64, gearID *int64) {
	t.Helper()
	decodeJSON[ActivityDetail](t, c.do(http.MethodPut, fmt.Sprintf("/api/activities/%d/annotation", activityID), ActivityAnnotation{GearId: gearID}, nil), http.StatusOK)
}

func TestGearTotalsAndThreshold(t *testing.T) {
	t.Parallel()
	c, _, _ := loggedIn(t, "UTC")
	shoes := createGear(t, c, GearInput{Kind: GearInputKindShoes, Name: "  Trainers ", ReplacementDistanceM: new(15000.0)})
	if shoes.Name != "Trainers" || shoes.Retired || shoes.DistanceM != 0 || shoes.Activities != 0 || shoes.ReplacementDue {
		t.Fatalf("created = %+v", shoes)
	}
	racers := createGear(t, c, GearInput{Kind: GearInputKindShoes, Name: "Racers"})

	easy := run(t, c, "2026-10-01T07:00", 10000, 3000)
	long := run(t, c, "2026-10-02T07:00", 21000, 7000)
	setGear(t, c, easy, &shoes.Id)
	setGear(t, c, long, &shoes.Id)

	gear := func(id int64) Gear {
		t.Helper()
		for _, g := range decodeJSON[GearPage](t, c.do(http.MethodGet, "/api/gear", nil, nil), http.StatusOK).Items {
			if g.Id == id {
				return g
			}
		}
		t.Fatalf("gear %d not listed", id)
		return Gear{}
	}
	// 31 km is past the 15 km threshold.
	if g := gear(shoes.Id); g.DistanceM != 31000 || g.TimeS != 10000 || g.Activities != 2 || !g.ReplacementDue {
		t.Errorf("after two runs: %+v", g)
	}
	if got := decodeJSON[ActivityDetail](t, c.do(http.MethodGet, fmt.Sprintf("/api/activities/%d", long), nil, nil), http.StatusOK); *got.GearId != shoes.Id {
		t.Errorf("detail gear = %v", got.GearId)
	}

	// Reassigning the long run moves its distance and clears the warning.
	setGear(t, c, long, &racers.Id)
	if g := gear(shoes.Id); g.DistanceM != 10000 || g.Activities != 1 || g.ReplacementDue {
		t.Errorf("shoes after reassigning: %+v", g)
	}
	if g := gear(racers.Id); g.DistanceM != 21000 || g.TimeS != 7000 || g.ReplacementDue {
		t.Errorf("racers after reassigning: %+v", g)
	}
	// The list filters by gear.
	page := decodeJSON[ActivityPage](t, c.do(http.MethodGet, fmt.Sprintf("/api/activities?gear=%d", racers.Id), nil, nil), http.StatusOK)
	if len(page.Items) != 1 || page.Items[0].Id != long {
		t.Errorf("gear filter: %+v", page.Items)
	}

	// Lowering the threshold to exactly the distance raises the warning; editing keeps the totals.
	g := decodeJSON[Gear](t, c.do(http.MethodPut, fmt.Sprintf("/api/gear/%d", shoes.Id),
		GearInput{Kind: GearInputKindShoes, Name: "Trainers", ReplacementDistanceM: new(10000.0), Retired: new(true)}, nil), http.StatusOK)
	if !g.ReplacementDue || !g.Retired || g.DistanceM != 10000 {
		t.Errorf("after editing: %+v", g)
	}

	// Deleting gear keeps the activity, without gear.
	if rec := c.do(http.MethodDelete, fmt.Sprintf("/api/gear/%d", racers.Id), nil, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("delete: %d", rec.Code)
	}
	if got := decodeJSON[ActivityDetail](t, c.do(http.MethodGet, fmt.Sprintf("/api/activities/%d", long), nil, nil), http.StatusOK); got.GearId != nil {
		t.Errorf("deleted gear still assigned: %d", *got.GearId)
	}
	wantProblem(t, c.do(http.MethodDelete, fmt.Sprintf("/api/gear/%d", racers.Id), nil, nil), KindNotFound)
}

func TestListGearPaginates(t *testing.T) {
	t.Parallel()
	c, _, _ := loggedIn(t, "UTC")
	for i := range 3 {
		createGear(t, c, GearInput{Kind: GearInputKindBike, Name: fmt.Sprint("bike ", i)})
	}
	if n := pageAll(t, c, "/api/gear", func(p GearPage) int { return len(p.Items) }, func(p GearPage) *string { return p.NextCursor }); n != 3 {
		t.Errorf("listed %d gear", n)
	}
	wantProblem(t, c.do(http.MethodGet, "/api/gear?limit=101", nil, nil), KindInvalidRequest)
	wantProblem(t, c.do(http.MethodGet, "/api/gear?cursor=nope", nil, nil), KindInvalidRequest)
}

func TestGearValidation(t *testing.T) {
	t.Parallel()
	c, _, _ := loggedIn(t, "UTC")
	g := createGear(t, c, GearInput{Kind: GearInputKindBike, Name: "Road"})
	for name, in := range map[string]GearInput{
		"kind":      {Kind: "skis", Name: "x"},
		"blank":     {Kind: GearInputKindBike, Name: "   "},
		"long":      {Kind: GearInputKindBike, Name: strings.Repeat("é", maxGearNameLen+1)},
		"threshold": {Kind: GearInputKindBike, Name: "x", ReplacementDistanceM: new(0.0)},
	} {
		t.Run(name, func(t *testing.T) {
			c.t = t
			wantProblem(t, c.do(http.MethodPost, "/api/gear", in, nil), KindInvalidRequest)
			wantProblem(t, c.do(http.MethodPut, fmt.Sprintf("/api/gear/%d", g.Id), in, nil), KindInvalidRequest)
		})
	}
	c.t = t
	if got := createGear(t, c, GearInput{Kind: GearInputKindBike, Name: strings.Repeat("é", maxGearNameLen)}); got.Name == "" {
		t.Error("an 80-character name was refused")
	}
}

func TestGearIsolationAPI(t *testing.T) {
	t.Parallel()
	c, _, _ := loggedIn(t, "UTC")
	mine := createGear(t, c, GearInput{Kind: GearInputKindBike, Name: "Alice's"})

	bob := &apiClient{t: t, h: c.h}
	bob.session(bob.do(http.MethodPost, "/api/auth/register", register("bob"), nil), http.StatusCreated)
	path := fmt.Sprintf("/api/gear/%d", mine.Id)
	wantProblem(t, bob.do(http.MethodPut, path, GearInput{Kind: GearInputKindBike, Name: "Bob's now"}, nil), KindNotFound)
	wantProblem(t, bob.do(http.MethodDelete, path, nil, nil), KindNotFound)
	if page := decodeJSON[GearPage](t, bob.do(http.MethodGet, "/api/gear", nil, nil), http.StatusOK); len(page.Items) != 0 {
		t.Errorf("bob lists %+v", page.Items)
	}
	// Bob cannot assign alice's gear to his own activity.
	ride := run(t, bob, "2026-10-01T07:00", 5000, 1500)
	wantProblem(t, bob.do(http.MethodPut, fmt.Sprintf("/api/activities/%d/annotation", ride), ActivityAnnotation{GearId: &mine.Id}, nil), KindInvalidRequest)

	if page := decodeJSON[GearPage](t, c.do(http.MethodGet, "/api/gear", nil, nil), http.StatusOK); len(page.Items) != 1 || page.Items[0].Name != "Alice's" {
		t.Errorf("alice's gear changed: %+v", page.Items)
	}
}
