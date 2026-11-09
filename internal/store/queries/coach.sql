-- name: CreateCoachReport :execrows
-- Writing the same kind for the same day again keeps the first one.
INSERT INTO coach_reports (user_id, kind, day, prompt_version, content)
VALUES (@user_id, @kind, @day, @prompt_version, @content)
ON CONFLICT (user_id, kind, day) DO NOTHING;

-- name: ListCoachReports :many
-- One page of a kind, latest day first; pass before_day for the page after it.
SELECT * FROM coach_reports
WHERE user_id = @user_id AND kind = @kind
    AND (sqlc.narg(before_day)::date IS NULL OR day < sqlc.narg(before_day)::date)
ORDER BY day DESC
LIMIT @max_rows;

-- name: ListCoachCandidates :many
-- The scheduler's scan across users, like ListDueGarminSyncs not keyed by one
-- user: users with an LLM provider after after_id, with what decides whether
-- they are due an insight or a report. Year 1 stands for never.
SELECT u.id AS user_id, u.timezone,
    coalesce(c.status = 'connected', false)::boolean AS garmin_connected,
    coalesce(c.last_sync_at, '0001-01-01 00:00:00+00')::timestamptz AS last_sync_at,
    coalesce((SELECT max(r.day) FROM coach_reports r WHERE r.user_id = u.id AND r.kind = 'daily_insight'), '0001-01-01')::date AS last_insight,
    coalesce((SELECT max(r.day) FROM coach_reports r WHERE r.user_id = u.id AND r.kind = 'weekly_report'), '0001-01-01')::date AS last_report
FROM users u
JOIN llm_providers p ON p.user_id = u.id
LEFT JOIN garmin_connections c ON c.user_id = u.id
WHERE u.id > @after_id
ORDER BY u.id
LIMIT @max_rows;
