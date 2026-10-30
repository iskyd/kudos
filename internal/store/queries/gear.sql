-- name: CreateGear :one
INSERT INTO gear (user_id, kind, name, replacement_distance_m, retired)
VALUES (@user_id, @kind, @name, @replacement_distance_m, @retired)
RETURNING *;

-- name: UpdateGear :execrows
UPDATE gear SET kind = @kind, name = @name, replacement_distance_m = @replacement_distance_m, retired = @retired
WHERE user_id = @user_id AND id = @id;

-- name: DeleteGear :execrows
DELETE FROM gear WHERE user_id = @user_id AND id = @id;

-- name: ListGearTotals :many
-- One page of gear, oldest first, with distance and time summed over the
-- assigned activities (time is timer time, else elapsed time). Pass id for one item.
SELECT sqlc.embed(g), t.distance_m, t.time_s, t.activities
FROM gear g,
    LATERAL (SELECT coalesce(sum(a.distance_m), 0)::float8 AS distance_m,
        coalesce(sum(coalesce(a.timer_time_s, a.elapsed_time_s)), 0)::float8 AS time_s,
        count(a.id) AS activities
    FROM activities a WHERE a.user_id = g.user_id AND a.gear_id = g.id) t
WHERE g.user_id = @user_id AND (sqlc.narg(id)::bigint IS NULL OR g.id = sqlc.narg(id)) AND g.id > @after
ORDER BY g.id
LIMIT @max_rows;
