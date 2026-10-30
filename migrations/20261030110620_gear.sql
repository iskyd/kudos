-- +goose Up
-- Shoes and bikes. Distance and time are summed from the assigned activities
-- on read, never stored.
CREATE TABLE gear (
    id                     bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id                bigint      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    kind                   text        NOT NULL CHECK (kind IN ('shoes', 'bike')),
    name                   text        NOT NULL CHECK (char_length(name) BETWEEN 1 AND 80),
    -- Distance at which the user wants to replace it; null for no warning.
    replacement_distance_m double precision CHECK (replacement_distance_m > 0),
    retired                boolean     NOT NULL DEFAULT false,
    created_at             timestamptz NOT NULL DEFAULT now(),
    UNIQUE (user_id, id)
);

-- The composite key keeps an activity from pointing at another user's gear.
-- Deleting gear unassigns it.
ALTER TABLE activities ADD COLUMN gear_id bigint,
    ADD CONSTRAINT activities_gear_fk FOREIGN KEY (user_id, gear_id)
        REFERENCES gear (user_id, id) ON DELETE SET NULL (gear_id);
CREATE INDEX activities_gear_idx ON activities (user_id, gear_id) WHERE gear_id IS NOT NULL;

-- +goose Down
DROP INDEX activities_gear_idx;
ALTER TABLE activities DROP CONSTRAINT activities_gear_fk, DROP COLUMN gear_id;
DROP TABLE gear;
