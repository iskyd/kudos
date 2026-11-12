-- name: CreatePlan :one
INSERT INTO plans (user_id) VALUES (@user_id) RETURNING *;

-- name: CreatePlannedWorkout :one
-- steps must already be validated (internal/plans).
INSERT INTO planned_workouts (user_id, plan_id, day, sport, title, description, steps)
VALUES (@user_id, @plan_id, @day, @sport, @title, @description, @steps)
RETURNING *;

-- name: GetPlannedWorkout :one
SELECT * FROM planned_workouts WHERE user_id = @user_id AND id = @id;

-- name: ListPlannedWorkoutsBetween :many
-- The workouts planned from from_day to to_day (inclusive), soonest first.
SELECT * FROM planned_workouts
WHERE user_id = @user_id AND day BETWEEN @from_day::date AND @to_day::date
ORDER BY day, id
LIMIT @max_rows;

-- name: UpdatePlannedWorkout :execrows
-- steps must already be validated (internal/plans).
UPDATE planned_workouts SET day = @day, sport = @sport, title = @title, description = @description, steps = @steps
WHERE user_id = @user_id AND id = @id;

-- name: PutPlannedWorkoutLink :one
-- Links a planned workout to an activity, or with a null activity_id marks it
-- unlinked. The unique index rejects an activity linked to another one.
INSERT INTO planned_workout_links (user_id, planned_workout_id, activity_id)
VALUES (@user_id, @planned_workout_id, sqlc.narg(activity_id))
ON CONFLICT (planned_workout_id) DO UPDATE SET activity_id = EXCLUDED.activity_id, created_at = now()
WHERE planned_workout_links.user_id = EXCLUDED.user_id
RETURNING *;

-- name: DeletePlannedWorkoutLink :execrows
DELETE FROM planned_workout_links WHERE user_id = @user_id AND planned_workout_id = @planned_workout_id;

-- name: ListPlannedWorkoutLinks :many
-- The links of the given planned workouts, and those to the given activities.
SELECT * FROM planned_workout_links
WHERE user_id = @user_id
    AND (planned_workout_id = ANY(@planned_workout_ids::bigint[]) OR activity_id = ANY(@activity_ids::bigint[]))
ORDER BY planned_workout_id
LIMIT @max_rows;

-- name: ListRecordedWorkoutNames :many
SELECT activity_id, name FROM recorded_workouts
WHERE user_id = @user_id AND activity_id = ANY(@activity_ids::bigint[]) AND name IS NOT NULL
LIMIT @max_rows;

-- name: DeletePlannedWorkout :execrows
DELETE FROM planned_workouts WHERE user_id = @user_id AND id = @id;

-- name: ExpirePlanProposals :exec
-- Fails proposals still generating since before: their job is gone.
UPDATE plan_proposals SET status = 'failed', failure = 'timeout'
WHERE user_id = @user_id AND status = 'generating' AND created_at < @before;

-- name: CreatePlanProposal :one
-- Fails with a unique violation while another proposal of the user is generating.
INSERT INTO plan_proposals (user_id, kind, from_day, to_day) VALUES (@user_id, @kind, @from_day, @to_day) RETURNING *;

-- name: GetPlanProposal :one
SELECT * FROM plan_proposals WHERE user_id = @user_id AND id = @id;

-- name: LockPlanProposal :one
SELECT * FROM plan_proposals WHERE user_id = @user_id AND id = @id FOR UPDATE;

-- name: GetLatestPlanProposal :one
-- An adjustment that changed nothing is left out: the daily check found the plan fine.
SELECT * FROM plan_proposals p
WHERE p.user_id = @user_id
    AND NOT (p.kind = 'adjustment' AND p.status = 'closed'
        AND NOT EXISTS (SELECT 1 FROM plan_proposal_items i WHERE i.user_id = p.user_id AND i.proposal_id = p.id))
ORDER BY p.id DESC
LIMIT 1;

-- name: FailPlanProposal :exec
UPDATE plan_proposals SET status = 'failed', failure = @failure
WHERE user_id = @user_id AND id = @id AND status = 'generating';

-- name: SupersedePlanProposals :exec
-- Supersedes the open proposals but id, waiting for decisions in flight on
-- them: they lock their rows.
UPDATE plan_proposals SET status = 'superseded' WHERE user_id = @user_id AND status = 'open' AND id <> @id;

-- name: FillPlanProposal :execrows
-- status is open, or closed when it proposes no change.
UPDATE plan_proposals SET status = @status, prompt_version = @prompt_version, rationale = @rationale, refs = @refs
WHERE user_id = @user_id AND id = @id AND status = 'generating';

-- name: SetPlanProposalPlan :exec
UPDATE plan_proposals SET plan_id = @plan_id WHERE user_id = @user_id AND id = @id;

-- name: SetPlanProposalStatus :exec
UPDATE plan_proposals SET status = @status WHERE user_id = @user_id AND id = @id;

-- name: CreatePlanProposalItem :exec
-- before and after must already be validated (internal/plans).
INSERT INTO plan_proposal_items (user_id, proposal_id, action, workout_id, before, after)
VALUES (@user_id, @proposal_id, @action, @workout_id, @before, @after);

-- name: ListPlanProposalItems :many
-- In the order they were proposed, which is by day.
SELECT * FROM plan_proposal_items WHERE user_id = @user_id AND proposal_id = @proposal_id
ORDER BY id
LIMIT @max_rows;

-- name: DecidePlanProposalItem :execrows
-- Decides a pending item; 0 rows when it is not pending in this proposal.
UPDATE plan_proposal_items SET status = @status
WHERE user_id = @user_id AND proposal_id = @proposal_id AND id = @id AND status = 'pending';

-- name: CountPendingPlanProposalItems :one
SELECT count(*) FROM plan_proposal_items WHERE user_id = @user_id AND proposal_id = @proposal_id AND status = 'pending';

-- name: ListPlanCandidates :many
-- The plan scheduler's scan across users, like ListCoachCandidates: users
-- with an LLM provider and a plan, after after_id, with what decides whether
-- they are due a new plan or an adjustment. Year 1 stands for never.
SELECT u.id AS user_id, u.timezone,
    coalesce(c.status = 'connected', false)::boolean AS garmin_connected,
    coalesce(c.last_sync_at, '0001-01-01 00:00:00+00')::timestamptz AS last_sync_at,
    coalesce((SELECT max(pp.created_at) FROM plan_proposals pp WHERE pp.user_id = u.id AND pp.kind = 'plan'), '0001-01-01 00:00:00+00')::timestamptz AS last_proposal,
    coalesce((SELECT max(pp.created_at) FROM plan_proposals pp WHERE pp.user_id = u.id AND pp.kind = 'adjustment'), '0001-01-01 00:00:00+00')::timestamptz AS last_adjustment,
    coalesce((SELECT max(r.day) FROM coach_reports r WHERE r.user_id = u.id AND r.kind = 'daily_insight'), '0001-01-01')::date AS last_insight,
    EXISTS (SELECT 1 FROM plan_proposals pp WHERE pp.user_id = u.id AND pp.status IN ('open', 'generating'))::boolean AS pending_proposal
FROM users u
JOIN llm_providers p ON p.user_id = u.id
LEFT JOIN garmin_connections c ON c.user_id = u.id
WHERE u.id > @after_id AND EXISTS (SELECT 1 FROM plans pl WHERE pl.user_id = u.id)
ORDER BY u.id
LIMIT @max_rows;
