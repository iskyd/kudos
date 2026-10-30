-- name: CreateZoneSet :one
INSERT INTO zone_sets (
    user_id, sport, effective_from, lthr_bpm, threshold_speed_m_s, ftp_w, css_m_s, max_hr_bpm, resting_hr_bpm
) VALUES (
    @user_id, @sport, @effective_from, @lthr_bpm, @threshold_speed_m_s, @ftp_w, @css_m_s, @max_hr_bpm, @resting_hr_bpm
)
RETURNING *;

-- name: DeleteZoneSet :execrows
DELETE FROM zone_sets WHERE user_id = @user_id AND id = @id;

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
