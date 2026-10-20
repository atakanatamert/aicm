// SPDX-License-Identifier: Apache-2.0

package omp

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite" // registers the "sqlite" driver
)

// dbTimeout bounds one read of agent.db; omp may hold a write lock briefly.
const dbTimeout = 5 * time.Second

// readDBKeys reads the plain API keys omp stores in agent.db (table
// auth_credentials, credential_type 'api_key'). The database is opened
// read-only and query-only. A missing file or an omp version without the
// table is not an error.
func readDBKeys(agentDir string) ([]keyEntry, error) {
	path := filepath.Join(agentDir, "agent.db")
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("statting %s: %w", path, err)
	}

	escaped := strings.NewReplacer("%", "%25", "?", "%3f", "#", "%23").Replace(filepath.ToSlash(path))
	db, err := sql.Open("sqlite", "file:"+escaped+"?mode=ro&_pragma=query_only(1)&_pragma=busy_timeout(2000)")
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}
	defer db.Close() //nolint:errcheck // read-only handle

	ctx, cancel := context.WithTimeout(context.Background(), dbTimeout)
	defer cancel()

	var tables int
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'auth_credentials'`).Scan(&tables); err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	if tables == 0 {
		return nil, nil
	}

	rows, err := db.QueryContext(ctx, `
		SELECT provider, data FROM auth_credentials
		WHERE credential_type = 'api_key' AND disabled_cause IS NULL
		ORDER BY provider, id`)
	if err != nil {
		return nil, fmt.Errorf("reading credentials from %s: %w", path, err)
	}
	defer rows.Close() //nolint:errcheck // read-only rows

	var entries []keyEntry
	for rows.Next() {
		var name, data string
		if err := rows.Scan(&name, &data); err != nil {
			return nil, fmt.Errorf("reading credentials from %s: %w", path, err)
		}
		entries = append(entries, newKeyEntries(name, []byte(data))...)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading credentials from %s: %w", path, err)
	}
	return entries, nil
}
