// SPDX-License-Identifier: AGPL-3.0-or-later

// Package jobs sets up River, the Postgres-backed job queue that runs every
// scheduled hub job: provider sync, renewals, FX refresh and alert checks.
//
// Feature packages define their own job args and workers next to their
// service and register them in cmd/aicm/main.go through Register:
//
//	river.AddWorker(reg.Workers, &fx.RefreshWorker{...})
//	reg.Periodic = append(reg.Periodic, river.NewPeriodicJob(...))
//
// Every job that sends something outside the hub (email, provider calls) must
// set InsertOpts.UniqueOpts so a restart never repeats it.
package jobs

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
)

// Client is the River client type used across the hub.
type Client = river.Client[pgx.Tx]

// Registry collects workers and periodic jobs before the client starts.
type Registry struct {
	Workers  *river.Workers
	Periodic []*river.PeriodicJob
	count    int
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{Workers: river.NewWorkers()}
}

// Register adds a worker. Use it instead of river.AddWorker so the registry
// knows whether any worker exists.
func Register[T river.JobArgs](r *Registry, w river.Worker[T]) {
	river.AddWorker(r.Workers, w)
	r.count++
}

// Migrate applies River's own schema migrations. Goose owns every other table.
func Migrate(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger) error {
	m, err := rivermigrate.New(riverpgxv5.New(pool), &rivermigrate.Config{Logger: log})
	if err != nil {
		return fmt.Errorf("creating river migrator: %w", err)
	}
	res, err := m.Migrate(ctx, rivermigrate.DirectionUp, nil)
	if err != nil {
		return fmt.Errorf("migrating river schema: %w", err)
	}
	for _, v := range res.Versions {
		log.InfoContext(ctx, "applied river migration", "version", v.Version)
	}
	return nil
}

// NewClient builds the River client. With no registered workers the client is
// insert-only and Start is a no-op, so the hub runs before any job exists.
func NewClient(pool *pgxpool.Pool, log *slog.Logger, r *Registry) (*Client, error) {
	cfg := &river.Config{Logger: log}
	if r.count > 0 {
		cfg.Workers = r.Workers
		cfg.PeriodicJobs = r.Periodic
		cfg.Queues = map[string]river.QueueConfig{river.QueueDefault: {MaxWorkers: 10}}
	}
	c, err := river.NewClient(riverpgxv5.New(pool), cfg)
	if err != nil {
		return nil, fmt.Errorf("creating river client: %w", err)
	}
	return c, nil
}

// Start starts working jobs if any worker is registered.
func Start(ctx context.Context, c *Client, r *Registry) error {
	if r.count == 0 {
		return nil
	}
	if err := c.Start(ctx); err != nil {
		return fmt.Errorf("starting river: %w", err)
	}
	return nil
}

// Stop waits for running jobs to finish, up to ctx's deadline.
func Stop(ctx context.Context, c *Client, r *Registry) error {
	if r.count == 0 {
		return nil
	}
	if err := c.Stop(ctx); err != nil {
		return fmt.Errorf("stopping river: %w", err)
	}
	return nil
}
