-- name: CreateZoneSet :one
INSERT INTO zone_sets (
    user_id, sport, effective_from, lthr_bpm, threshold_speed_m_s, ftp_w, css_m_s, max_hr_bpm, resting_hr_bpm
) VALUES (
    @user_id, @sport, @effective_from, @lthr_bpm, @threshold_speed_m_s, @ftp_w, @css_m_s, @max_hr_bpm, @resting_hr_bpm
)
RETURNING *;

-- name: DeleteZoneSet :one
-- Returns the deleted version's date, from which activities fall back to the version before.
DELETE FROM zone_sets WHERE user_id = @user_id AND id = @id
RETURNING effective_from;

-- name: ListZoneSets :many
-- One page of versions, latest effective date first; after_from and after_id are
-- the previous page's last row.
SELECT * FROM zone_sets
WHERE user_id = @user_id
    AND (sqlc.narg(sport)::text IS NULL OR sport = sqlc.narg(sport))
    AND (sqlc.narg(after_from)::date IS NULL
        OR (effective_from, id) < (sqlc.narg(after_from)::date, sqlc.narg(after_id)::bigint))
ORDER BY effective_from DESC, id DESC
LIMIT @max_rows;

-- name: ListZoneSetsAt :many
-- The version of each sport valid on day: the latest one effective on or before it.
SELECT DISTINCT ON (sport) * FROM zone_sets
WHERE user_id = @user_id AND effective_from <= @day::date
ORDER BY sport, effective_from DESC
LIMIT 4;

-- name: GetZoneSetAt :one
-- The version of one sport valid on day.
SELECT * FROM zone_sets
WHERE user_id = @user_id AND sport = @sport AND effective_from <= @day::date
ORDER BY effective_from DESC
LIMIT 1;

-- name: GetZoneSuggestionAt :one
-- The latest suggestion of a sport, pending or dismissed, on or before day.
SELECT * FROM zone_suggestions
WHERE user_id = @user_id AND sport = @sport AND effective_from <= @day::date
ORDER BY effective_from DESC
LIMIT 1;

-- name: GetNextZoneSuggestion :one
-- The earliest suggestion of a sport after day.
SELECT * FROM zone_suggestions
WHERE user_id = @user_id AND sport = @sport AND effective_from > @day::date
ORDER BY effective_from
LIMIT 1;

-- name: InsertZoneSuggestion :exec
-- The first file of a day wins: another one on the same day changes nothing.
INSERT INTO zone_suggestions (user_id, sport, effective_from, lthr_bpm, ftp_w, max_hr_bpm, resting_hr_bpm, activity_id)
VALUES (@user_id, @sport, @effective_from, @lthr_bpm, @ftp_w, @max_hr_bpm, @resting_hr_bpm, @activity_id)
ON CONFLICT (user_id, sport, effective_from) DO NOTHING;

-- name: SuggestEstimate :exec
-- An estimate joins a pending suggestion already on its day; a dismissed one
-- keeps the day.
INSERT INTO zone_suggestions (user_id, sport, effective_from, ftp_w, threshold_speed_m_s, activity_id)
VALUES (@user_id, @sport, @effective_from, @ftp_w, @threshold_speed_m_s, @activity_id)
ON CONFLICT (user_id, sport, effective_from) DO UPDATE SET
    ftp_w = coalesce(EXCLUDED.ftp_w, zone_suggestions.ftp_w),
    threshold_speed_m_s = coalesce(EXCLUDED.threshold_speed_m_s, zone_suggestions.threshold_speed_m_s)
WHERE zone_suggestions.status = 'pending';

-- name: MoveZoneSuggestion :exec
-- An older file with the same values moves a suggestion back to its date.
UPDATE zone_suggestions SET effective_from = @effective_from, activity_id = @activity_id
WHERE user_id = @user_id AND id = @id;

-- name: ListZoneSuggestions :many
-- One page of pending suggestions, latest first.
SELECT * FROM zone_suggestions
WHERE user_id = @user_id AND status = 'pending'
    AND (sqlc.narg(after_from)::date IS NULL
        OR (effective_from, id) < (sqlc.narg(after_from)::date, sqlc.narg(after_id)::bigint))
ORDER BY effective_from DESC, id DESC
LIMIT @max_rows;

-- name: GetPendingZoneSuggestion :one
SELECT * FROM zone_suggestions WHERE user_id = @user_id AND id = @id AND status = 'pending';

-- name: DismissZoneSuggestion :execrows
UPDATE zone_suggestions SET status = 'dismissed' WHERE user_id = @user_id AND id = @id AND status = 'pending';

-- name: ConfirmZoneSuggestion :one
-- Turns a pending suggestion into a zone version in one statement: the version
-- is created only if the suggestion was pending, and the suggestion goes only
-- if the version could be created.
WITH confirmed AS (
    DELETE FROM zone_suggestions
    WHERE zone_suggestions.user_id = @user_id AND zone_suggestions.id = @id AND zone_suggestions.status = 'pending'
    RETURNING zone_suggestions.user_id
)
INSERT INTO zone_sets (
    user_id, sport, effective_from, lthr_bpm, threshold_speed_m_s, ftp_w, css_m_s, max_hr_bpm, resting_hr_bpm
)
SELECT confirmed.user_id, @sport, @effective_from, @lthr_bpm, @threshold_speed_m_s, @ftp_w, @css_m_s, @max_hr_bpm, @resting_hr_bpm
FROM confirmed
RETURNING *;
