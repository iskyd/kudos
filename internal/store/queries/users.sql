-- name: CreateUser :one
INSERT INTO users (username, password_hash, timezone)
VALUES ($1, $2, $3)
RETURNING id, username, timezone, created_at;

-- name: GetUserCredentials :one
-- Login lookup by username; the only user query not keyed by user ID.
SELECT id, username, password_hash, timezone FROM users WHERE username = $1;

-- name: UpdateUserTimezone :one
UPDATE users SET timezone = $2 WHERE id = $1
RETURNING id, username, timezone, created_at;

-- name: GetUserTimezone :one
SELECT timezone FROM users WHERE id = $1;
