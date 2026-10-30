package api

import (
	"fmt"
	"net/http"
	"testing"
	"time"
)

func suggestionPage(t *testing.T, c *apiClient) []ZoneSuggestion {
	t.Helper()
	return decodeJSON[ZoneSuggestionPage](t, c.do(http.MethodGet, "/api/zones/suggestions", nil, nil), http.StatusOK).Items
}

func suggestion(t *testing.T, items []ZoneSuggestion, sport ZoneSport, day string) ZoneSuggestion {
	t.Helper()
	for _, s := range items {
		if s.Sport == sport && s.EffectiveFrom.Format(time.DateOnly) == day {
			return s
		}
	}
	t.Fatalf("no %s suggestion on %s in %+v", sport, day, items)
	return ZoneSuggestion{}
}

func TestZoneSuggestionsFromFiles(t *testing.T) {
	t.Parallel()
	c, _, _ := loggedIn(t, "UTC")
	for _, f := range []string{"pool_swim.fit", "strength.fit", "bike.fit", "hike.fit", "intervals.fit", "half_marathon.fit"} {
		uploadCorpus(t, c, f)
	}
	items := suggestionPage(t, c)
	if len(items) != 5 || items[0].Sport != ZoneSportRunning {
		t.Fatalf("suggestions, latest first: %+v", items)
	}
	if n := pageAll(t, c, "/api/zones/suggestions", func(p ZoneSuggestionPage) int { return len(p.Items) }, func(p ZoneSuggestionPage) *string { return p.NextCursor }); n != 5 {
		t.Errorf("paged %d", n)
	}
	run := suggestion(t, items, ZoneSportRunning, "2026-10-15")
	if *run.LthrBpm != 171 || *run.MaxHrBpm != 187 || run.FtpW != nil || run.ActivityId == nil || run.Current != nil {
		t.Errorf("running suggestion: %+v", run)
	}
	if bike := suggestion(t, items, ZoneSportCycling, "2026-08-07"); bike.FtpW != nil || *bike.LthrBpm != 185 {
		t.Errorf("cycling suggestion: %+v", bike)
	}

	// Confirming keeps what the version valid then had, with the suggestion over it.
	createZones(t, c, ZoneSetInput{Sport: ZoneSportRunning, EffectiveFrom: on(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)), ThresholdSpeedMS: new(4.0), LthrBpm: new(176)})
	items = suggestionPage(t, c)
	run = suggestion(t, items, ZoneSportRunning, "2026-10-15")
	if run.Current == nil || *run.Current.LthrBpm != 176 {
		t.Errorf("current version: %+v", run.Current)
	}
	v := decodeJSON[ZoneSet](t, c.do(http.MethodPost, fmt.Sprintf("/api/zones/suggestions/%d/confirm", run.Id), nil, nil), http.StatusCreated)
	if v.EffectiveFrom.Format(time.DateOnly) != "2026-10-15" || *v.LthrBpm != 171 || *v.ThresholdSpeedMS != 4 || *v.MaxHrBpm != 187 || *v.Zones.HrModel != "friel_run_hr" {
		t.Errorf("confirmed version: %+v", v)
	}
	wantProblem(t, c.do(http.MethodPost, fmt.Sprintf("/api/zones/suggestions/%d/confirm", run.Id), nil, nil), KindNotFound)

	// The general history: confirm November, then March shows it as current; dismiss March.
	nov := suggestion(t, items, ZoneSportGeneral, "2025-11-19")
	decodeJSON[ZoneSet](t, c.do(http.MethodPost, fmt.Sprintf("/api/zones/suggestions/%d/confirm", nov.Id), nil, nil), http.StatusCreated)
	mar := suggestion(t, suggestionPage(t, c), ZoneSportGeneral, "2026-03-23")
	if mar.Current == nil || *mar.Current.MaxHrBpm != 194 || *mar.MaxHrBpm != 192 {
		t.Errorf("march: %+v", mar)
	}
	if r := c.do(http.MethodPost, fmt.Sprintf("/api/zones/suggestions/%d/dismiss", mar.Id), nil, nil); r.Code != http.StatusNoContent {
		t.Errorf("dismiss: %d", r.Code)
	}
	wantProblem(t, c.do(http.MethodPost, fmt.Sprintf("/api/zones/suggestions/%d/dismiss", mar.Id), nil, nil), KindNotFound)
	if n := len(suggestionPage(t, c)); n != 2 {
		t.Errorf("%d pending after confirming two and dismissing one", n)
	}

	// A version already on that day stops a confirm; the suggestion stays.
	aug := suggestion(t, suggestionPage(t, c), ZoneSportGeneral, "2026-08-09")
	createZones(t, c, ZoneSetInput{Sport: ZoneSportGeneral, EffectiveFrom: aug.EffectiveFrom, MaxHrBpm: new(200), RestingHrBpm: new(50)})
	wantProblem(t, c.do(http.MethodPost, fmt.Sprintf("/api/zones/suggestions/%d/confirm", aug.Id), nil, nil), KindInvalidRequest)

	// Bob reaches none of it.
	bob := &apiClient{t: t, h: c.h}
	bob.session(bob.do(http.MethodPost, "/api/auth/register", register("bob"), nil), http.StatusCreated)
	if n := len(suggestionPage(t, bob)); n != 0 {
		t.Errorf("bob lists %d", n)
	}
	wantProblem(t, bob.do(http.MethodPost, fmt.Sprintf("/api/zones/suggestions/%d/confirm", aug.Id), nil, nil), KindNotFound)
	wantProblem(t, bob.do(http.MethodPost, fmt.Sprintf("/api/zones/suggestions/%d/dismiss", aug.Id), nil, nil), KindNotFound)
	for _, q := range []string{"limit=0", "cursor=bogus"} {
		wantProblem(t, c.do(http.MethodGet, "/api/zones/suggestions?"+q, nil, nil), KindInvalidRequest)
	}
}
