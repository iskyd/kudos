-- name: CreateGoal :one
INSERT INTO goals (user_id, period, start_day, sport, metric, target)
VALUES (@user_id, @period, @start_day, @sport, @metric, @target)
RETURNING *;

-- name: ListGoals :many
-- One page of the user's goals, newest first; pass before for the page after it.
SELECT * FROM goals
WHERE user_id = @user_id AND (sqlc.narg(before)::bigint IS NULL OR id < sqlc.narg(before))
ORDER BY id DESC
LIMIT @max_rows;

-- name: DeleteGoal :execrows
DELETE FROM goals WHERE user_id = @user_id AND id = @id;
