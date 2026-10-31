package store_test

import (
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/iskyd/kudos/internal/store/db"
	"github.com/iskyd/kudos/internal/testutil"
)

func TestRecordHistoryAndIsolation(t *testing.T) {
	t.Parallel()
	pool := pg.NewDB(t)
	alice, bob := testutil.TwoUsers(t, pool)
	q := db.New(pool)
	ctx := t.Context()
	day := func(d int) time.Time { return time.Date(2026, 10, d, 7, 0, 0, 0, time.UTC) }
	activity := func(d int) int64 {
		id, err := q.UpsertActivity(ctx, db.UpsertActivityParams{
			UserID: alice.ID, Source: "manual", SourceActivityID: fmt.Sprint(d), Sport: "running", Subtype: "outdoor", StartTime: day(d),
		})
		check(t, err)
		return id
	}
	efforts := func(userID, id int64, values map[string]float64) {
		p := db.InsertBestEffortsParams{UserID: userID, ActivityID: id}
		for k, v := range values {
			p.Efforts, p.EffortValues = append(p.Efforts, k), append(p.EffortValues, v)
		}
		check(t, q.InsertBestEfforts(ctx, p))
	}
	history := func(userID int64) []string {
		rows, err := q.ListRecordHistory(ctx, db.ListRecordHistoryParams{UserID: userID, MaxRows: 100})
		check(t, err)
		var out []string
		for _, r := range rows {
			out = append(out, fmt.Sprintf("%s %d %v", r.Effort, r.StartTime.Day(), r.Value))
		}
		return out
	}

	if _, err := q.GetRecordsSince(ctx, alice.ID); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("since without activities: %v", err)
	}
	// Created out of order: history follows start time, not ID.
	third, first, second := activity(10), activity(1), activity(5)
	merged := activity(12)
	check(t, q.SetActivityMergedInto(ctx, db.SetActivityMergedIntoParams{UserID: alice.ID, ID: merged, MergedInto: &third}))
	efforts(alice.ID, first, map[string]float64{"fastest_5k": 1500, "power_5s": 500})
	efforts(alice.ID, second, map[string]float64{"fastest_5k": 1400, "power_5s": 400})
	efforts(alice.ID, third, map[string]float64{"fastest_5k": 1450, "power_5s": 600})
	efforts(alice.ID, merged, map[string]float64{"fastest_5k": 1000})

	want := []string{"fastest_5k 1 1500", "fastest_5k 5 1400", "power_5s 1 500", "power_5s 10 600"}
	if got := history(alice.ID); !slices.Equal(got, want) {
		t.Errorf("history: %v, want %v", got, want)
	}
	if since, err := q.GetRecordsSince(ctx, alice.ID); err != nil || !since.Equal(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("since the first activity: %v %v", since, err)
	}
	_, err := q.CreateGarminBackfill(ctx, db.CreateGarminBackfillParams{
		UserID: alice.ID, WindowStart: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), WindowEnd: day(31),
	})
	check(t, err)
	if since, err := q.GetRecordsSince(ctx, alice.ID); err != nil || since.Month() != time.September || since.Day() != 1 {
		t.Errorf("since the backfill window: %v %v", since, err)
	}

	// Bob neither reads, adds to nor clears Alice's efforts.
	efforts(bob.ID, second, map[string]float64{"fastest_5k": 1})
	check(t, q.DeleteBestEfforts(ctx, db.DeleteBestEffortsParams{UserID: bob.ID, ActivityID: first}))
	if got := history(bob.ID); len(got) != 0 {
		t.Errorf("bob's history: %v", got)
	}
	if _, err := q.GetRecordsSince(ctx, bob.ID); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("bob's since: %v", err)
	}
	if got := history(alice.ID); !slices.Equal(got, want) {
		t.Errorf("after bob: %v", got)
	}
	check(t, q.DeleteBestEfforts(ctx, db.DeleteBestEffortsParams{UserID: alice.ID, ActivityID: first}))
	if got := history(alice.ID); !slices.Equal(got, []string{"fastest_5k 5 1400", "power_5s 5 400", "power_5s 10 600"}) {
		t.Errorf("after clearing the first: %v", got)
	}
}
