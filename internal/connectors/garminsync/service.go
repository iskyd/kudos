// Package garminsync imports a user's Garmin history: the onboarding backfill,
// worked by a resumable River job.
//
// The backfill lists the window's activities into garmin_activity_imports,
// imports them one at a time, then stores the window's daily metrics in 31-day
// chunks. Each step is recorded as it completes, so an interruption (a rate
// limit, a rejected key, a restart) resumes where it stopped, and every write
// is an idempotent upsert.
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

	"github.com/iskyd/kudos/internal/jobs"
	"github.com/iskyd/kudos/internal/store/db"
)

var (
	// ErrNotConnected means the user has no Garmin key.
	ErrNotConnected = errors.New("garmin is not connected")
	// ErrExists means the user already has a backfill.
	ErrExists = errors.New("backfill already started")
	// ErrNotFound means the user has no backfill.
	ErrNotFound = errors.New("no backfill")
)

// BackfillArgs runs or continues a user's backfill.
type BackfillArgs struct {
	UserID int64 `json:"user_id"`
}

// Kind implements river.JobArgs.
func (BackfillArgs) Kind() string { return "garmin_backfill" }

// InsertOpts keeps one backfill job per user queued or running, so requests
// stay sequential.
func (BackfillArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{UniqueOpts: river.UniqueOpts{ByArgs: true, ByState: []rivertype.JobState{
		rivertype.JobStateAvailable, rivertype.JobStatePending, rivertype.JobStateRetryable,
		rivertype.JobStateRunning, rivertype.JobStateScheduled,
	}}}
}

// Service starts and resumes backfills.
type Service struct {
	pool *pgxpool.Pool
	jobs *jobs.Client
}

// NewService returns a service enqueueing on jobClient.
func NewService(pool *pgxpool.Pool, jobClient *jobs.Client) *Service {
	return &Service{pool: pool, jobs: jobClient}
}

// Progress is a backfill's state and its activity counts.
type Progress struct {
	Backfill   db.GarminBackfill
	Activities db.CountGarminActivityImportsRow
}

// Start begins the backfill of the local days from..to.
func (s *Service) Start(ctx context.Context, userID int64, from, to time.Time) error {
	return inTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := db.New(tx)
		if _, err := q.GetGarminConnection(ctx, userID); errors.Is(err, pgx.ErrNoRows) {
			return ErrNotConnected
		} else if err != nil {
			return fmt.Errorf("reading the Garmin connection: %w", err)
		}
		_, err := q.CreateGarminBackfill(ctx, db.CreateGarminBackfillParams{UserID: userID, WindowStart: from, WindowEnd: to})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrExists
		}
		if err != nil {
			return fmt.Errorf("creating the backfill: %w", err)
		}
		return s.enqueue(ctx, tx, userID)
	})
}

// Resume continues a paused backfill and, with retryFailed, imports its failed
// activities again. A backfill with nothing left to do is left as it is.
func (s *Service) Resume(ctx context.Context, userID int64, retryFailed bool) error {
	return inTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := db.New(tx)
		if _, err := q.GetGarminBackfill(ctx, userID); errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return fmt.Errorf("reading the backfill: %w", err)
		}
		if retryFailed {
			if _, err := q.RetryGarminActivityImports(ctx, userID); err != nil {
				return fmt.Errorf("retrying failed imports: %w", err)
			}
		}
		_, err := q.RunGarminBackfill(ctx, userID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("resuming the backfill: %w", err)
		}
		return s.enqueue(ctx, tx, userID)
	})
}

func (s *Service) enqueue(ctx context.Context, tx pgx.Tx, userID int64) error {
	// shortcut: a resume that lands while the job is finishing is not queued (the job is still
	// running); the user resumes again. Upgrade by re-checking for work when the job exits.
	if _, err := s.jobs.InsertTx(ctx, tx, BackfillArgs{UserID: userID}, nil); err != nil {
		return fmt.Errorf("enqueueing the backfill: %w", err)
	}
	return nil
}

// Progress returns the user's backfill, or ErrNotFound.
func (s *Service) Progress(ctx context.Context, userID int64) (Progress, error) {
	q := db.New(s.pool)
	b, err := q.GetGarminBackfill(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Progress{}, ErrNotFound
	}
	if err != nil {
		return Progress{}, fmt.Errorf("reading the backfill: %w", err)
	}
	n, err := q.CountGarminActivityImports(ctx, userID)
	if err != nil {
		return Progress{}, fmt.Errorf("counting imports: %w", err)
	}
	return Progress{Backfill: b, Activities: n}, nil
}

// Failures lists failed imports after the cursor position, oldest first.
func (s *Service) Failures(ctx context.Context, userID int64, afterStart time.Time, afterID string, limit int32,
) ([]db.ListGarminActivityImportFailuresRow, error) {
	rows, err := db.New(s.pool).ListGarminActivityImportFailures(ctx, db.ListGarminActivityImportFailuresParams{
		UserID: userID, AfterStart: afterStart, AfterID: afterID, MaxRows: limit,
	})
	if err != nil {
		return nil, fmt.Errorf("listing failed imports: %w", err)
	}
	return rows, nil
}

// inTx runs fn in a transaction; fn's own errors come back unchanged so callers can match them.
func inTx(ctx context.Context, pool *pgxpool.Pool, fn func(pgx.Tx) error) error {
	var fnErr error
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		fnErr = fn(tx)
		return fnErr
	})
	if fnErr != nil {
		return fnErr
	}
	if err != nil {
		return fmt.Errorf("in transaction: %w", err)
	}
	return nil
}
