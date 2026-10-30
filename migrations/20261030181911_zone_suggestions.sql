-- +goose Up
-- Thresholds a recorded file carried that differ from the zone version valid on
-- its date, waiting for the user to confirm (which creates a zone version and
-- deletes the suggestion) or dismiss (kept, so the same values are not asked again).
-- Runs suggest running versions, rides cycling ones, everything else general ones.
CREATE TABLE zone_suggestions (
    id             bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id        bigint   NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    sport          text     NOT NULL CHECK (sport IN ('running', 'cycling', 'general')),
    effective_from date     NOT NULL,
    lthr_bpm       smallint,
    ftp_w          smallint,
    max_hr_bpm     smallint,
    resting_hr_bpm smallint,
    -- The activity whose file first carried these values.
    activity_id    bigint,
    status         text     NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'dismissed')),
    created_at     timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (user_id, activity_id) REFERENCES activities (user_id, id) ON DELETE SET NULL (activity_id),
    UNIQUE (user_id, sport, effective_from)
);

-- +goose Down
DROP TABLE zone_suggestions;
