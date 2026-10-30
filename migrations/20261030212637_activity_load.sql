-- +goose Up
-- Each activity's training load and the method that produced it (invariant 21);
-- null when no method applies. Rows get them when recomputed for version 2.
ALTER TABLE activity_derived
    ADD COLUMN load        double precision CHECK (load >= 0),
    ADD COLUMN load_method text CHECK (load_method IN ('power_tss', 'pace_rtss', 'swim_tss', 'hr_tss', 'session_rpe')),
    ADD CHECK ((load IS NULL) = (load_method IS NULL));

-- +goose Down
ALTER TABLE activity_derived DROP COLUMN load_method, DROP COLUMN load;
