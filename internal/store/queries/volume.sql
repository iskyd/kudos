-- name: ListVolume :many
-- The user's volume per period (week from Monday, month or year) of the local
-- days in [from_day, to_day], periods with activities only, oldest first.
-- Values are effective ones (edits applied, see overrides): distance, timer time
-- (else elapsed time) and ascent, plus load; merged-away activities count within
-- the one they joined. Sport and gear filter when set.
SELECT date_trunc(@period::text, x.day)::date AS start,
    count(*)::integer AS activities,
    coalesce(sum(x.distance_m), 0)::double precision AS distance_m,
    coalesce(sum(x.duration_s), 0)::double precision AS duration_s,
    coalesce(sum(x.ascent_m), 0)::double precision AS ascent_m,
    coalesce(sum(d.load), 0)::double precision AS load
FROM activities a
LEFT JOIN activity_derived d ON d.activity_id = a.id AND d.user_id = a.user_id
CROSS JOIN LATERAL (SELECT
    ((coalesce((a.overrides->>'start_time')::timestamptz, a.start_time)
        + make_interval(secs => a.utc_offset_s)) AT TIME ZONE 'UTC')::date AS day,
    CASE WHEN a.overrides->'summary' IS NULL THEN a.distance_m
        ELSE (a.overrides->'summary'->>'distance_m')::float8 END AS distance_m,
    CASE WHEN a.overrides->'summary' IS NULL THEN coalesce(a.timer_time_s, a.elapsed_time_s)
        ELSE coalesce((a.overrides->'summary'->>'timer_time_s')::float8, (a.overrides->'summary'->>'elapsed_time_s')::float8) END AS duration_s,
    CASE WHEN a.overrides->'summary' IS NULL THEN a.ascent_m::float8
        ELSE (a.overrides->'summary'->>'ascent_m')::float8 END AS ascent_m
) x
WHERE a.user_id = @user_id AND a.merged_into IS NULL
    AND x.day BETWEEN @from_day::date AND @to_day::date
    AND (sqlc.narg(sport)::text IS NULL OR coalesce(a.overrides->>'sport', a.sport) = sqlc.narg(sport)::text)
    AND (sqlc.narg(gear_id)::bigint IS NULL OR a.gear_id = sqlc.narg(gear_id))
GROUP BY 1
ORDER BY 1
LIMIT @max_rows;

-- name: SumTimeInZones :many
-- Seconds per zone over the activities ListVolume counts, summed per metric
-- (hr, pace, power) and zone model, zones numbered from 1.
SELECT m.metric::text AS metric, m.model::text AS model, u.zone::integer AS zone, sum(u.seconds)::double precision AS seconds
FROM activities a
JOIN activity_derived d ON d.activity_id = a.id AND d.user_id = a.user_id
CROSS JOIN LATERAL (VALUES
    ('hr', d.hr_zone_model, d.hr_zone_s),
    ('pace', d.pace_zone_model, d.pace_zone_s),
    ('power', d.power_zone_model, d.power_zone_s)
) m(metric, model, zone_s)
CROSS JOIN LATERAL unnest(m.zone_s) WITH ORDINALITY u(seconds, zone)
WHERE a.user_id = @user_id AND a.merged_into IS NULL AND m.model IS NOT NULL
    AND ((coalesce((a.overrides->>'start_time')::timestamptz, a.start_time)
        + make_interval(secs => a.utc_offset_s)) AT TIME ZONE 'UTC')::date BETWEEN @from_day::date AND @to_day::date
    AND (sqlc.narg(sport)::text IS NULL OR coalesce(a.overrides->>'sport', a.sport) = sqlc.narg(sport)::text)
    AND (sqlc.narg(gear_id)::bigint IS NULL OR a.gear_id = sqlc.narg(gear_id))
GROUP BY 1, 2, 3
ORDER BY 1, 2, 3
LIMIT 1000;
