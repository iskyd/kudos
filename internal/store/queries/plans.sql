-- name: CreatePlan :one
INSERT INTO plans (user_id) VALUES (@user_id) RETURNING *;

-- name: CreatePlannedWorkout :one
-- steps must already be validated (internal/plans).
INSERT INTO planned_workouts (user_id, plan_id, day, sport, title, description, steps)
VALUES (@user_id, @plan_id, @day, @sport, @title, @description, @steps)
RETURNING *;

-- name: GetPlannedWorkout :one
SELECT * FROM planned_workouts WHERE user_id = @user_id AND id = @id;

-- name: ListPlannedWorkoutsBetween :many
-- The workouts planned from from_day to to_day (inclusive), soonest first.
SELECT * FROM planned_workouts
WHERE user_id = @user_id AND day BETWEEN @from_day::date AND @to_day::date
ORDER BY day, id
LIMIT @max_rows;
