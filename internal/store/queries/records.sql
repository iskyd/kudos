-- name: DeleteBestEfforts :exec
DELETE FROM best_efforts WHERE user_id = @user_id AND activity_id = @activity_id;

-- name: InsertBestEfforts :exec
-- The WHERE stores nothing for an activity of another user.
INSERT INTO best_efforts (user_id, activity_id, effort, value)
SELECT @user_id, @activity_id, unnest(@efforts::text[]), unnest(@effort_values::double precision[])
WHERE EXISTS (SELECT 1 FROM activities WHERE user_id = @user_id AND id = @activity_id);

-- name: ListRecordHistory :many
-- Every effort's records in the order they were set: the activities whose value
-- beat all earlier ones (fastest efforts lower, the others higher), merged-away
-- activities left out.
SELECT effort, activity_id, start_time, utc_offset_s, value FROM (
    SELECT e.effort, e.activity_id, e.value, x.start_time, a.utc_offset_s, x.score,
        min(x.score) OVER (
            PARTITION BY e.effort ORDER BY x.start_time, e.activity_id
            ROWS BETWEEN UNBOUNDED PRECEDING AND 1 PRECEDING
        ) AS best_before
    FROM best_efforts e
    JOIN activities a ON a.id = e.activity_id AND a.user_id = e.user_id
    CROSS JOIN LATERAL (SELECT
        coalesce((a.overrides->>'start_time')::timestamptz, a.start_time)::timestamptz AS start_time,
        CASE WHEN e.effort LIKE 'fastest%' THEN e.value ELSE -e.value END AS score
    ) x
    WHERE e.user_id = @user_id AND a.merged_into IS NULL
) h
WHERE best_before IS NULL OR score < best_before
ORDER BY effort, start_time, activity_id
LIMIT @max_rows;

-- name: GetRecordsSince :one
-- The first local day records cover: the earlier of the Garmin backfill window's
-- start and the first activity's day; no row without either.
SELECT since FROM (SELECT LEAST(
    (SELECT window_start FROM garmin_backfills b WHERE b.user_id = @user_id),
    (SELECT min(((coalesce((a.overrides->>'start_time')::timestamptz, a.start_time)
        + make_interval(secs => a.utc_offset_s)) AT TIME ZONE 'UTC')::date)
     FROM activities a WHERE a.user_id = @user_id AND a.merged_into IS NULL)
)::date AS since) s
WHERE since IS NOT NULL;
