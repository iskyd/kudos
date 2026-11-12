// Package reports writes the coach's daily insights, after the morning sync,
// and weekly reports, on Monday mornings, in each user's timezone.
package reports

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/iskyd/kudos/internal/llm"
	"github.com/iskyd/kudos/internal/store/db"
)

// The kinds of output, as stored.
const (
	DailyInsight = "daily_insight"
	WeeklyReport = "weekly_report"
)

const (
	// morningHour is the local hour from which outputs are written...
	morningHour = 6
	// ...after the first Garmin sync from then, or from syncWaitHour without one.
	syncWaitHour = 10
	// contextTokens matches chat: small local models need the room.
	contextTokens = 6000
	scheduleEvery = 15 * time.Minute
	candidatePage = 500
	// generateTimeout allows for slow local models; it stays under River's
	// rescue of stuck jobs (internal/jobs).
	generateTimeout = 10 * time.Minute
	maxAttempts     = 3
)

// Due returns the day c is due a daily insight and the Monday it is due a weekly
// report, at now in c's timezone; nil when not due. Outputs wait for the
// morning: from 06:00 local, once a connected Garmin has synced since then, or
// from 10:00 if it has not. A report missed on its Monday is skipped.
func Due(c db.ListCoachCandidatesRow, now time.Time) (insight, report *time.Time) {
	today, ok := Morning(c.Timezone, c.GarminConnected, c.LastSyncAt, now)
	if !ok {
		return nil, nil
	}
	if c.LastInsight.Before(today) {
		insight = &today
	}
	if today.Weekday() == time.Monday && c.LastReport.Before(today) {
		report = &today
	}
	return insight, report
}

// Morning reports whether the morning's data is in at now in timezone: from
// 06:00 local, once a connected Garmin has synced since then, or from 10:00 if
// it has not. today is the local day, as a date at UTC midnight.
func Morning(timezone string, garminConnected bool, lastSync, now time.Time) (today time.Time, ok bool) {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		loc = time.UTC // validated when set; this is only a guard
	}
	y, m, d := now.In(loc).Date()
	at := func(hour int) time.Time { return time.Date(y, m, d, hour, 0, 0, 0, loc) }
	morning := at(morningHour)
	if now.Before(morning) || garminConnected && lastSync.Before(morning) && now.Before(at(syncWaitHour)) {
		return time.Time{}, false
	}
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC), true
}

// Deps are what the workers need. Context and Client come from the API server,
// which is built after the job client: cmd/server sets them before starting it.
type Deps struct {
	Pool    *pgxpool.Pool
	Logger  *slog.Logger
	Context func(ctx context.Context, userID int64, today time.Time) (llm.AthleteContext, error)
	Client  func(ctx context.Context, userID int64) (llm.Client, error)
	Now     func() time.Time // nil for time.Now
}

func (d *Deps) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now()
}

// Register adds the workers.
func (d *Deps) Register(w *river.Workers) {
	river.AddWorker(w, &GenerateWorker{Deps: d})
	river.AddWorker(w, &ScheduleWorker{Deps: d})
}

// GenerateArgs writes one output: Output is DailyInsight or WeeklyReport.
type GenerateArgs struct {
	UserID int64     `json:"user_id"`
	Output string    `json:"kind"`
	Day    time.Time `json:"day"`
}

// Kind implements river.JobArgs.
func (GenerateArgs) Kind() string { return "coach_report" }

// InsertOpts queues each output once, and a failed one is not tried again
// that day: every 15 minutes would cost provider calls for nothing.
func (GenerateArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: maxAttempts, UniqueOpts: river.UniqueOpts{ByArgs: true, ByState: []rivertype.JobState{
		rivertype.JobStateAvailable, rivertype.JobStateCompleted, rivertype.JobStateDiscarded, rivertype.JobStatePending,
		rivertype.JobStateRetryable, rivertype.JobStateRunning, rivertype.JobStateScheduled,
	}}}
}

// GenerateWorker works GenerateArgs.
type GenerateWorker struct {
	river.WorkerDefaults[GenerateArgs]
	*Deps
}

// Timeout implements river.Worker.
func (*GenerateWorker) Timeout(*river.Job[GenerateArgs]) time.Duration { return generateTimeout }

// Work writes the output and stores it with its prompt version. A user who
// removed their provider since it was queued is skipped.
func (w *GenerateWorker) Work(ctx context.Context, job *river.Job[GenerateArgs]) error {
	a := job.Args
	mode := llm.ModeDailyInsight
	if a.Output == WeeklyReport {
		mode = llm.ModeWeeklyReport
	}
	client, err := w.Client(ctx, a.UserID)
	if errors.Is(err, llm.ErrNotConfigured) {
		return nil
	}
	if err != nil {
		return err
	}
	c, err := w.Context(ctx, a.UserID, a.Day)
	if err != nil {
		return err
	}
	p, err := llm.Render(mode, llm.PromptInput{Today: a.Day, Context: llm.BuildContext(c, contextTokens)})
	if err != nil {
		return fmt.Errorf("rendering the %s prompt: %w", a.Output, err)
	}
	text, err := client.Complete(ctx, llm.Request{System: p.System, Messages: []llm.Message{{Role: "user", Content: p.User}}})
	if err == nil && strings.TrimSpace(text) == "" {
		err = llm.ErrMalformed
	}
	if err != nil {
		w.Logger.WarnContext(ctx, "coach output failed", "user_id", a.UserID, "kind", a.Output, "attempt", job.Attempt)
		return fmt.Errorf("writing the %s: %w", a.Output, err)
	}
	if _, err := db.New(w.Pool).CreateCoachReport(ctx, db.CreateCoachReportParams{
		UserID: a.UserID, Kind: a.Output, Day: a.Day, PromptVersion: p.Version, Content: strings.TrimSpace(text),
	}); err != nil {
		return fmt.Errorf("storing the %s: %w", a.Output, err)
	}
	return nil
}

// ScheduleArgs queues the outputs users are due.
type ScheduleArgs struct{}

// Kind implements river.JobArgs.
func (ScheduleArgs) Kind() string { return "coach_report_schedule" }

// SchedulePeriodic runs the scheduler every 15 minutes, starting at boot.
func SchedulePeriodic() *river.PeriodicJob {
	return river.NewPeriodicJob(river.PeriodicInterval(scheduleEvery),
		func() (river.JobArgs, *river.InsertOpts) { return ScheduleArgs{}, nil },
		&river.PeriodicJobOpts{RunOnStart: true})
}

// ScheduleWorker works ScheduleArgs.
type ScheduleWorker struct {
	river.WorkerDefaults[ScheduleArgs]
	*Deps
}

// Work implements river.Worker.
func (w *ScheduleWorker) Work(ctx context.Context, _ *river.Job[ScheduleArgs]) error {
	jobs := river.ClientFromContext[pgx.Tx](ctx)
	return w.queueDue(ctx, func(a GenerateArgs) error {
		_, err := jobs.Insert(ctx, a, nil)
		return err //nolint:wrapcheck // queueDue wraps it with the user and kind
	})
}

// queueDue queues every due output of every user with a provider.
func (d *Deps) queueDue(ctx context.Context, insert func(GenerateArgs) error) error {
	q := db.New(d.Pool)
	now := d.now()
	var after int64
	for {
		users, err := q.ListCoachCandidates(ctx, db.ListCoachCandidatesParams{AfterID: after, MaxRows: candidatePage})
		if err != nil {
			return fmt.Errorf("listing coach candidates: %w", err)
		}
		for _, u := range users {
			insight, report := Due(u, now)
			for kind, day := range map[string]*time.Time{DailyInsight: insight, WeeklyReport: report} {
				if day == nil {
					continue
				}
				if err := insert(GenerateArgs{UserID: u.UserID, Output: kind, Day: *day}); err != nil {
					return fmt.Errorf("queueing the %s of user %d: %w", kind, u.UserID, err)
				}
			}
		}
		if len(users) < candidatePage {
			return nil
		}
		after = users[len(users)-1].UserID
	}
}
