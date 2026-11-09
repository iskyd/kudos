package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// fakeProvider speaks the OpenAI chat completions stream and keeps the last request.
type fakeProvider struct {
	*httptest.Server
	mu   sync.Mutex
	last struct {
		Messages []struct{ Role, Content string }
	}
}

func newFakeProvider(t *testing.T, handle func(w http.ResponseWriter)) *fakeProvider {
	t.Helper()
	f := &fakeProvider{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		err := json.NewDecoder(r.Body).Decode(&f.last)
		f.mu.Unlock()
		if err != nil {
			t.Error(err)
		}
		handle(w)
	}))
	t.Cleanup(f.Close)
	return f
}

// replies streams pieces as OpenAI deltas.
func replies(pieces ...string) func(w http.ResponseWriter) {
	return func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, p := range pieces {
			b, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]string{"content": p}}}})
			fmt.Fprintf(w, "data: %s\n\n", b)
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}
}

func chatEvents(t *testing.T, rec *httptest.ResponseRecorder) []ChatEvent {
	t.Helper()
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "text/event-stream" {
		t.Fatalf("status %d, %q: %s", rec.Code, rec.Header().Get("Content-Type"), rec.Body)
	}
	var out []ChatEvent
	for block := range strings.SplitSeq(strings.TrimSpace(rec.Body.String()), "\n\n") {
		var e ChatEvent
		if err := json.Unmarshal([]byte(strings.TrimPrefix(block, "data: ")), &e); err != nil {
			t.Fatalf("event %q: %v", block, err)
		}
		out = append(out, e)
	}
	return out
}

func chatUser(t *testing.T, pool *pgxpool.Pool, name string) *apiClient {
	t.Helper()
	c, _ := newAPIOn(t, pool, true, nil)
	c.session(c.do(http.MethodPost, "/api/auth/register", register(name), nil), http.StatusCreated)
	return c
}

func ask(c *apiClient, turns ...string) *httptest.ResponseRecorder {
	req := ChatRequest{}
	for i, t := range turns {
		role := User
		if i%2 == 1 {
			role = Assistant
		}
		req.Messages = append(req.Messages, ChatMessage{Role: role, Content: t})
	}
	return c.do(http.MethodPost, "/api/chat", req, nil)
}

func TestChatStreamsWithTheUsersContext(t *testing.T) {
	t.Parallel()
	pool := pg.NewDB(t)
	alice, bob := chatUser(t, pool, "alice"), chatUser(t, pool, "bob")
	yesterday := time.Now().UTC().AddDate(0, 0, -1)
	actID := run(t, alice, yesterday.Format("2006-01-02T07:00"), 10000, 3000)
	decodeJSON[HealthLogEntry](t, alice.do(http.MethodPost, "/api/health-log",
		HealthLogInput{Kind: Injury, Description: new("Sore left knee"), StartDate: Day{Time: yesterday.AddDate(-1, 0, 0)}}, nil), http.StatusCreated)
	decodeJSON[ZoneSet](t, alice.do(http.MethodPost, "/api/zones",
		ZoneSetInput{Sport: ZoneSportRunning, EffectiveFrom: on(yesterday.AddDate(0, -1, 0)), LthrBpm: new(171)}, nil), http.StatusCreated)
	decodeJSON[Goal](t, alice.do(http.MethodPost, "/api/goals",
		GoalInput{Period: GoalInputPeriodYear, Metric: GoalInputMetricDistance, Start: on(yesterday), Target: 1e6}, nil), http.StatusCreated)
	putDay(t, alice, yesterday.Format(time.DateOnly), WellnessDayInput{Soreness: new(4), Note: new("Heavy legs")})
	decodeJSON[Race](t, alice.do(http.MethodPost, "/api/races", RaceInput{
		Name: "Autumn Half", Day: on(yesterday.AddDate(0, 0, 30)), Sport: ActivitySportRunning, DistanceM: 21097.5, Priority: RaceInputPriorityA,
	}, nil), http.StatusCreated)
	decodeJSON[Race](t, alice.do(http.MethodPost, "/api/races", RaceInput{
		Name: "Last Year's 10K", Day: on(yesterday.AddDate(-1, 0, 0)), Sport: ActivitySportRunning, DistanceM: 10000, Priority: RaceInputPriorityC,
	}, nil), http.StatusCreated)
	putDay(t, alice, yesterday.AddDate(0, 0, -1).Format(time.DateOnly), WellnessDayInput{WeightKg: new(70.0)})
	decodeJSON[HealthLogEntry](t, bob.do(http.MethodPost, "/api/health-log",
		HealthLogInput{Kind: Illness, Description: new("Bob's flu"), StartDate: Day{Time: yesterday}}, nil), http.StatusCreated)

	f := newFakeProvider(t, replies("Easy ", "run today [act:1]."))
	setLLM(t, alice, LLMProviderInput{Provider: Ollama, BaseUrl: &f.URL, Model: "m"})
	events := chatEvents(t, ask(alice, "How was my week?", "Good.", "And today?"))

	var text strings.Builder
	for _, e := range events[:len(events)-1] {
		if e.Type != ChatEventTypeText || e.Text == nil {
			t.Fatalf("event %+v", e)
		}
		text.WriteString(*e.Text)
	}
	if text.String() != "Easy run today [act:1]." || events[len(events)-1].Type != ChatEventTypeDone {
		t.Errorf("events %+v", events)
	}
	f.mu.Lock()
	msgs := f.last.Messages
	f.mu.Unlock()
	if len(msgs) != 4 || msgs[0].Role != "system" || msgs[3].Content != "And today?" || msgs[2].Role != "assistant" {
		t.Fatalf("provider got %+v", msgs)
	}
	for _, want := range []string{"<athlete_context>", "Sore left knee", fmt.Sprintf("[act:%d]", actID), "Cite every item",
		"LTHR 171", "Heavy legs", "Autumn Half", "priority A, in 29 days", "## Goals", "## Training load, last 6 weeks", "- Sports: running"} {
		if !strings.Contains(msgs[0].Content, want) {
			t.Errorf("system prompt lacks %q", want)
		}
	}
	if strings.Contains(msgs[0].Content, "Last Year's 10K") {
		t.Error("the context lists a past race as upcoming")
	}
	if strings.Contains(msgs[0].Content, "Bob's flu") {
		t.Error("alice's context has bob's health log")
	}
	// Bob has no provider of his own.
	wantProblem(t, ask(bob, "Hi"), KindLLMNotConfigured)
}

func TestChatStoresNothing(t *testing.T) {
	t.Parallel()
	pool := pg.NewDB(t)
	c := chatUser(t, pool, "alice")
	const question, answer = "zebra-question-7f3a", "zebra-answer-91c2"
	f := newFakeProvider(t, replies(answer))
	setLLM(t, c, LLMProviderInput{Provider: Ollama, BaseUrl: &f.URL, Model: "m"})
	chatEvents(t, ask(c, question, "earlier zebra-reply-55d0", question))

	rows, err := pool.Query(t.Context(), `SELECT format('%I.%I', table_schema, table_name) FROM information_schema.tables
		WHERE table_schema NOT IN ('pg_catalog', 'information_schema') AND table_schema NOT LIKE '\_timescaledb%' AND table_type = 'BASE TABLE'`)
	if err != nil {
		t.Fatal(err)
	}
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, name)
	}
	if rows.Err() != nil || len(tables) < 10 {
		t.Fatalf("%d tables, %v", len(tables), rows.Err())
	}
	for _, table := range tables {
		var n int
		// Table names come from the catalog, quoted by format.
		if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM "+table+" t WHERE t::text LIKE '%zebra-%'").Scan(&n); err != nil {
			t.Fatalf("%s: %v", table, err)
		}
		if n > 0 {
			t.Errorf("%s holds %d rows of chat content", table, n)
		}
	}
}

func TestChatRejectsBadConversations(t *testing.T) {
	t.Parallel()
	pool := pg.NewDB(t)
	c := chatUser(t, pool, "alice")
	many := make([]string, maxChatMessages+1)
	for i := range many {
		many[i] = "x"
	}
	for name, turns := range map[string][]string{
		"none":          {},
		"ends with ai":  {"hi", "hello"},
		"too many":      many,
		"blank":         {"  "},
		"too long":      {strings.Repeat("é", maxChatMessage+1)},
		"blank earlier": {"hi", " ", "again"},
	} {
		t.Run(name, func(t *testing.T) { wantProblem(t, ask(c, turns...), KindInvalidRequest) })
	}
	// Roles must alternate, starting with the user.
	wantProblem(t, c.do(http.MethodPost, "/api/chat", ChatRequest{Messages: []ChatMessage{
		{Role: Assistant, Content: "a"}, {Role: User, Content: "b"}, {Role: User, Content: "c"},
	}}, nil), KindInvalidRequest)
	// Checked before the provider: a valid conversation without one is a conflict.
	wantProblem(t, ask(c, "hi"), KindLLMNotConfigured)
}

func TestChatReportsProviderFailures(t *testing.T) {
	t.Parallel()
	pool := pg.NewDB(t)
	c := chatUser(t, pool, "alice")

	rejected := newFakeProvider(t, func(w http.ResponseWriter) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error": {"message": "invalid model"}}`)
	})
	setLLM(t, c, LLMProviderInput{Provider: Ollama, BaseUrl: &rejected.URL, Model: "m"})
	events := chatEvents(t, ask(c, "hi"))
	if len(events) != 1 || events[0].Type != ChatEventTypeError || !strings.Contains(*events[0].Detail, "invalid model") {
		t.Errorf("rejected: %+v", events)
	}

	gone := newFakeProvider(t, replies())
	gone.Close()
	setLLM(t, c, LLMProviderInput{Provider: Ollama, BaseUrl: &gone.URL, Model: "m"})
	events = chatEvents(t, ask(c, "hi"))
	if len(events) != 1 || events[0].Type != ChatEventTypeError || *events[0].Detail != "The AI coach provider could not be reached." {
		t.Errorf("unreachable: %+v", events)
	}
	if strings.Contains(*events[0].Detail, gone.URL) {
		t.Error("the error shows the provider's address")
	}
}
