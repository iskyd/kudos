package jobs

import (
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
)

// Client enqueues and works jobs.
type Client = river.Client[pgx.Tx]

const defaultQueueWorkers = 4

// NewClient returns a River client with every worker registered. Call Start to
// begin working jobs and Stop for a graceful shutdown.
func NewClient(pool *pgxpool.Pool, logger *slog.Logger) (*Client, error) {
	workers := river.NewWorkers()
	// AddWorker panics only on a duplicate registration: a programming error caught by any test.
	river.AddWorker(workers, &PingWorker{})

	client, err := river.NewClient(riverpgxv5.New(pool), &river.Config{
		Logger:  logger,
		Queues:  map[string]river.QueueConfig{river.QueueDefault: {MaxWorkers: defaultQueueWorkers}},
		Workers: workers,
	})
	if err != nil {
		return nil, fmt.Errorf("creating river client: %w", err)
	}
	return client, nil
}
