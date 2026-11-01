-- name: PutWellnessDay :one
-- Replaces the day's check-in and body metrics.
INSERT INTO wellness_days (
    user_id, day, soreness, sleep_quality, stress, motivation, note, weight_kg, body_fat_pct, hydration_l
) VALUES (
    @user_id, @day, @soreness, @sleep_quality, @stress, @motivation, @note, @weight_kg, @body_fat_pct, @hydration_l
)
ON CONFLICT (user_id, day) DO UPDATE SET
    soreness = EXCLUDED.soreness, sleep_quality = EXCLUDED.sleep_quality, stress = EXCLUDED.stress,
    motivation = EXCLUDED.motivation, note = EXCLUDED.note, weight_kg = EXCLUDED.weight_kg,
    body_fat_pct = EXCLUDED.body_fat_pct, hydration_l = EXCLUDED.hydration_l, updated_at = now()
RETURNING *;

-- name: DeleteWellnessDay :execrows
DELETE FROM wellness_days WHERE user_id = @user_id AND day = @day;

-- name: ListWellnessDays :many
-- One page of days, latest first; before is the previous page's last day.
SELECT * FROM wellness_days
WHERE user_id = @user_id AND (sqlc.narg(before)::date IS NULL OR day < sqlc.narg(before)::date)
ORDER BY day DESC
LIMIT @max_rows;

-- name: CreateHealthLogEntry :one
INSERT INTO health_log (user_id, kind, description, start_date, end_date)
VALUES (@user_id, @kind, @description, @start_date, @end_date)
RETURNING *;

-- name: UpdateHealthLogEntry :one
UPDATE health_log SET kind = @kind, description = @description, start_date = @start_date, end_date = @end_date
WHERE user_id = @user_id AND id = @id
RETURNING *;

-- name: DeleteHealthLogEntry :execrows
DELETE FROM health_log WHERE user_id = @user_id AND id = @id;

-- name: ListHealthLog :many
-- One page of entries, latest start first; after_start and after_id are the
-- previous page's last row.
SELECT * FROM health_log
WHERE user_id = @user_id
    AND (sqlc.narg(kind)::text IS NULL OR kind = sqlc.narg(kind))
    AND (sqlc.narg(after_start)::date IS NULL
        OR (start_date, id) < (sqlc.narg(after_start)::date, sqlc.narg(after_id)::bigint))
ORDER BY start_date DESC, id DESC
LIMIT @max_rows;

-- name: GetWellnessDay :one
SELECT * FROM wellness_days WHERE user_id = @user_id AND day = @day;

-- name: ListHealthLogOn :many
-- Illness and injury entries covering the day, latest start first.
SELECT * FROM health_log
WHERE user_id = @user_id AND kind IN ('illness', 'injury')
    AND start_date <= @day::date AND (end_date IS NULL OR end_date >= @day::date)
ORDER BY start_date DESC, id DESC
LIMIT @max_rows;

-- name: ListWellnessDaysBetween :many
-- The days from from_day to to_day (inclusive) with anything entered, oldest first.
SELECT * FROM wellness_days
WHERE user_id = @user_id AND day BETWEEN @from_day::date AND @to_day::date
ORDER BY day
LIMIT @max_rows;

-- name: ListHealthLogBetween :many
-- Illness and injury entries overlapping from_day to to_day (inclusive; an open
-- end is ongoing), earliest start first.
SELECT * FROM health_log
WHERE user_id = @user_id AND kind IN ('illness', 'injury')
    AND start_date <= @to_day::date AND (end_date IS NULL OR end_date >= @from_day::date)
ORDER BY start_date, id
LIMIT @max_rows;
