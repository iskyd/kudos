package jobs_test

import (
	"context"
	"errors"
	"log"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivertest"

	"github.com/iskyd/kudos/internal/jobs"
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

func discardLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func TestPingWorker(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	pool := pg.NewDB(t)

	w := rivertest.NewWorker(t, riverpgxv5.New(pool), &river.Config{}, &jobs.PingWorker{})
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after the test is best effort

	res, err := w.Work(ctx, t, tx, jobs.PingArgs{}, nil)
	if err != nil {
		t.Fatalf("Work: %v", err)
	}
	if res.EventKind != river.EventKindJobCompleted {
		t.Fatalf("EventKind = %s, want %s", res.EventKind, river.EventKindJobCompleted)
	}
}

func TestInsertTxCommitted(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	pool := pg.NewDB(t)

	client, err := jobs.NewClient(pool, discardLogger())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		_, err := client.InsertTx(ctx, tx, jobs.PingArgs{}, nil)
		return err
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	rivertest.RequireInserted(ctx, t, riverpgxv5.New(pool), jobs.PingArgs{}, nil)
}

func TestInsertTxRolledBackNeverRuns(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	pool := pg.NewDB(t)

	client, err := jobs.NewClient(pool, discardLogger())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	completed, cancel := client.Subscribe(river.EventKindJobCompleted)
	defer cancel()
	if err := client.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() {
		if err := client.Stop(context.WithoutCancel(ctx)); err != nil {
			t.Errorf("Stop: %v", err)
		}
	}()

	errRollback := errors.New("rollback")
	err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if _, err := client.InsertTx(ctx, tx, jobs.PingArgs{}, nil); err != nil {
			return err
		}
		return errRollback
	})
	if !errors.Is(err, errRollback) {
		t.Fatalf("BeginFunc error = %v, want rollback", err)
	}
	rivertest.RequireNotInserted(ctx, t, riverpgxv5.New(pool), jobs.PingArgs{}, nil)

	// A committed job runs, and it is the only one that ever does.
	var committedID int64
	err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		res, err := client.InsertTx(ctx, tx, jobs.PingArgs{}, nil)
		if err == nil {
			committedID = res.Job.ID
		}
		return err
	})
	if err != nil {
		t.Fatalf("committed insert: %v", err)
	}

	select {
	case ev := <-completed:
		if ev.Job.ID != committedID {
			t.Fatalf("completed job %d, want committed job %d", ev.Job.ID, committedID)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("committed job did not complete")
	}
	rivertest.RequireManyInserted(ctx, t, riverpgxv5.New(pool), []rivertest.ExpectedJob{{Args: jobs.PingArgs{}}})
}
