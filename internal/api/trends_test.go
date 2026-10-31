package api

import (
	"net/http"
	"testing"

	"github.com/iskyd/kudos/internal/store/db"
)

func TestActivityTrends(t *testing.T) {
	t.Parallel()
	c, q, me := loggedIn(t, "UTC")
	derived := func(id int64, d db.UpsertActivityDerivedParams) {
		t.Helper()
		d.UserID, d.ActivityID, d.Version = me.Id, id, 1
		if err := q.UpsertActivityDerived(t.Context(), d); err != nil {
			t.Fatal(err)
		}
	}
	first := run(t, c, "2026-10-01T07:00", 10000, 3000)
	second := run(t, c, "2026-10-05T07:00", 5000, 1400)
	derived(first, db.UpsertActivityDerivedParams{
		AvgSpeedMS: new(10000.0 / 3000), AvgHrBpm: new(150.0), AvgCadence: new(172.0),
		EfficiencyMPerBeat: new(1.33), DecouplingPct: new(4.5),
		AvgVerticalOscillationM: new(0.09), AvgStanceTimeS: new(0.24), AvgStepLengthM: new(1.16),
	})
	derived(second, db.UpsertActivityDerivedParams{EfficiencyMPerBeat: new(1.4)})
	body := ManualActivityRequest{Sport: SportPoolSwimming, LocalStartTime: "2026-10-03T07:00", DurationS: 1800}
	swim := decodeJSON[CreatedActivity](t, c.do(http.MethodPost, "/api/activities", body, nil), http.StatusCreated).Id
	derived(swim, db.UpsertActivityDerivedParams{AvgSwolf: new(105.0)})
	list := func(c *apiClient, query string) ActivityTrendPage {
		t.Helper()
		return decodeJSON[ActivityTrendPage](t, c.do(http.MethodGet, "/api/trends"+query, nil, nil), http.StatusOK)
	}

	page := list(c, "?kind=running&limit=1")
	if len(page.Items) != 1 || page.Items[0].ActivityId != second || page.NextCursor == nil {
		t.Fatalf("first page: %+v", page)
	}
	page = list(c, "?kind=running&limit=1&cursor="+*page.NextCursor)
	if len(page.Items) != 1 || page.NextCursor != nil {
		t.Fatalf("second page: %+v", page)
	}
	got := page.Items[0]
	if got.ActivityId != first || got.Sport != ActivitySportRunning || *got.EfficiencyMPerBeat != 1.33 || *got.DecouplingPct != 4.5 ||
		*got.CadenceSpm != 172 || *got.AvgHrBpm != 150 || *got.VerticalOscillationM != 0.09 || *got.StanceTimeS != 0.24 ||
		*got.StepLengthM != 1.16 || got.Swolf != nil || got.UtcOffsetS != 0 {
		t.Errorf("first run: %+v", got)
	}
	if page := list(c, "?kind=running&from=2026-10-02&to=2026-10-31"); len(page.Items) != 1 || page.Items[0].ActivityId != second {
		t.Errorf("from 2 October: %+v", page)
	}
	if page := list(c, "?kind=swimming"); len(page.Items) != 1 || page.Items[0].ActivityId != swim || *page.Items[0].Swolf != 105 {
		t.Errorf("swims: %+v", page)
	}

	for _, query := range []string{"?kind=cycling", "?kind=running&limit=0", "?kind=running&from=2026-10-05&to=2026-10-01", "?kind=running&cursor=x"} {
		wantProblem(t, c.do(http.MethodGet, "/api/trends"+query, nil, nil), KindInvalidRequest)
	}
	bob := &apiClient{t: t, h: c.h}
	bob.session(bob.do(http.MethodPost, "/api/auth/register", register("bob"), nil), http.StatusCreated)
	if page := list(bob, "?kind=running"); len(page.Items) != 0 {
		t.Errorf("bob: %+v", page)
	}
	wantProblem(t, (&apiClient{t: t, h: c.h}).do(http.MethodGet, "/api/trends?kind=running", nil, nil), KindUnauthorized)
}
