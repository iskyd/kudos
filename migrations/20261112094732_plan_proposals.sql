-- +goose Up
-- A plan the LLM proposed for the local days from_day to to_day. A job fills
-- in a generating proposal; plan_id is the plan its approved additions join,
-- created on the first approval.
CREATE TABLE plan_proposals (
    id             bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id        bigint      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    status         text        NOT NULL DEFAULT 'generating'
        CHECK (status IN ('generating', 'open', 'closed', 'failed', 'superseded')),
    failure        text        CHECK (failure IN ('invalid_output', 'provider_error', 'not_configured', 'timeout')),
    prompt_version text,
    from_day       date        NOT NULL,
    to_day         date        NOT NULL CHECK (to_day >= from_day),
    plan_id        bigint,
    created_at     timestamptz NOT NULL DEFAULT now(),
    UNIQUE (user_id, id),
    CHECK ((status = 'failed') = (failure IS NOT NULL)),
    FOREIGN KEY (user_id, plan_id) REFERENCES plans (user_id, id)
);
CREATE INDEX plan_proposals_user_idx ON plan_proposals (user_id, id);
-- One proposal at a time is being generated per user.
CREATE UNIQUE INDEX plan_proposals_generating_idx ON plan_proposals (user_id) WHERE status = 'generating';

-- A proposed change: before is the planned workout as it was (change, remove),
-- after the proposed one (add, change), both validated PlannedWorkoutInput JSON.
-- workout_id has no foreign key: an approved removal deletes the workout and
-- the item stays as its record.
CREATE TABLE plan_proposal_items (
    id          bigint NOT NULL GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id     bigint NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    proposal_id bigint NOT NULL,
    action      text   NOT NULL CHECK (action IN ('add', 'change', 'remove')),
    status      text   NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'rejected')),
    workout_id  bigint,
    before      jsonb,
    after       jsonb,
    CHECK ((action = 'add') = (workout_id IS NULL)),
    CHECK ((action = 'add') = (before IS NULL)),
    CHECK ((action = 'remove') = (after IS NULL)),
    FOREIGN KEY (user_id, proposal_id) REFERENCES plan_proposals (user_id, id) ON DELETE CASCADE
);
CREATE INDEX plan_proposal_items_proposal_idx ON plan_proposal_items (user_id, proposal_id, id);

-- +goose Down
DROP TABLE plan_proposal_items;
DROP TABLE plan_proposals;
