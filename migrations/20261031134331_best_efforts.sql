-- +goose Up
-- Each activity's best efforts, computed with its derived values from its edited
-- samples and replaced whole on every recompute. Records and their history are
-- computed on demand from these rows.
CREATE TABLE best_efforts (
    user_id     bigint           NOT NULL,
    activity_id bigint           NOT NULL,
    -- An analytics.EffortName.
    effort      text             NOT NULL,
    -- Seconds for fastest distances, metres for longest, watts for power.
    value       double precision NOT NULL CHECK (value > 0),
    PRIMARY KEY (user_id, activity_id, effort),
    FOREIGN KEY (user_id, activity_id) REFERENCES activities (user_id, id) ON DELETE CASCADE
);
CREATE INDEX best_efforts_effort_idx ON best_efforts (user_id, effort);

-- +goose Down
DROP TABLE best_efforts;
