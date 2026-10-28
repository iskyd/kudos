package jobs

import (
	"context"

	"github.com/riverqueue/river"
)

// PingArgs is a no-op job that proves the queue works end to end.
type PingArgs struct{}

// Kind implements river.JobArgs.
func (PingArgs) Kind() string { return "ping" }

// PingWorker works PingArgs.
type PingWorker struct {
	river.WorkerDefaults[PingArgs]
}

// Work does nothing and succeeds.
func (*PingWorker) Work(context.Context, *river.Job[PingArgs]) error {
	return nil
}
