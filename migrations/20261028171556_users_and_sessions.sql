-- +goose Up
CREATE TABLE users (
    id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    -- Stored lowercase; the API normalizes before every lookup.
    username      text        NOT NULL UNIQUE,
    -- argon2id in PHC string format.
    password_hash text        NOT NULL,
    -- IANA zone name, e.g. Europe/Rome.
    timezone      text        NOT NULL DEFAULT 'UTC',
    created_at    timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE sessions (
    -- SHA-256 of the cookie token: a database leak does not leak live sessions.
    token_hash bytea       PRIMARY KEY,
    user_id    bigint      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    csrf_token text        NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL
);

CREATE INDEX sessions_user_id_idx ON sessions (user_id);

-- +goose Down
DROP TABLE sessions;
DROP TABLE users;
