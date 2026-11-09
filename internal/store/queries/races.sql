-- name: CreateRace :one
INSERT INTO races (user_id, name, day, sport, distance_m, goal_time_s, priority)
VALUES (@user_id, @name, @day, @sport, @distance_m, @goal_time_s, @priority)
RETURNING *;

-- name: UpdateRace :one
UPDATE races SET name = @name, day = @day, sport = @sport, distance_m = @distance_m,
    goal_time_s = @goal_time_s, priority = @priority
WHERE user_id = @user_id AND id = @id
RETURNING *;

-- name: DeleteRace :execrows
DELETE FROM races WHERE user_id = @user_id AND id = @id;

-- name: ListRaces :many
-- One page, latest race day first; after_day and after_id are the previous
-- page's last row.
SELECT * FROM races
WHERE user_id = @user_id
    AND (sqlc.narg(after_day)::date IS NULL OR (day, id) < (sqlc.narg(after_day)::date, sqlc.narg(after_id)::bigint))
ORDER BY day DESC, id DESC
LIMIT @max_rows;

-- name: ListRacesBetween :many
-- Races from from_day to to_day (inclusive), soonest first.
SELECT * FROM races
WHERE user_id = @user_id AND day BETWEEN @from_day::date AND @to_day::date
ORDER BY day, id
LIMIT @max_rows;
