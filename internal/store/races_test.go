package store_test

import (
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/iskyd/kudos/internal/store/db"
	"github.com/iskyd/kudos/internal/testutil"
)

func TestRaces(t *testing.T) {
	t.Parallel()
	pool := pg.NewDB(t)
	alice, bob := testutil.TwoUsers(t, pool)
	q := db.New(pool)
	ctx := t.Context()
	on := func(s string) time.Time {
		d, _ := time.Parse(time.DateOnly, s)
		return d
	}
	create := func(userID int64, name, day string) db.Race {
		t.Helper()
		r, err := q.CreateRace(ctx, db.CreateRaceParams{UserID: userID, Name: name, Day: on(day), Sport: "running", DistanceM: 10000, Priority: "B"})
		check(t, err)
		return r
	}
	spring := create(alice.ID, "Spring 10K", "2026-04-12")
	autumn := create(alice.ID, "Autumn 10K", "2026-10-04")
	create(alice.ID, "Same day", "2026-10-04")

	page, err := q.ListRaces(ctx, db.ListRacesParams{UserID: alice.ID, MaxRows: 2})
	check(t, err)
	if len(page) != 2 || !page[0].Day.Equal(on("2026-10-04")) || page[0].ID < page[1].ID {
		t.Fatalf("first page %+v", page)
	}
	last := page[1]
	rest, err := q.ListRaces(ctx, db.ListRacesParams{UserID: alice.ID, AfterDay: &last.Day, AfterID: &last.ID, MaxRows: 2})
	check(t, err)
	if len(rest) != 1 || rest[0].ID != spring.ID {
		t.Errorf("second page %+v", rest)
	}
	between, err := q.ListRacesBetween(ctx, db.ListRacesBetweenParams{UserID: alice.ID, FromDay: on("2026-04-12"), ToDay: on("2026-10-03"), MaxRows: 10})
	check(t, err)
	if len(between) != 1 || between[0].ID != spring.ID {
		t.Errorf("between %+v", between)
	}

	for name, p := range map[string]db.CreateRaceParams{
		"bad priority":  {UserID: alice.ID, Name: "x", Day: on("2026-01-01"), Sport: "running", DistanceM: 1, Priority: "D"},
		"no distance":   {UserID: alice.ID, Name: "x", Day: on("2026-01-01"), Sport: "running", DistanceM: 0, Priority: "A"},
		"empty name":    {UserID: alice.ID, Name: "", Day: on("2026-01-01"), Sport: "running", DistanceM: 1, Priority: "A"},
		"negative goal": {UserID: alice.ID, Name: "x", Day: on("2026-01-01"), Sport: "running", DistanceM: 1, GoalTimeS: new(-1.0), Priority: "A"},
	} {
		if _, err := q.CreateRace(ctx, p); err == nil {
			t.Errorf("%s: stored", name)
		}
	}

	// Bob can neither see nor change alice's races.
	if rows, err := q.ListRaces(ctx, db.ListRacesParams{UserID: bob.ID, MaxRows: 10}); err != nil || len(rows) != 0 {
		t.Errorf("bob lists %+v, %v", rows, err)
	}
	if rows, err := q.ListRacesBetween(ctx, db.ListRacesBetweenParams{UserID: bob.ID, FromDay: on("2026-01-01"), ToDay: on("2026-12-31"), MaxRows: 10}); err != nil || len(rows) != 0 {
		t.Errorf("bob's calendar %+v, %v", rows, err)
	}
	if _, err := q.UpdateRace(ctx, db.UpdateRaceParams{UserID: bob.ID, ID: autumn.ID, Name: "Mine", Day: autumn.Day, Sport: "running", DistanceM: 1, Priority: "A"}); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("bob updates alice's race: %v", err)
	}
	if n, err := q.DeleteRace(ctx, db.DeleteRaceParams{UserID: bob.ID, ID: autumn.ID}); err != nil || n != 0 {
		t.Errorf("bob deletes %d, %v", n, err)
	}

	got, err := q.UpdateRace(ctx, db.UpdateRaceParams{UserID: alice.ID, ID: autumn.ID, Name: "Autumn Half", Day: on("2026-10-11"), Sport: "running",
		DistanceM: 21097.5, GoalTimeS: new(5400.0), Priority: "A"})
	check(t, err)
	if got.Name != "Autumn Half" || !got.Day.Equal(on("2026-10-11")) || *got.GoalTimeS != 5400 || got.Priority != "A" {
		t.Errorf("updated %+v", got)
	}
	if n, err := q.DeleteRace(ctx, db.DeleteRaceParams{UserID: alice.ID, ID: autumn.ID}); err != nil || n != 1 {
		t.Errorf("alice deletes %d, %v", n, err)
	}
}
