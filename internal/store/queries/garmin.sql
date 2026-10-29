-- name: UpsertGarminKey :one
-- A new key replaces the old one and its token and clears a rejection; the sync
-- interval and cursor are kept.
INSERT INTO garmin_connections (user_id, key_ciphertext, key_expires_on, token_ciphertext, token_expires_at)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (user_id) DO UPDATE SET
    key_ciphertext = EXCLUDED.key_ciphertext,
    key_expires_on = EXCLUDED.key_expires_on,
    token_ciphertext = EXCLUDED.token_ciphertext,
    token_expires_at = EXCLUDED.token_expires_at,
    status = 'connected',
    updated_at = now()
RETURNING *;

-- name: GetGarminConnection :one
SELECT * FROM garmin_connections WHERE user_id = $1;

-- name: SetGarminSyncInterval :one
UPDATE garmin_connections SET sync_interval_h = $2, updated_at = now()
WHERE user_id = $1
RETURNING *;

-- name: DeleteGarminConnection :execrows
DELETE FROM garmin_connections WHERE user_id = $1;

-- name: UpdateGarminToken :exec
UPDATE garmin_connections SET token_ciphertext = $2, token_expires_at = $3, updated_at = now()
WHERE user_id = $1;

-- name: SetGarminKeyRejected :exec
UPDATE garmin_connections SET status = 'key_rejected', updated_at = now() WHERE user_id = $1;

-- name: SetGarminSynced :exec
UPDATE garmin_connections SET synced_through = $2, last_sync_at = now(), last_error = NULL, updated_at = now()
WHERE user_id = $1;

-- name: StartGarminSyncAttempt :exec
UPDATE garmin_connections SET last_attempt_at = now(), updated_at = now() WHERE user_id = $1;

-- name: SetGarminSyncError :exec
UPDATE garmin_connections SET last_error = $2, updated_at = now() WHERE user_id = $1;

-- name: ListDueGarminSyncs :many
-- The scheduler's scan across users, like login the one query not keyed by a
-- user: it returns only the IDs of connected users whose interval has passed and
-- whose backfill, if any, is done.
SELECT c.user_id FROM garmin_connections c
LEFT JOIN garmin_backfills b ON b.user_id = c.user_id
WHERE c.status = 'connected'
    AND (b.user_id IS NULL OR b.status = 'done')
    AND (c.last_attempt_at IS NULL OR c.last_attempt_at + make_interval(hours => c.sync_interval_h) <= now())
ORDER BY c.user_id
LIMIT $1;

-- name: CreateGarminBackfill :one
-- Returns no row when the user already has a backfill.
INSERT INTO garmin_backfills (user_id, window_start, window_end) VALUES ($1, $2, $3)
ON CONFLICT (user_id) DO NOTHING
RETURNING *;

-- name: GetGarminBackfill :one
SELECT * FROM garmin_backfills WHERE user_id = $1;

-- name: SetGarminBackfillListed :exec
UPDATE garmin_backfills SET activities_listed = true, updated_at = now() WHERE user_id = $1;

-- name: SetGarminBackfillHealthThrough :exec
UPDATE garmin_backfills SET health_through = $2, updated_at = now() WHERE user_id = $1;

-- name: PauseGarminBackfill :exec
UPDATE garmin_backfills SET status = 'paused', pause_reason = $2, updated_at = now()
WHERE user_id = $1 AND status = 'running';

-- name: RunGarminBackfill :one
-- Marks an unfinished backfill running again; no row when there is none to run.
UPDATE garmin_backfills b SET status = 'running', pause_reason = NULL, updated_at = now()
WHERE b.user_id = $1 AND (b.status <> 'done' OR EXISTS (
    SELECT 1 FROM garmin_activity_imports i WHERE i.user_id = $1 AND i.status = 'pending'
))
RETURNING *;

-- name: FinishGarminBackfill :execrows
-- Finishes only when nothing is left to import, so a retry queued meanwhile is not lost.
UPDATE garmin_backfills b SET status = 'done', updated_at = now()
WHERE b.user_id = $1 AND b.status = 'running' AND NOT EXISTS (
    SELECT 1 FROM garmin_activity_imports i WHERE i.user_id = $1 AND i.status = 'pending'
);

-- name: AddGarminActivityImport :exec
-- Listing again keeps the row and its outcome.
INSERT INTO garmin_activity_imports (user_id, source_activity_id, start_time, utc_offset_s)
VALUES ($1, $2, $3, $4)
ON CONFLICT (user_id, source_activity_id) DO NOTHING;

-- name: NextGarminActivityImport :one
SELECT * FROM garmin_activity_imports
WHERE user_id = $1 AND status = 'pending'
ORDER BY start_time, source_activity_id
LIMIT 1;

-- name: SetGarminActivityImportDone :exec
UPDATE garmin_activity_imports SET status = 'done', error = NULL, attempts = attempts + 1, updated_at = now()
WHERE user_id = $1 AND source_activity_id = $2;

-- name: SetGarminActivityImportFailed :exec
UPDATE garmin_activity_imports SET status = 'failed', error = $3, attempts = attempts + 1, updated_at = now()
WHERE user_id = $1 AND source_activity_id = $2;

-- name: RetryGarminActivityImports :execrows
UPDATE garmin_activity_imports SET status = 'pending', error = NULL, updated_at = now()
WHERE user_id = $1 AND status = 'failed';

-- name: CountGarminActivityImports :one
SELECT
    count(*)::bigint AS total,
    count(*) FILTER (WHERE status = 'done')::bigint AS done,
    count(*) FILTER (WHERE status = 'failed')::bigint AS failed
FROM garmin_activity_imports WHERE user_id = $1;

-- name: ListGarminActivityImportFailures :many
-- Oldest first; the cursor is the last row's (start_time, source_activity_id).
SELECT source_activity_id, start_time, error FROM garmin_activity_imports
WHERE user_id = sqlc.arg(user_id) AND status = 'failed'
    AND (start_time, source_activity_id) > (sqlc.arg(after_start)::timestamptz, sqlc.arg(after_id)::text)
ORDER BY start_time, source_activity_id
LIMIT sqlc.arg(max_rows);
