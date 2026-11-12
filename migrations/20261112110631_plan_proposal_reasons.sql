-- +goose Up
-- kind: a plan of the coming two weeks, or an adjustment of the coming week
-- after the daily insight. rationale and refs are the model's reasons and the
-- context references ([act:…], [day:…]) they rest on, checked against the
-- context it was given.
ALTER TABLE plan_proposals
    ADD COLUMN kind text NOT NULL DEFAULT 'plan' CHECK (kind IN ('plan', 'adjustment')),
    ADD COLUMN rationale text CHECK (char_length(rationale) <= 1000),
    ADD COLUMN refs text[] NOT NULL DEFAULT '{}';

-- +goose Down
ALTER TABLE plan_proposals DROP COLUMN refs, DROP COLUMN rationale, DROP COLUMN kind;
