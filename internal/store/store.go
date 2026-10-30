package store

import (
	"context"
	"fmt"
	"io/fs"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/iskyd/kudos/migrations"
)

// Open connects to the database and applies pending migrations.
func Open(ctx context.Context, url string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("creating pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("connecting to database: %w", err)
	}
	if err := Migrate(ctx, pool); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}

// Migrate applies every pending migration. Already-applied migrations are skipped.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	return migrate(ctx, pool, migrations.FS)
}

// gooseTemplate marks a file goose created but nobody has written yet.
const gooseTemplate = "SELECT 'up SQL query';"

func migrate(ctx context.Context, pool *pgxpool.Pool, fsys fs.FS) error {
	// Live reload (`just dev`) rebuilds as soon as goose creates a file: applying
	// the template would record the version and the real SQL would never run.
	names, err := fs.Glob(fsys, "*.sql")
	if err != nil {
		return fmt.Errorf("listing migrations: %w", err)
	}
	for _, name := range names {
		b, err := fs.ReadFile(fsys, name)
		if err != nil {
			return fmt.Errorf("reading migration: %w", err)
		}
		if strings.Contains(string(b), gooseTemplate) {
			return fmt.Errorf("migration %s is still goose's template: write it before running the server", name)
		}
	}

	db := stdlib.OpenDBFromPool(pool)
	defer db.Close() // closes only the database/sql wrapper, not the shared pool

	provider, err := goose.NewProvider(goose.DialectPostgres, db, fsys)
	if err != nil {
		return fmt.Errorf("creating migration provider: %w", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("applying migrations: %w", err)
	}
	return nil
}
