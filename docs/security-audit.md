# Security and privacy audit (v1)

Done for T8.3 on 2026-11-12, against `AGENTS.md` section 3. Rerun it before each release.

## Data isolation (invariants 1 and 3)

- [x] Every query in `internal/store/queries/` filters by `user_id` in its SQL, except these, which cannot by design:
  - `CreateUser`, `GetUserCredentials` (login by username) and `GetSession` (lookup by cookie token): they find the user.
  - `ListDueGarminSyncs`, `ListCoachCandidates` and `ListPlanCandidates`: the schedulers' scans across users. They return user IDs and the scheduling fields only; each job then works for one user.
  - `TimescaleVersion`: reads no user data.
- [x] Every repository method has a cross-user test in `internal/store/*_test.go`. Method: list every query name, then find each one called as the second user (`bob`) in a store test. Of 156 queries, 22 had none; all are added now:
  - Garmin sync, backfill and imports (`TestGarminSyncAndBackfillIsolation`): `UpdateGarminToken`, `SetGarminKeyRejected`, `SetGarminSynced`, `StartGarminSyncAttempt`, `SetGarminSyncError`, `GetGarminBackfill`, `SetGarminBackfillListed`, `SetGarminBackfillHealthThrough`, `PauseGarminBackfill`, `RunGarminBackfill`, `FinishGarminBackfill`, `AddGarminActivityImport`, `NextGarminActivityImport`, `SetGarminActivityImportDone`, `SetGarminActivityImportFailed`, `RetryGarminActivityImports`, `CountGarminActivityImports`, `ListGarminActivityImportFailures`.
  - `SuggestEstimate` (`TestSuggestEstimateIsolation`): the same sport and day give each user their own suggestion, and an estimate citing another user's activity is rejected.
  - `GetUserTimezone` (`TestUpdateUserTimezoneIsolation`).
  - `UpsertOriginalFile` was already covered through the `importActivity` helper in `TestActivitiesIsolation`, where the same file hash gives each user their own row.

## Logs (invariant 38)

- [x] `just e2e` captures the server's log, at debug level, while every end-to-end flow runs. `web/e2e/scan-logs.mjs` then fails the run when:
  - a line is not a JSON record;
  - a field is not on the reviewed list (IDs, counts, durations, request metadata, wrapped error messages, River's bookkeeping), so a new field is reviewed before it ships;
  - a secret or coach text the run used appears anywhere: the Garmin keys, the password, the fake token, or the plan prompt and reply;
  - no request was logged, so a broken capture cannot pass.
- [x] The first scans found nothing to fix: no coordinates, health values, secrets, prompts or replies are logged.
- [ ] Follow-up, not a leak: a request the browser abandons (for example by navigating away) is logged as `request failed` at error level with `context canceled`. It is noise, not a server error.

## Sessions and CSRF (invariant 39)

- [x] The session cookie (`internal/api/session.go`) is `HttpOnly`, `SameSite=Lax`, `Path=/`, with a 30-day sliding expiry. It is `Secure` when the request came over HTTPS, directly or as `X-Forwarded-Proto: https` from a proxy. Trusting that header can only make the cookie stricter. Tested by `TestRegisterDefaultsAndSecureCookie`.
- [x] Only the session token's SHA-256 hash is stored, and logout deletes the session server-side (`TestLogout` replays a stolen cookie).
- [x] Every operation except health, register and login needs a session. `TestPublicOperations` keeps that list in step with the spec's `security: []`. Every route goes through the generated strict handler; the router adds none of its own.
- [x] Every state-changing request needs the session's CSRF token in `X-CSRF-Token`, compared in constant time. `http.CrossOriginProtection` also rejects browsers' cross-site writes before any handler, which covers login and register, the two writes made before a session exists (`TestCSRF`).

## Known vulnerabilities

- [x] `just audit` runs in CI on every pull request:
  - `govulncheck` (v1.9.0) fails when our code can reach a known Go vulnerability.
  - `npm audit --omit=dev` fails on any advisory in the packages that ship in the web build.
- [x] The first run found two reachable Go vulnerabilities, both fixed by a patch release:
  - GO-2026-6859 in `net/http`: Go 1.26.10 → 1.26.11 (`go.mod`).
  - GO-2026-6865 in `github.com/klauspost/compress`: v1.20.1 → v1.20.2.
- [x] One more advisory sits in a required module, but our code never calls the vulnerable function. `govulncheck` reports it without failing.
- [x] The npm packages that ship have no advisories. Moderate advisories remain in dev tooling (`minimatch` under ESLint and ts-morph, `deepmerge` under the shadcn CLI). They never reach the build, and their fixes need breaking major upgrades, so they are left for a tooling update.
