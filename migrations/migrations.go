// Package migrations embeds the goose SQL migrations so the server applies them on startup.
// Create new files with: go tool goose -dir migrations create <name> sql
package migrations

import "embed"

// FS holds every migration file.
//
//go:embed *.sql
var FS embed.FS
