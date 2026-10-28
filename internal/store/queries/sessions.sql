-- name: CreateSession :exec
INSERT INTO sessions (token_hash, user_id, csrf_token, expires_at)
VALUES ($1, $2, $3, $4);

-- name: GetSession :one
-- Authenticates a request by its cookie token, so it cannot filter by user ID.
SELECT s.user_id, s.csrf_token, s.expires_at, u.username, u.timezone
FROM sessions s
JOIN users u ON u.id = s.user_id
WHERE s.token_hash = $1 AND s.expires_at > now();

-- name: ExtendSession :exec
UPDATE sessions SET expires_at = $3 WHERE token_hash = $1 AND user_id = $2;

-- name: DeleteSession :exec
DELETE FROM sessions WHERE token_hash = $1 AND user_id = $2;

-- name: DeleteExpiredSessions :exec
DELETE FROM sessions WHERE user_id = $1 AND expires_at <= now();
