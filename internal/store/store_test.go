package store_test

import (
	"context"
	"log"
	"os"
	"strings"
	"testing"

	"github.com/iskyd/kudos/internal/store"
	"github.com/iskyd/kudos/internal/store/db"
	"github.com/iskyd/kudos/internal/testutil"
)

var pg *testutil.Postgres

func TestMain(m *testing.M) {
	ctx := context.Background()
	var err error
	if pg, err = testutil.StartPostgres(ctx); err != nil {
		log.Fatal(err)
	}
	code := m.Run()
	if err := pg.Terminate(ctx); err != nil {
		log.Print(err)
	}
	os.Exit(code)
}

func TestTimescaleVersion(t *testing.T) {
	t.Parallel()

	v, err := db.New(pg.NewDB(t)).TimescaleVersion(t.Context())
	if err != nil {
		t.Fatalf("TimescaleVersion: %v", err)
	}
	if !strings.HasPrefix(v, "2.") {
		t.Fatalf("TimescaleVersion = %q, want 2.x", v)
	}
}

func TestOpenMigratesAndIsIdempotent(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	pool, err := store.Open(ctx, testutil.URL(pg.NewEmptyDB(t)))
	if err != nil {
		t.Fatalf("Open on empty database: %v", err)
	}
	defer pool.Close()

	// Second and third runs must be no-ops, like every server restart.
	for range 2 {
		if err := store.Migrate(ctx, pool); err != nil {
			t.Fatalf("Migrate again: %v", err)
		}
	}
	if _, err := db.New(pool).TimescaleVersion(ctx); err != nil {
		t.Fatalf("TimescaleVersion after migrate: %v", err)
	}
}

func TestOpenErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		url  string
	}{
		{name: "unparsable url", url: "postgres://%zz"},
		{name: "unreachable server", url: "postgres://kudos:kudos@127.0.0.1:1/kudos?connect_timeout=1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if pool, err := store.Open(t.Context(), tt.url); err == nil {
				pool.Close()
				t.Fatal("Open() error = nil")
			}
		})
	}
}

func TestMigrateFailsOnClosedPool(t *testing.T) {
	t.Parallel()

	pool := pg.NewEmptyDB(t)
	pool.Close()
	if err := store.Migrate(t.Context(), pool); err == nil {
		t.Fatal("Migrate() error = nil")
	}
}
