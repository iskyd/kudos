package api

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/iskyd/kudos/internal/store/db"
)

func TestCurves(t *testing.T) {
	t.Parallel()
	c, q, me := loggedIn(t, "Europe/Rome")
	points := func(id int64, kind string, durations []int32, values []float64) {
		t.Helper()
		kinds := make([]string, len(durations))
		for i := range kinds {
			kinds[i] = kind
		}
		if err := q.InsertCurvePoints(t.Context(), db.InsertCurvePointsParams{
			UserID: me.Id, ActivityID: id, Kinds: kinds, Durations: durations, CurveValues: values,
		}); err != nil {
			t.Fatal(err)
		}
	}
	get := func(query string) Curves {
		t.Helper()
		return decodeJSON[Curves](t, c.do(http.MethodGet, "/api/curves"+query, nil, nil), http.StatusOK)
	}

	if empty := get(""); len(empty.Power) != 0 || len(empty.Pace) != 0 || empty.FtpW != nil || empty.ThresholdSpeedMS != nil {
		t.Errorf("no activities: %+v", empty)
	}

	// 00:30 in Rome on 1 October is that local day.
	first := run(t, c, "2026-10-01T00:30", 10000, 3000)
	second := run(t, c, "2026-10-05T07:00", 5000, 1400)
	// The first's 30 minutes beat its 20: the curve takes 210 W for both.
	points(first, "power", []int32{5, 1200, 1800}, []float64{500, 200, 210})
	points(first, "pace", []int32{1800}, []float64{4})
	points(second, "power", []int32{5, 60}, []float64{400, 300})
	points(second, "pace", []int32{60}, []float64{5})

	all := get("")
	want := []struct {
		d  int
		w  float64
		id int64
	}{{5, 500, first}, {60, 300, second}, {1200, 210, first}, {1800, 210, first}}
	if len(all.Power) != len(want) {
		t.Fatalf("power: %+v", all.Power)
	}
	for i, w := range want {
		if p := all.Power[i]; p.DurationS != w.d || p.Value != w.w || p.ActivityId != w.id {
			t.Errorf("power %d: %+v, want %+v", i, p, w)
		}
	}
	if p := all.Power[0]; p.UtcOffsetS != 7200 || !p.StartTime.Equal(time.Date(2026, 9, 30, 22, 30, 0, 0, time.UTC)) {
		t.Errorf("start: %+v", p)
	}
	if len(all.Pace) != 2 || all.Pace[0].Value != 5 || all.Pace[1].Value != 4 || all.Pace[1].ActivityId != first {
		t.Errorf("pace: %+v", all.Pace)
	}
	// FTP 95% of 210 W; 4 m/s is 4:10/km.
	if all.FtpW == nil || *all.FtpW != 199 || all.ThresholdSpeedMS == nil || *all.ThresholdSpeedMS != 4 {
		t.Errorf("estimates: %v %v", all.FtpW, all.ThresholdSpeedMS)
	}

	// From 2 October only the second counts: too short for estimates.
	later := get("?from=2026-10-02&to=2026-10-31")
	if len(later.Power) != 2 || later.Power[0].Value != 400 || len(later.Pace) != 1 || later.FtpW != nil || later.ThresholdSpeedMS != nil {
		t.Errorf("from 2 October: %+v", later)
	}
	if early := get("?to=2026-10-01"); len(early.Power) != 3 || early.Power[0].ActivityId != first {
		t.Errorf("to 1 October: %+v", early)
	}
	wantProblem(t, c.do(http.MethodGet, "/api/curves?from=2026-10-05&to=2026-10-01", nil, nil), KindInvalidRequest)

	bob := &apiClient{t: t, h: c.h}
	bob.session(bob.do(http.MethodPost, "/api/auth/register", register("bob"), nil), http.StatusCreated)
	if got := decodeJSON[Curves](t, bob.do(http.MethodGet, "/api/curves", nil, nil), http.StatusOK); len(got.Power)+len(got.Pace) != 0 {
		t.Errorf("bob: %+v", got)
	}
	wantProblem(t, (&apiClient{t: t, h: c.h}).do(http.MethodGet, "/api/curves", nil, nil), KindUnauthorized)
}

func TestEstimatedThresholdSuggestion(t *testing.T) {
	t.Parallel()
	c, q, me := loggedIn(t, "UTC")
	id := run(t, c, "2026-10-05T07:00", 10000, 2500)
	day := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	if err := q.SuggestEstimate(t.Context(), db.SuggestEstimateParams{
		UserID: me.Id, Sport: "running", EffectiveFrom: day, ThresholdSpeedMS: new(float32(4)), ActivityID: &id,
	}); err != nil {
		t.Fatal(err)
	}
	createZones(t, c, ZoneSetInput{Sport: ZoneSportRunning, EffectiveFrom: on(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)), ThresholdSpeedMS: new(3.5), LthrBpm: new(170)})

	s := suggestion(t, suggestionPage(t, c), ZoneSportRunning, "2026-10-05")
	if s.ThresholdSpeedMS == nil || *s.ThresholdSpeedMS != 4 || s.LthrBpm != nil || *s.ActivityId != id || *s.Current.ThresholdSpeedMS != 3.5 {
		t.Errorf("suggestion: %+v", s)
	}
	v := decodeJSON[ZoneSet](t, c.do(http.MethodPost, fmt.Sprintf("/api/zones/suggestions/%d/confirm", s.Id), nil, nil), http.StatusCreated)
	if *v.ThresholdSpeedMS != 4 || *v.LthrBpm != 170 {
		t.Errorf("confirmed version: %+v", v)
	}
}
