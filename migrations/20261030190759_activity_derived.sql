-- +goose Up
-- Values our analytics compute per activity from its edited samples and the
-- zone versions valid on its date (invariant 23). A zone change marks the rows
-- it affects stale; a background job recomputes stale rows, rows of an older
-- version of the computation, and activities without a row.
CREATE TABLE activity_derived (
    user_id             bigint      NOT NULL,
    activity_id         bigint      PRIMARY KEY,
    version             smallint    NOT NULL,
    stale               boolean     NOT NULL DEFAULT false,
    computed_at         timestamptz NOT NULL DEFAULT now(),
    -- The zone versions used, if any; informational, not keys.
    zone_set_id         bigint,
    general_zone_set_id bigint,
    -- Time-weighted over the samples; speed is distance over timer time when recorded.
    avg_hr_bpm          double precision,
    avg_power_w         double precision,
    avg_speed_m_s       double precision,
    avg_cadence         double precision,
    -- Seconds per zone, in the zones' order, with the model they come from.
    hr_zone_model       text,
    hr_zone_s           double precision[],
    pace_zone_model     text,
    pace_zone_s         double precision[],
    power_zone_model    text,
    power_zone_s        double precision[],
    FOREIGN KEY (user_id, activity_id) REFERENCES activities (user_id, id) ON DELETE CASCADE
);
CREATE INDEX activity_derived_stale_idx ON activity_derived (user_id, activity_id) WHERE stale;

-- +goose Down
DROP TABLE activity_derived;
