-- +goose Up
-- One row per user and local day of what the user enters: the check-in, scored
-- 1–5 (soreness and stress 5 = worst, sleep quality and motivation 5 = best),
-- and manual body metrics, kept apart from Garmin's daily_metrics so a sync
-- never overwrites them.
CREATE TABLE wellness_days (
    user_id        bigint   NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    day            date     NOT NULL,
    soreness       smallint CHECK (soreness BETWEEN 1 AND 5),
    sleep_quality  smallint CHECK (sleep_quality BETWEEN 1 AND 5),
    stress         smallint CHECK (stress BETWEEN 1 AND 5),
    motivation     smallint CHECK (motivation BETWEEN 1 AND 5),
    note           text     CHECK (char_length(note) BETWEEN 1 AND 2000),
    weight_kg      real     CHECK (weight_kg > 0),
    body_fat_pct   real     CHECK (body_fat_pct BETWEEN 0 AND 100),
    hydration_l    real     CHECK (hydration_l >= 0),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, day),
    CHECK (num_nonnulls(soreness, sleep_quality, stress, motivation, note, weight_kg, body_fat_pct, hydration_l) > 0)
);

-- Injuries, illnesses and menstrual periods with their dates; an open end date
-- means ongoing. The LLM always receives the whole log as context.
CREATE TABLE health_log (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id     bigint NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    kind        text   NOT NULL CHECK (kind IN ('injury', 'illness', 'period')),
    description text   CHECK (char_length(description) BETWEEN 1 AND 2000),
    start_date  date   NOT NULL,
    end_date    date   CHECK (end_date >= start_date),
    created_at  timestamptz NOT NULL DEFAULT now(),
    CHECK (kind = 'period' OR description IS NOT NULL)
);
CREATE INDEX health_log_user_start_idx ON health_log (user_id, start_date DESC, id DESC);

-- +goose Down
DROP TABLE health_log;
DROP TABLE wellness_days;
