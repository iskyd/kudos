package api

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/iskyd/kudos/internal/analytics"
	"github.com/iskyd/kudos/internal/importer/ingest"
	"github.com/iskyd/kudos/internal/llm"
	"github.com/iskyd/kudos/internal/secret"
	"github.com/iskyd/kudos/internal/store/db"
)

// Keep in step with ChatRequest in api/openapi.yaml.
const (
	maxChatMessages = 40
	maxChatMessage  = 8000
)

const (
	// chatContextTokens leaves room for the conversation in a small local model's window.
	chatContextTokens = 6000
	contextDays       = 14 // of activities and health trends
	contextCheckIns   = 7
	contextLoadWeeks  = 6
	// shortcut: past 500 health log entries or 50 goals the oldest are left out; page if anyone gets close.
	maxContextHealthLog = 500
	maxContextGoals     = 50
	maxContextActivity  = 100
)

// Chat streams the coach's reply to the conversation. Nothing is stored: the
// client sends the whole conversation each turn.
func (s *Server) Chat(ctx context.Context, req ChatRequestObject) (ChatResponseObject, error) {
	msgs := req.Body.Messages
	if err := validateChat(msgs); err != nil {
		return nil, err
	}
	sess := sessionFrom(ctx)
	client, err := s.llmClient(ctx, sess.userID)
	if err != nil {
		return nil, err
	}
	c, err := s.athleteContext(ctx, sess.userID, localToday(sess.timezone))
	if err != nil {
		return nil, err
	}
	p, err := llm.Render(llm.ModeChat, llm.PromptInput{Today: c.Today, Context: llm.BuildContext(c, chatContextTokens)})
	if err != nil {
		return nil, fmt.Errorf("rendering the chat prompt: %w", err)
	}
	r := llm.Request{System: p.System, Messages: make([]llm.Message, len(msgs))}
	for i, m := range msgs {
		r.Messages[i] = llm.Message{Role: string(m.Role), Content: m.Content}
	}

	pr, pw := io.Pipe()
	go func() {
		err := client.Stream(ctx, r, func(text string) error {
			return writeChatEvent(pw, ChatEvent{Type: ChatEventTypeText, Text: &text})
		})
		end := ChatEvent{Type: ChatEventTypeDone}
		if err != nil {
			end = ChatEvent{Type: ChatEventTypeError, Detail: new(chatErrorDetail(err))}
		}
		// A failed write means the client left; closing ends the stream either way.
		_ = writeChatEvent(pw, end)
		_ = pw.Close()
	}()
	// The generated response flushes after every chunk it reads, and closes the
	// body when the client leaves, which stops the stream.
	return Chat200TexteventStreamResponse{Body: pr}, nil
}

// validateChat accepts a conversation that alternates user and assistant
// turns, starting and ending with the user's.
func validateChat(msgs []ChatMessage) error {
	invalid := func(detail string) error { return &Error{Kind: KindInvalidRequest, Detail: detail} }
	if len(msgs) > maxChatMessages || len(msgs)%2 == 0 {
		return invalid(fmt.Sprintf("messages must be an odd number of turns, at most %d, starting and ending with the user's", maxChatMessages))
	}
	for i, m := range msgs {
		want := User
		if i%2 == 1 {
			want = Assistant
		}
		if m.Role != want {
			return invalid("messages must alternate user and assistant turns, starting with the user's")
		}
		if strings.TrimSpace(m.Content) == "" || utf8.RuneCountInString(m.Content) > maxChatMessage {
			return invalid(fmt.Sprintf("each message must be 1 to %d characters", maxChatMessage))
		}
	}
	return nil
}

func writeChatEvent(w io.Writer, e ChatEvent) error {
	b, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("encoding chat event: %w", err)
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
		return fmt.Errorf("writing chat event: %w", err)
	}
	return nil
}

// chatErrorDetail tells the user what went wrong without echoing network
// errors, which can carry the provider's address.
func chatErrorDetail(err error) string {
	var apiErr *llm.APIError
	if errors.As(err, &apiErr) || errors.Is(err, llm.ErrMalformed) {
		return err.Error()
	}
	return "The AI coach provider could not be reached."
}

// llmClient returns a client for the user's provider.
func (s *Server) llmClient(ctx context.Context, userID int64) (llm.Client, error) {
	p, err := s.q.GetLLMProvider(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, &Error{Kind: KindLLMNotConfigured, Detail: "set up a provider in Settings › AI coach"}
	}
	if err != nil {
		return nil, fmt.Errorf("reading the LLM provider: %w", err)
	}
	cfg := llm.Config{Provider: llm.Provider(p.Provider), BaseURL: p.BaseUrl, Model: p.Model}
	if p.KeyCiphertext != nil {
		key, err := s.cipher.Open(p.KeyCiphertext)
		if err != nil {
			return nil, fmt.Errorf("decrypting the LLM key: %w", err)
		}
		cfg.APIKey = secret.New(string(key))
	}
	// No client timeout: a reply streams for as long as the model writes.
	return llm.New(cfg, http.DefaultClient), nil
}

// athleteContext loads what the context builder summarizes, as of today.
func (s *Server) athleteContext(ctx context.Context, userID int64, today time.Time) (llm.AthleteContext, error) {
	c := llm.AthleteContext{Today: today}
	fail := func(what string, err error) (llm.AthleteContext, error) {
		return llm.AthleteContext{}, fmt.Errorf("loading %s for the coach: %w", what, err)
	}

	zones, err := s.q.ListZoneSetsAt(ctx, db.ListZoneSetsAtParams{UserID: userID, Day: today})
	if err != nil {
		return fail("zones", err)
	}
	for _, z := range zones {
		c.Thresholds = append(c.Thresholds, llm.Threshold{
			Sport: z.Sport, From: z.EffectiveFrom, LTHR: intPtr(z.LthrBpm), MaxHR: intPtr(z.MaxHrBpm), RestingHR: intPtr(z.RestingHrBpm),
			FTP: intPtr(z.FtpW), ThresholdSpeedMS: f64(z.ThresholdSpeedMS), CSSMS: f64(z.CssMS),
		})
	}

	log, err := s.q.ListHealthLog(ctx, db.ListHealthLogParams{UserID: userID, MaxRows: maxContextHealthLog})
	if err != nil {
		return fail("the health log", err)
	}
	for _, e := range log {
		c.HealthLog = append(c.HealthLog, llm.HealthEntry{Kind: e.Kind, Description: deref(e.Description), Start: e.StartDate, End: e.EndDate})
	}

	goals, err := s.q.ListGoals(ctx, db.ListGoalsParams{UserID: userID, MaxRows: maxContextGoals})
	if err != nil {
		return fail("goals", err)
	}
	for _, g := range goals {
		p, err := s.goalProgress(ctx, g, today)
		if err != nil {
			return llm.AthleteContext{}, err
		}
		c.Goals = append(c.Goals, llm.GoalProgress{
			Period: string(p.Period), Start: p.Start.Time, Sport: deref((*string)(p.Sport)), Metric: string(p.Metric),
			Target: p.Target, Value: p.Value, Projected: p.Projected, Status: string(p.Status),
		})
	}

	r, err := s.readiness(ctx, userID, today)
	if err != nil {
		return llm.AthleteContext{}, err
	}
	c.Readiness = &r

	wellness, err := s.q.ListWellnessDaysBetween(ctx, db.ListWellnessDaysBetweenParams{
		UserID: userID, FromDay: today.AddDate(0, 0, 1-contextCheckIns), ToDay: today, MaxRows: contextCheckIns,
	})
	if err != nil {
		return fail("check-ins", err)
	}
	for _, w := range wellness {
		if w.Soreness == nil && w.SleepQuality == nil && w.Stress == nil && w.Motivation == nil && w.Note == nil {
			continue // only body metrics that day
		}
		c.CheckIns = append(c.CheckIns, llm.CheckIn{
			Day: w.Day, Soreness: intPtr(w.Soreness), SleepQuality: intPtr(w.SleepQuality), Stress: intPtr(w.Stress),
			Motivation: intPtr(w.Motivation), Note: deref(w.Note),
		})
	}

	monday, _ := periodBounds("week", today)
	fitness, err := s.fitnessDays(ctx, userID, monday.AddDate(0, 0, -7*(contextLoadWeeks-1)), today)
	if err != nil {
		return llm.AthleteContext{}, err
	}
	for i := 0; i < len(fitness); i += 7 {
		end := fitness[min(i+6, len(fitness)-1)] // the week's last day, or today
		c.Load = append(c.Load, llm.LoadWeek{Start: fitness[i].Day, CTL: end.CTL, ATL: end.ATL, TSB: end.TSB})
	}

	start := today.AddDate(0, 0, 1-contextDays)
	rows, err := s.q.ListActivities(ctx, db.ListActivitiesParams{
		UserID: userID, Sort: "date", FromDay: &start, ToDay: &today,
		BeforeKey: math.MaxFloat64, BeforeID: math.MaxInt64, MaxRows: maxContextActivity,
	})
	if err != nil {
		return fail("activities", err)
	}
	for _, row := range rows {
		a, err := ingest.Effective(row.Activity)
		if err != nil {
			return llm.AthleteContext{}, err
		}
		sum := llm.ActivitySummary{
			ID: a.ID, Start: localStart(a.StartTime, a.UtcOffsetS), Sport: a.Sport,
			DistanceM: a.DistanceM, AvgHR: intPtr(a.AvgHrBpm), AvgPowerW: intPtr(a.AvgPowerW),
		}
		if t := cmp.Or(a.TimerTimeS, a.ElapsedTimeS); t != nil {
			sum.DurationS = *t
		}
		switch d, err := s.q.GetActivityDerived(ctx, db.GetActivityDerivedParams{UserID: userID, ActivityID: a.ID}); {
		case err == nil:
			sum.Load = d.Load
		case !errors.Is(err, pgx.ErrNoRows):
			return fail("activity load", err)
		}
		c.Activities = append(c.Activities, sum)
		if !slices.Contains(c.Sports, a.Sport) {
			c.Sports = append(c.Sports, a.Sport)
		}
	}

	metrics, err := s.q.ListDailyMetrics(ctx, db.ListDailyMetricsParams{
		UserID: userID, After: today.AddDate(0, 0, -contextDays), Until: today, MaxRows: contextDays,
	})
	if err != nil {
		return fail("health trends", err)
	}
	for _, m := range metrics {
		c.Health = append(c.Health, llm.HealthDay{Day: m.Day, HRV: f64(m.HrvMs), RestingHR: intPtr(m.RestingHrBpm), SleepS: i32f(m.SleepS)})
	}

	history, err := s.q.ListRecordHistory(ctx, db.ListRecordHistoryParams{UserID: userID, MaxRows: maxRecordRows})
	if err != nil {
		return fail("records", err)
	}
	current := map[string]db.ListRecordHistoryRow{}
	for _, h := range history { // oldest first per effort, so the last one stands
		current[h.Effort] = h
	}
	for _, e := range analytics.Efforts() {
		if h, ok := current[string(e)]; ok {
			c.Records = append(c.Records, llm.EffortRecord(e, h.Value, h.ActivityID, localDay(h.StartTime, h.UtcOffsetS)))
		}
	}
	return c, nil
}

// localStart is the wall-clock start where the activity happened, as a UTC time.
func localStart(t time.Time, offsetS int32) time.Time {
	return t.UTC().Add(time.Duration(offsetS) * time.Second)
}

func localDay(t time.Time, offsetS int32) time.Time {
	y, m, d := localStart(t, offsetS).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
