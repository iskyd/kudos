-- +goose Up
-- Zone versions: the thresholds valid for a sport from a date on. Zones are
-- computed from them by the zone models, never stored. An activity uses the
-- latest version on or before its local date. general holds max and resting HR
-- for heart rate reserve zones in any sport.
CREATE TABLE zone_sets (
    id                  bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id             bigint   NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    sport               text     NOT NULL CHECK (sport IN ('running', 'cycling', 'swimming', 'general')),
    effective_from      date     NOT NULL,
    lthr_bpm            smallint CHECK (lthr_bpm BETWEEN 60 AND 240),
    -- Running threshold pace as a speed.
    threshold_speed_m_s real     CHECK (threshold_speed_m_s > 0),
    ftp_w               smallint CHECK (ftp_w > 0),
    -- Critical swim speed.
    css_m_s             real     CHECK (css_m_s > 0),
    max_hr_bpm          smallint CHECK (max_hr_bpm BETWEEN 60 AND 250),
    resting_hr_bpm      smallint CHECK (resting_hr_bpm BETWEEN 20 AND 150),
    created_at          timestamptz NOT NULL DEFAULT now(),
    CHECK (resting_hr_bpm < max_hr_bpm),
    UNIQUE (user_id, sport, effective_from)
);

-- +goose Down
DROP TABLE zone_sets;
