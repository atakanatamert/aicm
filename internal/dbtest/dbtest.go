// SPDX-License-Identifier: SUL-1.0

// Package dbtest gives tests a fresh, migrated Postgres database.
//
// Set AICM_TEST_DATABASE_URL to a server where the user may create
// databases, for example
// postgres://aicm:aicm@localhost:55432/aicm?sslmode=disable. Tests that call
// New are skipped when it is unset.
package dbtest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"log/slog"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/atakanatamert/aicm/internal/db"
)

// New creates a database, applies every migration and returns a pool to it.
// The database is dropped when the test ends.
func New(t *testing.T) *pgxpool.Pool {
	t.Helper()
	base := os.Getenv("AICM_TEST_DATABASE_URL")
	if base == "" {
		t.Skip("AICM_TEST_DATABASE_URL not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var b [6]byte
	_, _ = rand.Read(b[:])
	name := "aicm_test_" + hex.EncodeToString(b[:])

	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatalf("connecting to test server: %v", err)
	}
	defer admin.Close(ctx)
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatalf("creating test database: %v", err)
	}

	u, err := url.Parse(base)
	if err != nil {
		t.Fatalf("parsing AICM_TEST_DATABASE_URL: %v", err)
	}
	u.Path = "/" + name
	pool, err := pgxpool.New(ctx, u.String())
	if err != nil {
		t.Fatalf("opening test pool: %v", err)
	}
	t.Cleanup(func() {
		pool.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		c, err := pgx.Connect(ctx, base)
		if err != nil {
			t.Errorf("connecting to drop test database: %v", err)
			return
		}
		defer c.Close(ctx)
		if _, err := c.Exec(ctx, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)"); err != nil {
			t.Errorf("dropping test database: %v", err)
		}
	})

	if err := db.MigrateUp(ctx, pool, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatalf("migrating test database: %v", err)
	}
	return pool
}

// DefaultOrgID returns the id of the org the first migration creates.
func DefaultOrgID(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	org, err := db.New(pool).GetOrgBySlug(context.Background(), "default")
	if err != nil {
		t.Fatalf("loading default org: %v", err)
	}
	return org.ID
}
