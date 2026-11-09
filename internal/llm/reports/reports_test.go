package reports

import (
	"bytes"
	"context"
	"errors"
	"log"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivertest"
	"github.com/riverqueue/river/rivertype"

	"github.com/iskyd/kudos/internal/llm"
	"github.com/iskyd/kudos/internal/store/db"
	"github.com/iskyd/kudos/internal/testutil"
)

var pg *testutil.Postgres

func TestMain(m *testing.M) {
	ctx := context.Background()
	var err error
	if pg, err = testutil.StartPostgres(ctx); err != nil {
		log.Fatal(err)
	}
	code := m.Run()
	if err := pg.Terminate(ctx); err != nil {
		log.Print(err)
	}
	os.Exit(code)
}

func at(t *testing.T, tz, local string) time.Time {
	t.Helper()
	loc, err := time.LoadLocation(tz)
	if err != nil {
		t.Fatal(err)
	}
	v, err := time.ParseInLocation("2006-01-02 15:04", local, loc)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func day(s string) time.Time {
	d, _ := time.Parse(time.DateOnly, s) // the tests' literals are valid
	return d
}

func show(d *time.Time) string {
	if d == nil {
		return ""
	}
	return d.Format(time.DateOnly)
}

func TestDue(t *testing.T) {
	t.Parallel()
	rome := func(c db.ListCoachCandidatesRow) db.ListCoachCandidatesRow { c.Timezone = "Europe/Rome"; return c }
	for _, tc := range []struct {
		name            string
		c               db.ListCoachCandidatesRow
		now             time.Time
		insight, report string // "" when not due
	}{
		{name: "before the morning", c: rome(db.ListCoachCandidatesRow{}), now: at(t, "Europe/Rome", "2026-11-10 05:59")},
		{name: "morning without Garmin", c: rome(db.ListCoachCandidatesRow{}), now: at(t, "Europe/Rome", "2026-11-10 06:00"), insight: "2026-11-10"},
		{name: "already written today", c: rome(db.ListCoachCandidatesRow{LastInsight: day("2026-11-10")}), now: at(t, "Europe/Rome", "2026-11-10 12:00")},
		{name: "written yesterday", c: rome(db.ListCoachCandidatesRow{LastInsight: day("2026-11-09")}), now: at(t, "Europe/Rome", "2026-11-10 23:59"), insight: "2026-11-10"},
		{name: "waits for the morning sync", c: rome(db.ListCoachCandidatesRow{GarminConnected: true, LastSyncAt: at(t, "Europe/Rome", "2026-11-10 05:30")}),
			now: at(t, "Europe/Rome", "2026-11-10 09:59")},
		{name: "after the morning sync", c: rome(db.ListCoachCandidatesRow{GarminConnected: true, LastSyncAt: at(t, "Europe/Rome", "2026-11-10 06:10")}),
			now: at(t, "Europe/Rome", "2026-11-10 06:15"), insight: "2026-11-10"},
		{name: "never synced, ten o'clock", c: rome(db.ListCoachCandidatesRow{GarminConnected: true}), now: at(t, "Europe/Rome", "2026-11-10 10:00"), insight: "2026-11-10"},
		{name: "Monday report", c: rome(db.ListCoachCandidatesRow{LastReport: day("2026-11-02")}), now: at(t, "Europe/Rome", "2026-11-09 07:00"),
			insight: "2026-11-09", report: "2026-11-09"},
		{name: "report written", c: rome(db.ListCoachCandidatesRow{LastInsight: day("2026-11-09"), LastReport: day("2026-11-09")}),
			now: at(t, "Europe/Rome", "2026-11-09 07:00")},
		{name: "a missed Monday is skipped", c: rome(db.ListCoachCandidatesRow{LastReport: day("2026-11-02")}), now: at(t, "Europe/Rome", "2026-11-10 07:00"),
			insight: "2026-11-10"},
		// Monday 06:30 in Auckland is still Sunday in UTC.
		{name: "Monday ahead of UTC", c: db.ListCoachCandidatesRow{Timezone: "Pacific/Auckland"}, now: at(t, "Pacific/Auckland", "2026-11-09 06:30"),
			insight: "2026-11-09", report: "2026-11-09"},
		// Sunday 23:00 in New York is Monday in UTC: no report yet.
		{name: "Sunday behind UTC", c: db.ListCoachCandidatesRow{Timezone: "America/New_York", LastInsight: day("2026-11-08")},
			now: at(t, "America/New_York", "2026-11-08 23:00")},
		{name: "half-hour zone, not yet", c: db.ListCoachCandidatesRow{Timezone: "Asia/Kolkata"}, now: time.Date(2026, 11, 10, 0, 29, 0, 0, time.UTC)},
		{name: "half-hour zone, morning", c: db.ListCoachCandidatesRow{Timezone: "Asia/Kolkata"}, now: time.Date(2026, 11, 10, 0, 30, 0, 0, time.UTC),
			insight: "2026-11-10"},
		// Rome's summer time starts on 29 March 2026: 06:00 local is 04:00 UTC.
		{name: "summer time starts, before", c: rome(db.ListCoachCandidatesRow{}), now: time.Date(2026, 3, 29, 3, 59, 0, 0, time.UTC)},
		{name: "summer time starts, morning", c: rome(db.ListCoachCandidatesRow{}), now: time.Date(2026, 3, 29, 4, 0, 0, 0, time.UTC), insight: "2026-03-29"},
		// It ends on 25 October 2026: 06:00 local is 05:00 UTC.
		{name: "summer time ends, before", c: rome(db.ListCoachCandidatesRow{}), now: time.Date(2026, 10, 25, 4, 59, 0, 0, time.UTC)},
		{name: "summer time ends, morning", c: rome(db.ListCoachCandidatesRow{}), now: time.Date(2026, 10, 25, 5, 0, 0, 0, time.UTC), insight: "2026-10-25"},
		{name: "unknown zone counts as UTC", c: db.ListCoachCandidatesRow{Timezone: "Mars/Olympus"}, now: time.Date(2026, 11, 10, 6, 0, 0, 0, time.UTC),
			insight: "2026-11-10"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			insight, report := Due(tc.c, tc.now)
			if got := show(insight); got != tc.insight {
				t.Errorf("insight = %q, want %q", got, tc.insight)
			}
			if got := show(report); got != tc.report {
				t.Errorf("report = %q, want %q", got, tc.report)
			}
		})
	}
}

// fakeClient answers Complete with reply or err and keeps the request.
type fakeClient struct {
	reply string
	err   error
	got   llm.Request
}

func (f *fakeClient) Complete(_ context.Context, r llm.Request) (string, error) {
	f.got = r
	return f.reply, f.err
}

func (*fakeClient) Stream(context.Context, llm.Request, func(string) error) error {
	return errors.New("not used")
}

func newDeps(pool *pgxpool.Pool, logs *bytes.Buffer, client llm.Client) *Deps {
	return &Deps{
		Pool:   pool,
		Logger: slog.New(slog.NewJSONHandler(logs, nil)),
		Context: func(_ context.Context, _ int64, today time.Time) (llm.AthleteContext, error) {
			return llm.AthleteContext{Today: today, HealthLog: []llm.HealthEntry{{Kind: "injury", Description: "Sore knee", Start: day("2026-10-01")}}}, nil
		},
		Client: func(context.Context, int64) (llm.Client, error) {
			if client == nil {
				return nil, llm.ErrNotConfigured
			}
			return client, nil
		},
	}
}

func work(d *Deps, args GenerateArgs) error {
	return (&GenerateWorker{Deps: d}).Work(context.Background(), &river.Job[GenerateArgs]{JobRow: &rivertype.JobRow{Attempt: 1}, Args: args})
}

func stored(t *testing.T, pool *pgxpool.Pool, userID int64, kind string) []db.CoachReport {
	t.Helper()
	rows, err := db.New(pool).ListCoachReports(t.Context(), db.ListCoachReportsParams{UserID: userID, Kind: kind, MaxRows: 10})
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestGenerateStoresTheOutputWithItsPromptVersion(t *testing.T) {
	t.Parallel()
	pool := pg.NewDB(t)
	alice, _ := testutil.TwoUsers(t, pool)
	var logs bytes.Buffer
	client := &fakeClient{reply: "  Easy day [day:2026-11-09].\n"}
	d := newDeps(pool, &logs, client)

	for _, tc := range []struct{ kind, version, task string }{
		{DailyInsight, "daily-insight.v1", "Write today's daily insight"},
		{WeeklyReport, "weekly-report.v1", "Write the weekly report"},
	} {
		if err := work(d, GenerateArgs{UserID: alice.ID, Output: tc.kind, Day: day("2026-11-09")}); err != nil {
			t.Fatal(err)
		}
		got := stored(t, pool, alice.ID, tc.kind)
		if len(got) != 1 || got[0].PromptVersion != tc.version || got[0].Content != "Easy day [day:2026-11-09]." || !got[0].Day.Equal(day("2026-11-09")) {
			t.Fatalf("%s stored %+v", tc.kind, got)
		}
		if !strings.Contains(client.got.System, "Sore knee") || !strings.Contains(client.got.System, "Today is 2026-11-09.") ||
			len(client.got.Messages) != 1 || !strings.HasPrefix(client.got.Messages[0].Content, tc.task) {
			t.Errorf("%s asked %+v", tc.kind, client.got)
		}
	}
	// Running a job twice keeps the first output.
	client.reply = "Another"
	if err := work(d, GenerateArgs{UserID: alice.ID, Output: DailyInsight, Day: day("2026-11-09")}); err != nil {
		t.Fatal(err)
	}
	if got := stored(t, pool, alice.ID, DailyInsight); len(got) != 1 || got[0].Content != "Easy day [day:2026-11-09]." {
		t.Errorf("after a rerun: %+v", got)
	}
}

func TestGenerateSkipsWithoutAProvider(t *testing.T) {
	t.Parallel()
	pool := pg.NewDB(t)
	alice, _ := testutil.TwoUsers(t, pool)
	var logs bytes.Buffer
	if err := work(newDeps(pool, &logs, nil), GenerateArgs{UserID: alice.ID, Output: DailyInsight, Day: day("2026-11-09")}); err != nil {
		t.Fatal(err)
	}
	if got := stored(t, pool, alice.ID, DailyInsight); len(got) != 0 {
		t.Errorf("stored %+v", got)
	}
}

func TestGenerateFailsVisibly(t *testing.T) {
	t.Parallel()
	pool := pg.NewDB(t)
	alice, _ := testutil.TwoUsers(t, pool)
	for name, client := range map[string]*fakeClient{
		"provider error": {err: &llm.APIError{Status: 500, Message: "down"}},
		"empty reply":    {reply: " \n"},
	} {
		var logs bytes.Buffer
		err := work(newDeps(pool, &logs, client), GenerateArgs{UserID: alice.ID, Output: DailyInsight, Day: day("2026-11-09")})
		if err == nil {
			t.Errorf("%s: no error", name)
		}
		if got := stored(t, pool, alice.ID, DailyInsight); len(got) != 0 {
			t.Errorf("%s: stored %+v", name, got)
		}
		if !strings.Contains(logs.String(), `"user_id"`) || strings.Contains(logs.String(), "Sore knee") {
			t.Errorf("%s: logs %s", name, logs.String())
		}
	}
	// Context failures pass through.
	d := newDeps(pool, &bytes.Buffer{}, &fakeClient{reply: "x"})
	d.Context = func(context.Context, int64, time.Time) (llm.AthleteContext, error) {
		return llm.AthleteContext{}, errors.New("db down")
	}
	if err := work(d, GenerateArgs{UserID: alice.ID, Output: DailyInsight, Day: day("2026-11-09")}); err == nil {
		t.Error("context failure: no error")
	}
	d.Client = func(context.Context, int64) (llm.Client, error) { return nil, errors.New("key unreadable") }
	if err := work(d, GenerateArgs{UserID: alice.ID, Output: DailyInsight, Day: day("2026-11-09")}); err == nil {
		t.Error("client failure: no error")
	}
}

func TestQueueDue(t *testing.T) {
	t.Parallel()
	pool := pg.NewDB(t)
	q := db.New(pool)
	ctx := t.Context()
	alice, bob := testutil.TwoUsers(t, pool)
	carol, err := q.CreateUser(ctx, db.CreateUserParams{Username: "carol", PasswordHash: "x", Timezone: "UTC"})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{alice.ID, carol.ID} { // bob has no provider
		if _, err := q.PutLLMProvider(ctx, db.PutLLMProviderParams{UserID: id, Provider: "ollama", BaseUrl: "http://ollama.example.test", Model: "m"}); err != nil {
			t.Fatal(err)
		}
	}
	// Carol's Garmin has not synced this morning yet.
	if _, err := pool.Exec(ctx, "INSERT INTO garmin_connections (user_id, key_ciphertext, key_expires_on) VALUES ($1, 'k', '2027-01-01')", carol.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "UPDATE users SET timezone = 'UTC' WHERE id = $1", alice.ID); err != nil {
		t.Fatal(err)
	}

	d := newDeps(pool, &bytes.Buffer{}, nil)
	d.Now = func() time.Time { return time.Date(2026, 11, 9, 7, 0, 0, 0, time.UTC) } // Monday
	var queued []GenerateArgs
	insert := func(a GenerateArgs) error { queued = append(queued, a); return nil }
	if err := d.queueDue(ctx, insert); err != nil {
		t.Fatal(err)
	}
	if len(queued) != 2 {
		t.Fatalf("queued %+v", queued)
	}
	kinds := map[string]bool{}
	for _, a := range queued {
		if a.UserID != alice.ID || !a.Day.Equal(day("2026-11-09")) {
			t.Errorf("queued %+v", a)
		}
		kinds[a.Output] = true
	}
	if !kinds[DailyInsight] || !kinds[WeeklyReport] {
		t.Errorf("kinds %v", kinds)
	}
	_ = bob

	// Once written, nothing is due until tomorrow.
	for _, kind := range []string{DailyInsight, WeeklyReport} {
		if _, err := q.CreateCoachReport(ctx, db.CreateCoachReportParams{UserID: alice.ID, Kind: kind, Day: day("2026-11-09"), PromptVersion: "v", Content: "c"}); err != nil {
			t.Fatal(err)
		}
	}
	queued = nil
	if err := d.queueDue(ctx, insert); err != nil || len(queued) != 0 {
		t.Errorf("after writing: %+v, %v", queued, err)
	}
	// Carol's turn comes at ten.
	d.Now = func() time.Time { return time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC) }
	if err := d.queueDue(ctx, insert); err != nil || len(queued) != 2 || queued[0].UserID != carol.ID {
		t.Errorf("at ten: %+v, %v", queued, err)
	}
	// Insert failures stop the scan.
	if err := d.queueDue(ctx, func(GenerateArgs) error { return errors.New("full") }); err == nil {
		t.Error("insert failure: no error")
	}
}

func TestScheduleQueuesEachOutputOnce(t *testing.T) {
	t.Parallel()
	pool := pg.NewDB(t)
	ctx := t.Context()
	alice, _ := testutil.TwoUsers(t, pool)
	if _, err := db.New(pool).PutLLMProvider(ctx, db.PutLLMProviderParams{UserID: alice.ID, Provider: "ollama", BaseUrl: "http://ollama.example.test", Model: "m"}); err != nil {
		t.Fatal(err)
	}
	d := newDeps(pool, &bytes.Buffer{}, nil)
	d.Now = func() time.Time { return time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC) } // Monday noon
	workers := river.NewWorkers()
	d.Register(workers)
	client, err := river.NewClient(riverpgxv5.New(pool), &river.Config{Workers: workers})
	if err != nil {
		t.Fatal(err)
	}
	count := func() (n int) {
		t.Helper()
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM river_job WHERE kind = 'coach_report'").Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	for range 2 { // the second scan finds both jobs already queued
		if err := d.queueDue(ctx, func(a GenerateArgs) error { _, err := client.Insert(ctx, a, nil); return err }); err != nil {
			t.Fatal(err)
		}
	}
	if n := count(); n != 2 {
		t.Errorf("%d jobs queued, want an insight and a report", n)
	}

	// The worker inserts through the client River gives it.
	w := rivertest.NewWorker(t, riverpgxv5.New(pool), &river.Config{Workers: workers}, &ScheduleWorker{Deps: d})
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after the test is best effort
	if res, err := w.Work(ctx, t, tx, ScheduleArgs{}, nil); err != nil || res.EventKind != river.EventKindJobCompleted {
		t.Fatalf("Work: %+v, %v", res, err)
	}
}

func TestPeriodicAndRegister(t *testing.T) {
	t.Parallel()
	if SchedulePeriodic() == nil {
		t.Fatal("no periodic job")
	}
	(&Deps{}).Register(river.NewWorkers()) // panics on a duplicate kind
	if (GenerateArgs{}).InsertOpts().MaxAttempts != maxAttempts || (&GenerateWorker{}).Timeout(nil) != generateTimeout {
		t.Error("job options")
	}
	if (&Deps{}).now().IsZero() {
		t.Error("clock")
	}
}
