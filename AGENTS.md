# AGENTS.md — KudOS

Rules for any coding agent (and human) working in this repository. They are mandatory. If a rule blocks you, stop and ask instead of working around it.

KudOS is a free, open-source (MIT), self-hosted training platform: a Go monolith (API + background jobs + embedded React build) backed by PostgreSQL with TimescaleDB, deployed with Docker Compose on a LAN. The product spec ("KudOS — Product Spec & Architecture (v1)") is the source of truth for behavior. When this file and the spec disagree, ask.

---

## 1. Commands

Always use these targets. Never skip `make check` before declaring work done.

```bash
make generate   # go generate ./... and npm --prefix web run generate
make lint       # golangci-lint run ./... and npm --prefix web run lint
make test       # go test -race ./... and npm --prefix web run test
make check      # generate, then git diff --exit-code, then lint, then test
make dev        # docker compose up for TimescaleDB + app with live reload
```

`make check` must pass with a clean `git diff` after generation. If generation changes files you did not expect, you edited a generated file or forgot to regenerate.

---

## 2. Scope guardrails

- Build v1 only. Do not implement v2 items unless the task explicitly asks: Garmin bulk export import, coach/athlete roles, nutrition and food logging, Strava or any second source, cross-source merging, manual strength logging, advanced cycling metrics (NP/IF/VI/W′/VAM), automatic interval detection, FIT workout export, customizable dashboards, job status UI, data export.
- Never build: write-back to Garmin, segments, privacy zones, public sharing, notifications.
- Do not add a dependency, service or container that is not in the approved stack (section 4) without asking.

---

## 3. Product invariants (never violate)

### Data ownership and isolation
1. Every user-owned table has a non-null `user_id`. Every query against user-owned data filters by `user_id` **in the SQL itself** (the sqlc query), never only in handlers or services.
2. There is no admin role. All accounts are regular users. Registration is controlled only by the `REGISTRATION_ENABLED` environment variable.
3. Every repository method that reads or writes user data has a test proving user A cannot read or modify user B's rows.

### Source data
4. Source data is trusted as recorded. Never modify stored samples. User edits (trim, sport change, excluded HR spikes, merge, split) are stored as overrides and must be reversible.
5. Original files are always kept, gzip-compressed, in the data volume. Parsing must be reproducible from the original.
6. Ingestion is idempotent: upserts keyed by source and source activity ID (fallback: start time + sport + device). Re-running a sync or backfill must never duplicate data.

### Units and time
7. Store and compute in SI units only: metres, seconds, m/s, watts, bpm, kg, °C. Convert to display units (min/km, km/h, km) only in the frontend formatting layer.
8. Store timestamps in UTC plus the activity's local UTC offset. Never use `time.Local` or the server's timezone. Display activities in the local time where they happened.
9. Local offset = FIT `activity.local_timestamp − timestamp`. If missing or invalid (Zwift writes the FIT epoch, 1989-12-31), fall back to Garmin activity metadata, then the user's timezone.
10. Never assume 1-second sampling. Some activities use smart recording (1–6 s gaps). All averages, time-in-zone and load calculations are time-weighted, never sample-count based.

### FIT parsing (muktihari/fit)
11. Drop invalid sentinel values (`255`, `65535`, `4294967295`, and the library's `Invalid` constants), including in developer fields.
12. Running cadence in FIT is strides/min: steps/min = `(cadence + fractional_cadence) × 2`. Cycling cadence is rpm as-is.
13. Positions are semicircles; convert with the library's degree helpers. Vertical oscillation and step length are mm. Workout step values are raw and must be interpreted by their duration/target type (distance cm, time ms, speed mm/s).
14. For `length` and `set` messages, use each message's `start_time`; their `timestamp` repeats the activity start.
15. Pool swims: distance, pace and strokes come from `length` messages (active vs idle), not records. Compute SWOLF (length time in s + strokes); the watch leaves it empty.
16. Strength: match `set` to `exercise_title` by exercise category + exercise number, never by position. The decoded name is a string array; use element 0 only. Sets and exercise titles are read from the library's unrelated-messages list.
17. Unknown/proprietary messages are ignored for analysis, never an error.
18. Virtual activities (`sub_sport = virtual_activity`) never get a map, never contribute GPS-derived metrics, and are excluded from distance and elevation records. They still count toward volume and load.

### Analytics
19. Code computes every number. LLMs never compute metrics, scores or zones; they only interpret values the code produced.
20. Zones are versioned per sport with an effective-from date. An activity always uses the version valid on its date. A running FTP (running power) is never used for cycling, and vice versa.
21. Training load uses the first applicable method, in order: power-based TSS (cycling with power and cycling FTP; running with power and running FTP), pace-based rTSS, swim TSS from CSS, HR-based hrTSS, session RPE × duration.
22. Readiness is deterministic, shows its breakdown, and is hidden until at least 14 days of HRV and resting HR exist.
23. Per-activity derived values live in `activity_derived` and are invalidated (then recomputed by a background job) when zones or the activity change. Aggregations and fitness/fatigue/form curves are computed on demand.
24. Analytics functions are pure: inputs in, values out, no database, clock or network access inside them.

### Garmin Personal API
25. Use only Garmin's official Personal API. Never call reverse-engineered or undocumented Garmin endpoints, never store Garmin passwords, never implement SSO/MFA login flows.
26. Read-only. Never write to Garmin.
27. Requests are sequential with exponential backoff and jitter. The onboarding backfill is resumable, defaults to 120 days and is capped at one year.
28. A 401 triggers exactly one token re-mint from the key; a second failure pauses sync and surfaces a UI warning. Never retry in a tight loop.
29. No live Garmin calls in tests or CI. Use recorded responses under `testdata/garmin/`.

### LLM
30. One active provider per user. Two client implementations only: OpenAI-compatible and native Anthropic.
31. The context builder is deterministic code. It includes the full injury history on every request, and every item carries a reference ID the model must cite.
32. Prompt templates are versioned files under `internal/llm/prompts/`, embedded with `go:embed`. They are not editable at runtime.
33. Structured outputs (workouts, plan changes) are validated against a JSON Schema. On failure: retry once with the validation errors, then fail visibly. Never persist unvalidated output.
34. The LLM never changes a plan directly. It creates a `plan_proposal`; only an explicit user approval applies it.
35. Chat history is not persisted. Daily insights and weekly reports (Monday morning) are.

### Security and secrets
36. Garmin keys, cached tokens and LLM API keys are encrypted with AES-256-GCM using the key from the Docker secret or environment variable. The encryption key never touches the database.
37. Secrets are write-only through the API: accept them, never return them (return only "set" plus expiry metadata).
38. Never log secrets, tokens, passwords, raw health values, GPS coordinates or LLM prompts/responses. Log IDs and counts.
39. Passwords use argon2id. Session cookies are `HttpOnly`, `SameSite=Lax`, and `Secure` when served over HTTPS. State-changing endpoints require a CSRF token.

### API behavior
40. Every list endpoint is paginated (cursor-based). No unbounded queries, ever.
41. Errors use `application/problem+json` (RFC 9457) with a stable `type` per error kind.

---

## 4. Approved stack

| Area | Choice |
| --- | --- |
| Backend | Go (version pinned in `go.mod`), chi, log/slog (JSON) |
| Database | PostgreSQL + TimescaleDB, pgx/v5, sqlc, goose |
| Jobs | River (Postgres-backed) |
| FIT | github.com/muktihari/fit |
| API contract | OpenAPI 3 (`api/openapi.yaml`), oapi-codegen (Go), openapi-typescript + openapi-fetch (TS) |
| Frontend | React, TypeScript (strict), Vite, TanStack Query, Tailwind, shadcn/ui, uPlot, MapLibre GL |
| Tests | Go `testing`, testcontainers-go (TimescaleDB), Vitest, React Testing Library, MSW, Playwright |

---

## 5. Backend rules

### 5.1 Code generation — never hand-write generated code
- All code generators are pinned as Go tools in `go.mod` and run with `go tool`:
  ```bash
  go get -tool github.com/sqlc-dev/sqlc/cmd/sqlc
  go get -tool github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen
  go get -tool github.com/pressly/goose/v3/cmd/goose
  go get -tool github.com/riverqueue/river/cmd/river
  go get -tool go.uber.org/mock/mockgen
  ```
- Generation is triggered only through `//go:generate` directives, run with `go generate ./...` (via `make generate`). Example:
  ```go
  // internal/store/generate.go
  package store

  //go:generate go tool sqlc generate -f ../../sqlc.yaml
  ```
  ```go
  // internal/api/generate.go
  package api

  //go:generate go tool oapi-codegen -config oapi-codegen.yaml ../../api/openapi.yaml
  ```
- **Never create or edit files containing `// Code generated ... DO NOT EDIT.`** Change the source (SQL query, OpenAPI spec, interface) and regenerate.
- New migrations are created with `go tool goose -dir migrations create <name> sql`, never by hand-naming files.
- River's schema migrations are produced with the River CLI (`go tool river migrate-get ...`) into goose migration files, never written by hand.
- Mocks, when needed, come from `mockgen` via `//go:generate`. Prefer real implementations or small fakes; never mock the database (use testcontainers).
- Generated code is committed. CI runs `make generate` and fails on any diff.

### 5.2 Database and SQL
- All SQL lives in `internal/store/queries/*.sql` and is compiled by sqlc. No ORM, no query builders, no `fmt.Sprintf` SQL, no raw `pgx` queries outside generated code.
- Migrations live in `migrations/`, are written in SQL, and are immutable once merged. Fix mistakes with a new migration.
- Hypertables, compression policies and indexes are created in migrations, never at runtime.
- For TimescaleDB functions sqlc cannot type (e.g. `time_bucket`), add explicit casts so sqlc generates correct Go types.
- Every query on user data includes `user_id = $n` (see invariant 1). Every list query has `LIMIT` and a cursor.
- Writes that must succeed together run in one transaction, including enqueueing River jobs (`InsertTx`).

### 5.3 Go code style
- `gofmt`/`goimports` clean; `golangci-lint` clean with the repo config. No `//nolint` without a written reason on the same line.
- Accept `context.Context` as the first parameter of anything doing I/O and pass it through. No `context.Background()` outside `main` and tests.
- Wrap errors with `fmt.Errorf("doing x: %w", err)`. Use sentinel errors or typed errors for conditions callers branch on. Never `panic` outside `main` initialization.
- No package-level mutable state and no `init()` side effects. Wire dependencies through constructors in `cmd/server`.
- Keep packages aligned with the repository layout: `api`, `store`, `connectors`, `importer`, `analytics`, `llm`, `plans`, `jobs`. `analytics` imports nothing from `store`, `api` or `connectors`.
- Configuration comes only from environment variables, parsed and validated once at startup; document every variable in `deploy/.env.example`.
- River jobs are idempotent and take IDs as arguments, never large payloads.

### 5.4 Backend testing — mandatory
- **Every new or changed function gets tests in the same change.** No exceptions for "simple" functions; generated code is the only exemption.
- Tests are table-driven, call `t.Parallel()` where safe, and live next to the code (`foo_test.go`).
- Run with the race detector: `go test -race ./...`.
- Coverage gates (generated code excluded): ≥ 90% for `internal/analytics`, `internal/importer` and `internal/llm` (context builder and validation); ≥ 80% for every other package. CI fails below the gate.
- Repository tests run against a real TimescaleDB via testcontainers-go, with migrations applied. Each test uses its own schema or transaction.
- Every repository method has a cross-user isolation test (invariant 3).
- FIT parsing uses the corpus in `testdata/fit/` (half marathon, intervals, Zwift ride, pool swim, strength). Parsers have golden-file tests (`-update` flag to regenerate) and a Go fuzz test (`FuzzDecodeActivity`).
- Analytics tests assert against hand-computed expected values with explicit float tolerances.
- HTTP handlers are tested with `httptest` against the generated server interface, including auth, pagination and problem+json errors.
- Garmin connector tests replay recorded responses, including 401, 429 and malformed payloads.
- Every bug fix starts with a failing regression test.
- Test data contains no real secrets and no unredacted home GPS tracks.

---

## 6. Frontend rules

### 6.1 Structure and generation
- TypeScript `strict: true`. No `any`, no `@ts-ignore`; use `unknown` and narrow.
- The API client is generated from `api/openapi.yaml` with `npm run generate` (openapi-typescript) into `web/src/api/generated/`. Never edit generated files. Never call `fetch` directly; use the generated client wrapped in TanStack Query hooks under `web/src/api/`.
- UI primitives are added with the shadcn CLI (`npx shadcn@latest add <component>`), not copied or hand-written.
- Server state lives only in TanStack Query. Local UI state uses React state. No global stores for server data.

### 6.2 Behavior
- Dark mode first. Colors and spacing come from design tokens (CSS variables / Tailwind theme); no hard-coded colors in components.
- All unit conversion and formatting goes through `web/src/lib/format/` (pace min/km, speed km/h, distance, duration, local activity time). Components never do math on units.
- Every query renders explicit loading, error and empty states. Every list uses the API's cursor pagination.
- Charts use uPlot (canvas) for streams; never render full-resolution streams with SVG chart libraries. Charts and map share a synchronized hover cursor.
- Maps use MapLibre GL with the tile URL from server config. Virtual activities render no map.
- Browser storage (`localStorage`) is allowed only for UI preferences, never for user data or secrets. Secrets are write-only inputs: never displayed after saving.
- Plan proposals always render as a diff with explicit approve/reject; the UI never applies LLM output without a user action.
- English only and metric only in v1; keep user-facing strings in components simple and consistent.

### 6.3 Frontend testing — mandatory
- Every formatting/util function has Vitest unit tests.
- Every component with logic has React Testing Library tests; API calls are mocked with MSW using types from the generated client.
- Playwright end-to-end tests cover: registration/login, Personal API key entry and backfill progress, activity list → activity detail (charts + map), plan proposal approve/reject.
- `npm run lint` (ESLint + Prettier) and `npm run typecheck` must pass.

---

## 7. Workflow

1. Read the relevant spec section before changing behavior. If the change alters behavior described in the spec, say so in the PR description.
2. API changes start in `api/openapi.yaml`, then `make generate`, then implement handlers and frontend hooks.
3. Schema changes start with a goose migration, then sqlc queries, then `make generate`.
4. Keep changes small and focused; one concern per PR. Use Conventional Commits (`feat:`, `fix:`, `test:`, `refactor:`, `chore:`, `docs:`).
5. Never commit secrets, `.env` files, real user exports or database dumps.

### Definition of done
- [ ] `make check` passes locally (generation diff clean, lint clean, all tests green with `-race`).
- [ ] New or changed Go functions have tests; coverage gates hold.
- [ ] New repository methods have cross-user isolation tests.
- [ ] No hand-edited generated files.
- [ ] No new dependency outside the approved stack (or explicit approval recorded in the PR).
- [ ] No secrets, health values or coordinates in logs.
- [ ] Spec invariants in section 3 still hold.
