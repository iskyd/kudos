package store_test

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/iskyd/kudos/internal/store/db"
	"github.com/iskyd/kudos/internal/testutil"
)

func TestWellnessDaysIsolationAndPaging(t *testing.T) {
	t.Parallel()
	pool := pg.NewDB(t)
	alice, bob := testutil.TwoUsers(t, pool)
	q := db.New(pool)
	ctx := t.Context()
	day := func(d int) time.Time { return time.Date(2026, 10, d, 0, 0, 0, 0, time.UTC) }

	for _, d := range []int{1, 2, 3} {
		_, err := q.PutWellnessDay(ctx, db.PutWellnessDayParams{UserID: alice.ID, Day: day(d), Soreness: new(int16(2))})
		check(t, err)
	}
	// Putting a day again replaces it whole.
	got, err := q.PutWellnessDay(ctx, db.PutWellnessDayParams{UserID: alice.ID, Day: day(3), WeightKg: new(float32(70.5))})
	check(t, err)
	if got.Soreness != nil || *got.WeightKg != 70.5 {
		t.Errorf("replaced: %+v", got)
	}
	// Bob's day 3 is his own row.
	_, err = q.PutWellnessDay(ctx, db.PutWellnessDayParams{UserID: bob.ID, Day: day(3), Stress: new(int16(5))})
	check(t, err)
	// An empty day is refused.
	if _, err := q.PutWellnessDay(ctx, db.PutWellnessDayParams{UserID: alice.ID, Day: day(4)}); err == nil {
		t.Error("stored an empty day")
	}

	page, err := q.ListWellnessDays(ctx, db.ListWellnessDaysParams{UserID: alice.ID, MaxRows: 2})
	check(t, err)
	if len(page) != 2 || page[0].Day.Day() != 3 || page[1].Day.Day() != 2 || *page[0].WeightKg != 70.5 {
		t.Fatalf("first page: %+v", page)
	}
	rest, err := q.ListWellnessDays(ctx, db.ListWellnessDaysParams{UserID: alice.ID, Before: &page[1].Day, MaxRows: 2})
	check(t, err)
	if len(rest) != 1 || rest[0].Day.Day() != 1 {
		t.Errorf("second page: %+v", rest)
	}

	if got, err := q.GetWellnessDay(ctx, db.GetWellnessDayParams{UserID: alice.ID, Day: day(3)}); err != nil || *got.WeightKg != 70.5 {
		t.Errorf("alice's day: %+v, %v", got, err)
	}
	if _, err := q.GetWellnessDay(ctx, db.GetWellnessDayParams{UserID: bob.ID, Day: day(2)}); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("bob got alice's day: %v", err)
	}

	// Bob reaches none of Alice's days.
	if n, err := q.DeleteWellnessDay(ctx, db.DeleteWellnessDayParams{UserID: bob.ID, Day: day(1)}); err != nil || n != 0 {
		t.Errorf("bob deleted %d: %v", n, err)
	}
	bobs, err := q.ListWellnessDays(ctx, db.ListWellnessDaysParams{UserID: bob.ID, MaxRows: 10})
	check(t, err)
	if len(bobs) != 1 || *bobs[0].Stress != 5 {
		t.Errorf("bob's days: %+v", bobs)
	}
	if n, err := q.DeleteWellnessDay(ctx, db.DeleteWellnessDayParams{UserID: alice.ID, Day: day(1)}); err != nil || n != 1 {
		t.Errorf("alice deleted %d: %v", n, err)
	}
}

func TestHealthLogIsolationAndPaging(t *testing.T) {
	t.Parallel()
	pool := pg.NewDB(t)
	alice, bob := testutil.TwoUsers(t, pool)
	q := db.New(pool)
	ctx := t.Context()
	day := func(d int) time.Time { return time.Date(2026, 10, d, 0, 0, 0, 0, time.UTC) }

	knee, err := q.CreateHealthLogEntry(ctx, db.CreateHealthLogEntryParams{UserID: alice.ID, Kind: "injury", Description: new("Sore knee"), StartDate: day(1), EndDate: new(day(5))})
	check(t, err)
	cold, err := q.CreateHealthLogEntry(ctx, db.CreateHealthLogEntryParams{UserID: alice.ID, Kind: "illness", Description: new("Cold"), StartDate: day(10)})
	check(t, err)
	period, err := q.CreateHealthLogEntry(ctx, db.CreateHealthLogEntryParams{UserID: alice.ID, Kind: "period", StartDate: day(10)})
	check(t, err)
	for name, p := range map[string]db.CreateHealthLogEntryParams{
		"injury without description": {UserID: alice.ID, Kind: "injury", StartDate: day(1)},
		"end before start":           {UserID: alice.ID, Kind: "period", StartDate: day(2), EndDate: new(day(1))},
	} {
		if _, err := q.CreateHealthLogEntry(ctx, p); err == nil {
			t.Errorf("%s: stored", name)
		}
	}

	page, err := q.ListHealthLog(ctx, db.ListHealthLogParams{UserID: alice.ID, MaxRows: 2})
	check(t, err)
	if len(page) != 2 || page[0].ID != period.ID || page[1].ID != cold.ID {
		t.Fatalf("first page: %+v", page)
	}
	rest, err := q.ListHealthLog(ctx, db.ListHealthLogParams{UserID: alice.ID, AfterStart: &page[1].StartDate, AfterID: &page[1].ID, MaxRows: 2})
	check(t, err)
	if len(rest) != 1 || rest[0].ID != knee.ID {
		t.Errorf("second page: %+v", rest)
	}
	periods, err := q.ListHealthLog(ctx, db.ListHealthLogParams{UserID: alice.ID, Kind: new("period"), MaxRows: 10})
	check(t, err)
	if len(periods) != 1 || periods[0].ID != period.ID {
		t.Errorf("periods: %+v", periods)
	}

	// Entries covering a day: illness and injury only, end date inclusive.
	for d, want := range map[int][]int64{10: {cold.ID}, 5: {knee.ID}, 6: nil} {
		on, err := q.ListHealthLogOn(ctx, db.ListHealthLogOnParams{UserID: alice.ID, Day: day(d), MaxRows: 10})
		check(t, err)
		ids := make([]int64, 0, len(on))
		for _, e := range on {
			ids = append(ids, e.ID)
		}
		if !slices.Equal(ids, want) && len(ids)+len(want) > 0 {
			t.Errorf("on day %d: %v, want %v", d, ids, want)
		}
	}

	// Bob reaches none of Alice's entries.
	if _, err := q.UpdateHealthLogEntry(ctx, db.UpdateHealthLogEntryParams{UserID: bob.ID, ID: knee.ID, Kind: "injury", Description: new("x"), StartDate: day(1)}); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("bob updated alice's entry: %v", err)
	}
	if n, err := q.DeleteHealthLogEntry(ctx, db.DeleteHealthLogEntryParams{UserID: bob.ID, ID: knee.ID}); err != nil || n != 0 {
		t.Errorf("bob deleted %d: %v", n, err)
	}
	if bobs, err := q.ListHealthLog(ctx, db.ListHealthLogParams{UserID: bob.ID, MaxRows: 10}); err != nil || len(bobs) != 0 {
		t.Errorf("bob's log: %+v, %v", bobs, err)
	}
	if bobs, err := q.ListHealthLogOn(ctx, db.ListHealthLogOnParams{UserID: bob.ID, Day: day(10), MaxRows: 10}); err != nil || len(bobs) != 0 {
		t.Errorf("bob's entries on day 10: %+v, %v", bobs, err)
	}

	healed, err := q.UpdateHealthLogEntry(ctx, db.UpdateHealthLogEntryParams{UserID: alice.ID, ID: cold.ID, Kind: "illness", Description: new("Cold"), StartDate: day(10), EndDate: new(day(14))})
	check(t, err)
	if !healed.EndDate.Equal(day(14)) {
		t.Errorf("updated: %+v", healed)
	}
	if n, err := q.DeleteHealthLogEntry(ctx, db.DeleteHealthLogEntryParams{UserID: alice.ID, ID: knee.ID}); err != nil || n != 1 {
		t.Errorf("alice deleted %d: %v", n, err)
	}
}
