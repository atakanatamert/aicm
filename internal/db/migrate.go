// SPDX-License-Identifier: AGPL-3.0-or-later

package db

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/atakanatamert/aicm/migrations"
)

// newMigrator returns a goose provider over the embedded migrations.
// The returned close func releases the database/sql handle.
func newMigrator(pool *pgxpool.Pool) (*goose.Provider, func() error, error) {
	sqlDB := stdlib.OpenDBFromPool(pool)
	p, err := goose.NewProvider(goose.DialectPostgres, sqlDB, migrations.FS)
	if err != nil {
		_ = sqlDB.Close()
		return nil, nil, fmt.Errorf("creating goose provider: %w", err)
	}
	return p, sqlDB.Close, nil
}

// MigrateUp applies every pending goose migration.
func MigrateUp(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger) error {
	p, closeDB, err := newMigrator(pool)
	if err != nil {
		return err
	}
	defer closeDB() //nolint:errcheck // closing the borrowed handle cannot lose data
	res, err := p.Up(ctx)
	if err != nil {
		return fmt.Errorf("applying migrations: %w", err)
	}
	for _, r := range res {
		log.InfoContext(ctx, "applied migration", "version", r.Source.Version, "file", r.Source.Path, "duration", r.Duration)
	}
	return nil
}

// MigrateDown rolls back the most recent goose migration.
func MigrateDown(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger) error {
	p, closeDB, err := newMigrator(pool)
	if err != nil {
		return err
	}
	defer closeDB() //nolint:errcheck // closing the borrowed handle cannot lose data
	r, err := p.Down(ctx)
	if err != nil {
		return fmt.Errorf("rolling back migration: %w", err)
	}
	log.InfoContext(ctx, "rolled back migration", "version", r.Source.Version, "file", r.Source.Path)
	return nil
}

// MigrationStatus reports each migration and whether it is applied.
func MigrationStatus(ctx context.Context, pool *pgxpool.Pool) ([]*goose.MigrationStatus, error) {
	p, closeDB, err := newMigrator(pool)
	if err != nil {
		return nil, err
	}
	defer closeDB() //nolint:errcheck // closing the borrowed handle cannot lose data
	st, err := p.Status(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading migration status: %w", err)
	}
	return st, nil
}
