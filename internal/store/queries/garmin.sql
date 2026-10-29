-- name: UpsertGarminKey :one
-- A new key replaces the old one and its token and clears a rejection; the sync
-- interval and cursor are kept.
INSERT INTO garmin_connections (user_id, key_ciphertext, key_expires_on, token_ciphertext, token_expires_at)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (user_id) DO UPDATE SET
    key_ciphertext = EXCLUDED.key_ciphertext,
    key_expires_on = EXCLUDED.key_expires_on,
    token_ciphertext = EXCLUDED.token_ciphertext,
    token_expires_at = EXCLUDED.token_expires_at,
    status = 'connected',
    updated_at = now()
RETURNING *;

-- name: GetGarminConnection :one
SELECT * FROM garmin_connections WHERE user_id = $1;

-- name: SetGarminSyncInterval :one
UPDATE garmin_connections SET sync_interval_h = $2, updated_at = now()
WHERE user_id = $1
RETURNING *;

-- name: DeleteGarminConnection :execrows
DELETE FROM garmin_connections WHERE user_id = $1;
