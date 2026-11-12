-- +goose Up
-- Whether planned workouts are matched to activities automatically.
ALTER TABLE users ADD COLUMN auto_match_workouts boolean NOT NULL DEFAULT true;

ALTER TABLE planned_workouts ADD CONSTRAINT planned_workouts_user_id_id_key UNIQUE (user_id, id);

-- The user's own say on which activity completed a planned workout: an
-- activity, or none (unlinked). It wins over automatic matching, which is
-- computed when read and never stored.
CREATE TABLE planned_workout_links (
    user_id            bigint      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    planned_workout_id bigint      PRIMARY KEY,
    activity_id        bigint,
    created_at         timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (user_id, planned_workout_id) REFERENCES planned_workouts (user_id, id) ON DELETE CASCADE,
    FOREIGN KEY (user_id, activity_id) REFERENCES activities (user_id, id) ON DELETE CASCADE
);
-- An activity completes at most one planned workout.
CREATE UNIQUE INDEX planned_workout_links_activity_idx ON planned_workout_links (user_id, activity_id) WHERE activity_id IS NOT NULL;

-- +goose Down
DROP TABLE planned_workout_links;
ALTER TABLE planned_workouts DROP CONSTRAINT planned_workouts_user_id_id_key;
ALTER TABLE users DROP COLUMN auto_match_workouts;
