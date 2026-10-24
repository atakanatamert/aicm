// SPDX-License-Identifier: SUL-1.0

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
	dir         configDir
	ref         agentlink.AccountRef
	label       string
	plan        string
	org         string
	tier        string
	started     string
	billingMode string
	extraUsage  string
}

type claudeJSON struct {
	OAuthAccount *struct {
		AccountUUID               string `json:"accountUuid"`
		EmailAddress              string `json:"emailAddress"`
		OrganizationUUID          string `json:"organizationUuid"`
		OrganizationRateLimitTier string `json:"organizationRateLimitTier"`
		SubscriptionCreatedAt     string `json:"subscriptionCreatedAt"`
		HasExtraUsageEnabled      *bool  `json:"hasExtraUsageEnabled"`
	} `json:"oauthAccount"`
	// CustomAPIKeyResponses lists API keys the user approved for this
	// machine. Only whether the list has entries is read.
	CustomAPIKeyResponses *struct {
		Approved json.RawMessage `json:"approved"`
	} `json:"customApiKeyResponses"`
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
		details[agentlink.DetailBillingMode] = a.billingMode
		if a.extraUsage != "" {
			details[agentlink.DetailExtraUsage] = a.extraUsage
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
		billingMode, extraUsage := billingDetails(dir, &cfg)
		out = append(out, dirAccount{
			dir:         dir,
			ref:         agentlink.AccountRef{ProviderID: "claude", ExternalID: cfg.OAuthAccount.AccountUUID},
			label:       cfg.OAuthAccount.EmailAddress,
			plan:        plan,
			org:         cfg.OAuthAccount.OrganizationUUID,
			tier:        tier,
			started:     cfg.OAuthAccount.SubscriptionCreatedAt,
			billingMode: billingMode,
			extraUsage:  extraUsage,
		})
	}
	return out
}

// billingDetails reports whether this config dir bills an API key instead of
// the subscription, and whether paid extra usage is on. Only the presence of
// the keys is inspected; key values never leave the file.
func billingDetails(dir configDir, cfg *claudeJSON) (mode, extra string) {
	mode = "subscription"
	if settingsUseAPIKey(filepath.Join(dir.path, "settings.json")) {
		mode = "api_key"
	}
	if cfg.CustomAPIKeyResponses != nil && hasApprovedKey(cfg.CustomAPIKeyResponses.Approved) {
		mode = "api_key"
	}
	if cfg.OAuthAccount != nil && cfg.OAuthAccount.HasExtraUsageEnabled != nil {
		extra = "off"
		if *cfg.OAuthAccount.HasExtraUsageEnabled {
			extra = "on"
		}
	}
	return mode, extra
}

// settingsUseAPIKey reports whether a Claude settings file points the CLI at
// an API key or auth token instead of the subscription login.
func settingsUseAPIKey(path string) bool {
	data, err := os.ReadFile(path) //nolint:gosec // path is a harness config dir chosen by New
	if err != nil {
		return false
	}
	var settings struct {
		Env          map[string]json.RawMessage `json:"env"`
		APIKeyHelper string                     `json:"apiKeyHelper"`
	}
	if json.Unmarshal(data, &settings) != nil {
		return false
	}
	if strings.TrimSpace(settings.APIKeyHelper) != "" {
		return true
	}
	for _, name := range []string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN"} {
		if nonEmptyJSON(settings.Env[name]) {
			return true
		}
	}
	return false
}

// nonEmptyJSON reports whether a raw JSON value holds something other than
// null or an empty string. The value itself is not kept.
func nonEmptyJSON(raw json.RawMessage) bool {
	value := strings.TrimSpace(string(raw))
	return value != "" && value != "null" && value != `""`
}

// hasApprovedKey reports whether customApiKeyResponses.approved has entries.
func hasApprovedKey(raw json.RawMessage) bool {
	var list []string
	if json.Unmarshal(raw, &list) == nil {
		return len(list) > 0
	}
	var byKey map[string]json.RawMessage
	if json.Unmarshal(raw, &byKey) == nil {
		return len(byKey) > 0
	}
	return false
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
