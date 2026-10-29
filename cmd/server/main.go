// Command server runs the KudOS HTTP server.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata" // timezone validation must not depend on the container having zoneinfo

	"github.com/riverqueue/river"

	"github.com/iskyd/kudos/internal/api"
	"github.com/iskyd/kudos/internal/config"
	"github.com/iskyd/kudos/internal/connectors/garmin"
	"github.com/iskyd/kudos/internal/connectors/garminsync"
	"github.com/iskyd/kudos/internal/crypto"
	"github.com/iskyd/kudos/internal/importer/ingest"
	"github.com/iskyd/kudos/internal/jobs"
	"github.com/iskyd/kudos/internal/store"
	"github.com/iskyd/kudos/internal/store/db"
	"github.com/iskyd/kudos/web"
)

const shutdownTimeout = 10 * time.Second

func main() {
	level := new(slog.LevelVar)
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
	if err := run(logger, level); err != nil {
		logger.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger, level *slog.LevelVar) error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	level.Set(cfg.LogLevel)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()

	pool, err := store.Open(ctx, cfg.DatabaseURL.Reveal())
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer pool.Close()
	logger.Info("database ready")

	cipher, err := crypto.New(cfg.EncryptionKey.Reveal())
	if err != nil {
		return fmt.Errorf("creating cipher: %w", err)
	}
	cursors, err := api.NewCursors(cfg.EncryptionKey.Reveal())
	if err != nil {
		return fmt.Errorf("creating cursors: %w", err)
	}
	garminClient := garmin.New(garmin.DefaultBaseURL, &http.Client{Timeout: time.Minute})
	backfill := &garminsync.Worker{Pool: pool, Connector: garminClient, Cipher: cipher, Logger: logger}

	jobClient, err := jobs.NewClient(pool, logger, func(w *river.Workers) { river.AddWorker(w, backfill) })
	if err != nil {
		return fmt.Errorf("creating job client: %w", err)
	}
	ingester := ingest.New(pool, jobClient, cfg.DataDir)
	backfill.Ingest = ingester
	// Detached from the signal context: canceling it would hard-stop running jobs; Stop below drains them.
	if err := jobClient.Start(context.WithoutCancel(ctx)); err != nil {
		return fmt.Errorf("starting job client: %w", err)
	}

	spa, err := web.Handler()
	if err != nil {
		return fmt.Errorf("loading frontend: %w", err)
	}

	listenAddr := cfg.ListenAddr
	srv := &http.Server{
		Addr: listenAddr,
		Handler: api.NewRouter(logger, api.NewServer(db.New(pool), ingester, cursors, api.Garmin{
			Auth: garminClient, Cipher: cipher, Backfill: garminsync.NewService(pool, jobClient),
		}, cfg.RegistrationEnabled), spa),
		ReadHeaderTimeout: 10 * time.Second,
	}

	errc := make(chan error, 1)
	go func() {
		logger.Info("listening", "addr", listenAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errc <- err
		}
		close(errc)
	}()

	select {
	case err := <-errc:
		return fmt.Errorf("listening: %w", err)
	case <-ctx.Done():
	}

	logger.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutting down http server: %w", err)
	}
	if err := jobClient.Stop(shutdownCtx); err != nil {
		return fmt.Errorf("stopping job client: %w", err)
	}
	return nil
}
