# Garmin Personal API fixtures

Recorded responses of Garmin's Personal API, replayed by the Garmin client tests. No test calls Garmin.

Record them with your own key. Keep the key in a file only you can read, and run the recorder in your own terminal:

```bash
GARMIN_PERSONAL_API_KEY_FILE=~/.config/kudos/garmin_key go run ./cmd/garmin-record -to 2026-10-28
```

It records the two days before `-to` and the first two activities in that range (`-from`, `-activities` and `-delay` change this). Pick a range with short activities: each activity's ZIP holds its full FIT file.

What it writes:

| File | Content |
| --- | --- |
| `index.json` | Every call: path, status, content type, size, rate-limit headers; the token's claim names and lifetime, never the token |
| `activities_search.json` | Activity search over the range |
| `activity_N.json`, `activity_N_workouts.json`, `activity_N.zip` | Details, workouts and the original-file download of each recorded activity |
| `daily_summaries.json`, `sleep_*.json`, `hrv_*.json`, `stress.json`, `body_battery.json`, `steps.json`, `heart_rate_day.json`, `weight.json`, `hydration.json`, `spo2_day.json`, `heart_rate_zones.json` | Health data |

Redaction happens before anything is written: profile, owner and device IDs become 1; names, emails, locations, activity names and notes become `"redacted"`; coordinates become 0; polylines are dropped; activity IDs are renumbered from 100001 in every file, path and ZIP entry; FIT files in the ZIPs go through `internal/anonymize`. The recorder then searches every file for each value it removed and stops if one reappears. Health values themselves (heart rate, sleep, weight…) are kept: they are what the tests need. Read the files before committing them.

Endpoints come from Garmin's Personal API OpenAPI spec and its official client, [garmin/connect-personal-api-python](https://github.com/garmin/connect-personal-api-python) (Apache-2.0).
