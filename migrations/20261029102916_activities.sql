-- +goose Up
-- All values are SI (metres, seconds, m/s, watts, bpm, kg, °C), except energy in kcal. Child tables carry
-- user_id and reference their parent by (user_id, id), so a row can never point
-- at another user's parent.

CREATE TABLE original_files (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id     bigint      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    format      text        NOT NULL CHECK (format IN ('fit', 'gpx', 'tcx')),
    -- Of the uncompressed file: the same upload twice is stored once.
    sha256      bytea       NOT NULL CHECK (length(sha256) = 32),
    size_bytes  bigint      NOT NULL CHECK (size_bytes >= 0),
    -- Gzip-compressed copy, relative to DATA_DIR.
    path        text        NOT NULL,
    received_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (user_id, id),
    UNIQUE (user_id, sha256)
);

CREATE TABLE multisport_groups (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id         bigint NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    source          text   NOT NULL CHECK (source IN ('garmin', 'upload')),
    source_group_id text   NOT NULL,
    UNIQUE (user_id, id),
    UNIQUE (user_id, source, source_group_id)
);

CREATE TABLE activities (
    id                    bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id               bigint      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    source                text        NOT NULL CHECK (source IN ('garmin', 'upload', 'manual')),
    -- The source's ID, or a key built from start time, sport and device when it has none.
    source_activity_id    text        NOT NULL,
    sport                 text        NOT NULL CHECK (sport IN (
        'running', 'trail_running', 'cycling', 'pool_swimming', 'open_water_swimming',
        'hiking', 'strength', 'transition', 'other')),
    subtype               text        NOT NULL CHECK (subtype IN ('outdoor', 'indoor', 'virtual', 'treadmill')),
    start_time            timestamptz NOT NULL,
    -- Local time = start_time + utc_offset_s.
    utc_offset_s          integer     NOT NULL CHECK (utc_offset_s BETWEEN -64800 AND 64800),
    original_file_id      bigint,
    multisport_group_id   bigint,

    -- Summary values as recorded by the device.
    elapsed_time_s        double precision CHECK (elapsed_time_s >= 0),
    timer_time_s          double precision CHECK (timer_time_s >= 0),
    distance_m            double precision CHECK (distance_m >= 0),
    ascent_m              real,
    descent_m             real,
    avg_speed_m_s         real,
    max_speed_m_s         real,
    avg_hr_bpm            smallint,
    max_hr_bpm            smallint,
    avg_power_w           smallint,
    max_power_w           smallint,
    -- Steps per minute for running, rpm for cycling.
    avg_cadence           real,
    max_cadence           real,
    avg_temperature_c     real,
    pool_length_m         real,
    calories_kcal         integer CHECK (calories_kcal >= 0),

    -- Pre-filled from the device on first import, then owned by the user.
    rpe                   real     CHECK (rpe BETWEEN 0 AND 10),
    feel                  smallint CHECK (feel BETWEEN 0 AND 100),
    notes                 text,
    weather_temperature_c real,
    weather_condition     text CHECK (weather_condition IN ('sun', 'cloud', 'rain', 'wind', 'snow')),
    -- Reversible user edits (trim bounds, sport change, excluded HR spikes). Samples are never modified.
    overrides             jsonb    NOT NULL DEFAULT '{}',

    UNIQUE (user_id, id),
    UNIQUE (user_id, source, source_activity_id),
    FOREIGN KEY (user_id, original_file_id) REFERENCES original_files (user_id, id),
    FOREIGN KEY (user_id, multisport_group_id) REFERENCES multisport_groups (user_id, id)
);

CREATE INDEX activities_user_start_idx ON activities (user_id, start_time DESC, id DESC);

CREATE TABLE activity_streams (
    user_id       bigint      NOT NULL,
    activity_id   bigint      NOT NULL,
    time          timestamptz NOT NULL,
    lat_deg       double precision,
    lon_deg       double precision,
    altitude_m    real,
    hr_bpm        smallint,
    power_w       smallint,
    cadence       real,
    speed_m_s     real,
    distance_m    double precision,
    temperature_c real,
    -- Sport-specific and developer fields, keyed by name.
    extras        jsonb,
    UNIQUE (activity_id, time),
    FOREIGN KEY (user_id, activity_id) REFERENCES activities (user_id, id) ON DELETE CASCADE
);

SELECT create_hypertable('activity_streams', by_range('time', INTERVAL '7 days'));
ALTER TABLE activity_streams SET (
    timescaledb.enable_columnstore,
    timescaledb.segmentby = 'user_id, activity_id',
    timescaledb.orderby = 'time'
);
CALL add_columnstore_policy('activity_streams', after => INTERVAL '30 days');

CREATE TABLE laps (
    user_id            bigint      NOT NULL,
    activity_id        bigint      NOT NULL,
    lap_index          integer     NOT NULL CHECK (lap_index >= 0),
    start_time         timestamptz NOT NULL,
    elapsed_time_s     double precision,
    timer_time_s       double precision,
    distance_m         double precision,
    ascent_m           real,
    descent_m          real,
    avg_speed_m_s      real,
    max_speed_m_s      real,
    avg_hr_bpm         smallint,
    max_hr_bpm         smallint,
    avg_power_w        smallint,
    max_power_w        smallint,
    avg_cadence        real,
    intensity          text CHECK (intensity IN ('active', 'rest', 'warmup', 'cooldown', 'recovery', 'interval', 'other')),
    -- Index into the recorded workout's steps.
    workout_step_index integer,
    PRIMARY KEY (activity_id, lap_index),
    FOREIGN KEY (user_id, activity_id) REFERENCES activities (user_id, id) ON DELETE CASCADE
);

CREATE TABLE swim_lengths (
    user_id        bigint      NOT NULL,
    activity_id    bigint      NOT NULL,
    length_index   integer     NOT NULL CHECK (length_index >= 0),
    start_time     timestamptz NOT NULL,
    elapsed_time_s double precision,
    timer_time_s   double precision,
    -- false for idle (rest) lengths.
    active         boolean     NOT NULL,
    stroke         text CHECK (stroke IN ('freestyle', 'backstroke', 'breaststroke', 'butterfly', 'drill', 'mixed', 'im')),
    strokes        smallint,
    avg_speed_m_s  real,
    PRIMARY KEY (activity_id, length_index),
    FOREIGN KEY (user_id, activity_id) REFERENCES activities (user_id, id) ON DELETE CASCADE
);

CREATE TABLE strength_sets (
    user_id            bigint      NOT NULL,
    activity_id        bigint      NOT NULL,
    set_index          integer     NOT NULL CHECK (set_index >= 0),
    start_time         timestamptz NOT NULL,
    duration_s         double precision,
    -- false for rest sets.
    active             boolean     NOT NULL,
    reps               smallint,
    -- 0 when the user did not enter one.
    weight_kg          real,
    -- FIT exercise category and number; exercise_name is resolved from the file's exercise titles.
    exercise_category  integer,
    exercise_number    integer,
    exercise_name      text,
    workout_step_index integer,
    PRIMARY KEY (activity_id, set_index),
    FOREIGN KEY (user_id, activity_id) REFERENCES activities (user_id, id) ON DELETE CASCADE
);

CREATE TABLE recorded_workouts (
    user_id     bigint NOT NULL,
    activity_id bigint PRIMARY KEY,
    name        text,
    -- Steps with durations and targets already converted to SI.
    steps       jsonb  NOT NULL,
    FOREIGN KEY (user_id, activity_id) REFERENCES activities (user_id, id) ON DELETE CASCADE
);

-- Thresholds the device recorded as valid on the activity's day.
CREATE TABLE activity_thresholds (
    user_id          bigint NOT NULL,
    activity_id      bigint PRIMARY KEY,
    ftp_w            smallint,
    threshold_hr_bpm smallint,
    max_hr_bpm       smallint,
    resting_hr_bpm   smallint,
    -- Upper bounds of each zone, ascending.
    hr_zones_bpm     smallint[],
    power_zones_w    smallint[],
    FOREIGN KEY (user_id, activity_id) REFERENCES activities (user_id, id) ON DELETE CASCADE
);

-- +goose Down
DROP TABLE activity_thresholds;
DROP TABLE recorded_workouts;
DROP TABLE strength_sets;
DROP TABLE swim_lengths;
DROP TABLE laps;
DROP TABLE activity_streams;
DROP TABLE activities;
DROP TABLE multisport_groups;
DROP TABLE original_files;
