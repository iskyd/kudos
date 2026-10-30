package jobs

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
)

// Client enqueues and works jobs.
type Client = river.Client[pgx.Tx]

const (
	defaultQueueWorkers = 4
	// rescueStuckJobsAfter retries a job left running by a crash or restart. It
	// must exceed every worker's timeout.
	rescueStuckJobsAfter = 15 * time.Minute
)

// NewClient returns a River client with every worker registered, including
// those register adds (workers in packages that themselves enqueue jobs). Call
// Start to begin working jobs and Stop for a graceful shutdown.
func NewClient(pool *pgxpool.Pool, logger *slog.Logger, register ...func(*river.Workers)) (*Client, error) {
	workers := river.NewWorkers()
	// AddWorker panics only on a duplicate registration: a programming error caught by any test.
	river.AddWorker(workers, &PingWorker{})
	for _, r := range register {
		r(workers)
	}

	client, err := river.NewClient(riverpgxv5.New(pool), &river.Config{
		Logger:               logger,
		RescueStuckJobsAfter: rescueStuckJobsAfter,
		Queues:               map[string]river.QueueConfig{river.QueueDefault: {MaxWorkers: defaultQueueWorkers}},
		Workers:              workers,
	})
	if err != nil {
		return nil, fmt.Errorf("creating river client: %w", err)
	}
	return client, nil
}
