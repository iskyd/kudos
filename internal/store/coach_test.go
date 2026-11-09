package store_test

import (
	"testing"
	"time"

	"github.com/iskyd/kudos/internal/store/db"
	"github.com/iskyd/kudos/internal/testutil"
)

func TestCoachReports(t *testing.T) {
	t.Parallel()
	pool := pg.NewDB(t)
	alice, bob := testutil.TwoUsers(t, pool)
	q := db.New(pool)
	ctx := t.Context()
	put := func(userID int64, kind, day, content string) int64 {
		t.Helper()
		d, _ := time.Parse(time.DateOnly, day)
		n, err := q.CreateCoachReport(ctx, db.CreateCoachReportParams{UserID: userID, Kind: kind, Day: d, PromptVersion: "daily-insight.v1", Content: content})
		check(t, err)
		return n
	}
	list := func(userID int64, kind string, before *time.Time, limit int32) []db.CoachReport {
		t.Helper()
		rows, err := q.ListCoachReports(ctx, db.ListCoachReportsParams{UserID: userID, Kind: kind, BeforeDay: before, MaxRows: limit})
		check(t, err)
		return rows
	}

	for _, d := range []string{"2026-11-07", "2026-11-09", "2026-11-08"} {
		put(alice.ID, "daily_insight", d, "insight "+d)
	}
	if n := put(alice.ID, "daily_insight", "2026-11-09", "again"); n != 0 {
		t.Errorf("a second insight for a day was stored")
	}
	page := list(alice.ID, "daily_insight", nil, 2)
	if len(page) != 2 || page[0].Content != "insight 2026-11-09" || page[1].Content != "insight 2026-11-08" {
		t.Fatalf("first page %+v", page)
	}
	if rest := list(alice.ID, "daily_insight", &page[1].Day, 2); len(rest) != 1 || rest[0].Content != "insight 2026-11-07" {
		t.Errorf("second page %+v", rest)
	}
	if got := list(alice.ID, "weekly_report", nil, 10); len(got) != 0 {
		t.Errorf("reports %+v", got)
	}
	put(alice.ID, "weekly_report", "2026-11-09", "report")
	tuesday, _ := time.Parse(time.DateOnly, "2026-11-10")
	if _, err := q.CreateCoachReport(ctx, db.CreateCoachReportParams{UserID: alice.ID, Kind: "weekly_report", Day: tuesday, PromptVersion: "v", Content: "c"}); err == nil {
		t.Error("a report on a Tuesday was stored")
	}

	// Bob sees none of alice's, and his own day is his.
	if got := list(bob.ID, "daily_insight", nil, 10); len(got) != 0 {
		t.Errorf("bob reads alice's insights: %+v", got)
	}
	if n := put(bob.ID, "daily_insight", "2026-11-09", "bob's"); n != 1 {
		t.Error("bob's insight was not stored")
	}
	if got := list(alice.ID, "daily_insight", nil, 1); got[0].Content != "insight 2026-11-09" {
		t.Errorf("alice's insight changed: %+v", got)
	}

	// Candidates: only users with a provider, with their latest days.
	_, err := q.PutLLMProvider(ctx, db.PutLLMProviderParams{UserID: alice.ID, Provider: "ollama", BaseUrl: "http://ollama.example.test", Model: "m"})
	check(t, err)
	cands, err := q.ListCoachCandidates(ctx, db.ListCoachCandidatesParams{AfterID: 0, MaxRows: 10})
	check(t, err)
	if len(cands) != 1 || cands[0].UserID != alice.ID || cands[0].LastInsight.Format(time.DateOnly) != "2026-11-09" ||
		cands[0].LastReport.Format(time.DateOnly) != "2026-11-09" || cands[0].GarminConnected || !cands[0].LastSyncAt.Before(tuesday) {
		t.Errorf("candidates %+v", cands)
	}
	if more, err := q.ListCoachCandidates(ctx, db.ListCoachCandidatesParams{AfterID: alice.ID, MaxRows: 10}); err != nil || len(more) != 0 {
		t.Errorf("after alice: %+v, %v", more, err)
	}
}
