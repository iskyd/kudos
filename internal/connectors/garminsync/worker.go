package garminsync

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/iskyd/kudos/internal/connectors"
	"github.com/iskyd/kudos/internal/crypto"
	"github.com/iskyd/kudos/internal/importer/ingest"
	"github.com/iskyd/kudos/internal/secret"
	"github.com/iskyd/kudos/internal/store/db"
)

const (
	// runBudget bounds one run; the job then snoozes and continues, so a restart
	// loses at most one run and other jobs get a turn.
	runBudget = 2 * time.Minute
	// jobTimeout covers a run plus one request's retries; it stays under the
	// client's stuck-job rescue (15 minutes).
	jobTimeout = 10 * time.Minute
	// rateLimitPause is how long a rate-limited backfill waits before continuing.
	rateLimitPause  = 15 * time.Minute
	healthChunkDays = 31
	maxErrorLen     = 300
)

var errBudget = errors.New("run budget spent")

// Deps are what the Garmin workers share. Ingest is set after construction,
// since the ingest service needs the River client the workers are registered with.
type Deps struct {
	Pool      *pgxpool.Pool
	Connector connectors.Connector
	Cipher    *crypto.Cipher
	Ingest    *ingest.Service
	Logger    *slog.Logger

	now    func() time.Time // for tests; time.Now when nil
	budget time.Duration    // for tests; runBudget when zero
}

// Worker works BackfillArgs.
type Worker struct {
	river.WorkerDefaults[BackfillArgs]
	*Deps
}

// Timeout implements river.Worker.
func (*Worker) Timeout(*river.Job[BackfillArgs]) time.Duration { return jobTimeout }

// Work runs the backfill until it is done, paused or out of budget.
func (w *Worker) Work(ctx context.Context, job *river.Job[BackfillArgs]) error {
	userID := job.Args.UserID
	q := db.New(w.Pool)
	b, err := q.GetGarminBackfill(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("reading the backfill: %w", err)
	}
	switch {
	case b.Status == "done":
		return nil
	case b.Status == "paused" && *b.PauseReason != "rate_limited":
		return nil // waits for Resume
	case b.Status == "paused":
		if b, err = q.RunGarminBackfill(ctx, userID); err != nil {
			return fmt.Errorf("resuming after the rate limit: %w", err)
		}
	}

	conn, err := q.GetGarminConnection(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return w.pause(ctx, q, userID, "disconnected")
	}
	if err != nil {
		return fmt.Errorf("reading the Garmin connection: %w", err)
	}
	if conn.Status == "key_rejected" {
		return w.pause(ctx, q, userID, "key_rejected")
	}
	sess, err := w.session(conn)
	if err != nil {
		return err
	}
	token := sess.Token.Reveal()
	// The connector may re-mint the token; keep it, even when the run was cancelled.
	defer w.saveToken(context.WithoutCancel(ctx), q, userID, sess, token)

	err = w.run(ctx, q, b, sess)
	switch {
	case err == nil:
		n, err := q.FinishGarminBackfill(ctx, userID)
		if err != nil {
			return fmt.Errorf("finishing the backfill: %w", err)
		}
		if n == 0 { // a retry was queued meanwhile
			return river.JobSnooze(0) //nolint:wrapcheck // River recognizes the snooze by its type
		}
		if err := q.SetGarminSynced(ctx, db.SetGarminSyncedParams{UserID: userID, SyncedThrough: &b.WindowEnd}); err != nil {
			return fmt.Errorf("recording the sync: %w", err)
		}
		w.log().InfoContext(ctx, "garmin backfill done", "user_id", userID)
		return nil
	case errors.Is(err, errBudget):
		return river.JobSnooze(0) //nolint:wrapcheck // River recognizes the snooze by its type
	case errors.Is(err, connectors.ErrKeyRejected):
		if err := q.SetGarminKeyRejected(ctx, userID); err != nil {
			return fmt.Errorf("recording the rejected key: %w", err)
		}
		return w.pause(ctx, q, userID, "key_rejected")
	case errors.Is(err, connectors.ErrRateLimited):
		if err := w.pause(ctx, q, userID, "rate_limited"); err != nil {
			return err
		}
		return river.JobSnooze(rateLimitPause) //nolint:wrapcheck // River recognizes the snooze by its type
	default:
		return err // River retries with its own backoff
	}
}

func (w *Worker) pause(ctx context.Context, q *db.Queries, userID int64, reason string) error {
	if err := q.PauseGarminBackfill(ctx, db.PauseGarminBackfillParams{UserID: userID, PauseReason: &reason}); err != nil {
		return fmt.Errorf("pausing the backfill: %w", err)
	}
	w.log().InfoContext(ctx, "garmin backfill paused", "user_id", userID, "reason", reason)
	return nil
}

func (w *Deps) session(c db.GarminConnection) (*connectors.Session, error) {
	key, err := w.Cipher.Open(c.KeyCiphertext)
	if err != nil {
		return nil, fmt.Errorf("decrypting the Garmin key: %w", err)
	}
	s := &connectors.Session{Credentials: connectors.Credentials{Key: secret.New(string(key))}}
	if c.TokenCiphertext != nil && c.TokenExpiresAt != nil {
		token, err := w.Cipher.Open(c.TokenCiphertext)
		if err != nil {
			return nil, fmt.Errorf("decrypting the Garmin token: %w", err)
		}
		s.Token, s.Expires = secret.New(string(token)), *c.TokenExpiresAt
	}
	return s, nil
}

func (w *Deps) saveToken(ctx context.Context, q *db.Queries, userID int64, s *connectors.Session, old string) {
	if s.Token.Reveal() == old || s.Token.Reveal() == "" {
		return
	}
	ct, err := w.Cipher.Seal([]byte(s.Token.Reveal()))
	if err == nil {
		err = q.UpdateGarminToken(ctx, db.UpdateGarminTokenParams{UserID: userID, TokenCiphertext: ct, TokenExpiresAt: &s.Expires})
	}
	if err != nil {
		w.log().ErrorContext(ctx, "storing the Garmin token", "user_id", userID, "error", err)
	}
}

// run lists the window's activities once, imports the pending ones, then
// stores the remaining days' metrics.
func (w *Worker) run(ctx context.Context, q *db.Queries, b db.GarminBackfill, s *connectors.Session) error {
	deadline := w.clock().Add(w.runBudget())
	tz, err := q.GetUserTimezone(ctx, b.UserID)
	if err != nil {
		return fmt.Errorf("reading the timezone: %w", err)
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		loc = time.UTC
	}

	if !b.ActivitiesListed {
		acts, err := w.Connector.FetchActivities(ctx, s, b.WindowStart, b.WindowEnd)
		if err != nil {
			return fmt.Errorf("listing activities: %w", err)
		}
		if err := w.addImports(ctx, q, b.UserID, acts); err != nil {
			return err
		}
		if err := q.SetGarminBackfillListed(ctx, b.UserID); err != nil {
			return fmt.Errorf("recording the listing: %w", err)
		}
	}

	// The budget is checked after each step, so every run makes progress.
	for {
		imp, err := q.NextGarminActivityImport(ctx, b.UserID)
		if errors.Is(err, pgx.ErrNoRows) {
			break
		}
		if err != nil {
			return fmt.Errorf("reading the next import: %w", err)
		}
		if err := w.importActivity(ctx, q, imp, loc, s); err != nil {
			return err
		}
		if w.clock().After(deadline) {
			return errBudget
		}
	}

	from := b.WindowStart
	if b.HealthThrough != nil {
		from = b.HealthThrough.AddDate(0, 0, 1)
	}
	for !from.After(b.WindowEnd) {
		to := from.AddDate(0, 0, healthChunkDays-1)
		if to.After(b.WindowEnd) {
			to = b.WindowEnd
		}
		days, err := w.Connector.FetchDailyMetrics(ctx, s, from, to)
		if err != nil {
			return fmt.Errorf("fetching daily metrics: %w", err)
		}
		for _, d := range days {
			if err := q.UpsertDailyMetrics(ctx, dailyMetrics(b.UserID, d)); err != nil {
				return fmt.Errorf("storing daily metrics: %w", err)
			}
		}
		if err := q.SetGarminBackfillHealthThrough(ctx, db.SetGarminBackfillHealthThroughParams{UserID: b.UserID, HealthThrough: &to}); err != nil {
			return fmt.Errorf("recording the health cursor: %w", err)
		}
		from = to.AddDate(0, 0, 1)
		if !from.After(b.WindowEnd) && w.clock().After(deadline) {
			return errBudget
		}
	}
	return nil
}

// importActivity downloads and ingests one activity. A file Garmin cannot
// deliver or KudOS cannot read is recorded as failed and the backfill goes on.
func (w *Deps) importActivity(ctx context.Context, q *db.Queries, imp db.GarminActivityImport, loc *time.Location, s *connectors.Session) error {
	id := imp.SourceActivityID
	f, err := w.Connector.DownloadActivityFile(ctx, s, id)
	switch {
	case errors.Is(err, connectors.ErrKeyRejected), errors.Is(err, connectors.ErrRateLimited), ctx.Err() != nil:
		return fmt.Errorf("downloading activity %s: %w", id, err)
	case err != nil:
		return w.fail(ctx, q, imp, "download failed: "+err.Error())
	}
	// Garmin's offset is the fallback when the file has none (invariant 9).
	if imp.UtcOffsetS != nil {
		loc = time.FixedZone("", int(*imp.UtcOffsetS))
	}
	_, err = w.Ingest.Import(ctx, imp.UserID, loc, f.Name, f.Data, ingest.Origin{Source: "garmin", ActivityID: id})
	switch {
	case errors.Is(err, ingest.ErrInvalidFile), errors.Is(err, ingest.ErrUnsupportedFormat):
		return w.fail(ctx, q, imp, err.Error())
	case err != nil:
		return fmt.Errorf("ingesting activity %s: %w", id, err)
	}
	if err := q.SetGarminActivityImportDone(ctx, db.SetGarminActivityImportDoneParams{UserID: imp.UserID, SourceActivityID: id}); err != nil {
		return fmt.Errorf("recording activity %s: %w", id, err)
	}
	return nil
}

func (w *Deps) fail(ctx context.Context, q *db.Queries, imp db.GarminActivityImport, reason string) error {
	reason = truncate(reason)
	if err := q.SetGarminActivityImportFailed(ctx, db.SetGarminActivityImportFailedParams{
		UserID: imp.UserID, SourceActivityID: imp.SourceActivityID, Error: &reason,
	}); err != nil {
		return fmt.Errorf("recording activity %s: %w", imp.SourceActivityID, err)
	}
	w.log().WarnContext(ctx, "garmin activity import failed", "user_id", imp.UserID, "source_activity_id", imp.SourceActivityID)
	return nil
}

func (w *Deps) clock() time.Time {
	if w.now != nil {
		return w.now()
	}
	return time.Now()
}

func (w *Deps) runBudget() time.Duration {
	if w.budget > 0 {
		return w.budget
	}
	return runBudget
}

func (w *Deps) log() *slog.Logger {
	if w.Logger != nil {
		return w.Logger
	}
	return slog.New(slog.DiscardHandler)
}

func dailyMetrics(userID int64, d connectors.DailyMetrics) db.UpsertDailyMetricsParams {
	return db.UpsertDailyMetricsParams{
		UserID: userID, Day: d.Day, RestingHrBpm: i16(d.RestingHRBpm), HrvMs: f32(d.HRVMs),
		SleepS: i32(d.SleepS), SleepDeepS: i32(d.SleepDeepS), SleepLightS: i32(d.SleepLightS),
		SleepRemS: i32(d.SleepRemS), SleepAwakeS: i32(d.SleepAwakeS), SleepScore: i16(d.SleepScore),
		Stress: i16(d.Stress), BodyBatteryMax: i16(d.BodyBatteryMax), BodyBatteryMin: i16(d.BodyBatteryMin),
		Steps: i32(d.Steps), WeightKg: f32(d.WeightKg), BodyFatPct: f32(d.BodyFatPct), BodyWaterPct: f32(d.BodyWaterPct),
		MuscleMassKg: f32(d.MuscleMassKg), BoneMassKg: f32(d.BoneMassKg), Spo2Pct: f32(d.Spo2Pct), HydrationL: f32(d.HydrationL),
	}
}

func i16(v *int) *int16 {
	if v == nil {
		return nil
	}
	n := int16(*v) //nolint:gosec // G115: scores, heart rates and levels are small; the table checks ranges
	return &n
}

func i32(v *int) *int32 {
	if v == nil {
		return nil
	}
	n := int32(*v) //nolint:gosec // G115: seconds in a day and step counts fit
	return &n
}

func f32(v *float64) *float32 {
	if v == nil {
		return nil
	}
	n := float32(*v)
	return &n
}
