// Command seed fills an existing account without activities with years of
// synthetic training data (see package seed), then computes its derived values,
// for performance work and demos:
//
//	DATABASE_URL=postgres://… DATA_DIR=.data/files go run ./cmd/seed -user demo [-years 10] [-seed 1]
//
// Register the account first. The import also queues a derived-values job per
// activity, which a running server works again (the values do not change).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"time"

	"github.com/iskyd/kudos/internal/config"
	"github.com/iskyd/kudos/internal/importer/ingest"
	"github.com/iskyd/kudos/internal/jobs"
	"github.com/iskyd/kudos/internal/seed"
	"github.com/iskyd/kudos/internal/store"
	"github.com/iskyd/kudos/internal/store/db"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	err := run(ctx, os.Args[1:], os.Getenv, os.Stdout)
	stop()
	if err != nil {
		fmt.Fprintln(os.Stderr, "seed:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, getenv func(string) string, out io.Writer) error {
	fs := flag.NewFlagSet("seed", flag.ContinueOnError)
	user := fs.String("user", "", "username of the account to fill (required)")
	years := fs.Int("years", 10, "years of history, ending yesterday")
	seedValue := fs.Uint64("seed", 1, "random seed; the same seed generates the same data")
	if err := fs.Parse(args); err != nil {
		return err //nolint:wrapcheck // flag already reports the problem
	}
	if *user == "" || *years < 1 {
		return errors.New("-user is required and -years must be at least 1")
	}
	url := getenv("DATABASE_URL")
	if url == "" {
		return errors.New("DATABASE_URL is required")
	}
	dataDir := getenv("DATA_DIR")
	if dataDir == "" {
		dataDir = config.DefaultDataDir
	}

	pool, err := store.Open(ctx, url)
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer pool.Close()
	if err := store.Migrate(ctx, pool); err != nil {
		return fmt.Errorf("migrating: %w", err)
	}
	q := db.New(pool)
	u, err := q.GetUserCredentials(ctx, *user)
	if err != nil {
		return fmt.Errorf("finding user %q: %w", *user, err)
	}
	loc, err := time.LoadLocation(u.Timezone)
	if err != nil {
		return fmt.Errorf("loading the user's timezone: %w", err)
	}
	workers := &ingest.Workers{}
	client, err := jobs.NewClient(pool, slog.New(slog.DiscardHandler), workers.Register)
	if err != nil {
		return fmt.Errorf("creating job client: %w", err)
	}
	svc := ingest.New(pool, client, dataDir)

	started := time.Now()
	end := started.In(loc)
	s, err := seed.Run(ctx, svc, q, u.ID, loc, seed.Options{
		Days: int(end.Sub(end.AddDate(-*years, 0, 0)).Hours()/24 + 0.5), End: end, Seed: *seedValue,
		Progress: func(day time.Time, s seed.Stats) {
			if s.Days%365 == 0 {
				fmt.Fprintf(out, "%s: %d activities, %d samples\n", day.Format(time.DateOnly), s.Activities, s.Samples)
			}
		},
	})
	if err != nil {
		return fmt.Errorf("seeding: %w", err)
	}
	fmt.Fprintf(out, "imported %d days, %d activities and %d samples in %s\n", s.Days, s.Activities, s.Samples, time.Since(started).Round(time.Second))

	started = time.Now()
	n, err := svc.RecomputeDerived(ctx, u.ID)
	if err != nil {
		return fmt.Errorf("computing derived values: %w", err)
	}
	fmt.Fprintf(out, "computed derived values of %d activities in %s\n", n, time.Since(started).Round(time.Second))
	return nil
}
