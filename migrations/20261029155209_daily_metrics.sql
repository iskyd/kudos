-- +goose Up
-- One row per user and local calendar day, as Garmin reports it. SI units, except
-- percentages (0–100), scores (0–100) and hydration in litres. Manual body metrics
-- live in their own table.
CREATE TABLE daily_metrics (
    user_id          bigint NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    day              date   NOT NULL,
    resting_hr_bpm   smallint CHECK (resting_hr_bpm > 0),
    -- Overnight average.
    hrv_ms           real     CHECK (hrv_ms > 0),
    sleep_s          integer  CHECK (sleep_s >= 0),
    sleep_deep_s     integer  CHECK (sleep_deep_s >= 0),
    sleep_light_s    integer  CHECK (sleep_light_s >= 0),
    sleep_rem_s      integer  CHECK (sleep_rem_s >= 0),
    sleep_awake_s    integer  CHECK (sleep_awake_s >= 0),
    sleep_score      smallint CHECK (sleep_score BETWEEN 0 AND 100),
    -- Daily average.
    stress           smallint CHECK (stress BETWEEN 0 AND 100),
    body_battery_max smallint CHECK (body_battery_max BETWEEN 0 AND 100),
    body_battery_min smallint CHECK (body_battery_min BETWEEN 0 AND 100),
    steps            integer  CHECK (steps >= 0),
    weight_kg        real     CHECK (weight_kg > 0),
    body_fat_pct     real     CHECK (body_fat_pct BETWEEN 0 AND 100),
    body_water_pct   real     CHECK (body_water_pct BETWEEN 0 AND 100),
    muscle_mass_kg   real     CHECK (muscle_mass_kg > 0),
    bone_mass_kg     real     CHECK (bone_mass_kg > 0),
    -- Daily average.
    spo2_pct         real     CHECK (spo2_pct BETWEEN 0 AND 100),
    hydration_l      real     CHECK (hydration_l >= 0),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    UNIQUE (user_id, day)
);

-- A few hundred rows per user and year: yearly chunks, no compression.
SELECT create_hypertable('daily_metrics', by_range('day', INTERVAL '1 year'));

-- +goose Down
DROP TABLE daily_metrics;
