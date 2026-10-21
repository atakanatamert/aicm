// SPDX-License-Identifier: Apache-2.0

package omp

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite" // registers the "sqlite" driver

	"github.com/atakanatamert/aicm/internal/agentlink"
)

// dbTimeout bounds one read of agent.db; omp may hold a write lock briefly.
const dbTimeout = 5 * time.Second

// openAgentDB opens omp's database read-only and query-only.
func openAgentDB(path string) (*sql.DB, error) {
	escaped := strings.NewReplacer("%", "%25", "?", "%3f", "#", "%23").Replace(filepath.ToSlash(path))
	db, err := sql.Open("sqlite", "file:"+escaped+"?mode=ro&_pragma=query_only(1)&_pragma=busy_timeout(2000)")
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}
	return db, nil
}

// hasCredentialsTable reports whether this omp version stores credentials in
// agent.db.
func hasCredentialsTable(ctx context.Context, db *sql.DB) (bool, error) {
	var tables int
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'auth_credentials'`).Scan(&tables); err != nil {
		return false, fmt.Errorf("reading agent.db: %w", err)
	}
	return tables > 0, nil
}

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

	db, err := openAgentDB(path)
	if err != nil {
		return nil, err
	}
	defer db.Close() //nolint:errcheck // read-only handle

	ctx, cancel := context.WithTimeout(context.Background(), dbTimeout)
	defer cancel()
	if ok, err := hasCredentialsTable(ctx, db); err != nil || !ok {
		return nil, err
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

// oauthProviderIDs maps omp's subscription logins to the hub catalog
// accounts the Claude Code and Codex harnesses report, whose external ids are
// the same account ids omp stores.
var oauthProviderIDs = map[string]string{
	"anthropic":    "claude",
	"openai-codex": "chatgpt",
}

// oauthAccount is one subscription login omp holds.
type oauthAccount struct {
	Name  string // omp provider name
	Ref   agentlink.AccountRef
	Email string
}

// readOAuthAccounts returns omp's subscription logins whose provider has a
// hub catalog account. The decoder keeps only the account id and email:
// tokens in the same blob are never read into memory as fields. A provider
// with several logins is left out, because a session line does not say which
// login it used.
func readOAuthAccounts(agentDir string) ([]oauthAccount, error) {
	path := filepath.Join(agentDir, "agent.db")
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("statting %s: %w", path, err)
	}
	db, err := openAgentDB(path)
	if err != nil {
		return nil, err
	}
	defer db.Close() //nolint:errcheck // read-only handle

	ctx, cancel := context.WithTimeout(context.Background(), dbTimeout)
	defer cancel()
	if ok, err := hasCredentialsTable(ctx, db); err != nil || !ok {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, `
		SELECT provider, data FROM auth_credentials
		WHERE credential_type = 'oauth' AND disabled_cause IS NULL
		ORDER BY provider, id`)
	if err != nil {
		return nil, fmt.Errorf("reading logins from %s: %w", path, err)
	}
	defer rows.Close() //nolint:errcheck // read-only rows

	byName := map[string][]oauthAccount{}
	for rows.Next() {
		var name, data string
		if err := rows.Scan(&name, &data); err != nil {
			return nil, fmt.Errorf("reading logins from %s: %w", path, err)
		}
		name = strings.ToLower(name)
		providerID, ok := oauthProviderIDs[name]
		if !ok {
			continue
		}
		var identity struct {
			AccountID string `json:"accountId"`
			Email     string `json:"email"`
		}
		if json.Unmarshal([]byte(data), &identity) != nil || identity.AccountID == "" {
			continue
		}
		byName[name] = append(byName[name], oauthAccount{
			Name:  name,
			Ref:   agentlink.AccountRef{ProviderID: providerID, ExternalID: identity.AccountID},
			Email: identity.Email,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading logins from %s: %w", path, err)
	}
	var out []oauthAccount
	for _, name := range []string{"anthropic", "openai-codex"} {
		if logins := byName[name]; len(logins) == 1 {
			out = append(out, logins[0])
		}
	}
	return out, nil
}
