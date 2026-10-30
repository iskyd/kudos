-- name: UpsertActivityDerived :exec
-- The WHERE makes a conflicting row of another user a no-op instead of an overwrite.
INSERT INTO activity_derived (
    user_id, activity_id, version, zone_set_id, general_zone_set_id,
    avg_hr_bpm, avg_power_w, avg_speed_m_s, avg_cadence,
    hr_zone_model, hr_zone_s, pace_zone_model, pace_zone_s, power_zone_model, power_zone_s
) VALUES (
    @user_id, @activity_id, @version, @zone_set_id, @general_zone_set_id,
    @avg_hr_bpm, @avg_power_w, @avg_speed_m_s, @avg_cadence,
    @hr_zone_model, @hr_zone_s, @pace_zone_model, @pace_zone_s, @power_zone_model, @power_zone_s
)
ON CONFLICT (activity_id) DO UPDATE SET
    version = EXCLUDED.version, stale = false, computed_at = now(),
    zone_set_id = EXCLUDED.zone_set_id, general_zone_set_id = EXCLUDED.general_zone_set_id,
    avg_hr_bpm = EXCLUDED.avg_hr_bpm, avg_power_w = EXCLUDED.avg_power_w,
    avg_speed_m_s = EXCLUDED.avg_speed_m_s, avg_cadence = EXCLUDED.avg_cadence,
    hr_zone_model = EXCLUDED.hr_zone_model, hr_zone_s = EXCLUDED.hr_zone_s,
    pace_zone_model = EXCLUDED.pace_zone_model, pace_zone_s = EXCLUDED.pace_zone_s,
    power_zone_model = EXCLUDED.power_zone_model, power_zone_s = EXCLUDED.power_zone_s
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
