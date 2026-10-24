// SPDX-License-Identifier: SUL-1.0

package claudecode

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/atakanatamert/aicm/internal/agentlink"
	"github.com/atakanatamert/aicm/internal/harness"
	"github.com/atakanatamert/aicm/internal/meter"
	"github.com/atakanatamert/aicm/internal/version"
)

// Opt-in Claude usage poll (Decision 8). It runs only for accounts whose owner
// allowed unofficial endpoints, reads the local Claude Code access token to
// make one request, and never refreshes, stores or returns that token.
const (
	usagePollInterval   = 15 * time.Minute
	usagePollFreshFor   = 30 * time.Minute
	usagePollTimeout    = 15 * time.Second
	usagePollDefaultGap = usagePollInterval
	usagePollRetryAfter = 30 * time.Minute
	usagePollMaxGap     = 4 * time.Hour
	usagePollCacheName  = "claude-usage-poll.json"
)

// usagePollURL is a variable so tests can point the poll at an httptest
// server. Nothing in the agent changes it.
var usagePollURL = "https://api.anthropic.com/api/oauth/usage"

// usagePollNow is a variable so tests can inject a clock.
var usagePollNow = time.Now

var (
	sevenDayOpusDef   = meter.Def{Key: "seven_day_opus", Label: "Weekly limit (Opus)", Unit: meter.UnitPercent, ResetKind: meter.ResetFixed, WindowMinutes: 10080}
	sevenDaySonnetDef = meter.Def{Key: "seven_day_sonnet", Label: "Weekly limit (Sonnet)", Unit: meter.UnitPercent, ResetKind: meter.ResetFixed, WindowMinutes: 10080}
)

var _ harness.Poller = (*Harness)(nil)

// usagePollCache is the per-account rate-limit state, persisted in the agent's
// data dir so restarts do not hammer the endpoint.
type usagePollCache struct {
	Accounts map[string]*usagePollEntry `json:"accounts,omitempty"`
}

type usagePollEntry struct {
	LastAttempt time.Time `json:"last_attempt,omitzero"`
	// RetryAfterUntil is the absolute end of a 429's Retry-After window.
	RetryAfterUntil time.Time `json:"retry_after_until,omitzero"`
	// MinGapSeconds is the backoff gap after a 429; zero means PollInterval.
	MinGapSeconds int `json:"min_gap_seconds,omitempty"`
}

// PollInterval is the minimum gap between usage polls.
func (h *Harness) PollInterval() time.Duration { return usagePollInterval }

// Poll reads the usage endpoint for opted-in Claude accounts whose statusline
// reading is stale, honouring the per-account cache and Retry-After.
func (h *Harness) Poll(ctx context.Context, cfg agentlink.AgentConfig) ([]agentlink.MeterReport, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	accounts := h.accounts(ctx)
	if len(accounts) == 0 {
		return nil, nil
	}

	optIn := map[string]bool{}
	for _, c := range cfg.Accounts {
		if c.Ref.ProviderID != "claude" || c.Status == "dismissed" || !c.AllowUnofficial {
			continue
		}
		optIn[c.Ref.ExternalID] = true
	}
	if len(optIn) == 0 {
		return nil, nil
	}

	now := usagePollNow().UTC()
	cache, err := h.loadUsagePollCache()
	if err != nil {
		return nil, err
	}
	fresh := h.freshStatuslineAccounts(accounts, now)

	var reports []agentlink.MeterReport
	changed := false
	for _, acct := range accounts {
		if !optIn[acct.ref.ExternalID] || fresh[acct.ref.ExternalID] {
			continue
		}
		entry := cache.entry(acct.ref.ExternalID)
		if !usagePollDue(entry, now) {
			continue
		}
		token, ok := h.usageToken(ctx, acct.dir, now)
		if !ok {
			continue
		}

		got, result, err := usagePollOnce(ctx, acct.ref, token, now)
		entry.LastAttempt = now
		changed = true
		if err != nil {
			entry.RetryAfterUntil = time.Time{}
			if saveErr := h.saveUsagePollCache(cache); saveErr != nil {
				return reports, saveErr
			}
			return reports, fmt.Errorf("polling Claude usage for %s: %w", acct.ref.ExternalID, err)
		}
		switch result.status {
		case usagePollRateLimited:
			entry.RetryAfterUntil = now.Add(result.retryAfter)
			entry.MinGapSeconds = doubledGap(entry.MinGapSeconds)
		case usagePollUnauthorized:
			// The token is likely expired. Claude Code refreshes it; wait for
			// the next interval without touching it.
			entry.RetryAfterUntil = time.Time{}
		case usagePollOK:
			entry.RetryAfterUntil = time.Time{}
			entry.MinGapSeconds = 0
			reports = append(reports, got...)
		}
	}
	if changed {
		if err := h.saveUsagePollCache(cache); err != nil {
			return reports, err
		}
	}
	return reports, nil
}

// freshStatuslineAccounts reports the accounts whose newest statusline capture
// is less than 30 minutes old. The statusline is cheaper and fresher, so the
// poll yields to it.
func (h *Harness) freshStatuslineAccounts(accounts []dirAccount, now time.Time) map[string]bool {
	fresh := map[string]bool{}
	f, err := os.Open(h.spoolPath()) //nolint:gosec // the agent's own data dir
	if err != nil {
		return fresh
	}
	defer f.Close() //nolint:errcheck // read-only spool

	cutoff := now.Add(-usagePollFreshFor)
	newest := map[string]time.Time{}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1<<20)
	for scanner.Scan() {
		var c capture
		if json.Unmarshal(scanner.Bytes(), &c) != nil || c.CapturedAt.IsZero() {
			continue
		}
		ref, ok := accountForTranscript(accounts, c.TranscriptPath)
		if !ok {
			continue
		}
		if c.CapturedAt.After(newest[ref.ExternalID]) {
			newest[ref.ExternalID] = c.CapturedAt
		}
	}
	for ref, capturedAt := range newest {
		if capturedAt.After(cutoff) {
			fresh[ref] = true
		}
	}
	return fresh
}

// usagePollDue reports whether the account may be polled at now. A 429's
// Retry-After governs until it passes; otherwise the normal minimum gap since
// the last attempt applies.
func usagePollDue(entry *usagePollEntry, now time.Time) bool {
	if !entry.RetryAfterUntil.IsZero() {
		return !now.Before(entry.RetryAfterUntil)
	}
	gap := time.Duration(entry.MinGapSeconds) * time.Second
	if gap <= 0 {
		gap = usagePollDefaultGap
	}
	return entry.LastAttempt.IsZero() || !now.Before(entry.LastAttempt.Add(gap))
}

// doubledGap doubles the gap after a 429, capped at 4 hours.
func doubledGap(seconds int) int {
	gap := time.Duration(seconds) * time.Second
	if gap <= 0 {
		gap = usagePollDefaultGap
	}
	gap *= 2
	if gap > usagePollMaxGap {
		gap = usagePollMaxGap
	}
	return int(gap / time.Second)
}

func (h *Harness) usagePollCachePath() string {
	return filepath.Join(h.dataDir, usagePollCacheName)
}

func (h *Harness) loadUsagePollCache() (*usagePollCache, error) {
	cache := &usagePollCache{}
	data, err := os.ReadFile(h.usagePollCachePath()) //nolint:gosec // the agent's own data dir
	if err != nil {
		if os.IsNotExist(err) {
			return cache, nil
		}
		return nil, fmt.Errorf("reading Claude usage poll cache: %w", err)
	}
	if len(data) == 0 {
		return cache, nil
	}
	if err := json.Unmarshal(data, cache); err != nil {
		// A corrupt cache costs at most one extra attempt; start fresh.
		return &usagePollCache{}, nil
	}
	return cache, nil
}

func (h *Harness) saveUsagePollCache(cache *usagePollCache) error {
	data, err := json.Marshal(cache)
	if err != nil {
		return fmt.Errorf("encoding Claude usage poll cache: %w", err)
	}
	if err := os.MkdirAll(h.dataDir, 0o700); err != nil {
		return fmt.Errorf("creating data dir: %w", err)
	}
	if err := os.WriteFile(h.usagePollCachePath(), data, 0o600); err != nil { //nolint:gosec // the agent's own data dir
		return fmt.Errorf("writing Claude usage poll cache: %w", err)
	}
	return nil
}

func (c *usagePollCache) entry(key string) *usagePollEntry {
	if c.Accounts == nil {
		c.Accounts = map[string]*usagePollEntry{}
	}
	entry := c.Accounts[key]
	if entry == nil {
		entry = &usagePollEntry{}
		c.Accounts[key] = entry
	}
	return entry
}

// usageToken reads the access token the same way Discover reads the plan: the
// default dir's macOS Keychain item, else the config dir's credentials file.
// The token lives only in the returned local value.
func (h *Harness) usageToken(ctx context.Context, dir configDir, now time.Time) (string, bool) {
	if dir.defaultDir && runtime.GOOS == "darwin" && h.keychain != nil {
		if out, err := h.keychain(ctx); err == nil {
			if token, ok := parseUsageToken(out, now); ok {
				return token, true
			}
		}
	}
	data, err := os.ReadFile(filepath.Join(dir.path, ".credentials.json")) //nolint:gosec // path is a harness config dir chosen by New
	if err != nil {
		return "", false
	}
	return parseUsageToken(data, now)
}

type usageCredentials struct {
	ClaudeAiOauth *struct {
		AccessToken string `json:"accessToken"`
		ExpiresAt   int64  `json:"expiresAt"`
	} `json:"claudeAiOauth"`
}

// parseUsageToken returns the local access token, or false when it is missing
// or expired. It never refreshes a token.
func parseUsageToken(data []byte, now time.Time) (string, bool) {
	var creds usageCredentials
	if json.Unmarshal(data, &creds) != nil || creds.ClaudeAiOauth == nil {
		return "", false
	}
	oauth := creds.ClaudeAiOauth
	if oauth.AccessToken == "" {
		return "", false
	}
	// Claude Code stores expiresAt in milliseconds since the Unix epoch.
	if oauth.ExpiresAt > 0 && !now.Before(time.UnixMilli(oauth.ExpiresAt)) {
		return "", false
	}
	return oauth.AccessToken, true
}

type usagePollStatus int

const (
	usagePollOK usagePollStatus = iota
	usagePollRateLimited
	usagePollUnauthorized
)

type usagePollResult struct {
	status     usagePollStatus
	retryAfter time.Duration
}

// usagePollOnce makes one request. It returns no reports unless the response
// is a 200 with a decodable body.
func usagePollOnce(ctx context.Context, ref agentlink.AccountRef, token string, now time.Time) ([]agentlink.MeterReport, usagePollResult, error) {
	reqCtx, cancel := context.WithTimeout(ctx, usagePollTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, usagePollURL, nil)
	if err != nil {
		return nil, usagePollResult{}, fmt.Errorf("building Claude usage request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")
	req.Header.Set("User-Agent", version.AgentBinary+"/"+version.Version)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, usagePollResult{}, fmt.Errorf("calling Claude usage endpoint: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // read-only response body

	switch resp.StatusCode {
	case http.StatusOK:
		var body usageResponse
		if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
			return nil, usagePollResult{}, fmt.Errorf("decoding Claude usage response: %w", err)
		}
		return usagePollReports(ref, body, now), usagePollResult{status: usagePollOK}, nil
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, usagePollResult{status: usagePollUnauthorized}, nil
	case http.StatusTooManyRequests:
		return nil, usagePollResult{
			status:     usagePollRateLimited,
			retryAfter: retryAfterDelay(resp.Header.Get("Retry-After"), now),
		}, nil
	default:
		return nil, usagePollResult{}, fmt.Errorf("claude usage endpoint returned %s", resp.Status)
	}
}

// retryAfterDelay parses Retry-After as seconds or an HTTP date, defaulting to
// 30 minutes when it is absent or unparseable.
func retryAfterDelay(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return usagePollRetryAfter
	}
	if seconds, err := strconv.Atoi(value); err == nil {
		if seconds <= 0 {
			return 0
		}
		return time.Duration(seconds) * time.Second
	}
	if at, err := http.ParseTime(value); err == nil {
		if delay := at.Sub(now); delay > 0 {
			return delay
		}
		return 0
	}
	return usagePollRetryAfter
}

// usageResponse is the subset of the endpoint this poll maps. Windows it does
// not map this task (seven_day_oauth_apps, extra_usage) are ignored.
type usageResponse struct {
	FiveHour       *usageWindow `json:"five_hour"`
	SevenDay       *usageWindow `json:"seven_day"`
	SevenDayOpus   *usageWindow `json:"seven_day_opus"`
	SevenDaySonnet *usageWindow `json:"seven_day_sonnet"`
}

type usageWindow struct {
	Utilization *float64 `json:"utilization"`
	ResetsAt    string   `json:"resets_at"`
}

func usagePollReports(ref agentlink.AccountRef, body usageResponse, now time.Time) []agentlink.MeterReport {
	windows := []struct {
		def    meter.Def
		window *usageWindow
	}{
		{fiveHourDef, body.FiveHour},
		{sevenDayDef, body.SevenDay},
		{sevenDayOpusDef, body.SevenDayOpus},
		{sevenDaySonnetDef, body.SevenDaySonnet},
	}

	reports := make([]agentlink.MeterReport, 0, len(windows))
	for _, item := range windows {
		if item.window == nil || item.window.Utilization == nil {
			continue
		}
		used := *item.window.Utilization
		limit := 100.0
		reading := meter.Reading{
			Key:        item.def.Key,
			Used:       &used,
			Limit:      &limit,
			CapturedAt: now,
			Source:     meter.SourceAPI,
		}
		if item.window.ResetsAt != "" {
			if resetsAt, err := time.Parse(time.RFC3339Nano, item.window.ResetsAt); err == nil {
				resetsAt = resetsAt.UTC()
				reading.ResetsAt = &resetsAt
			}
		}
		if err := reading.Validate(item.def); err != nil {
			continue
		}
		reports = append(reports, agentlink.MeterReport{
			Account: ref,
			Def:     item.def,
			Reading: reading,
		})
	}
	return reports
}
