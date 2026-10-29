-- name: UpsertOriginalFile :one
-- The same file uploaded twice returns the existing row unchanged.
INSERT INTO original_files (user_id, format, sha256, size_bytes, path)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (user_id, sha256) DO UPDATE SET sha256 = EXCLUDED.sha256
RETURNING *;

-- name: GetOriginalFile :one
SELECT * FROM original_files WHERE user_id = $1 AND id = $2;

-- name: UpsertMultisportGroup :one
INSERT INTO multisport_groups (user_id, source, source_group_id)
VALUES ($1, $2, $3)
ON CONFLICT (user_id, source, source_group_id) DO UPDATE SET source_group_id = EXCLUDED.source_group_id
RETURNING id;

-- name: UpsertActivity :one
-- Re-importing updates what the source owns and keeps what the user owns
-- (rpe, feel, notes, weather, overrides).
INSERT INTO activities (
    user_id, source, source_activity_id, sport, subtype, start_time, utc_offset_s,
    original_file_id, multisport_group_id,
    elapsed_time_s, timer_time_s, distance_m, ascent_m, descent_m,
    avg_speed_m_s, max_speed_m_s, avg_hr_bpm, max_hr_bpm, avg_power_w, max_power_w,
    avg_cadence, max_cadence, avg_temperature_c, pool_length_m, calories_kcal, rpe, feel
) VALUES (
    $1, $2, $3, $4, $5, $6, $7,
    $8, $9,
    $10, $11, $12, $13, $14,
    $15, $16, $17, $18, $19, $20,
    $21, $22, $23, $24, $25, $26, $27
)
ON CONFLICT (user_id, source, source_activity_id) DO UPDATE SET
    sport = EXCLUDED.sport,
    subtype = EXCLUDED.subtype,
    start_time = EXCLUDED.start_time,
    utc_offset_s = EXCLUDED.utc_offset_s,
    original_file_id = EXCLUDED.original_file_id,
    multisport_group_id = EXCLUDED.multisport_group_id,
    elapsed_time_s = EXCLUDED.elapsed_time_s,
    timer_time_s = EXCLUDED.timer_time_s,
    distance_m = EXCLUDED.distance_m,
    ascent_m = EXCLUDED.ascent_m,
    descent_m = EXCLUDED.descent_m,
    avg_speed_m_s = EXCLUDED.avg_speed_m_s,
    max_speed_m_s = EXCLUDED.max_speed_m_s,
    avg_hr_bpm = EXCLUDED.avg_hr_bpm,
    max_hr_bpm = EXCLUDED.max_hr_bpm,
    avg_power_w = EXCLUDED.avg_power_w,
    max_power_w = EXCLUDED.max_power_w,
    avg_cadence = EXCLUDED.avg_cadence,
    max_cadence = EXCLUDED.max_cadence,
    avg_temperature_c = EXCLUDED.avg_temperature_c,
    pool_length_m = EXCLUDED.pool_length_m,
    calories_kcal = EXCLUDED.calories_kcal
RETURNING id;

-- name: GetActivity :one
SELECT * FROM activities WHERE user_id = $1 AND id = $2;

-- name: ListActivities :many
-- Newest first. Pass the last row's start_time and id to get the next page.
SELECT * FROM activities
WHERE user_id = @user_id AND (start_time, id) < (@before_start::timestamptz, @before_id::bigint)
ORDER BY start_time DESC, id DESC
LIMIT @max_rows;

-- name: InsertActivityStreams :copyfrom
INSERT INTO activity_streams (
    user_id, activity_id, time, lat_deg, lon_deg, altitude_m, hr_bpm, power_w,
    cadence, speed_m_s, distance_m, temperature_c, extras
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13);

-- name: DeleteActivityStreams :exec
DELETE FROM activity_streams WHERE user_id = $1 AND activity_id = $2;

-- name: ListActivityStreams :many
SELECT * FROM activity_streams
WHERE user_id = @user_id AND activity_id = @activity_id AND time > @after::timestamptz
ORDER BY time
LIMIT @max_rows;

-- name: InsertLaps :copyfrom
INSERT INTO laps (
    user_id, activity_id, lap_index, start_time, elapsed_time_s, timer_time_s, distance_m,
    ascent_m, descent_m, avg_speed_m_s, max_speed_m_s, avg_hr_bpm, max_hr_bpm,
    avg_power_w, max_power_w, avg_cadence, intensity, workout_step_index
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18);

-- name: DeleteLaps :exec
DELETE FROM laps WHERE user_id = $1 AND activity_id = $2;

-- name: ListLaps :many
SELECT * FROM laps
WHERE user_id = @user_id AND activity_id = @activity_id AND lap_index > @after::integer
ORDER BY lap_index
LIMIT @max_rows;

-- name: InsertSwimLengths :copyfrom
INSERT INTO swim_lengths (
    user_id, activity_id, length_index, start_time, elapsed_time_s, timer_time_s,
    active, stroke, strokes, avg_speed_m_s
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10);

-- name: DeleteSwimLengths :exec
DELETE FROM swim_lengths WHERE user_id = $1 AND activity_id = $2;

-- name: ListSwimLengths :many
SELECT * FROM swim_lengths
WHERE user_id = @user_id AND activity_id = @activity_id AND length_index > @after::integer
ORDER BY length_index
LIMIT @max_rows;

-- name: InsertStrengthSets :copyfrom
INSERT INTO strength_sets (
    user_id, activity_id, set_index, start_time, duration_s, active, reps, weight_kg,
    exercise_category, exercise_number, exercise_name, workout_step_index
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12);

-- name: DeleteStrengthSets :exec
DELETE FROM strength_sets WHERE user_id = $1 AND activity_id = $2;

-- name: ListStrengthSets :many
SELECT * FROM strength_sets
WHERE user_id = @user_id AND activity_id = @activity_id AND set_index > @after::integer
ORDER BY set_index
LIMIT @max_rows;

-- name: UpsertRecordedWorkout :exec
-- The WHERE makes a conflicting row of another user a no-op instead of an overwrite.
INSERT INTO recorded_workouts (user_id, activity_id, name, steps)
VALUES ($1, $2, $3, $4)
ON CONFLICT (activity_id) DO UPDATE SET name = EXCLUDED.name, steps = EXCLUDED.steps
WHERE recorded_workouts.user_id = EXCLUDED.user_id;

-- name: GetRecordedWorkout :one
SELECT * FROM recorded_workouts WHERE user_id = $1 AND activity_id = $2;

-- name: UpsertActivityThresholds :exec
-- The WHERE makes a conflicting row of another user a no-op instead of an overwrite.
INSERT INTO activity_thresholds (
    user_id, activity_id, ftp_w, threshold_hr_bpm, max_hr_bpm, resting_hr_bpm, hr_zones_bpm, power_zones_w
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (activity_id) DO UPDATE SET
    ftp_w = EXCLUDED.ftp_w,
    threshold_hr_bpm = EXCLUDED.threshold_hr_bpm,
    max_hr_bpm = EXCLUDED.max_hr_bpm,
    resting_hr_bpm = EXCLUDED.resting_hr_bpm,
    hr_zones_bpm = EXCLUDED.hr_zones_bpm,
    power_zones_w = EXCLUDED.power_zones_w
WHERE activity_thresholds.user_id = EXCLUDED.user_id;

-- name: GetActivityThresholds :one
SELECT * FROM activity_thresholds WHERE user_id = $1 AND activity_id = $2;
