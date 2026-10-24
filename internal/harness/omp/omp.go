// SPDX-License-Identifier: SUL-1.0

// Package omp imports accounts and usage from omp (oh-my-pi).
//
// Discovery reads the plain API keys omp stores in agent.db (current omp) and
// auth.json (older omp). OAuth credentials are ignored: subscriptions are
// covered by the Claude Code and Codex harnesses, and subscription tokens
// never leave this machine.
package omp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/atakanatamert/aicm/internal/agentlink"
	"github.com/atakanatamert/aicm/internal/harness"
)

const ID = "omp"

var (
	_ harness.Harness   = (*Harness)(nil)
	_ harness.KeyReader = (*Harness)(nil)
)

// providerIDs maps omp's provider names to hub catalog ids. Providers not in
// this table have no adapter on the hub, so their keys are not imported.
var providerIDs = map[string]string{
	"anthropic":  "anthropic",
	"openai":     "openai",
	"openrouter": "openrouter",
	"deepseek":   "deepseek",
	"xai":        "xai",
	"moonshot":   "moonshot",
	"zai":        "zai",
	"typesafe":   "typesafe",
	"google":     "google-ai-studio",
}

// alsoProviderIDs lists extra catalog accounts one key stands for. A Z.ai
// key reads both the GLM Coding Plan quota and the separately billed
// pay-as-you-go API balance.
var alsoProviderIDs = map[string][]string{
	"zai": {"zai-api"},
}

// Harness reads omp's agent directory. The active directory is selected by
// PI_CODING_AGENT_DIR, defaulting to <home>/.omp/agent; named profiles under
// <home>/.omp/profiles/<p>/agent are read as well.
type Harness struct {
	dirs []string
}

// New returns a harness for the omp installation under home.
func New(home string) *Harness {
	var dirs []string
	if dir := os.Getenv("PI_CODING_AGENT_DIR"); dir != "" {
		dirs = append(dirs, filepath.Clean(dir))
	} else if home != "" {
		dirs = append(dirs, filepath.Join(home, ".omp", "agent"))
	}

	if home != "" {
		profiles := filepath.Join(home, ".omp", "profiles")
		entries, err := os.ReadDir(profiles) //nolint:gosec // path built from the caller's home
		if err == nil {
			for _, entry := range entries {
				if entry.IsDir() {
					dirs = append(dirs, filepath.Join(profiles, entry.Name(), "agent"))
				}
			}
		}
	}

	seen := make(map[string]struct{}, len(dirs))
	unique := dirs[:0]
	for _, dir := range dirs {
		if _, ok := seen[dir]; ok {
			continue
		}
		seen[dir] = struct{}{}
		unique = append(unique, dir)
	}
	return &Harness{dirs: unique}
}

// ID implements harness.Harness.
func (h *Harness) ID() string { return ID }

// keyEntry is one plain API key found in agent.db or auth.json.
type keyEntry struct {
	// Name is the omp provider name, lower-cased for matching session lines.
	Name string
	// ProviderID is the hub catalog id.
	ProviderID string
	Key        string
	Ref        agentlink.AccountRef
}

// Discover implements harness.Harness. It reports one account per distinct
// plain API key whose provider has a hub catalog id, plus the Claude and
// ChatGPT subscription logins omp holds (by account id, never the token).
func (h *Harness) Discover(_ context.Context) ([]agentlink.DiscoveredAccount, error) {
	entries, err := h.keyEntries()
	if err != nil {
		return nil, err
	}

	seen := make(map[agentlink.AccountRef]struct{}, len(entries))
	accounts := make([]agentlink.DiscoveredAccount, 0, len(entries))
	for _, entry := range entries {
		if _, ok := seen[entry.Ref]; ok {
			continue
		}
		seen[entry.Ref] = struct{}{}
		accounts = append(accounts, agentlink.DiscoveredAccount{
			Ref:       entry.Ref,
			Harness:   ID,
			Label:     fmt.Sprintf("%s key …%s", entry.ProviderID, lastFour(entry.Key)),
			AuthKind:  "api_key",
			HasAPIKey: true,
		})
	}

	for _, dir := range h.dirs {
		logins, err := readOAuthAccounts(dir)
		if err != nil {
			return nil, fmt.Errorf("reading omp logins in %s: %w", dir, err)
		}
		for _, login := range logins {
			if _, ok := seen[login.Ref]; ok {
				continue
			}
			seen[login.Ref] = struct{}{}
			accounts = append(accounts, agentlink.DiscoveredAccount{
				Ref:      login.Ref,
				Harness:  ID,
				Label:    login.Email,
				AuthKind: "agent_capture",
			})
		}
	}

	sort.Slice(accounts, func(i, j int) bool {
		if accounts[i].Ref.ProviderID != accounts[j].Ref.ProviderID {
			return accounts[i].Ref.ProviderID < accounts[j].Ref.ProviderID
		}
		return accounts[i].Ref.ExternalID < accounts[j].Ref.ExternalID
	})
	return accounts, nil
}

// ReadAPIKey implements harness.KeyReader. It returns a key only for an
// account Discover reported; the value goes straight to a secret_reply.
func (h *Harness) ReadAPIKey(ctx context.Context, ref agentlink.AccountRef) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("reading omp API key: %w", err)
	}
	entries, err := h.keyEntries()
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		if entry.Ref == ref {
			return entry.Key, nil
		}
	}
	return "", fmt.Errorf("omp: no plain API key for %s/%s", ref.ProviderID, ref.ExternalID)
}

// keyEntries reads the keys from every agent directory and returns the plain
// API keys with catalog providers. Entries are sorted for deterministic
// discovery when profiles hold several keys for one provider.
func (h *Harness) keyEntries() ([]keyEntry, error) {
	var all []keyEntry
	for _, dir := range h.dirs {
		entries, err := readAPIKeys(dir)
		if err != nil {
			return nil, fmt.Errorf("reading omp auth in %s: %w", dir, err)
		}
		all = append(all, entries...)
	}
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].Name != all[j].Name {
			return all[i].Name < all[j].Name
		}
		return all[i].Ref.ExternalID < all[j].Ref.ExternalID
	})
	return all, nil
}

// readAPIKeys reads one agent directory's keys: agent.db first, then
// auth.json. Missing files are not errors: omp installs that only use OAuth
// have no API keys.
func readAPIKeys(agentDir string) ([]keyEntry, error) {
	entries, err := readDBKeys(agentDir)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(agentDir, "auth.json")
	data, err := os.ReadFile(path) //nolint:gosec // harness path built by New
	if err != nil {
		if os.IsNotExist(err) {
			return entries, nil
		}
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return entries, nil
	}

	var stored map[string]json.RawMessage
	if err := json.Unmarshal(data, &stored); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	names := make([]string, 0, len(stored))
	for name := range stored {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		entries = append(entries, newKeyEntries(name, stored[name])...)
	}
	return entries, nil
}

// newKeyEntries builds the entries for omp's provider name and stored
// credential: the primary catalog account first, then any extra accounts the
// key stands for. OAuth, command-backed keys and providers without a hub
// catalog id give none.
func newKeyEntries(name string, raw json.RawMessage) []keyEntry {
	key, ok := plainAPIKey(raw)
	if !ok {
		return nil
	}
	name = strings.ToLower(name)
	primary, ok := providerIDs[name]
	if !ok {
		return nil
	}
	ids := append([]string{primary}, alsoProviderIDs[name]...)
	entries := make([]keyEntry, 0, len(ids))
	for _, providerID := range ids {
		entries = append(entries, keyEntry{
			Name:       name,
			ProviderID: providerID,
			Key:        key,
			Ref: agentlink.AccountRef{
				ProviderID: providerID,
				ExternalID: keyRef(key),
			},
		})
	}
	return entries
}

// plainAPIKey extracts a stored plain API key. A string value is a key; an
// object is a key only when its type is api_key. OAuth entries and keys backed
// by a shell command ("!...") are not plain keys and are not imported.
func plainAPIKey(raw json.RawMessage) (string, bool) {
	var literal string
	if err := json.Unmarshal(raw, &literal); err == nil {
		literal = strings.TrimSpace(literal)
		return literal, literal != "" && !strings.HasPrefix(literal, "!")
	}

	var stored struct {
		Type    string `json:"type"`
		Key     string `json:"key"`
		Access  string `json:"access"`
		Refresh string `json:"refresh"`
	}
	if err := json.Unmarshal(raw, &stored); err != nil {
		return "", false
	}
	if stored.Type == "oauth" || stored.Access != "" || stored.Refresh != "" {
		return "", false
	}
	if stored.Type != "" && stored.Type != "api_key" {
		return "", false
	}
	key := strings.TrimSpace(stored.Key)
	return key, key != "" && !strings.HasPrefix(key, "!")
}

// keyRef is the protocol's API-key account id: "key:" plus the first 16 hex
// characters of the key's SHA-256. The key itself never leaves this function.
func keyRef(key string) string {
	sum := sha256.Sum256([]byte(key))
	return "key:" + hex.EncodeToString(sum[:])[:16]
}

func lastFour(key string) string {
	if len(key) <= 4 {
		return key
	}
	return key[len(key)-4:]
}
