package api

import (
	"math"
	"net/http"
	"testing"
	"time"

	"github.com/iskyd/kudos/internal/store/db"
)

func TestReadiness(t *testing.T) {
	t.Parallel()
	c, q, me := loggedIn(t, "UTC")
	ctx := t.Context()
	day := time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC)
	metrics := func(d time.Time, sleepS *int32) {
		t.Helper()
		if err := q.UpsertDailyMetrics(ctx, db.UpsertDailyMetricsParams{
			UserID: me.Id, Day: d, HrvMs: new(float32(60)), RestingHrBpm: new(int16(50)), SleepS: sleepS,
		}); err != nil {
			t.Fatal(err)
		}
	}
	for i := 2; i <= 14; i++ {
		metrics(day.AddDate(0, 0, -i), nil)
	}
	const path = "/api/readiness?day=2026-10-31"

	got := decodeJSON[Readiness](t, c.do(http.MethodGet, path, nil, nil), http.StatusOK)
	if got.Status != CollectingBaseline || got.BaselineDays != 13 || got.RequiredDays != 14 || len(got.Components) != 0 || got.Score != nil {
		t.Fatalf("collecting: %+v", got)
	}

	// At baseline with no load, HRV, resting HR and form score 75; 8 h of sleep
	// and a check-in without soreness score 100:
	// 0.3·75 + 0.2·75 + 0.2·100 + 0.15·75 + 0.15·100 = 83.75.
	metrics(day.AddDate(0, 0, -1), nil)
	metrics(day, new(int32(8*3600)))
	putDay(t, c, "2026-10-31", WellnessDayInput{Soreness: new(1)})
	got = decodeJSON[Readiness](t, c.do(http.MethodGet, path, nil, nil), http.StatusOK)
	if got.Status != Ready || got.BaselineDays != 14 || *got.Score != 84 || *got.UncappedScore != 84 || got.Cap != nil || len(got.Components) != 5 {
		t.Fatalf("ready: %+v", got)
	}
	if hrv := got.Components[0]; hrv.Name != Hrv || math.Abs(hrv.Score-75) > 1e-9 || *hrv.Value != 60 || math.Abs(*hrv.Baseline-60) > 1e-9 || hrv.Share != 0.3 {
		t.Errorf("hrv: %+v", hrv)
	}

	cold := decodeJSON[HealthLogEntry](t, c.do(http.MethodPost, "/api/health-log", HealthLogInput{
		Kind: Illness, Description: new("Cold"), StartDate: Day{Time: day.AddDate(0, 0, -2)},
	}, nil), http.StatusCreated)
	got = decodeJSON[Readiness](t, c.do(http.MethodGet, path, nil, nil), http.StatusOK)
	if *got.Score != 30 || *got.UncappedScore != 84 || got.Cap == nil || *got.Cap != (ReadinessCap{EntryId: cold.Id, Kind: Illness, Max: 30}) {
		t.Errorf("capped: %+v", got)
	}

	// Bob sees none of Alice's baseline.
	bob := &apiClient{t: t, h: c.h}
	bob.session(bob.do(http.MethodPost, "/api/auth/register", register("bob"), nil), http.StatusCreated)
	if got := decodeJSON[Readiness](t, bob.do(http.MethodGet, path, nil, nil), http.StatusOK); got.BaselineDays != 0 || got.Status != CollectingBaseline {
		t.Errorf("bob: %+v", got)
	}
	wantProblem(t, (&apiClient{t: t, h: c.h}).do(http.MethodGet, path, nil, nil), KindUnauthorized)
	wantProblem(t, c.do(http.MethodGet, "/api/readiness?day=today", nil, nil), KindInvalidRequest)
}
