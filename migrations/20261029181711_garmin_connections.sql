-- +goose Up
-- One Garmin Personal API connection per user. The key and the cached token are
-- AES-256-GCM ciphertext; the encryption key never touches the database.
CREATE TABLE garmin_connections (
    user_id          bigint      PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    key_ciphertext   bytea       NOT NULL,
    -- Entered by the user: Garmin offers no way to read a key's expiry.
    key_expires_on   date        NOT NULL,
    token_ciphertext bytea,
    token_expires_at timestamptz,
    -- connected, or key_rejected when Garmin refused the key (sync pauses).
    status           text        NOT NULL DEFAULT 'connected' CHECK (status IN ('connected', 'key_rejected')),
    sync_interval_h  smallint    NOT NULL DEFAULT 1 CHECK (sync_interval_h BETWEEN 1 AND 24),
    -- Sync cursor: the last local day fully synced.
    synced_through   date,
    last_sync_at     timestamptz,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    CHECK ((token_ciphertext IS NULL) = (token_expires_at IS NULL))
);

-- +goose Down
DROP TABLE garmin_connections;
