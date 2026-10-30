package store_test

import (
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/iskyd/kudos/internal/store/db"
	"github.com/iskyd/kudos/internal/testutil"
)

func date(t *testing.T, s string) time.Time {
	t.Helper()
	d, err := time.Parse(time.DateOnly, s)
	check(t, err)
	return d
}

func TestZoneSetsResolveByDate(t *testing.T) {
	t.Parallel()
	pool := pg.NewDB(t)
	alice, bob := testutil.TwoUsers(t, pool)
	q := db.New(pool)
	ctx := t.Context()

	// The samples' lactate threshold history: 176, 177, then 171.
	for _, v := range []struct {
		from string
		lthr int16
	}{{"2025-11-01", 176}, {"2026-03-15", 177}, {"2026-09-20", 171}} {
		_, err := q.CreateZoneSet(ctx, db.CreateZoneSetParams{UserID: alice.ID, Sport: "running", EffectiveFrom: date(t, v.from), LthrBpm: ptr(v.lthr)})
		check(t, err)
	}
	_, err := q.CreateZoneSet(ctx, db.CreateZoneSetParams{UserID: alice.ID, Sport: "general", EffectiveFrom: date(t, "2026-01-01"), MaxHrBpm: ptr[int16](195), RestingHrBpm: ptr[int16](45)})
	check(t, err)
	// Bob's version is newer and must never apply to Alice.
	_, err = q.CreateZoneSet(ctx, db.CreateZoneSetParams{UserID: bob.ID, Sport: "running", EffectiveFrom: date(t, "2026-10-01"), LthrBpm: ptr[int16](150)})
	check(t, err)

	at := func(d string) map[string]int16 {
		rows, err := q.ListZoneSetsAt(ctx, db.ListZoneSetsAtParams{UserID: alice.ID, Day: date(t, d)})
		check(t, err)
		out := map[string]int16{}
		for _, r := range rows {
			if r.UserID != alice.ID {
				t.Errorf("read another user's version: %+v", r)
			}
			switch {
			case r.LthrBpm != nil:
				out[r.Sport] = *r.LthrBpm
			case r.MaxHrBpm != nil:
				out[r.Sport] = *r.MaxHrBpm
			}
		}
		return out
	}
	for d, want := range map[string]map[string]int16{
		"2025-10-31": {},
		"2025-11-01": {"running": 176},
		"2026-03-14": {"running": 176, "general": 195},
		"2026-03-15": {"running": 177, "general": 195},
		"2026-10-30": {"running": 171, "general": 195},
	} {
		got := at(d)
		if len(got) != len(want) {
			t.Errorf("%s: %v, want %v", d, got, want)
			continue
		}
		for k, v := range want {
			if got[k] != v {
				t.Errorf("%s: %v, want %v", d, got, want)
			}
		}
	}

	// One version per sport and day.
	_, err = q.CreateZoneSet(ctx, db.CreateZoneSetParams{UserID: alice.ID, Sport: "running", EffectiveFrom: date(t, "2026-03-15"), LthrBpm: ptr[int16](180)})
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		t.Errorf("duplicate day: %v", err)
	}
}

func TestZoneSetsPageAndIsolation(t *testing.T) {
	t.Parallel()
	pool := pg.NewDB(t)
	alice, bob := testutil.TwoUsers(t, pool)
	q := db.New(pool)
	ctx := t.Context()

	var ids []int64
	for _, d := range []string{"2026-01-01", "2026-02-01", "2026-03-01"} {
		z, err := q.CreateZoneSet(ctx, db.CreateZoneSetParams{UserID: alice.ID, Sport: "cycling", EffectiveFrom: date(t, d), FtpW: ptr[int16](250)})
		check(t, err)
		ids = append(ids, z.ID)
	}
	_, err := q.CreateZoneSet(ctx, db.CreateZoneSetParams{UserID: alice.ID, Sport: "swimming", EffectiveFrom: date(t, "2026-02-15"), CssMS: ptr[float32](1.2)})
	check(t, err)

	// Latest first, two per page, filtered by sport.
	cycling := "cycling"
	page, err := q.ListZoneSets(ctx, db.ListZoneSetsParams{UserID: alice.ID, Sport: &cycling, MaxRows: 2})
	check(t, err)
	if len(page) != 2 || page[0].ID != ids[2] || page[1].ID != ids[1] {
		t.Fatalf("first page: %+v", page)
	}
	page, err = q.ListZoneSets(ctx, db.ListZoneSetsParams{UserID: alice.ID, Sport: &cycling, AfterFrom: &page[1].EffectiveFrom, AfterID: &page[1].ID, MaxRows: 2})
	check(t, err)
	if len(page) != 1 || page[0].ID != ids[0] {
		t.Fatalf("second page: %+v", page)
	}
	all, err := q.ListZoneSets(ctx, db.ListZoneSetsParams{UserID: alice.ID, MaxRows: 10})
	check(t, err)
	if len(all) != 4 {
		t.Errorf("all sports: %d", len(all))
	}

	// Bob can neither list nor delete Alice's versions.
	if rows, err := q.ListZoneSets(ctx, db.ListZoneSetsParams{UserID: bob.ID, MaxRows: 10}); err != nil || len(rows) != 0 {
		t.Errorf("bob listed %d: %v", len(rows), err)
	}
	if n, err := q.DeleteZoneSet(ctx, db.DeleteZoneSetParams{UserID: bob.ID, ID: ids[0]}); err != nil || n != 0 {
		t.Errorf("bob deleted %d: %v", n, err)
	}
	if n, err := q.DeleteZoneSet(ctx, db.DeleteZoneSetParams{UserID: alice.ID, ID: ids[0]}); err != nil || n != 1 {
		t.Errorf("alice deleted %d: %v", n, err)
	}
}
