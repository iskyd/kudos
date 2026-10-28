// Package testutil provides shared test infrastructure.
package testutil

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/iskyd/kudos/internal/store"
)

// Image is the TimescaleDB image used by tests. Keep it in sync with deploy/.
const Image = "timescale/timescaledb:2.31.0-pg18"

const templateDB = "kudos_template"

// Postgres is a TimescaleDB container shared by one test package. Start it in
// TestMain; each test gets its own database cloned from a migrated template.
type Postgres struct {
	container *postgres.PostgresContainer
	admin     *pgxpool.Pool
	config    *pgxpool.Config
	seq       atomic.Int64
}

// StartPostgres starts the container and builds the migrated template database.
func StartPostgres(ctx context.Context) (*Postgres, error) {
	c, err := postgres.Run(ctx, Image,
		postgres.WithDatabase("postgres"),
		postgres.WithUsername("kudos"),
		postgres.WithPassword("kudos"),
		postgres.BasicWaitStrategies(),
	)
	if err != nil {
		return nil, fmt.Errorf("starting timescaledb container: %w", err)
	}
	p := &Postgres{container: c}
	if err := p.init(ctx); err != nil {
		_ = p.Terminate(ctx)
		return nil, err
	}
	return p, nil
}

func (p *Postgres) init(ctx context.Context) error {
	dsn, err := p.container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		return fmt.Errorf("getting connection string: %w", err)
	}
	if p.config, err = pgxpool.ParseConfig(dsn); err != nil {
		return fmt.Errorf("parsing connection string: %w", err)
	}
	if p.admin, err = pgxpool.NewWithConfig(ctx, p.config); err != nil {
		return fmt.Errorf("connecting admin pool: %w", err)
	}

	// Database DDL cannot be parameterized or expressed in sqlc; names are sanitized identifiers.
	if _, err := p.admin.Exec(ctx, "CREATE DATABASE "+ident(templateDB)); err != nil {
		return fmt.Errorf("creating template database: %w", err)
	}
	tmpl, err := pgxpool.NewWithConfig(ctx, p.configFor(templateDB))
	if err != nil {
		return fmt.Errorf("connecting template database: %w", err)
	}
	err = store.Migrate(ctx, tmpl)
	tmpl.Close()
	if err != nil {
		return fmt.Errorf("migrating template database: %w", err)
	}

	// Block new connections (including TimescaleDB background workers) and drop
	// existing ones: CREATE DATABASE ... TEMPLATE needs an idle source.
	if _, err := p.admin.Exec(ctx, "ALTER DATABASE "+ident(templateDB)+" WITH ALLOW_CONNECTIONS false IS_TEMPLATE true"); err != nil {
		return fmt.Errorf("locking template database: %w", err)
	}
	if _, err := p.admin.Exec(ctx,
		"SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1 AND pid <> pg_backend_pid()", templateDB); err != nil {
		return fmt.Errorf("disconnecting template database: %w", err)
	}
	return nil
}

// Terminate stops the container.
func (p *Postgres) Terminate(ctx context.Context) error {
	if p.admin != nil {
		p.admin.Close()
	}
	if err := testcontainers.TerminateContainer(p.container, testcontainers.StopContext(ctx)); err != nil {
		return fmt.Errorf("terminating container: %w", err)
	}
	return nil
}

// NewDB returns a pool on a fresh, fully migrated database dropped after the test.
func (p *Postgres) NewDB(t testing.TB) *pgxpool.Pool {
	t.Helper()
	return p.newDB(t, templateDB)
}

// NewEmptyDB returns a pool on a fresh database with no migrations applied.
func (p *Postgres) NewEmptyDB(t testing.TB) *pgxpool.Pool {
	t.Helper()
	return p.newDB(t, "template0")
}

func (p *Postgres) newDB(t testing.TB, template string) *pgxpool.Pool {
	t.Helper()
	ctx := t.Context()
	name := fmt.Sprintf("test_%d", p.seq.Add(1))

	if _, err := p.admin.Exec(ctx, "CREATE DATABASE "+ident(name)+" TEMPLATE "+ident(template)); err != nil {
		t.Fatalf("creating test database: %v", err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, p.configFor(name))
	if err != nil {
		t.Fatalf("connecting test database: %v", err)
	}

	// t.Context is already canceled when cleanups run.
	cleanupCtx := context.WithoutCancel(ctx)
	t.Cleanup(func() {
		pool.Close()
		if _, err := p.admin.Exec(cleanupCtx, "DROP DATABASE "+ident(name)+" WITH (FORCE)"); err != nil {
			t.Errorf("dropping test database: %v", err)
		}
	})
	return pool
}

// URL returns the connection string of the database behind pool.
func URL(pool *pgxpool.Pool) string {
	return pool.Config().ConnString()
}

func (p *Postgres) configFor(database string) *pgxpool.Config {
	cfg := p.config.Copy()
	cfg.ConnConfig.Database = database
	return cfg
}

func ident(name string) string {
	return pgx.Identifier{name}.Sanitize()
}
