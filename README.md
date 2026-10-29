# KudOS

**A free, open-source, self-hosted training platform for runners, cyclists, swimmers and triathletes.**

KudOS syncs your Garmin data, analyzes every activity at full resolution, tracks fitness, fatigue and readiness, and connects to the LLM of your choice for advice on training, rest and recovery. It runs on your own hardware, keeps your data on your network, and costs nothing: no subscriptions, no paid tiers, no features held back.

> **Status: in development.** KudOS has not had its first release yet. Installation instructions will be added before v1.

---

## Why KudOS

Training platforms keep their best analysis behind subscriptions, and your data lives on their servers. KudOS takes the opposite approach:

- **Free and open source.** Everything is included, under the MIT license.
- **Self-hosted.** It runs on a home server, NAS or Raspberry Pi on your LAN. Your training and health data never leave your network unless you choose a cloud LLM.
- **Bring your own AI.** Use a local model through Ollama, or your own API key for OpenAI, Anthropic, Gemini or Perplexity.
- **Transparent analytics.** KudOS computes its own metrics from your raw data and shows how it got there, instead of copying opaque scores.

---

## Features (v1)

### Your data, synced automatically
- **Garmin sync through the official Personal API.** Paste a key from Garmin Connect, choose how much history to pull (120 days by default, up to a year), and KudOS keeps syncing every hour or on demand. No Garmin password is ever stored.
- **Activities and health data:** original activity files plus sleep, HRV, resting heart rate, stress, Body Battery, steps and weight.
- **Manual uploads** of FIT, GPX and TCX files, and manual activities without a file.

### Every activity in detail
- **Seven sports:** road running, trail running, cycling (outdoor, indoor and virtual), pool and open-water swimming, triathlon, hiking and strength.
- **Full-resolution pages:** map, elevation, and synchronized charts for heart rate, pace, power and cadence, plus laps, running dynamics, swim lengths, strength sets and triathlon legs.
- **Workout execution:** structured workouts from your watch are compared step by step with their targets, and Zwift workouts against their target power.
- **Notes and context:** RPE (pre-filled from your watch), mood, weather, gear and pain tags.
- **Non-destructive editing:** trim, fix heart rate spikes, change sport, merge or split, always reversible.

### Analytics
- **Training zones rebuilt from your history.** Every watch file carries the thresholds valid that day, so KudOS reconstructs how your zones changed over time.
- **Training load, fitness, fatigue and form** across all sports, using power, pace, heart rate or RPE depending on what each activity recorded.
- **Readiness** from HRV, resting heart rate, sleep, training load and your daily check-in, with a visible breakdown.
- **Records and curves:** personal bests, power and pace curves, VO2max, race predictions, efficiency and aerobic decoupling.
- **Dashboards, goals and a training calendar** with planned vs. completed workouts.

### AI coaching
- **Chat, a daily insight and a Monday weekly report,** grounded in your data, with references to the activities and days behind every suggestion.
- **Your injury history is always considered.**
- **Rolling training plans** for your target races, adapted to how your recent workouts went. The AI proposes changes; nothing changes until you approve them.

### Built for households and small teams
- **Multiple users with full isolation:** up to about ten people on one instance, each seeing only their own data.

---

## How it works

KudOS is a single Go application with a React interface, backed by PostgreSQL with TimescaleDB for high-resolution time-series data.

- **Sync:** each user connects their own Garmin account through Garmin's [Personal API](https://developer.garmin.com/gc-developer-program/personal/personal-faq/). KudOS backfills recent history in the background, then polls for new activities and health data.
- **Parsing:** original FIT files are decoded and stored at full resolution, and the originals are kept so data can always be re-processed.
- **Analytics:** all numbers (zones, load, readiness, records) are computed by KudOS's own code, deterministically and testably.
- **AI:** KudOS builds a compact summary of your training, health and injury history and sends it to the model you configured. The model interprets and advises; it never computes your metrics, and it can't change your plan without your approval.

### Tech stack

- **Backend:** Go monolith (chi, log/slog), River for background jobs, [muktihari/fit](https://github.com/muktihari/fit) for FIT files
- **Database:** PostgreSQL with TimescaleDB, sqlc + pgx, goose migrations
- **Frontend:** React, TypeScript, Vite, TanStack Query, Tailwind, shadcn/ui, uPlot, MapLibre GL
- **API:** OpenAPI 3, with generated Go server interfaces and TypeScript client

### Repository layout

```text
cmd/server/              entry point
internal/api/            HTTP handlers, auth, OpenAPI
internal/store/          sqlc queries and repositories
internal/connectors/     source connectors (Garmin Personal API)
internal/importer/       FIT/GPX/TCX parsing
internal/analytics/      zones, load, readiness, records, curves
internal/llm/            provider clients, context builder, prompts
internal/plans/          races, plans, proposals, matching
internal/jobs/           background workers and schedules
migrations/              SQL migrations
web/                     React app, embedded in the binary at build time
deploy/                  deployment configuration
testdata/                sample FIT files and recorded API responses
```

---

## Roadmap

**v1:** Garmin Personal API backfill and sync, activity analysis, analytics, dashboards, AI coaching, rolling training plans.

**v2 and later**

- Full-history import from Garmin's account data export
- Coach and athlete roles
- Nutrition recommendations and food logging (Open Food Facts)
- Strava and other sources, with duplicate merging
- Manual strength logging with an exercise library
- Advanced cycling metrics (NP, IF, VI, W′ balance, VAM)
- Automatic interval detection and FIT workout export for the watch
- Customizable dashboards and data export

---

## Contributing

Contributions are welcome. Before opening a pull request:

1. Read [AGENTS.md](AGENTS.md). Its rules apply to humans and coding agents alike, including mandatory code generation and tests for every new or changed function.
2. Keep the change focused.
3. Install frontend dependencies once with `npm --prefix web ci`, then run `just check` (generation diff, lint, tests and coverage gates) and make sure it passes. `just e2e` runs the Playwright tests against the real server (embedding a production build) and a throwaway TimescaleDB container on port 5433 (`KUDOS_E2E_DB_PORT`).
4. Use [Conventional Commits](https://www.conventionalcommits.org) for commit messages.

For local development, `just dev` starts TimescaleDB (`deploy/compose.dev.yml`), the Go server with live reload and the Vite dev server: open http://localhost:5173. Ctrl-C stops the app; `just dev-down` stops the database. Set `KUDOS_PORT` or `KUDOS_DB_PORT` if 8080 or 5432 is taken. A per-checkout encryption key is created in `.data/`.

The Nix dev shell installs [pre-commit](https://pre-commit.com) hooks automatically (outside Nix: install pre-commit, pre-commit-hooks and gitleaks, then run `pre-commit install`). On every commit they fix whitespace, block merge markers, large files and leaked secrets (gitleaks), check the commit message format and run `just check`. Run them on the whole tree with `pre-commit run --all-files`.

When reporting a parsing bug, attach the FIT file if you can, but remove or crop any GPS track you don't want to share.

---

## Disclaimers

KudOS is an independent project and is not affiliated with, endorsed by or sponsored by Garmin. Garmin and Garmin Connect are trademarks of Garmin Ltd. or its subsidiaries. Use of the Garmin Personal API is subject to Garmin's terms.

KudOS provides training information and AI-generated suggestions, not medical advice. Talk to a doctor about injuries, illness or unusual heart rate readings.

## License

[MIT](LICENSE)
