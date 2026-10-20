// SPDX-License-Identifier: Apache-2.0

// Package claudecode imports Claude Code's local account data, its per-request
// usage logs and its statusline rate-limit captures.
//
// Every file this package reads is read-only. Subscription tokens are never
// read beyond the plan fields of the credentials blob, and never stored,
// logged or sent anywhere.
package claudecode

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/atakanatamert/aicm/internal/agentlink"
	"github.com/atakanatamert/aicm/internal/harness"
)

// ID is the harness id used as UsageEvent.Source.
const ID = "claudecode"

var _ harness.Harness = (*Harness)(nil)

// Harness discovers Claude Code accounts and reads their usage and meters.
type Harness struct {
	home    string
	dataDir string
	dirs    []configDir
	// keychain reads the default dir's credentials blob; tests replace it to
	// keep discovery off the real Keychain.
	keychain func(context.Context) ([]byte, error)
}

type configDir struct {
	path string
	// defaultDir marks the implicit ~/.claude and ~/.config/claude dirs. Only
	// those use the default ~/.claude.json and the plain Keychain item.
	defaultDir bool
}

// New finds config dirs from CLAUDE_CONFIG_DIR (comma-separated) or, when
// unset, <home>/.claude and <home>/.config/claude (whichever exist).
func New(home, dataDir string) *Harness {
	h := &Harness{home: home, dataDir: dataDir, keychain: keychainCredentials}
	seen := map[string]bool{}
	add := func(path string, defaultDir bool) {
		path = filepath.Clean(path)
		if seen[path] {
			return
		}
		info, err := os.Stat(path) //nolint:gosec // config dir from CLAUDE_CONFIG_DIR or home
		if err != nil || !info.IsDir() {
			return
		}
		seen[path] = true
		h.dirs = append(h.dirs, configDir{path: path, defaultDir: defaultDir})
	}
	if raw := strings.TrimSpace(os.Getenv("CLAUDE_CONFIG_DIR")); raw != "" {
		for _, path := range strings.Split(raw, ",") {
			if path = strings.TrimSpace(path); path != "" {
				add(path, false)
			}
		}
		return h
	}
	add(filepath.Join(home, ".claude"), true)
	add(filepath.Join(home, ".config", "claude"), true)
	return h
}

// ID implements harness.Harness.
func (h *Harness) ID() string { return ID }

type dirAccount struct {
	dir     configDir
	ref     agentlink.AccountRef
	label   string
	plan    string
	org     string
	tier    string
	started string
}

type claudeJSON struct {
	OAuthAccount *struct {
		AccountUUID               string `json:"accountUuid"`
		EmailAddress              string `json:"emailAddress"`
		OrganizationUUID          string `json:"organizationUuid"`
		OrganizationRateLimitTier string `json:"organizationRateLimitTier"`
		SubscriptionCreatedAt     string `json:"subscriptionCreatedAt"`
	} `json:"oauthAccount"`
}

type credentialsJSON struct {
	ClaudeAiOauth *struct {
		SubscriptionType string `json:"subscriptionType"`
		RateLimitTier    string `json:"rateLimitTier"`
	} `json:"claudeAiOauth"`
}

// Discover implements harness.Harness. Dirs without an oauthAccount (API-key
// use) are skipped; their usage is not reported.
func (h *Harness) Discover(ctx context.Context) ([]agentlink.DiscoveredAccount, error) {
	accounts := h.accounts(ctx)
	if len(accounts) == 0 {
		return nil, nil
	}
	out := make([]agentlink.DiscoveredAccount, 0, len(accounts))
	for _, a := range accounts {
		details := map[string]string{}
		if a.tier != "" {
			details["rate_limit_tier"] = a.tier
		}
		if a.org != "" {
			details["organization_uuid"] = a.org
		}
		if tier := planTier(a.plan, a.tier); tier != "" {
			details[agentlink.DetailPlanTier] = tier
		}
		if a.started != "" {
			details[agentlink.DetailSubscriptionStarted] = a.started
		}
		if len(details) == 0 {
			details = nil
		}
		out = append(out, agentlink.DiscoveredAccount{
			Ref:      a.ref,
			Harness:  ID,
			Label:    a.label,
			Plan:     a.plan,
			AuthKind: "agent_capture",
			Details:  details,
		})
	}
	return out, nil
}

func (h *Harness) accounts(ctx context.Context) []dirAccount {
	if ctx == nil {
		ctx = context.Background()
	}
	var out []dirAccount
	for _, dir := range h.dirs {
		var cfg claudeJSON
		data, err := os.ReadFile(h.configPath(dir)) //nolint:gosec // path is a harness config dir chosen by New
		if err != nil || json.Unmarshal(data, &cfg) != nil {
			continue
		}
		if cfg.OAuthAccount == nil || cfg.OAuthAccount.AccountUUID == "" {
			continue
		}
		plan, tier := h.credentials(ctx, dir)
		if tier == "" {
			tier = cfg.OAuthAccount.OrganizationRateLimitTier
		}
		out = append(out, dirAccount{
			dir:     dir,
			ref:     agentlink.AccountRef{ProviderID: "claude", ExternalID: cfg.OAuthAccount.AccountUUID},
			label:   cfg.OAuthAccount.EmailAddress,
			plan:    plan,
			org:     cfg.OAuthAccount.OrganizationUUID,
			tier:    tier,
			started: cfg.OAuthAccount.SubscriptionCreatedAt,
		})
	}
	return out
}

func (h *Harness) configPath(dir configDir) string {
	if dir.defaultDir {
		return filepath.Join(h.home, ".claude.json")
	}
	return filepath.Join(dir.path, ".claude.json")
}

// credentials returns only subscriptionType and rateLimitTier. Every other
// field of the blob, including tokens, is discarded by the decoder.
func (h *Harness) credentials(ctx context.Context, dir configDir) (plan, tier string) {
	if dir.defaultDir && runtime.GOOS == "darwin" && h.keychain != nil {
		// The plain Keychain item belongs to the default config dir; explicit
		// CLAUDE_CONFIG_DIR dirs use the hashed service name, which this
		// package does not guess.
		if out, err := h.keychain(ctx); err == nil {
			if p, t, ok := parseCredentials(out); ok {
				return p, t
			}
		}
	}
	data, err := os.ReadFile(filepath.Join(dir.path, ".credentials.json")) //nolint:gosec // path is a harness config dir chosen by New
	if err != nil {
		return "", ""
	}
	if p, t, ok := parseCredentials(data); ok {
		return p, t
	}
	return "", ""
}

func keychainCredentials(ctx context.Context) ([]byte, error) {
	out, err := exec.CommandContext(ctx, "security",
		"find-generic-password", "-s", "Claude Code-credentials", "-w").Output() //nolint:gosec // fixed command, read-only lookup
	if err != nil {
		return nil, fmt.Errorf("reading Claude Code credentials from Keychain: %w", err)
	}
	return out, nil
}

func parseCredentials(data []byte) (plan, tier string, ok bool) {
	var creds credentialsJSON
	if json.Unmarshal(data, &creds) != nil || creds.ClaudeAiOauth == nil {
		return "", "", false
	}
	return creds.ClaudeAiOauth.SubscriptionType, creds.ClaudeAiOauth.RateLimitTier, true
}

func (h *Harness) spoolPath() string {
	return filepath.Join(h.dataDir, "claude-statusline.jsonl")
}

// planTier maps Claude's subscription type and rate limit tier to the hub
// catalog's tier ids: "pro", "max_5x", "max_20x". Unknown plans give "".
func planTier(subscriptionType, rateLimitTier string) string {
	tier := strings.ToLower(rateLimitTier)
	switch {
	case strings.Contains(tier, "max_20x"):
		return "max_20x"
	case strings.Contains(tier, "max_5x"):
		return "max_5x"
	}
	if strings.EqualFold(subscriptionType, "pro") {
		return "pro"
	}
	return "" // Max without a tier: the price is unknown, so leave it unset
}
