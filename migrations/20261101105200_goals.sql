-- +goose Up
-- Volume goals for one week (from Monday), month or year, e.g. 2,000 km run
-- in 2026. Progress is summed from the activities on read, never stored.
CREATE TABLE goals (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id    bigint           NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    period     text             NOT NULL CHECK (period IN ('week', 'month', 'year')),
    -- The period's first local day.
    start_day  date             NOT NULL CHECK (start_day = date_trunc(period, start_day)::date),
    -- Null counts every sport.
    sport      text,
    metric     text             NOT NULL CHECK (metric IN ('distance', 'duration', 'ascent')),
    -- In SI units: metres or seconds.
    target     double precision NOT NULL CHECK (target > 0),
    created_at timestamptz      NOT NULL DEFAULT now()
);
CREATE INDEX goals_user_idx ON goals (user_id, id);

-- +goose Down
DROP TABLE goals;
