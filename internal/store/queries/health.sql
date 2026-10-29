-- name: TimescaleVersion :one
SELECT extversion FROM pg_catalog.pg_extension WHERE extname = 'timescaledb';

-- name: UpsertDailyMetrics :exec
-- Values the source sends replace stored ones; values it leaves out (nil) are kept,
-- so a day can be filled from several endpoints.
INSERT INTO daily_metrics (
    user_id, day, resting_hr_bpm, hrv_ms, sleep_s, sleep_deep_s, sleep_light_s, sleep_rem_s,
    sleep_awake_s, sleep_score, stress, body_battery_max, body_battery_min, steps, weight_kg,
    body_fat_pct, body_water_pct, muscle_mass_kg, bone_mass_kg, spo2_pct, hydration_l
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21
)
ON CONFLICT (user_id, day) DO UPDATE SET
    resting_hr_bpm = COALESCE(EXCLUDED.resting_hr_bpm, daily_metrics.resting_hr_bpm),
    hrv_ms = COALESCE(EXCLUDED.hrv_ms, daily_metrics.hrv_ms),
    sleep_s = COALESCE(EXCLUDED.sleep_s, daily_metrics.sleep_s),
    sleep_deep_s = COALESCE(EXCLUDED.sleep_deep_s, daily_metrics.sleep_deep_s),
    sleep_light_s = COALESCE(EXCLUDED.sleep_light_s, daily_metrics.sleep_light_s),
    sleep_rem_s = COALESCE(EXCLUDED.sleep_rem_s, daily_metrics.sleep_rem_s),
    sleep_awake_s = COALESCE(EXCLUDED.sleep_awake_s, daily_metrics.sleep_awake_s),
    sleep_score = COALESCE(EXCLUDED.sleep_score, daily_metrics.sleep_score),
    stress = COALESCE(EXCLUDED.stress, daily_metrics.stress),
    body_battery_max = COALESCE(EXCLUDED.body_battery_max, daily_metrics.body_battery_max),
    body_battery_min = COALESCE(EXCLUDED.body_battery_min, daily_metrics.body_battery_min),
    steps = COALESCE(EXCLUDED.steps, daily_metrics.steps),
    weight_kg = COALESCE(EXCLUDED.weight_kg, daily_metrics.weight_kg),
    body_fat_pct = COALESCE(EXCLUDED.body_fat_pct, daily_metrics.body_fat_pct),
    body_water_pct = COALESCE(EXCLUDED.body_water_pct, daily_metrics.body_water_pct),
    muscle_mass_kg = COALESCE(EXCLUDED.muscle_mass_kg, daily_metrics.muscle_mass_kg),
    bone_mass_kg = COALESCE(EXCLUDED.bone_mass_kg, daily_metrics.bone_mass_kg),
    spo2_pct = COALESCE(EXCLUDED.spo2_pct, daily_metrics.spo2_pct),
    hydration_l = COALESCE(EXCLUDED.hydration_l, daily_metrics.hydration_l),
    updated_at = now();

-- name: GetDailyMetrics :one
SELECT * FROM daily_metrics WHERE user_id = $1 AND day = $2;

-- name: ListDailyMetrics :many
-- Oldest first, from the day after @after up to @until. Pass the last row's day as @after for the next page.
SELECT * FROM daily_metrics
WHERE user_id = @user_id AND day > @after::date AND day <= @until::date
ORDER BY day
LIMIT @max_rows;
