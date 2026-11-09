-- +goose Up
-- Daily insights and weekly reports the coach wrote. day is the local day an
-- insight is for, or the Monday a report was written (covering the week
-- before). prompt_version names the template that produced the content.
CREATE TABLE coach_reports (
    id             bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id        bigint      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    kind           text        NOT NULL CHECK (kind IN ('daily_insight', 'weekly_report')),
    day            date        NOT NULL,
    prompt_version text        NOT NULL,
    content        text        NOT NULL,
    created_at     timestamptz NOT NULL DEFAULT now(),
    UNIQUE (user_id, kind, day),
    CHECK (kind <> 'weekly_report' OR extract(isodow FROM day) = 1)
);

-- +goose Down
DROP TABLE coach_reports;
