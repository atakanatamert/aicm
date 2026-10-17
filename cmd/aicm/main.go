// SPDX-License-Identifier: AGPL-3.0-or-later

// Command aicm is the hub: HTTP API, embedded web UI and background jobs.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/atakanatamert/aicm/internal/config"
	"github.com/atakanatamert/aicm/internal/db"
	"github.com/atakanatamert/aicm/internal/httpapi"
	"github.com/atakanatamert/aicm/internal/jobs"
	"github.com/atakanatamert/aicm/internal/version"
)

const usage = `Usage: ` + version.Name + ` <command> [flags]

Commands:
  serve                     run the hub (HTTP API, UI, jobs); applies migrations first
  migrate up|down|status    manage the database schema
  version                   print the version
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var err error
	switch args[0] {
	case "serve":
		err = serve(ctx, args[1:])
	case "migrate":
		err = migrate(ctx, args[1:], stdout)
	case "version":
		fmt.Fprintln(stdout, version.Name, version.Version)
	case "-h", "--help", "help":
		fmt.Fprint(stdout, usage)
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n%s", args[0], usage)
		return 2
	}
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	return 0
}

// setup loads the configuration, installs the logger and opens the pool.
func setup(ctx context.Context) (config.Hub, *slog.Logger, *pgxpool.Pool, error) {
	cfg, err := config.LoadHub()
	if err != nil {
		return cfg, nil, nil, fmt.Errorf("loading config: %w", err)
	}
	log := cfg.NewLogger()
	slog.SetDefault(log)
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return cfg, nil, nil, fmt.Errorf("opening database pool: %w", err)
	}
	return cfg, log, pool, nil
}

// migrateAll applies goose migrations, then River's.
func migrateAll(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger) error {
	if err := db.MigrateUp(ctx, pool, log); err != nil {
		return err
	}
	return jobs.Migrate(ctx, pool, log)
}

func migrate(ctx context.Context, args []string, stdout io.Writer) error {
	if len(args) != 1 {
		return errors.New("usage: migrate up|down|status")
	}
	_, log, pool, err := setup(ctx)
	if err != nil {
		return err
	}
	defer pool.Close()

	switch args[0] {
	case "up":
		return migrateAll(ctx, pool, log)
	case "down":
		return db.MigrateDown(ctx, pool, log)
	case "status":
		st, err := db.MigrationStatus(ctx, pool)
		if err != nil {
			return err
		}
		for _, s := range st {
			applied := "pending"
			if !s.AppliedAt.IsZero() {
				applied = s.AppliedAt.UTC().Format(time.RFC3339)
			}
			fmt.Fprintf(stdout, "%05d  %-8s  %s\n", s.Source.Version, s.State, applied)
		}
		return nil
	default:
		return fmt.Errorf("unknown migrate command %q (want up, down or status)", args[0])
	}
}

func serve(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	autoMigrate := fs.Bool("migrate", true, "apply pending migrations before serving")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("parsing serve flags: %w", err)
	}
	cfg, log, pool, err := setup(ctx)
	if err != nil {
		return err
	}
	defer pool.Close()

	if *autoMigrate {
		if err := migrateAll(ctx, pool, log); err != nil {
			return err
		}
	}

	reg := jobs.NewRegistry()
	riverClient, err := jobs.NewClient(pool, log, reg)
	if err != nil {
		return err
	}
	if err := jobs.Start(ctx, riverClient, reg); err != nil {
		return err
	}

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           httpapi.NewRouter(httpapi.Deps{Log: log, DB: pool}),
		ReadHeaderTimeout: 10 * time.Second,
	}
	errc := make(chan error, 1)
	go func() {
		log.Info("hub listening", "addr", cfg.ListenAddr, "version", version.Version)
		errc <- srv.ListenAndServe()
	}()

	var serveErr error
	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			serveErr = fmt.Errorf("serving http: %w", err)
		}
	case <-ctx.Done():
		log.Info("shutting down")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		serveErr = errors.Join(serveErr, fmt.Errorf("shutting down http: %w", err))
	}
	if err := jobs.Stop(shutdownCtx, riverClient, reg); err != nil {
		serveErr = errors.Join(serveErr, err)
	}
	return serveErr
}
