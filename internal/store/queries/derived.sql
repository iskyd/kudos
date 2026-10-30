-- name: UpsertActivityDerived :exec
-- The WHERE makes a conflicting row of another user a no-op instead of an overwrite.
INSERT INTO activity_derived (
    user_id, activity_id, version, zone_set_id, general_zone_set_id,
    avg_hr_bpm, avg_power_w, avg_speed_m_s, avg_cadence,
    hr_zone_model, hr_zone_s, pace_zone_model, pace_zone_s, power_zone_model, power_zone_s,
    load, load_method
) VALUES (
    @user_id, @activity_id, @version, @zone_set_id, @general_zone_set_id,
    @avg_hr_bpm, @avg_power_w, @avg_speed_m_s, @avg_cadence,
    @hr_zone_model, @hr_zone_s, @pace_zone_model, @pace_zone_s, @power_zone_model, @power_zone_s,
    @load, @load_method
)
ON CONFLICT (activity_id) DO UPDATE SET
    version = EXCLUDED.version, stale = false, computed_at = now(),
    zone_set_id = EXCLUDED.zone_set_id, general_zone_set_id = EXCLUDED.general_zone_set_id,
    avg_hr_bpm = EXCLUDED.avg_hr_bpm, avg_power_w = EXCLUDED.avg_power_w,
    avg_speed_m_s = EXCLUDED.avg_speed_m_s, avg_cadence = EXCLUDED.avg_cadence,
    hr_zone_model = EXCLUDED.hr_zone_model, hr_zone_s = EXCLUDED.hr_zone_s,
    pace_zone_model = EXCLUDED.pace_zone_model, pace_zone_s = EXCLUDED.pace_zone_s,
    power_zone_model = EXCLUDED.power_zone_model, power_zone_s = EXCLUDED.power_zone_s,
    load = EXCLUDED.load, load_method = EXCLUDED.load_method
WHERE activity_derived.user_id = EXCLUDED.user_id;

-- name: GetActivityDerived :one
SELECT * FROM activity_derived WHERE user_id = @user_id AND activity_id = @activity_id;

-- name: ListActivitiesNeedingDerived :many
-- One page of the user's activities whose derived values are missing, stale or
-- from an older version of the computation.
SELECT a.id FROM activities a
LEFT JOIN activity_derived d ON d.activity_id = a.id AND d.user_id = a.user_id
WHERE a.user_id = @user_id AND a.id > @after
    AND (d.activity_id IS NULL OR d.stale OR d.version < @version::smallint)
ORDER BY a.id
LIMIT @max_rows;

-- name: InvalidateDerivedFrom :execrows
-- Marks stale the derived values of the user's activities on or after a local
-- date, where a zone change applies.
UPDATE activity_derived d SET stale = true
FROM activities a
WHERE d.user_id = @user_id AND a.user_id = @user_id AND a.id = d.activity_id
    AND ((coalesce((a.overrides->>'start_time')::timestamptz, a.start_time)
        + make_interval(secs => a.utc_offset_s)) AT TIME ZONE 'UTC')::date >= @from_day::date;

-- name: ListUserIDs :many
-- One page of user IDs, for jobs that run for every user.
SELECT id FROM users WHERE id > @after ORDER BY id LIMIT @max_rows;

-- name: ListDailyLoads :many
-- The user's summed load per local date in [from_day, to_day], days with load
-- only, in date order; merged-away activities count within the one they joined.
SELECT day, sum(load)::double precision AS load FROM (
    SELECT ((coalesce((a.overrides->>'start_time')::timestamptz, a.start_time)
            + make_interval(secs => a.utc_offset_s)) AT TIME ZONE 'UTC')::date AS day, d.load
    FROM activity_derived d
    JOIN activities a ON a.id = d.activity_id AND a.user_id = d.user_id
    WHERE d.user_id = @user_id AND d.load IS NOT NULL AND a.merged_into IS NULL
) l
WHERE day BETWEEN @from_day::date AND @to_day::date
GROUP BY day
ORDER BY day
LIMIT @max_rows;
