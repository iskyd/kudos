# KudOS — Product Spec & Architecture (v1)

Oct 27, 2026 · @Mattia

## Overview

We are building KudOS, a free, open-source, self-hosted training platform for endurance and multisport athletes. It imports Garmin history, syncs new data, analyzes and aggregates every activity, and connects to the user's choice of LLM for training, rest and nutrition advice. It replaces paid subscriptions such as Strava premium and Garmin Connect+.

**Target users.** Anyone who wants to own their training data and run the app themselves. A typical instance serves one user; a coach-run instance may serve up to 10.

**Guiding principles**

- Free and open source, with no paid tier and no features held back.
- Self-hosted on a LAN, shipped as pre-built Docker images and run with Docker Compose.
- Bring your own LLM: local (Ollama) or a cloud provider via the user's API key.
- Trust source data as recorded; compute our own analytics on top.
- Code computes numbers; LLMs interpret and advise.
- Opinionated defaults in v1; customization later.

**License.** MIT. Commercial reuse, including hosted forks, is explicitly allowed.

## Scope

v1 delivers a single-source (Garmin), single-athlete experience end to end: onboarding and sync through Garmin's Personal API, analysis, dashboards, LLM coaching and rolling training plans. Coach mode, the full-history bulk import, nutrition features and additional sources follow in v2.

**v1 build order** (each step usable before the next starts)

1. Garmin Personal API onboarding backfill and sync
2. Activity detail view and editing
3. Analytics (zones, load, readiness, records, curves)
4. Dashboards, goals and calendar
5. LLM chat, daily insights and weekly reports
6. Training plans and races

| Feature | Release | Notes |
| --- | --- | --- |
| Personal API backfill (120 days by default, up to one year) and sync, manual FIT/GPX/TCX upload, manual activities | v1 | One source at a time |
| Full-resolution streams, activity detail, editing, comparison, gear | v1 |  |
| Laps and workout steps from Garmin as intervals | v1 |  |
| Zones, training load, readiness, records, power and pace curves | v1 |  |
| Running analytics (VO2max, race predictions, efficiency, decoupling) | v1 |  |
| Dashboards, goals, training calendar | v1 | Fixed layout |
| Wellness check-in, body metrics, injury and illness log | v1 | Feeds readiness and LLM context |
| LLM chat, daily insights, weekly report | v1 | One active provider at a time |
| Rolling training plans, races, planned vs. completed | v1 | Workouts entered on the watch manually |
| Coach and athlete roles | v2 | Data model ready in v1 |
| Garmin bulk export import (full history) | v2 | Design kept in Data sources |
| Nutrition recommendations and food logging (Open Food Facts) | v2 | General LLM chat may still discuss food |
| Strava and other sources, merging duplicates across sources | v2 | Connector interface ready in v1 |
| Manual strength logging with an exercise library | v2 | v1 uses Garmin strength data as-is |
| Advanced cycling metrics (NP, IF, VI, W′ balance, L/R balance, VAM) | v2 |  |
| Automatic interval detection | v2 |  |
| FIT workout file export for the watch | v2 | Cheap to add |
| Customizable dashboards, ad-hoc analysis | v2 |  |
| Job status UI, full data export | v2 | v1 shows backfill progress only |
| Push to Garmin, segments, privacy zones, public sharing, notifications | Not planned |  |

## Users and accounts

Every user's data is fully isolated, enforced in the data layer from v1 even though most instances have one user.

- **Authentication:** local username and password, no 2FA, no SSO in v1.
- **Registration:** self-registration, switched on or off by an instance setting (the REGISTRATION\_ENABLED environment variable, on by default). There is no admin role in v1: every account is a regular user.
- **Isolation:** every user-owned table carries `user_id`; every query is scoped by it in the repository layer, never only in handlers.
- **Per-user settings:** Garmin Personal API key, LLM provider and key, zones, sync interval, units (metric only in v1), timezone.
- **Coach mode (v2):** a coach sees and comments on linked athletes' data. Athletes still never see each other. Designed as an explicit grant table so v1 isolation stays the default.
- **Deletion:** not handled in v1.

## Data sources

Garmin's Personal API is the only source in v1: a backfill of recent history at onboarding, then ongoing sync, plus manual uploads. The full-history bulk export import moves to v2. Every source sits behind a connector interface so Strava and others can be added without touching the core.

### Onboarding backfill (Personal API)

1. The user pastes their Personal API key (see sync below) and picks a history window: 120 days by default, up to one year. A date window covers activities and daily health metrics alike, and 120 days is enough for the fitness curve (42-day average) and the 60-day readiness baseline to be meaningful from day one.
2. A background job lists the activities in the window, downloads each original file, and fetches daily health metrics for every day in the window.
3. Requests are sequential and paced with backoff. The job is resumable: a rate limit, an expired key or a restart pauses it, and it continues where it stopped. The UI shows progress.
4. Each activity is parsed and committed on its own. A file that fails to parse is listed in the UI with the reason and can be retried, without blocking the rest.
5. Derived metrics are computed in the background as activities land; original files are kept (gzip).
6. When the backfill finishes, sync continues from the end of the window. Records and trends are labeled with the window's start ("since 29 June 2026") until full history arrives through the v2 bulk import.

The one-year cap follows the only public signal of Garmin's tolerance: on its partner API, Garmin limited intervals.icu's history fetches to one year.

### Bulk export import (v2)

For full history, v2 imports Garmin's account export (requested from the Garmin account's data management page and delivered by email). The design is kept: resumable browser upload of the ZIP (tus), a staging area, strict all-or-nothing commit with a report naming each failing file, unrecognized file types skipped and listed, parsers for the activity files (under `DI_CONNECT/DI-Connect-Uploaded-Files`) and the wellness JSON, and deduplication against API-synced activities by Garmin activity ID.

### Garmin Connect sync (Personal API)

- **Access:** Garmin's official [Personal API](https://developer.garmin.com/gc-developer-program/personal/personal-faq/). Each user creates a Personal API key in Garmin Connect (Settings, Personal API, with an expiry of 30, 90 or 180 days or one year) and pastes it into KudOS. No Garmin password or MFA code is ever stored.
- **Tokens:** KudOS stores the key encrypted and exchanges it for an access token valid for 24 hours. There are no refresh tokens, so a new token is minted from the key when the cached one has less than an hour left. Implemented natively in Go, using Garmin's Apache-2.0 Python client as the reference.
- **Key expiry:** the settings page shows the key's expiry date and a UI banner warns 7 days before (Garmin also emails). A rejected key pauses sync until the user enters a new one.
- **Schedule:** automatic sync every hour by default, configurable up to once a day, plus a "Sync now" button.
- **Scope:** activity search (date-filtered), activity details, workout steps and the activity file download (a ZIP, expected to contain the original FIT; to verify during the sync milestone). Health: daily summaries, sleep, HRV, stress, Body Battery, steps, heart rate, weight, hydration, SpO2 and heart rate zones.
- **Cursor:** continues from the end of the onboarding backfill and advances after each successful sync. Each run re-fetches a 48-hour lookback window because Garmin finalizes sleep and HRV hours after the fact. All writes are idempotent upserts.
- **Direction:** read-only; the API only offers reads. Edits or deletions in Garmin Connect are not propagated.
- **Failure handling:** a UI banner plus structured logs. On a 401, KudOS mints a new token once, then pauses sync. Rate limits are undocumented, so requests are sequential with backoff and limits are measured during the sync milestone.

### Manual input

- Upload individual FIT, GPX or TCX files at any time; originals are kept.
- Create manual activities without a file (sport, date, duration, distance, RPE, notes).
- Deduplication uses the Garmin activity ID when present, otherwise start time, sport and device.

### FIT parsing rules

FIT files are decoded with muktihari/fit and mapped to the internal model by a thin translation layer. These rules come from five real samples: a half marathon, an interval session, a pool swim and a strength workout recorded by a Forerunner 255, and a Zwift ride written by Zwift.

- **Units:** positions are semicircles (convert to degrees); vertical oscillation and step length are millimetres; speed is m/s. Workout step values are raw and must be read by their type: distance in centimetres, time in milliseconds, speed in mm/s.
- **Cadence:** running cadence is stored as strides per minute plus a fraction, so steps per minute = (cadence + fractional cadence) × 2. Cycling cadence is rpm as-is.
- **Invalid values:** unset values use filler numbers (255, 65535, 4294967295) and are dropped, including in developer fields. Zwift declares CORE body-temperature fields that are always empty.
- **Local time:** offset = activity local timestamp − UTC timestamp. Watch files are correct (+2 h on 15 October, +1 h on 25 October, the day summer time ended). Zwift writes the FIT epoch (31 Dec 1989) instead, so the fallback order is: Garmin activity metadata from the API, then the user's timezone.
- **Sample rate:** most activities record every second, but the strength sample used smart recording (gaps of 1–6 s, heart rate only). Analytics use time-weighted calculations, never sample counts.
- **Message timestamps:** in lengths and sets, the timestamp field repeats the activity start, so each message's own start time is used.
- **Durations:** timer and elapsed time can disagree slightly (Zwift reports timer 1 s longer than elapsed), so validation is tolerant.
- **Unknown messages:** Garmin files carry thousands of proprietary messages (7,151 in the half marathon). They are skipped for analysis; the original file is kept.
- **Developer fields:** resolved by name through their field descriptions. Known ones are mapped (Zwift's per-second `target_power`); others go to the stream's JSONB extras.
- **Thresholds:** watch files carry the thresholds valid that day (FTP, lactate threshold HR, max and resting HR, zone boundaries). Third-party files carry none.
- **RPE and feel:** session RPE is 0–100 (divide by 10); workout feel is 0–100. Both are optional (the strength sample has neither).
- **Structured workouts:** watch files embed the workout name and steps (with targets and repeats), including strength workouts created in Garmin Connect, and every lap or set carries its step index.
- **Pool swims:** distance, pace and strokes come from length messages, not records, which carry only heart rate. Lengths are active or idle (rest), and laps alternate rest and swim. The watch leaves SWOLF and stroke distance empty, so KudOS computes them (SWOLF = length time in seconds + strokes; about 90–104 in the 50 m sample).
- **Strength:** sets carry type (active or rest), reps, weight (0 when not entered), duration and start time; there are no laps. Exercise names come from exercise-title messages matched by exercise category and exercise number, not by position, and the name field decodes as a string array whose first element is the name. The library doesn't place sets or exercise titles in its activity struct, so they are read from its list of other messages.
- **Virtual activities:** Zwift positions are real coordinates in the Pacific where its virtual worlds are mapped, so virtual activities never get a map or GPS-derived metrics.

### Connector interface

Each source implements a common interface and maps its data into the internal model. v1 allows one active source per user; cross-source merging comes in v2.

```go
type Connector interface {
    ID() string
    // Garmin: exchanges the Personal API key for a 24-hour bearer token.
    Authenticate(ctx context.Context, creds Credentials) (Session, error)
    FetchActivities(ctx context.Context, s Session, since time.Time) ([]RawActivity, error)
    DownloadActivityFile(ctx context.Context, s Session, sourceID string) (io.ReadCloser, error)
    FetchDailyMetrics(ctx context.Context, s Session, from, to time.Time) ([]DailyMetrics, error)
}

type BulkImporter interface {
    ID() string
    Import(ctx context.Context, archive io.ReaderAt, size int64) (ImportReport, error)
}
```

## Sports and data model

Seven sports are supported from v1, each with its own structure, and full-resolution streams are stored forever in TimescaleDB.

| Sport | Structure | Key data |
| --- | --- | --- |
| Road running | Streams + laps | GPS, HR, pace, cadence, elevation, running dynamics when recorded |
| Trail running | Streams + laps | Same as road, with elevation gain and grade emphasized |
| Cycling | Streams + laps | Power, cadence, HR, speed, GPS, elevation |
| Swimming (pool) | Lengths + laps | Pool length, lengths, stroke type, stroke count, SWOLF (computed); wrist HR |
| Swimming (open water) | Streams + laps | GPS, pace, stroke rate |
| Triathlon | Group of separate activities | Swim, T1, bike, T2, run linked by a multisport group |
| Hiking | Streams | GPS, elevation, HR |
| Strength | Sets | Garmin exercise names, sets, reps, weight, duration; HR only, no GPS |

### Core entities

| Entity | Purpose |
| --- | --- |
| `activities` | One row per activity: sport, start (UTC + local offset), duration, distance, elevation, summary values, source and source ID, gear, RPE, notes, weather, trim bounds, multisport group |
| `activity_streams` | Hypertable of samples: timestamp, lat, lon, altitude, HR, power, cadence, speed, distance, temperature, plus JSONB for sport-specific extras |
| `laps` | Laps and structured workout steps from the device |
| `swim_lengths`, `strength_sets` | Sport-specific detail |
| `multisport_groups` | Links triathlon legs and transitions |
| `activity_derived` | Cached computed values: load, time in zones, best efforts, curve points, the zone version used |
| `daily_metrics` | Hypertable of daily health data: resting HR, HRV, sleep, stress, Body Battery, steps, weight |
| `wellness_checkins`, `body_metrics`, `injuries` | User-entered context; injuries are free text with dates |
| `zone_sets` | Versioned zones per sport, each with an effective-from date |
| `gear` | Shoes and bikes with cumulative distance and retirement |
| `records`, `goals`, `races` | Personal bests, volume goals, target events |
| `plans`, `planned_workouts`, `plan_proposals` | Rolling plans and LLM-proposed changes awaiting approval |
| `llm_providers`, `llm_reports`, `insights` | LLM configuration, daily insights and weekly reports (chat history is not stored) |
| `original_files`, `sync_state`, `import_jobs` | Provenance and ingestion state |

### Rules

- **Source data is trusted as recorded.** User edits are stored as overrides (sport type, trim bounds, excluded HR spikes) rather than rewriting samples, so every edit can be undone.
- **Subtypes:** every sport has a subtype: outdoor, indoor trainer, virtual or treadmill. Virtual activities count toward volume and load but get no map, and their distance and elevation are excluded from records.
- **Time:** stored in UTC with the activity's local offset (see FIT parsing rules); activities display in the local time where they happened.
- **Storage estimate:** 700 hours a year at 1 sample per second is about 2.5 million stream rows a year, roughly 25 million over ten years. Original FIT files run about 5–7 KB per minute and roughly halve when compressed. Timescale compression applies to chunks older than 30 days. Nothing is downsampled.

## Activity features

Each activity has a detail page showing everything recorded, plus tools to edit, compare and annotate it.

**Detail page**

- Map with the route (OpenStreetMap by default; tile URL configurable for self-hosted or keyed providers).
- Elevation profile and synchronized charts for HR, pace or speed, power and cadence; hovering one chart highlights the same point on the map and other charts.
- Laps and splits table, including structured workout steps recorded by the watch.
- Time in zones per metric, using the zone version valid on the activity date.
- Best efforts in the activity and any new personal records.
- Training load, plus running analytics where relevant.
- Sport-specific panels: swim lengths and strokes, strength sets, triathlon legs with transitions.

**Annotation:** RPE (pre-filled from the watch when recorded), mood, notes, injury or pain tags, gear, and manually entered weather (temperature plus a condition such as sun, cloud, rain, wind, snow).

**Editing:** trim start or end, exclude HR spikes, change sport type, merge two activities, split one in two. Edits are non-destructive overrides and trigger recomputation of derived values.

**Comparison:** overlay two activities' charts aligned by distance or time, with a side-by-side summary.

**Workout execution:** for structured workouts run from the watch, laps are grouped by workout step and each step is compared with its target. A step can span several auto-laps (a 2 km warm-up recorded as two 1 km laps), and skipped steps are tolerated (the sample skips the last recovery). In the interval sample, every 400 m rep ran at 3:36–3:45/km against a 3:50–4:00/km target. Zwift workouts have no steps: all laps are marked manual and active, so intervals come from changes in the per-second target power, and compliance compares actual with target power (96% of seconds within 10 W in the sample).

**Gear:** shoes and bikes accumulate distance and time from assigned activities, with a configurable replacement threshold and a warning when it's reached.

**List view:** paginated, filterable by sport, date range, gear and tags, sortable by date, distance, duration and load.

## Analytics

All analytics are computed by our own code from the stored data; Garmin's computed values are displayed but not reused as inputs, except threshold estimates offered as suggestions.

### Zones

Zones are defined per sport from thresholds and versioned with an effective-from date. A new version can be added at any time; every activity uses the version valid on its date.

| Sport | Threshold input | Zone model |
| --- | --- | --- |
| Running | Lactate threshold HR, threshold pace | Friel-style HR and pace zones |
| Cycling | FTP, lactate threshold HR | Coggan 7 power zones, HR zones |
| Swimming | CSS (critical swim speed) | Pace zones |
| Any sport (fallback) | Max HR, resting HR | % of heart rate reserve |

**Zone history from FIT files.** Every file recorded by a Garmin watch carries the thresholds valid that day. During import, KudOS creates a dated zone version per sport each time they change and asks the user to confirm. The samples alone show three versions between November 2025 and October 2026: lactate threshold HR 176, 177, then 171, and max HR 194, 195, then 187. A running FTP from the watch is a running-power threshold (378 W in the samples) and is never reused for cycling. Zwift files carry no thresholds, so cycling FTP comes from the user or from best efforts.

After onboarding, when Garmin reports a new lactate threshold or FTP estimate, or our own best efforts imply one (for example 95% of best 20-minute power), the app suggests a new zone version and the user confirms.

### Training load

Every activity gets one load score, using the first method its data allows:

1. Power-based TSS (cycling with power and a cycling FTP; running with power and a running FTP)
2. Pace-based rTSS (running without power or a running FTP)
3. Swim TSS from CSS (swimming)
4. HR-based hrTSS (any activity with HR)
5. Session RPE × duration (strength, or anything without sensors)

Daily load feeds three curves across all sports: fitness (CTL, 42-day), fatigue (ATL, 7-day) and form (TSB):

```latex
\mathrm{CTL}_t = \mathrm{CTL}_{t-1} + \frac{L_t - \mathrm{CTL}_{t-1}}{42}, \qquad \mathrm{ATL}_t = \mathrm{ATL}_{t-1} + \frac{L_t - \mathrm{ATL}_{t-1}}{7}, \qquad \mathrm{TSB}_t = \mathrm{CTL}_{t-1} - \mathrm{ATL}_{t-1}
```

### Readiness

A deterministic daily score with a visible breakdown, which the LLM then interprets with injury history and context. Inputs: HRV versus the user's 7-day and 60-day baselines, resting HR deviation, sleep duration and quality, current form (TSB), and the wellness check-in (soreness, sleep quality, stress, motivation). Illness and injury entries cap the score. Garmin's own readiness formulas are not reused. The score appears only once enough baseline data exists (at least 14 days of HRV and resting HR). Imported history usually covers this on day one; a new user without history sees a “collecting baseline” state instead. Weights start as fixed defaults and are tuned once real data is available.

### Records and curves

- **Running records:** fastest 1K, 5K, 10K, half marathon and marathon (best segment within any run), plus longest run.
- **Other records:** longest ride, longest swim, longest hike, plus best power for standard durations.
- **Curves:** power-duration (cycling) and pace-duration (running), all-time and for a selected period, with estimated FTP and threshold pace over time.
- **Running analytics:** VO2max estimate, race predictions, efficiency factor, aerobic decoupling, running dynamics trends.

### Computation strategy

Per-activity derived values are computed once at ingest and cached in `activity_derived`. A zone change or an activity edit invalidates the affected rows, which a background job recomputes. Aggregations and the fitness/fatigue curve are computed on demand: ten years of daily values is about 3,650 points, a few milliseconds in Go.

## Dashboards, goals and calendar

v1 ships one fixed, opinionated dashboard plus dedicated pages for trends, records, goals and the calendar; customization comes in v2.

- **Home dashboard:** today's readiness with its breakdown, form and fitness trend, this week's volume against the plan, next planned workouts, latest activities, the latest LLM insight, sync status.
- **Aggregations:** volume (distance, time, elevation, load) by week, month and year; filterable by sport and gear; time in zones by period; year-over-year comparison.
- **Records page:** all personal bests with history of when each was set.
- **Goals:** volume goals per period and sport (for example 2,000 km run in a year), with progress and projected finish.
- **Calendar:** month and week views showing completed and planned workouts, races, check-ins and injury or illness periods, with planned vs. completed status.
- **Performance:** every list and history view is paginated; dashboard queries target under 300 ms on 10 years of data.

## Health, wellness and nutrition

Health data in v1 serves readiness and LLM context; dedicated nutrition features wait for v2.

- **From Garmin:** resting HR, HRV, sleep (duration, stages, score), stress, Body Battery, steps, weight and body composition.
- **Daily check-in (v1):** a quick form for soreness, sleep quality, stress, motivation and an optional note.
- **Body metrics (v1):** manual weight, body fat and hydration entries alongside Garmin's.
- **Injury and illness log (v1):** free-text description with start and end dates. The LLM interprets it and always receives it as context. Optional menstrual cycle tracking feeds the same context.
- **Nutrition (v2):** daily carbohydrate and protein targets, in-workout fueling plans (carbs per hour) and race-day nutrition, then food logging via Open Food Facts.

## LLM integration and training planning

The LLM receives a compact, pre-built summary of the athlete's data, gives advice that cites that data, and can propose training plan changes that the user must approve.

### Providers

Each user configures one active provider at a time: Ollama (local or on another machine), OpenAI, Gemini, Perplexity or Anthropic. Two client implementations cover them all: an OpenAI-compatible client (Ollama, OpenAI, Gemini, Perplexity) and a native Anthropic client. Settings are base URL, model name and API key (encrypted). Sending health and GPS-derived data to cloud providers is accepted; the settings page states it plainly. Token cost tracking is not in scope.

### Context builder

Code, not the LLM, assembles a deterministic summary for every request, in priority order so it can be cut to fit smaller local models:

1. Athlete profile: sports, thresholds and zones, preferences and constraints the user wrote down
2. Full injury and illness history (always included)
3. Upcoming races and goals
4. Today's readiness with its breakdown, and recent check-ins
5. Load trend: weekly fitness, fatigue and form for the last 6 weeks
6. Activity summaries for the last 7–14 days
7. Current plan and adherence
8. Health trends: HRV, resting HR, sleep
9. Personal records

Every item carries a short reference (an activity or a date). The prompt asks the model to cite these, and the UI turns them into links so the user can check the reasoning. Prompt templates ship versioned in the repository and are not editable in the UI.

### Interaction modes

- **Chat:** free conversation with fresh context on every turn. History lasts for the session; conversations are not stored, searched or rated in v1.
- **Daily insight:** generated once a day after the morning sync, covering yesterday's training, today's readiness and today's planned workout. Stored and shown on the dashboard.
- **Weekly report:** generated every Monday morning, covering volume, load, adherence and recommendations for rest and training.

Medical-adjacent topics (injuries, illness, unusual heart rate) get a standard disclaimer, and clear red flags lead to a recommendation to see a doctor.

### Training plans

- **Races:** date, sport, distance, goal time, priority A, B or C.
- **Rolling plans:** the LLM plans the next 1–2 weeks from races, current load, readiness, injuries and the results of recent workouts (for example missed target paces or high HR drift). Regenerated weekly or on demand.
- **Structured output:** workouts come back as JSON validated against a schema: sport, date, title, steps (warm-up, interval, recovery, cool-down) with duration or distance, repeats and targets as zone, pace, power or HR. An invalid response is retried once with the validation errors, then reported as a failure.
- **Approval:** every change is a proposal showing added, changed and removed workouts. The user approves or rejects all or individual items; nothing changes without approval.
- **On the watch:** in v1 the user recreates planned workouts in Garmin manually; FIT workout export comes in v2.
- **Planned vs. completed:** automatic matching by date and sport, using the workout name recorded in the FIT file when present and the closest duration otherwise, with manual link and unlink. If automatic matching proves unreliable, v1 ships manual-only.
- **Adherence:** completion rate, actual vs. planned duration and load, and per-step target compliance, fed back into the next plan.

## Architecture

The app is a Go monolith: one binary serves the API and the embedded React build and runs background jobs, backed by a single TimescaleDB instance that also holds the job queue. No Redis, no message broker.

&#91;embedded content: deployment architecture · 1 app container, 1 database\]

The browser talks only to the app through the user's reverse proxy; outbound traffic goes to Garmin's Personal API and the chosen LLM provider, and map tiles load directly in the browser.

### Technology choices

| Layer | Choice | Reason |
| --- | --- | --- |
| Backend language | Go | Team choice; single static binary, low memory |
| HTTP router | chi | Standard library compatible, minimal |
| Database | PostgreSQL + TimescaleDB | Hypertables and compression for streams and daily metrics; no PostGIS in v1 |
| SQL access | sqlc + pgx | Type-safe generated code from plain SQL, works with Timescale functions |
| Migrations | goose, embedded, run on startup | Upgrades are just pulling a new image |
| Background jobs | River (Postgres-backed queue) | Retries, scheduling and transactional enqueue without extra infrastructure |
| FIT parsing | muktihari/fit | Decoded all three samples, including Zwift developer fields (half marathon in about 21 ms); current FIT profile; keeps unknown messages. tormoder/fit last released in 2023 on an older profile |
| GPX / TCX | gpxgo; small custom TCX parser |  |
| Large uploads | tusd (Go library) | Resumable chunked upload for multi-GB exports (v2 bulk import only) |
| Secret encryption | AES-256-GCM, key from Docker secret or env | Protects Garmin and LLM API keys at rest |
| API contract | REST with an OpenAPI spec | Generated TypeScript client for the frontend |
| Frontend | React + TypeScript + Vite | Team choice |
| Data fetching | TanStack Query | Caching and pagination |
| Charts | uPlot | Canvas rendering handles full-resolution streams |
| Maps | MapLibre GL with configurable raster tiles | OpenStreetMap default |
| UI | Tailwind with shadcn/ui components | Fast dark-mode-first styling |

### Repository layout

```text
cmd/server/              main entry point
internal/api/            HTTP handlers, auth, OpenAPI
internal/store/          sqlc queries, repositories scoped by user_id
internal/connectors/     connector interface; garmin/ (SSO, API client)
internal/importer/       FIT/GPX/TCX parsing; Garmin bulk export (v2)
internal/analytics/      zones, load, readiness, records, curves
internal/llm/            provider clients, context builder, prompts
internal/plans/          races, plans, proposals, matching
internal/jobs/           River workers and schedules
migrations/              goose SQL migrations
web/                     React app, embedded at build time
deploy/                  docker-compose.yml, example env
testdata/                sample FIT files and anonymized exports
```

## Non-functional requirements, security, deployment and CI

The app must stay fast on decades of data for up to 10 users, install with one Compose file, and keep secrets encrypted at rest.

### Performance and scale

| Requirement | Target |
| --- | --- |
| Users per instance | 1 typical, up to 10 |
| Activities per user | Tens of thousands, no history limit |
| List and history views | Always paginated |
| Dashboard and activity page | Under 300 ms server time with 10 years of data |
| Onboarding backfill (120 days) | Bound by Garmin's pacing rather than KudOS; resumable, with progress shown |
| Full recompute of derived values | Minutes |

### Security

- Local accounts with hashed passwords (argon2id) and secure session cookies.
- Garmin Personal API keys, cached Garmin tokens and LLM API keys encrypted with AES-256-GCM; the key comes from a Docker secret or environment variable and never lives in the database.
- LAN-only by design; HTTPS and any external exposure are the reverse proxy's job.
- No 2FA, audit log or compliance features in v1.

### Deployment and operations

- Pre-built Docker images; one `docker-compose.yml` with the app and TimescaleDB, plus an optional Ollama service.
- Configuration through environment variables, with a documented `.env.example`.
- Database migrations run automatically on startup.
- Backups are host-level backups of the database and data volumes; the README documents which volumes to back up.
- Logs are structured JSON on stdout. No metrics or tracing in v1.
- Network access needed only for Garmin sync, cloud LLMs and map tiles; everything else works offline.

### CI

- **From the start:** lint, unit tests and integration tests (against a real TimescaleDB container) on every pull request, for both Go and the frontend. The test corpus uses sample FIT files and an anonymized Garmin export.
- **Before the v1 release:** multi-arch image builds (amd64 and arm64, for NAS and Raspberry Pi users) published to GHCR on tagged releases.
- **Project setup:** MIT license, contribution guide, and issue templates for bug reports that ask for a sample file.

## Risks and open questions

Garmin's official Personal API removes the biggest earlier risk: sync no longer depends on an unofficial login flow. The main remaining risks are that the API is brand new (its official client reached 1.0.0 in October 2026) and that its terms target individual, non-commercial use.

| Risk | Impact | Mitigation |
| --- | --- | --- |
| Personal API is new: endpoints, limits or terms change ([Personal API FAQ](https://developer.garmin.com/gc-developer-program/personal/personal-faq/)) | Sync stops or loses fields | Isolated connector package, tests against recorded responses, clear UI failure state; manual upload keeps working |
| Personal API terms target individuals, not multi-user commercial apps | A shared coach instance (v2) may not fit | Each user brings their own key for their own data; review the terms before building coach mode |
| Personal API key expires (30 days to one year) | Sync stops | Show expiry in settings, warn 7 days ahead, pause cleanly on a rejected key |
| Personal API rate limits are undocumented | Throttling during the onboarding backfill | Paced, resumable backfill; 120-day default, one-year cap; measure limits during the sync milestone |
| Only recent history in v1 | Records and long-term trends cover the backfill window only | Label them "since \<date>"; full history through the v2 bulk import |
| Third-party FIT files lack metadata (Zwift: no thresholds, invalid local time, virtual coordinates, no workout steps) | Wrong times, maps or zones | Fallback rules in FIT parsing; subtype rules for virtual activities; intervals derived from target power |
| Undocumented export formats change (v2) | Strict bulk import fails | Skip and list unrecognized files; parsers tested against several real exports; error report names each file |
| Full export misses some activities (v2, reported by users) | Gaps in history | Deduplicate by Garmin activity ID against API-synced data |
| Small local models produce invalid workouts or ignore context | Poor plans and advice | Schema validation with one retry, prioritized context trimming |
| Planned vs. completed matching misfires | Wrong adherence data | Match on the FIT workout name first; manual link and unlink; manual-only fallback for v1 |
| Open self-registration on a shared LAN | Strangers can create accounts | Instance setting to disable registration after setup |
| OpenStreetMap tile usage policy | Tiles throttled or blocked | Configurable tile URL |

### Open questions

- [x] Sample FIT files: half marathon, interval session, Zwift ride, pool swim, strength workout
- [ ] Confirm during the sync milestone that the Personal API's activity download returns the same ZIP as the samples
- [ ] Full Garmin export (requested; needed only for the v2 bulk import)
