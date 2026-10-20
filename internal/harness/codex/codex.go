// SPDX-License-Identifier: Apache-2.0

// Package codex imports the ChatGPT account and quota data used by the Codex
// CLI. It reads Codex's local files read-only and may start the official
// codex app-server to read fresh rate-limit windows. It never refreshes an
// OAuth token and never returns one.
package codex

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/atakanatamert/aicm/internal/agentlink"
	"github.com/atakanatamert/aicm/internal/harness"
)

// ID is the harness id used as UsageEvent.Source.
const ID = "codex"

const providerID = "chatgpt"

// Harness reads Codex accounts, rollouts and rate limits.
type Harness struct {
	dir string

	mu      sync.Mutex
	account *agentlink.DiscoveredAccount
	now     func() time.Time
}

var _ harness.Harness = (*Harness)(nil)
var _ harness.Poller = (*Harness)(nil)

// New uses CODEX_HOME, else <home>/.codex.
func New(home string) *Harness {
	dir := os.Getenv("CODEX_HOME")
	if dir == "" {
		dir = filepath.Join(home, ".codex")
	}
	return &Harness{dir: dir, now: time.Now}
}

// ID returns the harness id.
func (h *Harness) ID() string { return ID }

// Discover finds the ChatGPT account configured for Codex. auth.json is read
// only to decode the id_token payload; access and refresh tokens are never
// decoded. Keyring builds have no auth.json, so the account is taken from the
// app-server account/read result when it exposes an account id.
func (h *Harness) Discover(ctx context.Context) ([]agentlink.DiscoveredAccount, error) {
	acct, authPresent, err := h.accountFromAuth()
	if err != nil {
		return nil, err
	}
	if acct != nil {
		h.setAccount(acct)
		return []agentlink.DiscoveredAccount{*acct}, nil
	}
	if authPresent {
		// API-key auth (or an unusable id_token) has no ChatGPT account.
		return nil, nil
	}

	acct, err = h.accountFromAppServer(ctx)
	if err != nil {
		return nil, err
	}
	if acct == nil {
		return nil, nil
	}
	h.setAccount(acct)
	return []agentlink.DiscoveredAccount{*acct}, nil
}

// Scan reads new rollout lines and returns the advanced cursors. Compressed
// rollouts are skipped; their count is reported in the returned error so the
// agent can surface it as HarnessStatus.Error.
func (h *Harness) Scan(ctx context.Context, cursors harness.Cursors) (harness.Batch, error) {
	acct, err := h.accountForScan()
	if err != nil {
		return harness.Batch{Cursors: copyCursors(cursors)}, err
	}
	if acct == nil {
		return harness.Batch{Cursors: copyCursors(cursors)}, nil
	}
	return h.scanRollouts(ctx, acct.Ref, cursors)
}

// accountFromAuth reports whether auth.json exists and, when it holds a
// ChatGPT id_token, the account it describes.
func (h *Harness) accountFromAuth() (*agentlink.DiscoveredAccount, bool, error) {
	data, err := os.ReadFile(filepath.Join(h.dir, "auth.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, true, fmt.Errorf("reading codex auth: %w", err)
	}

	// Only id_token is decoded. The struct omits access_token, refresh_token
	// and OPENAI_API_KEY so they never live in memory here.
	var auth struct {
		Tokens struct {
			IDToken string `json:"id_token"`
		} `json:"tokens"`
	}
	if err := json.Unmarshal(data, &auth); err != nil {
		return nil, true, fmt.Errorf("parsing codex auth: %w", err)
	}
	if auth.Tokens.IDToken == "" {
		return nil, true, nil
	}

	claims, err := decodeIDToken(auth.Tokens.IDToken)
	if err != nil {
		return nil, true, fmt.Errorf("decoding codex id_token: %w", err)
	}
	if claims.OpenAIAuth.ChatGPTAccountID == "" {
		return nil, true, nil
	}
	acct := discoveredAccount(
		claims.OpenAIAuth.ChatGPTAccountID,
		claims.Email,
		claims.OpenAIAuth.ChatGPTPlanType,
	)
	if started := claims.OpenAIAuth.SubscriptionActiveStart; started != "" && claims.OpenAIAuth.ChatGPTPlanType != "" {
		acct.Details = map[string]string{
			agentlink.DetailPlanTier:            strings.ToLower(claims.OpenAIAuth.ChatGPTPlanType),
			agentlink.DetailSubscriptionStarted: started,
		}
	}
	return acct, true, nil
}

// accountForScan re-reads auth.json so a changed login is picked up. The
// app-server result cached by Discover is used only when auth.json is absent.
func (h *Harness) accountForScan() (*agentlink.DiscoveredAccount, error) {
	acct, present, err := h.accountFromAuth()
	if err != nil {
		return nil, err
	}
	if acct != nil {
		h.setAccount(acct)
		return acct, nil
	}
	if present {
		return nil, nil
	}
	return h.cachedAccount(), nil
}

func (h *Harness) accountFromAppServer(ctx context.Context) (*agentlink.DiscoveredAccount, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	client, err := startAppServer(ctx)
	if err != nil {
		// Not installed is not an error; a missing account is reported as none.
		if errors.Is(err, errCodexNotFound) {
			return nil, nil
		}
		return nil, err
	}
	defer client.close()

	if err := client.initialize(ctx); err != nil {
		return nil, err
	}
	return client.readAccount(ctx)
}

func (h *Harness) setAccount(acct *agentlink.DiscoveredAccount) {
	h.mu.Lock()
	h.account = acct
	h.mu.Unlock()
}

func (h *Harness) cachedAccount() *agentlink.DiscoveredAccount {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.account
}

func (h *Harness) nowTime() time.Time { return h.now() }

func discoveredAccount(accountID, email, plan string) *agentlink.DiscoveredAccount {
	return &agentlink.DiscoveredAccount{
		Ref: agentlink.AccountRef{
			ProviderID: providerID,
			ExternalID: accountID,
		},
		Harness:  ID,
		Label:    email,
		Plan:     plan,
		AuthKind: "agent_capture",
	}
}

// idTokenClaims is the subset of the ChatGPT id_token payload this harness
// uses. The signature is deliberately not checked and no network call is made.
type idTokenClaims struct {
	Email      string `json:"email"`
	OpenAIAuth struct {
		ChatGPTAccountID string `json:"chatgpt_account_id"`
		ChatGPTPlanType  string `json:"chatgpt_plan_type"`
		// SubscriptionActiveStart is when the paid plan began; its day of
		// month is the billing day.
		SubscriptionActiveStart string `json:"chatgpt_subscription_active_start"`
	} `json:"https://api.openai.com/auth"`
}

func decodeIDToken(token string) (*idTokenClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return nil, errors.New("id_token is not a JWT")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		payload, err = base64.URLEncoding.DecodeString(parts[1])
		if err != nil {
			return nil, fmt.Errorf("decoding JWT payload: %w", err)
		}
	}
	var claims idTokenClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, fmt.Errorf("parsing JWT payload: %w", err)
	}
	return &claims, nil
}
