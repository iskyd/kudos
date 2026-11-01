# KudOS task runner. See AGENTS.md section 1.

default:
    @just --list

# Run all code generators.
generate:
    go generate ./...
    npm --prefix web run generate

# Lint Go and frontend code.
lint:
    go mod tidy -diff
    golangci-lint run ./...
    npm --prefix web run lint
    npm --prefix web run typecheck

# Run all tests with the race detector.
test:
    go test -race -coverprofile=coverage.out ./...
    npm --prefix web run test

# Generate, verify a clean diff, lint and test. Must pass before work is done.
check: generate
    git diff --exit-code
    just lint
    just test
    just coverage

# Enforce the Go coverage gates on the profile written by `just test`.
coverage:
    scripts/check-coverage.sh coverage.out

E2E_DB_PORT := env("KUDOS_E2E_DB_PORT", "5433")

# Fuzz the FIT, GPX and TCX decoders briefly (CI runs this on every pull request).
fuzz time="30s":
    go test ./internal/importer/fit -run '^$' -fuzz FuzzDecodeActivity -fuzztime {{time}}
    go test ./internal/importer/gpx -run '^$' -fuzz FuzzDecodeGPX -fuzztime {{time}}
    go test ./internal/importer/tcx -run '^$' -fuzz FuzzDecodeTCX -fuzztime {{time}}

# Run the Playwright end-to-end tests against the real server and a throwaway database.
e2e:
    #!/usr/bin/env bash
    set -euo pipefail
    export KUDOS_DB_PORT={{E2E_DB_PORT}}
    compose="docker compose -p kudos-e2e -f deploy/compose.dev.yml"
    trap "$compose down -v" EXIT
    $compose up -d --wait
    DATABASE_URL=postgres://kudos:kudos@127.0.0.1:{{E2E_DB_PORT}}/kudos?sslmode=disable npm --prefix web run test:e2e

# Build the frontend, then the server binary (embedding it) into bin/.
build:
    npm --prefix web run build
    go build -o bin/kudos ./cmd/server

DEV_PORT := env("KUDOS_PORT", "8080")
DEV_DB_PORT := env("KUDOS_DB_PORT", "5432")

# Start TimescaleDB, the Go server with live reload and the Vite dev server (http://localhost:5173).
dev:
    #!/usr/bin/env bash
    set -euo pipefail
    docker compose -f deploy/compose.dev.yml up -d --wait
    mkdir -p .data/files
    # Per-checkout dev key, never committed (.data/ is ignored).
    [ -s .data/encryption_key ] || head -c 32 /dev/urandom | base64 > .data/encryption_key
    export DATABASE_URL=postgres://kudos:kudos@127.0.0.1:{{DEV_DB_PORT}}/kudos ENCRYPTION_KEY_FILE=.data/encryption_key DATA_DIR=.data/files LISTEN_ADDR=127.0.0.1:{{DEV_PORT}} LOG_LEVEL=debug
    trap "kill 0" EXIT
    air &
    KUDOS_API_URL=http://127.0.0.1:{{DEV_PORT}} npm --prefix web run dev &
    wait

# Stop the development database (data is kept in a Docker volume).
dev-down:
    docker compose -f deploy/compose.dev.yml down

# Fill an existing account in the dev database with years of synthetic data (register it first).
seed user years="10":
    DATABASE_URL=postgres://kudos:kudos@127.0.0.1:{{DEV_DB_PORT}}/kudos DATA_DIR=.data/files go run ./cmd/seed -user {{user}} -years {{years}}

# Time the dashboard and activity page against seeded data, failing over 300 ms (CI runs it nightly).
bench years="10":
    KUDOS_BENCH_YEARS={{years}} go test ./internal/api -run '^$' -bench BenchmarkPages -benchtime 20x -timeout 2h
