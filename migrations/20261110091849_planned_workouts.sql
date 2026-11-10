-- +goose Up
-- A plan groups the workouts one approved proposal put in place.
CREATE TABLE plans (
    id         bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id    bigint      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (user_id, id)
);

-- Workouts planned for a local day. steps is the PlanStep array of
-- api/openapi.yaml, validated (internal/plans) before it is stored.
CREATE TABLE planned_workouts (
    id          bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id     bigint      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    plan_id     bigint      NOT NULL,
    day         date        NOT NULL,
    sport       text        NOT NULL,
    title       text        NOT NULL CHECK (char_length(title) BETWEEN 1 AND 200),
    description text        CHECK (char_length(description) <= 2000),
    steps       jsonb       NOT NULL CHECK (jsonb_typeof(steps) = 'array'),
    created_at  timestamptz NOT NULL DEFAULT now(),
    -- A workout belongs to a plan of the same user.
    FOREIGN KEY (user_id, plan_id) REFERENCES plans (user_id, id) ON DELETE CASCADE
);
CREATE INDEX planned_workouts_user_day_idx ON planned_workouts (user_id, day, id);

-- +goose Down
DROP TABLE planned_workouts;
DROP TABLE plans;
