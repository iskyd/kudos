package api

import (
	"net/http"
	"testing"
	"time"

	"github.com/iskyd/kudos/internal/store/db"
)

func TestListCoachReports(t *testing.T) {
	t.Parallel()
	pool := pg.NewDB(t)
	alice, q := newAPIOn(t, pool, true, nil)
	aliceID := alice.session(alice.do(http.MethodPost, "/api/auth/register", register("alice"), nil), http.StatusCreated).Id
	bob := chatUser(t, pool, "bob")
	for _, d := range []string{"2026-11-08", "2026-11-09", "2026-11-10"} {
		day, _ := time.Parse(time.DateOnly, d)
		if _, err := q.CreateCoachReport(t.Context(), db.CreateCoachReportParams{
			UserID: aliceID, Kind: "daily_insight", Day: day, PromptVersion: "daily-insight.v1", Content: "Insight " + d,
		}); err != nil {
			t.Fatal(err)
		}
	}

	page := decodeJSON[CoachReportPage](t, alice.do(http.MethodGet, "/api/coach-reports?kind=daily_insight&limit=2", nil, nil), http.StatusOK)
	if len(page.Items) != 2 || page.Items[0].Content != "Insight 2026-11-10" || page.Items[0].PromptVersion != "daily-insight.v1" ||
		page.Items[0].Kind != DailyInsight || page.Items[0].Day.Format(time.DateOnly) != "2026-11-10" || page.NextCursor == nil {
		t.Fatalf("first page %+v", page)
	}
	rest := decodeJSON[CoachReportPage](t, alice.do(http.MethodGet, "/api/coach-reports?kind=daily_insight&limit=2&cursor="+*page.NextCursor, nil, nil), http.StatusOK)
	if len(rest.Items) != 1 || rest.Items[0].Content != "Insight 2026-11-08" || rest.NextCursor != nil {
		t.Errorf("second page %+v", rest)
	}
	if reports := decodeJSON[CoachReportPage](t, alice.do(http.MethodGet, "/api/coach-reports?kind=weekly_report", nil, nil), http.StatusOK); len(reports.Items) != 0 {
		t.Errorf("weekly reports %+v", reports)
	}
	if other := decodeJSON[CoachReportPage](t, bob.do(http.MethodGet, "/api/coach-reports?kind=daily_insight", nil, nil), http.StatusOK); len(other.Items) != 0 {
		t.Errorf("bob reads alice's insights: %+v", other)
	}

	for _, path := range []string{"/api/coach-reports?kind=poem", "/api/coach-reports", "/api/coach-reports?kind=daily_insight&limit=0",
		"/api/coach-reports?kind=daily_insight&cursor=junk"} {
		wantProblem(t, alice.do(http.MethodGet, path, nil, nil), KindInvalidRequest)
	}
	anon, _ := newAPIOn(t, pool, true, nil)
	wantProblem(t, anon.do(http.MethodGet, "/api/coach-reports?kind=daily_insight", nil, nil), KindUnauthorized)
}
