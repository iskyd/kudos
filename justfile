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
    go test -race ./...
    npm --prefix web run test

# Generate, verify a clean diff, lint and test. Must pass before work is done.
check: generate
    git diff --exit-code
    just lint
    just test

# Run the Playwright end-to-end tests against a production build.
e2e:
    npm --prefix web run test:e2e

# Build the frontend, then the server binary (embedding it) into bin/.
build:
    npm --prefix web run build
    go build -o bin/kudos ./cmd/server

# Start TimescaleDB and the app with live reload.
dev:
    @echo "just dev is wired up in T0.7 (deploy/compose.dev.yml + air + Vite)"; exit 1
