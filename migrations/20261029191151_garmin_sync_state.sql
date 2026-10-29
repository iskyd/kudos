-- +goose Up
-- Ongoing sync state. last_attempt_at paces the scheduler even when syncs fail;
-- last_error explains the last failed sync and is cleared by the next success.
ALTER TABLE garmin_connections
    ADD COLUMN last_attempt_at timestamptz,
    ADD COLUMN last_error      text;

-- +goose Down
ALTER TABLE garmin_connections
    DROP COLUMN last_error,
    DROP COLUMN last_attempt_at;
