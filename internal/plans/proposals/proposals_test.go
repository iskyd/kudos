package proposals

import (
	"bytes"
	"context"
	"errors"
	"log"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivertest"
	"github.com/riverqueue/river/rivertype"

	"github.com/iskyd/kudos/internal/jobs"
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

func day(s string) time.Time {
	d, _ := time.Parse(time.DateOnly, s) // the tests' literals are valid
	return d
}

// today is the day the tests plan from: proposals cover 13 to 26 November.
var today = day("2026-11-12")

// scripted answers Complete with its replies in order, or err, and keeps the requests.
type scripted struct {
	replies []string
	err     error
	got     []llm.Request
}

func (s *scripted) Complete(_ context.Context, r llm.Request) (string, error) {
	s.got = append(s.got, r)
	if s.err != nil {
		return "", s.err
	}
	return s.replies[len(s.got)-1], nil
}

func (*scripted) Stream(context.Context, llm.Request, func(string) error) error {
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

func newJobs(t *testing.T, pool *pgxpool.Pool) *jobs.Client {
	t.Helper()
	workers := river.NewWorkers()
	(&Deps{}).Register(workers)
	c, err := river.NewClient(riverpgxv5.New(pool), &river.Config{Workers: workers})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func work(d *Deps, p db.PlanProposal, attempt int) error {
	return (&GenerateWorker{Deps: d}).Work(context.Background(), &river.Job[GenerateArgs]{
		JobRow: &rivertype.JobRow{Attempt: attempt, MaxAttempts: maxAttempts}, Args: GenerateArgs{UserID: p.UserID, ProposalID: p.ID},
	})
}

func get(t *testing.T, q *db.Queries, p db.PlanProposal) (db.PlanProposal, []db.PlanProposalItem) {
	t.Helper()
	got, err := q.GetPlanProposal(t.Context(), db.GetPlanProposalParams{UserID: p.UserID, ID: p.ID})
	if err != nil {
		t.Fatal(err)
	}
	items, err := q.ListPlanProposalItems(t.Context(), db.ListPlanProposalItemsParams{UserID: p.UserID, ProposalID: p.ID, MaxRows: 100})
	if err != nil {
		t.Fatal(err)
	}
	return got, items
}

const easySteps = `[{"kind": "recovery", "durationType": "time", "duration": 2700}]`

// planned stores a workout in a plan of its own.
func planned(t *testing.T, q *db.Queries, userID int64, on, sport, title string) db.PlannedWorkout {
	t.Helper()
	plan, err := q.CreatePlan(t.Context(), userID)
	if err != nil {
		t.Fatal(err)
	}
	w, err := q.CreatePlannedWorkout(t.Context(), db.CreatePlannedWorkoutParams{
		UserID: userID, PlanID: plan.ID, Day: day(on), Sport: sport, Title: title, Steps: []byte(easySteps),
	})
	if err != nil {
		t.Fatal(err)
	}
	return w
}

// workouts lists the user's planned workouts as "day sport title".
func workouts(t *testing.T, q *db.Queries, userID int64) []string {
	t.Helper()
	rows, err := q.ListPlannedWorkoutsBetween(t.Context(), db.ListPlannedWorkoutsBetweenParams{
		UserID: userID, FromDay: day("2026-01-01"), ToDay: day("2027-01-01"), MaxRows: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.Day.Format(time.DateOnly) + " " + r.Sport + " " + r.Title
	}
	return out
}

func draftJSON(keep string, ws ...string) string {
	return `{"keep": [` + keep + `], "workouts": [` + strings.Join(ws, ",") + `]}`
}

func workoutJSON(on, sport, title string) string {
	return `{"day": "` + on + `", "sport": "` + sport + `", "title": "` + title + `", "steps": ` + easySteps + `}`
}

func id(n int64) string { return strconv.FormatInt(n, 10) }

func TestStartQueuesOneProposalAtATime(t *testing.T) {
	t.Parallel()
	pool := pg.NewDB(t)
	alice, bob := testutil.TwoUsers(t, pool)
	s := NewService(pool, newJobs(t, pool))
	ctx := t.Context()

	p, err := s.Start(ctx, alice.ID, today)
	if err != nil {
		t.Fatal(err)
	}
	if p.Status != "generating" || !p.FromDay.Equal(day("2026-11-13")) || !p.ToDay.Equal(day("2026-11-26")) {
		t.Errorf("started %+v", p)
	}
	if _, err := s.Start(ctx, alice.ID, today); !errors.Is(err, ErrGenerating) {
		t.Errorf("second start: %v", err)
	}
	if _, err := s.Start(ctx, bob.ID, today); err != nil {
		t.Errorf("bob's start: %v", err)
	}
	var n int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM river_job WHERE kind = 'plan_proposal'").Scan(&n); err != nil || n != 2 {
		t.Errorf("%d jobs, %v", n, err)
	}
	// A proposal generating for longer than any job runs has lost its job.
	if _, err := pool.Exec(ctx, "UPDATE plan_proposals SET created_at = now() - interval '2 hours' WHERE id = $1", p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Start(ctx, alice.ID, today); err != nil {
		t.Fatalf("start after a lost job: %v", err)
	}
	if old, _ := get(t, db.New(pool), p); old.Status != "failed" || *old.Failure != "timeout" {
		t.Errorf("the lost one is %+v", old)
	}
}

func TestGenerateProposesWithoutChangingThePlan(t *testing.T) {
	t.Parallel()
	pool := pg.NewDB(t)
	q := db.New(pool)
	alice, bob := testutil.TwoUsers(t, pool)
	s := NewService(pool, newJobs(t, pool))
	ctx := t.Context()
	kept := planned(t, q, alice.ID, "2026-11-13", "running", "Easy")
	changed := planned(t, q, alice.ID, "2026-11-15", "running", "Easy")
	removed := planned(t, q, alice.ID, "2026-11-16", "cycling", "Ride")
	planned(t, q, alice.ID, "2026-11-12", "running", "Today") // before the plan's days
	planned(t, q, bob.ID, "2026-11-14", "running", "Bob's run")

	// A plan the same as the current one proposes no change.
	old, err := s.Start(ctx, alice.ID, today)
	if err != nil {
		t.Fatal(err)
	}
	same := &scripted{replies: []string{draftJSON(id(kept.ID) + "," + id(changed.ID) + "," + id(removed.ID))}}
	if err := work(newDeps(pool, &bytes.Buffer{}, same), old, 1); err != nil {
		t.Fatal(err)
	}
	if prev, items := get(t, q, old); prev.Status != "closed" || len(items) != 0 {
		t.Errorf("a proposal with no change: %+v %+v", prev, items)
	}
	before := workouts(t, q, alice.ID)

	p, err := s.Start(ctx, alice.ID, today)
	if err != nil {
		t.Fatal(err)
	}
	client := &scripted{replies: []string{"```json\n" + draftJSON(id(kept.ID),
		workoutJSON("2026-11-15", "running", "Tempo"),
		workoutJSON("2026-11-17", "running", "Long run"),
	) + "\n```"}}
	if err := work(newDeps(pool, &bytes.Buffer{}, client), p, 1); err != nil {
		t.Fatal(err)
	}
	got, items := get(t, q, p)
	if got.Status != "open" || *got.PromptVersion != "plan.v2" || len(items) != 3 {
		t.Fatalf("proposal %+v, items %+v", got, items)
	}
	for i, want := range []struct {
		action  string
		workout int64
	}{{"change", changed.ID}, {"remove", removed.ID}, {"add", 0}} {
		it := items[i]
		if it.Action != want.action || it.Status != "pending" || (want.workout != 0) != (it.WorkoutID != nil) || it.WorkoutID != nil && *it.WorkoutID != want.workout {
			t.Errorf("item %d = %+v, want %s of %d", i, it, want.action, want.workout)
		}
	}
	if !strings.Contains(string(items[0].Before), `"title": "Easy"`) || !strings.Contains(string(items[0].After), `"title": "Tempo"`) || items[1].After != nil {
		t.Errorf("snapshots %s / %s / %s", items[0].Before, items[0].After, items[1].After)
	}
	// Nothing changes without approval (invariant 34).
	if after := workouts(t, q, alice.ID); strings.Join(after, "|") != strings.Join(before, "|") {
		t.Errorf("the plan changed: %v, was %v", after, before)
	}
	// The model sees the plan's days, the current workouts and the schema; it is asked once.
	req := client.got[0]
	if len(client.got) != 1 || !strings.Contains(req.System, "Sore knee") || !strings.Contains(req.Messages[0].Content, "from 2026-11-13 to 2026-11-26") ||
		!strings.Contains(req.Messages[0].Content, `"keep"`) {
		t.Errorf("asked %+v", client.got)
	}

	// A newer proposal supersedes the open one.
	newer, err := s.Start(ctx, alice.ID, today)
	if err != nil {
		t.Fatal(err)
	}
	if err := work(newDeps(pool, &bytes.Buffer{}, &scripted{replies: []string{draftJSON("", workoutJSON("2026-11-20", "running", "Strides"))}}), newer, 1); err != nil {
		t.Fatal(err)
	}
	if prev, _ := get(t, q, p); prev.Status != "superseded" {
		t.Errorf("the older proposal is %q", prev.Status)
	}
	// Bob's plan is untouched throughout.
	if got := workouts(t, q, bob.ID); len(got) != 1 {
		t.Errorf("bob's plan %v", got)
	}
}

func TestInvalidOutputNeverReachesAProposal(t *testing.T) {
	t.Parallel()
	pool := pg.NewDB(t)
	q := db.New(pool)
	alice, _ := testutil.TwoUsers(t, pool)
	s := NewService(pool, newJobs(t, pool))
	ctx := t.Context()
	w := planned(t, q, alice.ID, "2026-11-14", "running", "Easy")

	for name, replies := range map[string][]string{
		"not JSON":           {"Here is your plan: run a lot.", "Sorry."},
		"schema":             {`{"workouts": []}`, `{"keep": [], "workouts": [{"day": "2026-11-14"}]}`},
		"outside the days":   {draftJSON("", workoutJSON("2026-11-12", "running", "Today")), draftJSON("", workoutJSON("2026-11-27", "running", "Later"))},
		"unknown keep":       {draftJSON("999999"), draftJSON("999998")},
		"a step's own rules": {draftJSON("", strings.Replace(workoutJSON("2026-11-14", "running", "x"), `"duration": 2700`, `"duration": 2700, "count": 3`, 1)), "no"},
	} {
		p, err := s.Start(ctx, alice.ID, today)
		if err != nil {
			t.Fatal(err)
		}
		var logs bytes.Buffer
		client := &scripted{replies: replies}
		if err := work(newDeps(pool, &logs, client), p, 1); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		got, items := get(t, q, p)
		if got.Status != "failed" || *got.Failure != FailedInvalidOutput || len(items) != 0 || len(client.got) != 2 {
			t.Errorf("%s: proposal %+v, items %+v, %d calls", name, got, items, len(client.got))
		}
		// The retry shows the model its errors; the logs hold no content.
		if retry := client.got[1].Messages; len(retry) != 3 || !strings.Contains(retry[2].Content, "invalid") {
			t.Errorf("%s: retry %+v", name, retry)
		}
		if strings.Contains(logs.String(), "Sore knee") || strings.Contains(logs.String(), replies[0]) || !strings.Contains(logs.String(), `"proposal_id"`) {
			t.Errorf("%s: logs %s", name, logs.String())
		}
	}
	if got := workouts(t, q, alice.ID); len(got) != 1 || !strings.HasSuffix(got[0], "Easy") {
		t.Errorf("the plan changed: %v", got)
	}

	// Corrected on the retry, it is proposed.
	p, err := s.Start(ctx, alice.ID, today)
	if err != nil {
		t.Fatal(err)
	}
	client := &scripted{replies: []string{draftJSON("", workoutJSON("2026-11-27", "running", "Later")), draftJSON(id(w.ID))}}
	if err := work(newDeps(pool, &bytes.Buffer{}, client), p, 1); err != nil {
		t.Fatal(err)
	}
	if got, items := get(t, q, p); got.Status != "closed" || len(items) != 0 || !strings.Contains(client.got[1].Messages[2].Content, "workouts/0: day 2026-11-27") {
		t.Errorf("after a corrected retry: %+v %+v %+v", got, items, client.got[1].Messages[2])
	}
}

func TestGenerateFailures(t *testing.T) {
	t.Parallel()
	pool := pg.NewDB(t)
	q := db.New(pool)
	alice, _ := testutil.TwoUsers(t, pool)
	s := NewService(pool, newJobs(t, pool))
	ctx := t.Context()
	start := func() db.PlanProposal {
		t.Helper()
		p, err := s.Start(ctx, alice.ID, today)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}

	// Provider errors retry, then fail the proposal on the last attempt.
	p := start()
	down := &scripted{err: &llm.APIError{Status: 503, Message: "down"}}
	if err := work(newDeps(pool, &bytes.Buffer{}, down), p, 1); err == nil {
		t.Error("attempt 1: no error")
	}
	if got, _ := get(t, q, p); got.Status != "generating" {
		t.Errorf("after attempt 1: %q", got.Status)
	}
	if err := work(newDeps(pool, &bytes.Buffer{}, down), p, maxAttempts); err != nil {
		t.Errorf("last attempt: %v", err)
	}
	if got, _ := get(t, q, p); got.Status != "failed" || *got.Failure != FailedProvider {
		t.Errorf("after the last attempt: %+v", got)
	}
	// A job for a proposal no longer generating does nothing.
	unused := &scripted{}
	if err := work(newDeps(pool, &bytes.Buffer{}, unused), p, 1); err != nil || len(unused.got) != 0 {
		t.Errorf("rerun: %v, %d calls", err, len(unused.got))
	}
	if err := work(newDeps(pool, &bytes.Buffer{}, unused), db.PlanProposal{UserID: alice.ID, ID: -1}, 1); err != nil {
		t.Errorf("missing proposal: %v", err)
	}

	// A provider removed since fails it at once.
	p = start()
	if err := work(newDeps(pool, &bytes.Buffer{}, nil), p, 1); err != nil {
		t.Fatal(err)
	}
	if got, _ := get(t, q, p); got.Status != "failed" || *got.Failure != FailedNotConfigured {
		t.Errorf("without a provider: %+v", got)
	}

	// Context and client failures pass through for River to retry.
	p = start()
	d := newDeps(pool, &bytes.Buffer{}, unused)
	d.Context = func(context.Context, int64, time.Time) (llm.AthleteContext, error) {
		return llm.AthleteContext{}, errors.New("db down")
	}
	if err := work(d, p, 1); err == nil {
		t.Error("context failure: no error")
	}
	d.Client = func(context.Context, int64) (llm.Client, error) { return nil, errors.New("key unreadable") }
	if err := work(d, p, 1); err == nil {
		t.Error("client failure: no error")
	}
}

// proposal opens one for the user: change 15 Nov, remove 16 Nov, add 17 and 18 Nov.
func proposal(t *testing.T, pool *pgxpool.Pool, s *Service, userID int64) (db.PlanProposal, []db.PlanProposalItem) {
	t.Helper()
	q := db.New(pool)
	planned(t, q, userID, "2026-11-15", "running", "Easy")
	planned(t, q, userID, "2026-11-16", "cycling", "Ride")
	p, err := s.Start(t.Context(), userID, today)
	if err != nil {
		t.Fatal(err)
	}
	client := &scripted{replies: []string{draftJSON("",
		workoutJSON("2026-11-15", "running", "Tempo"), workoutJSON("2026-11-17", "running", "Long"), workoutJSON("2026-11-18", "running", "Strides"))}}
	if err := work(newDeps(pool, &bytes.Buffer{}, client), p, 1); err != nil {
		t.Fatal(err)
	}
	p, items := get(t, q, p)
	if p.Status != "open" || len(items) != 4 {
		t.Fatalf("proposal %+v, items %+v", p, items)
	}
	return p, items
}

func TestDecideAppliesExactlyTheApprovedItems(t *testing.T) {
	t.Parallel()
	pool := pg.NewDB(t)
	q := db.New(pool)
	alice, _ := testutil.TwoUsers(t, pool)
	s := NewService(pool, newJobs(t, pool))
	ctx := t.Context()
	p, items := proposal(t, pool, s, alice.ID)
	change, remove, addLong, addStrides := items[0].ID, items[1].ID, items[2].ID, items[3].ID

	// Approve the change and one addition, reject the removal; leave one pending.
	if err := s.Decide(ctx, alice.ID, p.ID, []int64{change, addLong}, []int64{remove}); err != nil {
		t.Fatal(err)
	}
	want := []string{"2026-11-15 running Tempo", "2026-11-16 cycling Ride", "2026-11-17 running Long"}
	if got := workouts(t, q, alice.ID); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("plan %v, want %v", got, want)
	}
	got, items := get(t, q, p)
	var statuses []string
	for _, it := range items {
		statuses = append(statuses, it.Status)
	}
	if got.Status != "open" || got.PlanID == nil || strings.Join(statuses, " ") != "approved rejected approved pending" {
		t.Errorf("proposal %+v, items %v", got, statuses)
	}

	// The last item closes it; additions join the same plan.
	if err := s.Decide(ctx, alice.ID, p.ID, []int64{addStrides}, nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := get(t, q, p); got.Status != "closed" {
		t.Errorf("after deciding all: %q", got.Status)
	}
	var plansUsed int
	if err := pool.QueryRow(ctx, "SELECT count(DISTINCT plan_id) FROM planned_workouts WHERE user_id = $1 AND title IN ('Long', 'Strides')", alice.ID).Scan(&plansUsed); err != nil || plansUsed != 1 {
		t.Errorf("additions in %d plans, %v", plansUsed, err)
	}
	if err := s.Decide(ctx, alice.ID, p.ID, nil, []int64{addStrides}); !errors.Is(err, ErrClosed) {
		t.Errorf("deciding a closed proposal: %v", err)
	}
}

func TestDecideRejectsBadDecisionsWhole(t *testing.T) {
	t.Parallel()
	pool := pg.NewDB(t)
	q := db.New(pool)
	alice, bob := testutil.TwoUsers(t, pool)
	s := NewService(pool, newJobs(t, pool))
	ctx := t.Context()
	p, items := proposal(t, pool, s, alice.ID)
	before := workouts(t, q, alice.ID)

	for name, tc := range map[string]struct{ approve, reject []int64 }{
		"named twice":           {[]int64{items[0].ID}, []int64{items[0].ID}},
		"unknown item":          {[]int64{items[0].ID, 999999}, nil},
		"unknown rejected item": {[]int64{items[2].ID}, []int64{999998}},
	} {
		if err := s.Decide(ctx, alice.ID, p.ID, tc.approve, tc.reject); !errors.Is(err, ErrBadItems) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// Bob can neither see nor decide alice's proposal.
	if err := s.Decide(ctx, bob.ID, p.ID, []int64{items[2].ID}, nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("bob decides: %v", err)
	}
	// Nothing was applied or decided.
	if got := workouts(t, q, alice.ID); strings.Join(got, "|") != strings.Join(before, "|") {
		t.Errorf("plan %v, was %v", got, before)
	}
	if _, items := get(t, q, p); items[0].Status != "pending" || items[2].Status != "pending" {
		t.Errorf("items %+v", items)
	}

	// An approved change whose workout is gone fails whole.
	if _, err := q.DeletePlannedWorkout(ctx, db.DeletePlannedWorkoutParams{UserID: alice.ID, ID: *items[0].WorkoutID}); err != nil {
		t.Fatal(err)
	}
	if err := s.Decide(ctx, alice.ID, p.ID, []int64{items[2].ID, items[0].ID}, nil); !errors.Is(err, ErrBadItems) {
		t.Errorf("approving a change of a deleted workout: %v", err)
	}
	if got := workouts(t, q, alice.ID); len(got) != 1 {
		t.Errorf("the addition was applied: %v", got)
	}
}

func TestWeeklyDue(t *testing.T) {
	t.Parallel()
	rome, err := time.LoadLocation("Europe/Rome")
	if err != nil {
		t.Fatal(err)
	}
	monday := func(h, m int) time.Time { return time.Date(2026, 11, 9, h, m, 0, 0, rome) }
	for _, tc := range []struct {
		name string
		c    db.ListPlanCandidatesRow
		now  time.Time
		want string
	}{
		{"Monday morning", db.ListPlanCandidatesRow{Timezone: "Europe/Rome"}, monday(6, 0), "2026-11-09"},
		{"before the morning", db.ListPlanCandidatesRow{Timezone: "Europe/Rome"}, monday(5, 59), ""},
		{"Tuesday", db.ListPlanCandidatesRow{Timezone: "Europe/Rome"}, monday(6, 0).AddDate(0, 0, 1), ""},
		{"proposed last week", db.ListPlanCandidatesRow{Timezone: "Europe/Rome", LastProposal: monday(0, 0).Add(-time.Minute)}, monday(7, 0), "2026-11-09"},
		{"proposed this Monday", db.ListPlanCandidatesRow{Timezone: "Europe/Rome", LastProposal: monday(0, 0)}, monday(7, 0), ""},
		{"waits for the sync", db.ListPlanCandidatesRow{Timezone: "Europe/Rome", GarminConnected: true, LastSyncAt: monday(5, 0)}, monday(9, 0), ""},
		{"unknown zone counts as UTC", db.ListPlanCandidatesRow{Timezone: "Mars/Olympus"}, time.Date(2026, 11, 9, 6, 0, 0, 0, time.UTC), "2026-11-09"},
	} {
		got := ""
		if d := WeeklyDue(tc.c, tc.now); d != nil {
			got = d.Format(time.DateOnly)
		}
		if got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestScheduleRegeneratesPlansWeekly(t *testing.T) {
	t.Parallel()
	pool := pg.NewDB(t)
	q := db.New(pool)
	ctx := t.Context()
	alice, bob := testutil.TwoUsers(t, pool)
	carol, err := q.CreateUser(ctx, db.CreateUserParams{Username: "carol", PasswordHash: "x", Timezone: "UTC"})
	if err != nil {
		t.Fatal(err)
	}
	// Alice and carol have a provider, alice and bob a plan: only alice is regenerated.
	for _, id := range []int64{alice.ID, carol.ID} {
		if _, err := q.PutLLMProvider(ctx, db.PutLLMProviderParams{UserID: id, Provider: "ollama", BaseUrl: "http://ollama.example.test", Model: "m"}); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []int64{alice.ID, bob.ID} {
		planned(t, q, id, "2026-11-05", "running", "Easy")
	}
	if _, err := pool.Exec(ctx, "UPDATE users SET timezone = 'UTC'"); err != nil {
		t.Fatal(err)
	}
	d := newDeps(pool, &bytes.Buffer{}, nil)
	d.Now = func() time.Time { return time.Date(2026, 11, 9, 7, 0, 0, 0, time.UTC) } // Monday
	workers := river.NewWorkers()
	d.Register(workers)
	w := rivertest.NewWorker(t, riverpgxv5.New(pool), &river.Config{Workers: workers}, &ScheduleWorker{Deps: d})
	count := func(userID int64) (n int) {
		t.Helper()
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM plan_proposals WHERE user_id = $1", userID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	for range 2 { // the second run finds this week's proposal
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if res, err := w.Work(ctx, t, tx, ScheduleArgs{}, nil); err != nil || res.EventKind != river.EventKindJobCompleted {
			t.Fatalf("Work: %+v, %v", res, err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if a, b, c := count(alice.ID), count(bob.ID), count(carol.ID); a != 1 || b != 0 || c != 0 {
		t.Errorf("proposals: alice %d, bob %d, carol %d", a, b, c)
	}
	p, err := q.GetLatestPlanProposal(ctx, alice.ID)
	if err != nil || !p.FromDay.Equal(day("2026-11-10")) || p.Status != "generating" {
		t.Errorf("alice's %+v, %v", p, err)
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
