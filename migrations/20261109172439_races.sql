-- +goose Up
-- Races the athlete plans to run, ride or swim. day is the local race day;
-- priority A is the season's goal, B a tune-up worth racing, C training.
CREATE TABLE races (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id     bigint           NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name        text             NOT NULL CHECK (char_length(name) BETWEEN 1 AND 200),
    day         date             NOT NULL,
    sport       text             NOT NULL,
    -- SI units: metres and seconds.
    distance_m  double precision NOT NULL CHECK (distance_m > 0),
    goal_time_s double precision CHECK (goal_time_s > 0),
    priority    text             NOT NULL CHECK (priority IN ('A', 'B', 'C')),
    created_at  timestamptz      NOT NULL DEFAULT now()
);
CREATE INDEX races_user_day_idx ON races (user_id, day, id);

-- +goose Down
DROP TABLE races;
