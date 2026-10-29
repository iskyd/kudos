package jobs

import (
	"context"

	"github.com/riverqueue/river"
)

// DerivedArgs recomputes an activity's derived values (activity_derived).
type DerivedArgs struct {
	UserID     int64 `json:"user_id"`
	ActivityID int64 `json:"activity_id"`
}

// Kind implements river.JobArgs.
func (DerivedArgs) Kind() string { return "activity_derived" }

// DerivedWorker works DerivedArgs.
type DerivedWorker struct {
	river.WorkerDefaults[DerivedArgs]
}

// Work does nothing yet.
// shortcut: no-op until analytics computes derived values (T4.3), which replaces this body.
func (*DerivedWorker) Work(context.Context, *river.Job[DerivedArgs]) error {
	return nil
}
