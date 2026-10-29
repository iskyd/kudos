package store_test

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/iskyd/kudos/internal/store/db"
	"github.com/iskyd/kudos/internal/testutil"
)

func day(d int) time.Time { return time.Date(2026, 10, d, 0, 0, 0, 0, time.UTC) }

func fullDay(userID int64, d time.Time) db.UpsertDailyMetricsParams {
	return db.UpsertDailyMetricsParams{
		UserID: userID, Day: d, RestingHrBpm: ptr[int16](48), HrvMs: ptr[float32](62.5),
		SleepS: ptr[int32](27000), SleepDeepS: ptr[int32](5400), SleepLightS: ptr[int32](14400),
		SleepRemS: ptr[int32](6000), SleepAwakeS: ptr[int32](1200), SleepScore: ptr[int16](81),
		Stress: ptr[int16](28), BodyBatteryMax: ptr[int16](95), BodyBatteryMin: ptr[int16](20),
		Steps: ptr[int32](12034), WeightKg: ptr[float32](70.4), BodyFatPct: ptr[float32](14.2),
		BodyWaterPct: ptr[float32](58.1), MuscleMassKg: ptr[float32](33.9), BoneMassKg: ptr[float32](3.4),
		Spo2Pct: ptr[float32](96), HydrationL: ptr[float32](2.5),
	}
}

func TestDailyMetricsRoundTrip(t *testing.T) {
	t.Parallel()
	pool := pg.NewDB(t)
	alice, _ := testutil.TwoUsers(t, pool)
	q := db.New(pool)

	in := fullDay(alice.ID, day(15))
	check(t, q.UpsertDailyMetrics(t.Context(), in))
	got, err := q.GetDailyMetrics(t.Context(), db.GetDailyMetricsParams{UserID: alice.ID, Day: day(15)})
	check(t, err)
	want := db.DailyMetric{
		UserID: in.UserID, Day: in.Day, RestingHrBpm: in.RestingHrBpm, HrvMs: in.HrvMs, SleepS: in.SleepS,
		SleepDeepS: in.SleepDeepS, SleepLightS: in.SleepLightS, SleepRemS: in.SleepRemS, SleepAwakeS: in.SleepAwakeS,
		SleepScore: in.SleepScore, Stress: in.Stress, BodyBatteryMax: in.BodyBatteryMax, BodyBatteryMin: in.BodyBatteryMin,
		Steps: in.Steps, WeightKg: in.WeightKg, BodyFatPct: in.BodyFatPct, BodyWaterPct: in.BodyWaterPct,
		MuscleMassKg: in.MuscleMassKg, BoneMassKg: in.BoneMassKg, Spo2Pct: in.Spo2Pct, HydrationL: in.HydrationL,
		UpdatedAt: got.UpdatedAt,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
}

func TestUpsertDailyMetricsRevises(t *testing.T) {
	t.Parallel()
	pool := pg.NewDB(t)
	alice, _ := testutil.TwoUsers(t, pool)
	q := db.New(pool)
	ctx := t.Context()

	check(t, q.UpsertDailyMetrics(ctx, fullDay(alice.ID, day(15))))
	first, err := q.GetDailyMetrics(ctx, db.GetDailyMetricsParams{UserID: alice.ID, Day: day(15)})
	check(t, err)

	// A revision replaces the values it sends and keeps the ones it leaves out.
	check(t, q.UpsertDailyMetrics(ctx, db.UpsertDailyMetricsParams{
		UserID: alice.ID, Day: day(15), SleepScore: ptr[int16](77), Steps: ptr[int32](13500),
	}))
	got, err := q.GetDailyMetrics(ctx, db.GetDailyMetricsParams{UserID: alice.ID, Day: day(15)})
	check(t, err)
	if *got.SleepScore != 77 || *got.Steps != 13500 {
		t.Errorf("revised values = %d, %d, want 77, 13500", *got.SleepScore, *got.Steps)
	}
	if *got.RestingHrBpm != 48 || *got.WeightKg != 70.4 || *got.HydrationL != 2.5 {
		t.Errorf("values not sent were lost: %+v", got)
	}
	if got.UpdatedAt.Before(first.UpdatedAt) {
		t.Errorf("updated_at went back: %v then %v", first.UpdatedAt, got.UpdatedAt)
	}
}

func TestDailyMetricsRejectsOutOfRange(t *testing.T) {
	t.Parallel()
	pool := pg.NewDB(t)
	alice, _ := testutil.TwoUsers(t, pool)
	q := db.New(pool)

	for name, change := range map[string]func(*db.UpsertDailyMetricsParams){
		"sleep score":  func(p *db.UpsertDailyMetricsParams) { p.SleepScore = ptr[int16](101) },
		"body battery": func(p *db.UpsertDailyMetricsParams) { p.BodyBatteryMin = ptr[int16](-1) },
		"steps":        func(p *db.UpsertDailyMetricsParams) { p.Steps = ptr[int32](-5) },
		"spo2":         func(p *db.UpsertDailyMetricsParams) { p.Spo2Pct = ptr[float32](101) },
		"weight":       func(p *db.UpsertDailyMetricsParams) { p.WeightKg = ptr[float32](0) },
	} {
		p := fullDay(alice.ID, day(15))
		change(&p)
		var pgErr *pgconn.PgError
		if err := q.UpsertDailyMetrics(t.Context(), p); !errors.As(err, &pgErr) || pgErr.Code != "23514" {
			t.Errorf("%s: error = %v, want a check violation", name, err)
		}
	}
}

func TestListDailyMetricsPagination(t *testing.T) {
	t.Parallel()
	pool := pg.NewDB(t)
	alice, _ := testutil.TwoUsers(t, pool)
	q := db.New(pool)
	ctx := t.Context()
	for _, d := range []int{20, 10, 12, 11, 30} {
		check(t, q.UpsertDailyMetrics(ctx, db.UpsertDailyMetricsParams{UserID: alice.ID, Day: day(d), Steps: ptr(int32(d))}))
	}

	// Days 11..20, two per page, oldest first.
	var got []int
	after := day(10)
	for {
		page, err := q.ListDailyMetrics(ctx, db.ListDailyMetricsParams{UserID: alice.ID, After: after, Until: day(20), MaxRows: 2})
		check(t, err)
		for _, m := range page {
			got = append(got, m.Day.Day())
		}
		if len(page) < 2 {
			break
		}
		after = page[len(page)-1].Day
	}
	if want := []int{11, 12, 20}; !reflect.DeepEqual(got, want) {
		t.Errorf("days = %v, want %v", got, want)
	}
}

func TestDailyMetricsIsolation(t *testing.T) {
	t.Parallel()
	pool := pg.NewDB(t)
	alice, bob := testutil.TwoUsers(t, pool)
	q := db.New(pool)
	ctx := t.Context()

	check(t, q.UpsertDailyMetrics(ctx, fullDay(alice.ID, day(15))))
	// Bob's row for the same day is his own and leaves Alice's untouched.
	check(t, q.UpsertDailyMetrics(ctx, db.UpsertDailyMetricsParams{UserID: bob.ID, Day: day(15), Steps: ptr[int32](1)}))

	a, err := q.GetDailyMetrics(ctx, db.GetDailyMetricsParams{UserID: alice.ID, Day: day(15)})
	check(t, err)
	if *a.Steps != 12034 {
		t.Errorf("alice's steps = %d after bob's upsert", *a.Steps)
	}
	b, err := q.GetDailyMetrics(ctx, db.GetDailyMetricsParams{UserID: bob.ID, Day: day(15)})
	check(t, err)
	if *b.Steps != 1 || b.RestingHrBpm != nil {
		t.Errorf("bob reads %+v", b)
	}
	if _, err := q.GetDailyMetrics(ctx, db.GetDailyMetricsParams{UserID: bob.ID, Day: day(16)}); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("missing day error = %v, want ErrNoRows", err)
	}
	rows, err := q.ListDailyMetrics(ctx, db.ListDailyMetricsParams{UserID: bob.ID, After: day(1), Until: day(31), MaxRows: 10})
	check(t, err)
	if len(rows) != 1 || rows[0].UserID != bob.ID {
		t.Errorf("bob lists %+v", rows)
	}
}
