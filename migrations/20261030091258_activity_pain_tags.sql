-- +goose Up
-- Injury or pain tags the user attaches to an activity ("left knee"), stored
-- trimmed and lowercase. User-owned: re-imports never touch them.
ALTER TABLE activities ADD COLUMN pain_tags text[] NOT NULL DEFAULT '{}';
CREATE INDEX activities_pain_tags_idx ON activities USING gin (pain_tags);

-- +goose Down
DROP INDEX activities_pain_tags_idx;
ALTER TABLE activities DROP COLUMN pain_tags;
