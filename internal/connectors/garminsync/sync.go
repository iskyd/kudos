package garminsync

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/iskyd/kudos/internal/connectors"
	"github.com/iskyd/kudos/internal/store/db"
)

const (
	// lookbackDays re-fetches the day before the cursor: Garmin finalizes sleep
	// and HRV hours after the fact, so each sync covers at least 48 hours.
	lookbackDays = 1
	// maxGapDays caps how far back a sync reaches after a long pause, like the backfill.
	maxGapDays = 365
	// scheduleEvery is how often the scheduler looks for users due a sync.
	scheduleEvery = 5 * time.Minute
	maxDuePerRun  = 1000
)

// ErrBackfillRunning means the backfill still owns the user's Garmin requests.
var ErrBackfillRunning = errors.New("the backfill is still running")

// SyncArgs syncs a user's recent Garmin data.
type SyncArgs struct {
	UserID int64 `json:"user_id"`
}

// Kind implements river.JobArgs.
func (SyncArgs) Kind() string { return "garmin_sync" }

// InsertOpts keeps one sync per user queued or running: "Sync now" during a
// sync does not start another.
func (SyncArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{UniqueOpts: river.UniqueOpts{ByArgs: true, ByState: []rivertype.JobState{
		rivertype.JobStateAvailable, rivertype.JobStatePending, rivertype.JobStateRetryable,
		rivertype.JobStateRunning, rivertype.JobStateScheduled,
	}}}
}

// SyncWorker works SyncArgs.
type SyncWorker struct {
	river.WorkerDefaults[SyncArgs]
	*Deps
}

// Timeout implements river.Worker.
func (*SyncWorker) Timeout(*river.Job[SyncArgs]) time.Duration { return jobTimeout }

// Work fetches the activities and daily metrics from the day before the cursor
// through today. Failures are recorded on the connection for the UI and retried
// at the next interval; a rejected key pauses sync until a new key is set.
func (w *SyncWorker) Work(ctx context.Context, job *river.Job[SyncArgs]) error {
	userID := job.Args.UserID
	q := db.New(w.Pool)
	conn, err := q.GetGarminConnection(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("reading the Garmin connection: %w", err)
	}
	if conn.Status == "key_rejected" {
		return nil
	}
	if b, err := q.GetGarminBackfill(ctx, userID); err == nil && b.Status != "done" {
		return nil // the backfill's job owns the requests until it finishes
	} else if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("reading the backfill: %w", err)
	}
	if err := q.StartGarminSyncAttempt(ctx, userID); err != nil {
		return fmt.Errorf("recording the attempt: %w", err)
	}
	tz, err := q.GetUserTimezone(ctx, userID)
	if err != nil {
		return fmt.Errorf("reading the timezone: %w", err)
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		loc = time.UTC
	}
	y, m, d := w.clock().In(loc).Date()
	today := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	from := syncFrom(conn.SyncedThrough, today)

	sess, err := w.session(conn)
	if err != nil {
		return err
	}
	token := sess.Token.Reveal()
	defer w.saveToken(context.WithoutCancel(ctx), q, userID, sess, token)

	err = w.sync(ctx, q, userID, loc, from, today, sess)
	switch {
	case err == nil:
		if err := q.SetGarminSynced(ctx, db.SetGarminSyncedParams{UserID: userID, SyncedThrough: &today}); err != nil {
			return fmt.Errorf("recording the sync: %w", err)
		}
		return nil
	case errors.Is(err, errBudget):
		return river.JobSnooze(0) //nolint:wrapcheck // River recognizes the snooze by its type
	case errors.Is(err, connectors.ErrKeyRejected):
		if err := q.SetGarminKeyRejected(ctx, userID); err != nil {
			return fmt.Errorf("recording the rejected key: %w", err)
		}
		w.log().InfoContext(ctx, "garmin sync paused: key rejected", "user_id", userID)
		return nil
	case errors.Is(err, connectors.ErrRateLimited), errors.Is(err, connectors.ErrUpstream):
		reason := "Garmin is limiting requests; sync retries at the next interval"
		if !errors.Is(err, connectors.ErrRateLimited) {
			reason = truncate("Garmin failed: " + err.Error())
		}
		if err := q.SetGarminSyncError(ctx, db.SetGarminSyncErrorParams{UserID: userID, LastError: &reason}); err != nil {
			return fmt.Errorf("recording the sync error: %w", err)
		}
		w.log().WarnContext(ctx, "garmin sync failed", "user_id", userID)
		return nil
	default:
		return err // River retries
	}
}

// syncFrom is the first day to fetch: the day before the cursor, or yesterday
// without one, and never more than a year back.
func syncFrom(cursor *time.Time, today time.Time) time.Time {
	from := today.AddDate(0, 0, -lookbackDays)
	if cursor != nil && cursor.Before(today) {
		from = cursor.AddDate(0, 0, -lookbackDays)
	}
	if oldest := today.AddDate(0, 0, 1-maxGapDays); from.Before(oldest) {
		from = oldest
	}
	return from
}

func (w *SyncWorker) sync(ctx context.Context, q *db.Queries, userID int64, loc *time.Location, from, to time.Time,
	s *connectors.Session,
) error {
	deadline := w.clock().Add(w.runBudget())
	acts, err := w.Connector.FetchActivities(ctx, s, from, to)
	if err != nil {
		return fmt.Errorf("listing activities: %w", err)
	}
	if err := w.addImports(ctx, q, userID, acts); err != nil {
		return err
	}
	for {
		imp, err := q.NextGarminActivityImport(ctx, userID)
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
	// Revised days (sleep and HRV finalized late) replace what was stored.
	for start := from; !start.After(to); start = start.AddDate(0, 0, healthChunkDays) {
		end := start.AddDate(0, 0, healthChunkDays-1)
		if end.After(to) {
			end = to
		}
		days, err := w.Connector.FetchDailyMetrics(ctx, s, start, end)
		if err != nil {
			return fmt.Errorf("fetching daily metrics: %w", err)
		}
		for _, d := range days {
			if err := q.UpsertDailyMetrics(ctx, dailyMetrics(userID, d)); err != nil {
				return fmt.Errorf("storing daily metrics: %w", err)
			}
		}
	}
	return nil
}

// ScheduleArgs looks for users due a sync.
type ScheduleArgs struct{}

// Kind implements river.JobArgs.
func (ScheduleArgs) Kind() string { return "garmin_sync_schedule" }

// SchedulePeriodic runs the scheduler every few minutes, starting at boot.
func SchedulePeriodic() *river.PeriodicJob {
	return river.NewPeriodicJob(river.PeriodicInterval(scheduleEvery),
		func() (river.JobArgs, *river.InsertOpts) { return ScheduleArgs{}, nil },
		&river.PeriodicJobOpts{RunOnStart: true})
}

// ScheduleWorker queues a sync for every user whose interval has passed.
// Service is set after construction, like Deps.Ingest.
type ScheduleWorker struct {
	river.WorkerDefaults[ScheduleArgs]
	Pool    *pgxpool.Pool
	Service *Service
}

// Work implements river.Worker.
func (w *ScheduleWorker) Work(ctx context.Context, _ *river.Job[ScheduleArgs]) error {
	ids, err := db.New(w.Pool).ListDueGarminSyncs(ctx, maxDuePerRun)
	if err != nil {
		return fmt.Errorf("listing due syncs: %w", err)
	}
	for _, id := range ids {
		if _, err := w.Service.jobs.Insert(ctx, SyncArgs{UserID: id}, nil); err != nil {
			return fmt.Errorf("queueing the sync of user %d: %w", id, err)
		}
	}
	return nil
}

// SyncNow queues a sync for the user unless one is already queued or running;
// queued reports whether a new one was queued.
func (s *Service) SyncNow(ctx context.Context, userID int64) (queued bool, err error) {
	q := db.New(s.pool)
	conn, err := q.GetGarminConnection(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, ErrNotConnected
	}
	if err != nil {
		return false, fmt.Errorf("reading the Garmin connection: %w", err)
	}
	if conn.Status == "key_rejected" {
		return false, fmt.Errorf("syncing: %w", connectors.ErrKeyRejected)
	}
	if b, err := q.GetGarminBackfill(ctx, userID); err == nil && b.Status != "done" {
		return false, ErrBackfillRunning
	} else if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return false, fmt.Errorf("reading the backfill: %w", err)
	}
	res, err := s.jobs.Insert(ctx, SyncArgs{UserID: userID}, nil)
	if err != nil {
		return false, fmt.Errorf("queueing the sync: %w", err)
	}
	return !res.UniqueSkippedAsDuplicate, nil
}

// addImports records listed activities; ones already imported keep their outcome.
func (w *Deps) addImports(ctx context.Context, q *db.Queries, userID int64, acts []connectors.RawActivity) error {
	for _, a := range acts {
		var offset *int32
		if a.LocalOffset != nil {
			secs := int32(a.LocalOffset.Seconds())
			offset = &secs
		}
		if err := q.AddGarminActivityImport(ctx, db.AddGarminActivityImportParams{
			UserID: userID, SourceActivityID: a.SourceID, StartTime: a.Start, UtcOffsetS: offset,
		}); err != nil {
			return fmt.Errorf("recording activity %s: %w", a.SourceID, err)
		}
	}
	return nil
}

func truncate(s string) string {
	if len(s) > maxErrorLen {
		return s[:maxErrorLen]
	}
	return s
}
