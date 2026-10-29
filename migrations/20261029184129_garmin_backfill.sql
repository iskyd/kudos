-- +goose Up
-- The onboarding backfill: one per user, worked by a resumable River job.
CREATE TABLE garmin_backfills (
    user_id           bigint      PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    -- Local calendar days, inclusive.
    window_start      date        NOT NULL,
    window_end        date        NOT NULL,
    status            text        NOT NULL DEFAULT 'running' CHECK (status IN ('running', 'paused', 'done')),
    pause_reason      text        CHECK (pause_reason IN ('rate_limited', 'key_rejected', 'disconnected')),
    -- True once the window's activities are in garmin_activity_imports.
    activities_listed boolean     NOT NULL DEFAULT false,
    -- Health cursor: the last day whose metrics are stored.
    health_through    date,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),
    CHECK (window_end >= window_start AND window_end - window_start < 366),
    CHECK ((status = 'paused') = (pause_reason IS NOT NULL))
);

-- Garmin activities to import, one row per Garmin activity ID: the backfill's
-- work list and its failure list.
CREATE TABLE garmin_activity_imports (
    user_id            bigint      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    source_activity_id text        NOT NULL,
    start_time         timestamptz NOT NULL,
    -- Garmin's local offset for the activity, the fallback when the file has none.
    utc_offset_s       integer,
    status             text        NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'done', 'failed')),
    -- Why the last attempt failed; never holds secrets.
    error              text,
    attempts           integer     NOT NULL DEFAULT 0,
    updated_at         timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, source_activity_id),
    CHECK ((status = 'failed') = (error IS NOT NULL))
);

CREATE INDEX garmin_activity_imports_status_idx ON garmin_activity_imports (user_id, status, start_time, source_activity_id);

-- +goose Down
DROP TABLE garmin_activity_imports;
DROP TABLE garmin_backfills;
