-- +goose Up
-- User edits, one row each so each can be undone. Times in params are absolute (UTC).
CREATE TABLE activity_edits (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id     bigint      NOT NULL,
    activity_id bigint      NOT NULL,
    kind        text        NOT NULL CHECK (kind IN ('sport', 'trim', 'exclude_hr', 'merge', 'split')),
    params      jsonb       NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (user_id, activity_id) REFERENCES activities (user_id, id) ON DELETE CASCADE
);
CREATE INDEX activity_edits_activity_idx ON activity_edits (user_id, activity_id, id);

-- A merged activity points at the one that absorbed it and leaves the list.
-- A split creates a second activity (source 'split') that reads its parent's samples.
ALTER TABLE activities
    ADD COLUMN merged_into bigint,
    ADD COLUMN split_of bigint,
    ADD CONSTRAINT activities_merged_into_fk FOREIGN KEY (user_id, merged_into)
        REFERENCES activities (user_id, id) ON DELETE SET NULL (merged_into),
    ADD CONSTRAINT activities_split_of_fk FOREIGN KEY (user_id, split_of)
        REFERENCES activities (user_id, id) ON DELETE CASCADE,
    DROP CONSTRAINT activities_source_check,
    ADD CONSTRAINT activities_source_check CHECK (source IN ('garmin', 'upload', 'manual', 'split'));

COMMENT ON COLUMN activities.overrides IS
    'Effective values computed from activity_edits and the samples (sport, start, summary); recorded columns stay as recorded.';

-- +goose Down
COMMENT ON COLUMN activities.overrides IS NULL;
DELETE FROM activities WHERE source = 'split';
ALTER TABLE activities
    DROP CONSTRAINT activities_source_check,
    ADD CONSTRAINT activities_source_check CHECK (source IN ('garmin', 'upload', 'manual')),
    DROP CONSTRAINT activities_split_of_fk,
    DROP CONSTRAINT activities_merged_into_fk,
    DROP COLUMN split_of,
    DROP COLUMN merged_into;
DROP TABLE activity_edits;
