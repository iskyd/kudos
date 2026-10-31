-- +goose Up
-- Each activity's duration curve points, computed with its derived values from
-- its edited samples and replaced whole on every recompute. Curves for a period
-- are computed on demand from these rows.
CREATE TABLE curve_points (
    user_id     bigint           NOT NULL,
    activity_id bigint           NOT NULL,
    -- An analytics.CurveKind: power (W, rides) or pace (m/s, runs).
    kind        text             NOT NULL CHECK (kind IN ('power', 'pace')),
    duration_s  integer          NOT NULL CHECK (duration_s > 0),
    value       double precision NOT NULL CHECK (value > 0),
    PRIMARY KEY (user_id, activity_id, kind, duration_s),
    FOREIGN KEY (user_id, activity_id) REFERENCES activities (user_id, id) ON DELETE CASCADE
);
CREATE INDEX curve_points_kind_idx ON curve_points (user_id, kind, duration_s);

-- Threshold pace estimated from runs' best efforts, as a speed.
ALTER TABLE zone_suggestions ADD COLUMN threshold_speed_m_s real CHECK (threshold_speed_m_s > 0);

-- +goose Down
ALTER TABLE zone_suggestions DROP COLUMN threshold_speed_m_s;
DROP TABLE curve_points;
