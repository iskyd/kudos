-- +goose Up
-- Running and swim analytics per activity, computed with its derived values.
ALTER TABLE activity_derived
    -- Runs: Friel's efficiency factor (metres per heartbeat), aerobic decoupling
    -- (%) and time-weighted running dynamics in metres and seconds.
    ADD COLUMN efficiency_m_per_beat      double precision,
    ADD COLUMN decoupling_pct             double precision,
    ADD COLUMN avg_vertical_oscillation_m double precision,
    ADD COLUMN avg_stance_time_s          double precision,
    ADD COLUMN avg_step_length_m          double precision,
    -- Pool swims: mean SWOLF of the active lengths.
    ADD COLUMN avg_swolf                  double precision;

-- +goose Down
ALTER TABLE activity_derived
    DROP COLUMN efficiency_m_per_beat,
    DROP COLUMN decoupling_pct,
    DROP COLUMN avg_vertical_oscillation_m,
    DROP COLUMN avg_stance_time_s,
    DROP COLUMN avg_step_length_m,
    DROP COLUMN avg_swolf;
