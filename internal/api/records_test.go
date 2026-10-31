package api

import (
	"net/http"
	"testing"

	"github.com/iskyd/kudos/internal/store/db"
)

func TestRecords(t *testing.T) {
	t.Parallel()
	c, q, me := loggedIn(t, "Europe/Rome")
	efforts := func(id int64, names []string, values []float64) {
		t.Helper()
		if err := q.InsertBestEfforts(t.Context(), db.InsertBestEffortsParams{UserID: me.Id, ActivityID: id, Efforts: names, EffortValues: values}); err != nil {
			t.Fatal(err)
		}
	}

	empty := decodeJSON[Records](t, c.do(http.MethodGet, "/api/records", nil, nil), http.StatusOK)
	if empty.Since != nil || len(empty.Records) != 0 {
		t.Errorf("no activities: %+v", empty)
	}

	// 00:30 in Rome on 1 October: records cover that local day on.
	first := run(t, c, "2026-10-01T00:30", 5000, 1500)
	second := run(t, c, "2026-10-05T07:00", 5000, 1400)
	efforts(first, []string{"power_5s", "fastest_5k"}, []float64{500, 1500})
	efforts(second, []string{"fastest_5k", "power_5s"}, []float64{1400, 400})

	got := decodeJSON[Records](t, c.do(http.MethodGet, "/api/records", nil, nil), http.StatusOK)
	if got.Since == nil || got.Since.String() != "2026-10-01" || len(got.Records) != 2 {
		t.Fatalf("records: %+v", got)
	}
	// In display order: running before power.
	five, power := got.Records[0], got.Records[1]
	if five.Effort != Fastest5k || len(five.History) != 2 || five.History[0].ActivityId != first || five.History[1].Value != 1400 ||
		five.History[1].UtcOffsetS != 7200 || five.History[1].StartTime.UTC().Hour() != 5 {
		t.Errorf("5K: %+v", five)
	}
	if power.Effort != Power5s || len(power.History) != 1 || power.History[0].Value != 500 {
		t.Errorf("power: %+v", power)
	}

	bob := &apiClient{t: t, h: c.h}
	bob.session(bob.do(http.MethodPost, "/api/auth/register", register("bob"), nil), http.StatusCreated)
	if got := decodeJSON[Records](t, bob.do(http.MethodGet, "/api/records", nil, nil), http.StatusOK); len(got.Records) != 0 || got.Since != nil {
		t.Errorf("bob: %+v", got)
	}
	wantProblem(t, (&apiClient{t: t, h: c.h}).do(http.MethodGet, "/api/records", nil, nil), KindUnauthorized)
}
