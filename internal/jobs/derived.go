package jobs

// DerivedArgs recomputes an activity's derived values (activity_derived). The
// ingest package works it: it enqueues these jobs and owns the computation.
type DerivedArgs struct {
	UserID     int64 `json:"user_id"`
	ActivityID int64 `json:"activity_id"`
}

// Kind implements river.JobArgs.
func (DerivedArgs) Kind() string { return "activity_derived" }

// RecomputeArgs recomputes a user's derived values that are missing, stale or
// from an older version of the computation.
type RecomputeArgs struct {
	UserID int64 `json:"user_id"`
}

// Kind implements river.JobArgs.
func (RecomputeArgs) Kind() string { return "activity_derived_recompute" }

// RecomputeAllArgs queues RecomputeArgs for every user; the server enqueues it
// at startup so a new version of the computation reaches every activity.
type RecomputeAllArgs struct{}

// Kind implements river.JobArgs.
func (RecomputeAllArgs) Kind() string { return "activity_derived_recompute_all" }
