# KudOS task runner. See AGENTS.md section 1.

# Frontend steps run only once web/ exists (T0.6).
web := if path_exists("web/package.json") == "true" { "true" } else { "false" }

default:
    @just --list

# Run all code generators.
generate:
    go generate ./...
    {{ if web == "true" { "npm --prefix web run generate" } else { "" } }}

# Lint Go and frontend code.
lint:
    golangci-lint run ./...
    {{ if web == "true" { "npm --prefix web run lint" } else { "" } }}

# Run all tests with the race detector.
test:
    go test -race ./...
    {{ if web == "true" { "npm --prefix web run test" } else { "" } }}

# Generate, verify a clean diff, lint and test. Must pass before work is done.
check: generate
    git diff --exit-code
    just lint
    just test

# Build the server binary into bin/.
build:
    go build -o bin/kudos ./cmd/server

# Start TimescaleDB and the app with live reload.
dev:
    @echo "just dev is wired up in T0.7 (deploy/compose.dev.yml + air + Vite)"; exit 1
