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

# Run the Playwright end-to-end tests against a production build.
e2e:
    npm --prefix web run test:e2e

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
