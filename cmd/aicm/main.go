// SPDX-License-Identifier: AGPL-3.0-or-later

// Command aicm is the hub: HTTP API, embedded web UI and background jobs.
package main

import (
	"context"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/atakanatamert/aicm/internal/account"
	"github.com/atakanatamert/aicm/internal/agenthub"
	"github.com/atakanatamert/aicm/internal/alert"
	"github.com/atakanatamert/aicm/internal/audit"
	"github.com/atakanatamert/aicm/internal/auth"
	"github.com/atakanatamert/aicm/internal/charge"
	"github.com/atakanatamert/aicm/internal/config"
	"github.com/atakanatamert/aicm/internal/db"
	"github.com/atakanatamert/aicm/internal/fx"
	"github.com/atakanatamert/aicm/internal/httpapi"
	"github.com/atakanatamert/aicm/internal/jobs"
	"github.com/atakanatamert/aicm/internal/machine"
	"github.com/atakanatamert/aicm/internal/notify"
	"github.com/atakanatamert/aicm/internal/pricing"
	"github.com/atakanatamert/aicm/internal/provider"
	"github.com/atakanatamert/aicm/internal/provider/registry"
	"github.com/atakanatamert/aicm/internal/quota"
	"github.com/atakanatamert/aicm/internal/secrets"
	"github.com/atakanatamert/aicm/internal/settings"
	"github.com/atakanatamert/aicm/internal/syncer"
	usagepkg "github.com/atakanatamert/aicm/internal/usage"
	"github.com/atakanatamert/aicm/internal/version"
)

const usage = `Usage: ` + version.Name + ` <command> [flags]

Commands:
  serve                     run the hub (HTTP API, UI, jobs); applies migrations first
  migrate up|down|status    manage the database schema
  rotate-key --new-key-file PATH  re-wrap every org data key with a new master key
  create-admin --email E --name N
                            create an admin (password from AICM_ADMIN_PASSWORD)
  providers --markdown      print the supported-providers table
  providers --write FILE    replace the providers table in FILE between the markers
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
	case "rotate-key":
		err = rotateKey(ctx, args[1:], stdout)
	case "create-admin":
		err = createAdmin(ctx, args[1:], stdout)
	case "providers":
		err = providers(args[1:], stdout)
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

// rotateKey re-wraps every org data key with a new master key.
func rotateKey(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("rotate-key", flag.ContinueOnError)
	newKeyFile := fs.String("new-key-file", "", "file containing the base64-encoded new master key")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("parsing rotate-key flags: %w", err)
	}
	if *newKeyFile == "" {
		return errors.New("usage: rotate-key --new-key-file PATH")
	}
	oldKey, err := config.LoadMasterKey()
	if err != nil {
		return fmt.Errorf("loading old master key: %w", err)
	}
	raw, err := os.ReadFile(*newKeyFile)
	if err != nil {
		return fmt.Errorf("reading new key file: %w", err)
	}
	newKey, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil {
		return fmt.Errorf("decoding new master key: %w", err)
	}
	if len(newKey) != 32 {
		return fmt.Errorf("new master key must be 32 bytes, got %d", len(newKey))
	}

	_, _, pool, err := setup(ctx)
	if err != nil {
		return err
	}
	defer pool.Close()

	n, err := secrets.RotateMasterKey(ctx, pool, oldKey, newKey)
	if err != nil {
		return fmt.Errorf("rotating master key: %w", err)
	}
	fmt.Fprintf(stdout, "re-wrapped %d data keys; now set AICM_MASTER_KEY(_FILE) to the new key and restart\n", n)
	return nil
}

// createAdmin adds an admin in the default org. The password comes from the
// environment so it never appears in shell history or process arguments.
func createAdmin(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("create-admin", flag.ContinueOnError)
	email := fs.String("email", "", "admin email")
	name := fs.String("name", "", "admin name")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("parsing create-admin flags: %w", err)
	}
	if *email == "" {
		return errors.New("usage: create-admin --email E --name N")
	}
	password := os.Getenv("AICM_ADMIN_PASSWORD")
	if password == "" {
		return errors.New("AICM_ADMIN_PASSWORD is required")
	}
	cfg, log, pool, err := setup(ctx)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := auth.NewService(pool, log, cfg.PublicURL).CreateAdmin(ctx, *email, *name, password); err != nil {
		return err
	}
	fmt.Fprintln(stdout, "admin created:", *email)
	return nil
}

// providers prints the catalog table or rewrites it in a file.
func providers(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("providers", flag.ContinueOnError)
	markdown := fs.Bool("markdown", false, "print the supported-providers table")
	write := fs.String("write", "", "replace the providers block in this file")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("parsing providers flags: %w", err)
	}

	table := registry.Markdown(registry.All(nil))
	if *write != "" {
		if err := writeProvidersTable(*write, table); err != nil {
			return err
		}
		return nil
	}
	if !*markdown {
		return errors.New("usage: providers --markdown | --write FILE")
	}
	if _, err := io.WriteString(stdout, table); err != nil {
		return fmt.Errorf("writing providers table: %w", err)
	}
	return nil
}

const (
	providersStart = "<!-- providers:start -->"
	providersEnd   = "<!-- providers:end -->"
)

// writeProvidersTable replaces the table inside the README markers, keeping
// the note above it. A second run leaves the file unchanged.
func writeProvidersTable(path, table string) error {
	b, err := os.ReadFile(path) //nolint:gosec // path is an explicit CLI argument
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	src := string(b)
	start := strings.Index(src, providersStart)
	if start < 0 {
		return fmt.Errorf("%s: missing %q", path, providersStart)
	}
	body := start + len(providersStart)
	rel := strings.Index(src[body:], providersEnd)
	if rel < 0 {
		return fmt.Errorf("%s: missing %q", path, providersEnd)
	}
	end := body + rel

	// Keep the note above the table and drop the stale rows.
	prefix := src[body:end]
	if i := strings.Index(prefix, "\n|"); i >= 0 {
		prefix = prefix[:i+1]
	} else if strings.HasPrefix(prefix, "|") {
		prefix = "\n"
	}
	if prefix == "" {
		prefix = "\n"
	}
	if !strings.HasSuffix(prefix, "\n") {
		prefix += "\n"
	}
	if !strings.HasSuffix(prefix, "\n\n") {
		prefix += "\n"
	}

	out := src[:body] + prefix + table + src[end:]
	if out == src {
		return nil
	}
	if err := os.WriteFile(path, []byte(out), 0o600); err != nil { //nolint:gosec // existing file keeps its mode
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
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

	store, err := secrets.NewBuiltin(pool, cfg.MasterKey)
	if err != nil {
		return fmt.Errorf("building secret store: %w", err)
	}
	if err := store.Check(ctx); err != nil {
		return fmt.Errorf("checking secret store: %w", err)
	}

	// Services first, then the job registry (workers need the services),
	// then the River client (handlers need it to queue jobs).
	httpc := provider.NewHTTPClient(log, nil)
	providers := registry.All(httpc)
	catalog := func(id string) (provider.Info, bool) {
		p, ok := registry.ByID(providers, id)
		if !ok {
			return provider.Info{}, false
		}
		return p.Info(), true
	}
	prices := pricing.NewService(pool, httpc, log)
	usageSvc := usagepkg.NewService(pool, prices, log)
	fxSvc := fx.NewService(pool, log, fx.NewFrankfurter(httpc), fx.NewECB(httpc))
	quotaSvc := quota.NewService(pool, log, catalog, quota.PriceFromTable(prices))
	auditSvc := audit.NewService(pool)
	accounts := account.NewService(pool, store, providers, log)
	charges := charge.NewService(pool, fxSvc, log)
	syncSvc := syncer.NewService(pool, log, providers, store, quotaSvc, usageSvc, charges)
	quotaSvc.SetBalanceHook(charges.TopUpHook())
	alertSvc := alert.NewService(pool, log, quotaSvc, providers)
	settingsSvc := settings.NewService(pool, store)
	alertSvc.SetHook(notify.Hook())

	reg := jobs.NewRegistry()
	jobs.Register(reg, &fx.RefreshWorker{Service: fxSvc})
	reg.Periodic = append(reg.Periodic, fx.PeriodicJob())
	jobs.Register(reg, &pricing.ImportWorker{Service: prices})
	reg.Periodic = append(reg.Periodic, pricing.PeriodicJob())
	jobs.Register(reg, &syncer.SyncWorker{Service: syncSvc})
	jobs.Register(reg, &syncer.DispatchWorker{Service: syncSvc})
	reg.Periodic = append(reg.Periodic, syncer.PeriodicJob())
	jobs.Register(reg, &account.RollForwardWorker{Service: accounts, Hook: charges.RenewalHook()})
	reg.Periodic = append(reg.Periodic, account.PeriodicJob())
	jobs.Register(reg, &alert.EvaluateWorker{Service: alertSvc})
	reg.Periodic = append(reg.Periodic, alert.PeriodicJob())
	jobs.Register(reg, &notify.SendAlertWorker{
		Pool:      pool,
		Settings:  settingsSvc,
		PublicURL: cfg.PublicURL,
		Log:       log,
	})
	riverClient, err := jobs.NewClient(pool, log, reg)
	if err != nil {
		return err
	}
	if err := jobs.Start(ctx, riverClient, reg); err != nil {
		return err
	}
	machineSvc := machine.NewService(pool, store, cfg.PublicURL, log)
	agentHub := agenthub.New(pool, log, machineSvc, usageSvc, quotaSvc, accounts, providers)

	srv := &http.Server{
		Addr: cfg.ListenAddr,
		Handler: httpapi.NewRouter(httpapi.Deps{
			Log:       log,
			DB:        pool,
			Jobs:      riverClient,
			Secrets:   store,
			Auth:      auth.NewService(pool, log, cfg.PublicURL),
			Usage:     usageSvc,
			Quota:     quotaSvc,
			Audit:     auditSvc,
			Sync:      syncSvc,
			Accounts:  accounts,
			FX:        fxSvc,
			Charges:   charges,
			Providers: providers,
			Machines:  machineSvc,
			AgentHub:  agentHub,
			Alerts:    alertSvc,
			Settings:  settingsSvc,
		}),
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

	// WebSocket handlers end only when their connection closes, so the hub
	// goes first; otherwise srv.Shutdown waits for them until it times out.
	agentHub.Shutdown()

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
