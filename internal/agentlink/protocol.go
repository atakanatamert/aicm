// SPDX-License-Identifier: Apache-2.0

// Package agentlink is the protocol between aicm-agent and the hub: one
// outbound WebSocket per machine, JSON messages in an Envelope, an Ed25519
// signed handshake and at-least-once delivery with acks. The spec is
// docs/agentlink.md; this package is its source of truth.
//
// Security rule: nothing in this protocol carries a subscription OAuth token.
// The only credential that ever crosses it is a plain provider API key, and
// only in SecretReply after the user confirmed the import in the hub.
package agentlink

import (
	"time"

	"github.com/atakanatamert/aicm/internal/meter"
)

// Protocol constants.
const (
	// Path is the hub endpoint agents dial.
	Path = "/api/agent/connect"
	// Subprotocol is negotiated in the WebSocket upgrade.
	Subprotocol = "aicm.agentlink.v1"
	// Version is sent in Hello. The hub rejects versions it doesn't speak.
	Version = 1
	// MaxMessageBytes is the read limit on both sides.
	MaxMessageBytes = 1 << 20
	// MaxBatchItems caps usage events or meter reports in one message.
	MaxBatchItems = 1000
)

// Type names a message.
type Type string

// Handshake messages, in order.
const (
	TypeHello     Type = "hello"     // agent → hub
	TypeChallenge Type = "challenge" // hub → agent
	TypeAuth      Type = "auth"      // agent → hub
	TypeWelcome   Type = "welcome"   // hub → agent
)

// Messages after the handshake.
const (
	TypeAccounts      Type = "accounts"       // agent → hub, acked
	TypeUsage         Type = "usage"          // agent → hub, acked
	TypeMeters        Type = "meters"         // agent → hub, acked
	TypeStatus        Type = "status"         // agent → hub, acked
	TypeSecretReply   Type = "secret_reply"   // agent → hub, acked
	TypeAck           Type = "ack"            // hub → agent
	TypeConfig        Type = "config"         // hub → agent
	TypeSecretRequest Type = "secret_request" // hub → agent
	TypeError         Type = "error"          // either side, then close
)

// Hello opens the handshake.
type Hello struct {
	Version      int    `json:"version"`
	OrgID        string `json:"org_id"`
	Fingerprint  string `json:"fingerprint"`
	Hostname     string `json:"hostname"`
	OS           string `json:"os"`
	Arch         string `json:"arch"`
	AgentVersion string `json:"agent_version"`
	// Nonce is 32 random bytes, base64. The hub signs it.
	Nonce string `json:"nonce"`
}

// Challenge proves the hub holds the private key the agent pinned.
type Challenge struct {
	// HubNonce is 32 random bytes, base64.
	HubNonce string `json:"hub_nonce"`
	// Signature is base64 Ed25519 over Transcript(hello, hub_nonce).
	Signature string `json:"signature"`
}

// Auth carries exactly one credential.
type Auth struct {
	// EnrollToken is the universal token from the install command, used once
	// per machine to enroll.
	EnrollToken string `json:"enroll_token,omitempty"`
	// MachineToken is the per-machine credential from Welcome, used on every
	// later connection.
	MachineToken string `json:"machine_token,omitempty"`
}

// Welcome completes the handshake.
type Welcome struct {
	MachineID string `json:"machine_id"`
	// MachineToken is set only when the agent authenticated with an enroll
	// token. The agent stores it (file mode 0600) and uses it from then on.
	MachineToken string      `json:"machine_token,omitempty"`
	Config       AgentConfig `json:"config"`
	ServerTime   time.Time   `json:"server_time"`
}

// AgentConfig is what the hub tells the agent to do. Sent in Welcome and
// again as TypeConfig whenever it changes.
type AgentConfig struct {
	// Accounts the hub knows for this org, keyed by AccountRef. The agent
	// drops data for dismissed accounts and sends pending ones as usual.
	Accounts []AccountConfig `json:"accounts"`
	// ScanInterval is how often harnesses are scanned (seconds).
	ScanIntervalSeconds int `json:"scan_interval_seconds"`
}

// AccountConfig is the hub's view of one account the agent reports.
type AccountConfig struct {
	Ref    AccountRef `json:"ref"`
	Status string     `json:"status"` // pending, active, dismissed
	// AllowUnofficial enables opt-in polls such as the Claude usage endpoint
	// (at most every 15 minutes, honouring Retry-After).
	AllowUnofficial bool `json:"allow_unofficial"`
}

// AccountRef identifies an account by the provider's own id, so the same
// account seen on several machines merges into one.
type AccountRef struct {
	// ProviderID is a catalog id: "claude" (Claude.ai plans), "chatgpt"
	// (ChatGPT plans via Codex), or an API provider such as "deepseek".
	ProviderID string `json:"provider_id"`
	// ExternalID is the provider's account id (Claude accountUuid, ChatGPT
	// account id). For a plain API key it is "key:" + the first 16 hex
	// characters of the key's SHA-256, never the key itself.
	ExternalID string `json:"external_id"`
}

// Accounts is a full snapshot of what the agent sees on this machine.
// Accounts missing from a later snapshot are not deleted on the hub.
type Accounts struct {
	Accounts []DiscoveredAccount `json:"accounts"`
}

// DiscoveredAccount is an account found in a harness's local files.
type DiscoveredAccount struct {
	Ref     AccountRef `json:"ref"`
	Harness string     `json:"harness"` // claudecode, codex, omp
	// Label is a human name, usually the account email.
	Label string `json:"label"`
	// Plan is the provider's plan name as stored locally ("max", "pro").
	Plan string `json:"plan,omitempty"`
	// AuthKind is "agent_capture" for subscriptions or "api_key" when the
	// harness holds a plain API key.
	AuthKind string `json:"auth_kind"`
	// HasAPIKey means the agent can answer a SecretRequest for this account.
	HasAPIKey bool `json:"has_api_key"`
	// Details holds non-secret extras (rate limit tier, org name); the
	// Detail* keys below have fixed meanings.
	Details map[string]string `json:"details,omitempty"`
}

// Usage is a batch of per-request usage.
type Usage struct {
	Events []UsageEvent `json:"events"`
}

// UsageEvent is one request's token usage from a harness log. The hub
// dedupes on (machine, Source, MessageID).
type UsageEvent struct {
	Account AccountRef `json:"account"`
	// Source is the harness id.
	Source string `json:"source"`
	// MessageID is unique per request within the harness, for example
	// Claude Code's message.id + ":" + requestId.
	MessageID        string    `json:"message_id"`
	OccurredAt       time.Time `json:"occurred_at"`
	Model            string    `json:"model"`
	InputTokens      int64     `json:"input_tokens"`
	OutputTokens     int64     `json:"output_tokens"`
	CacheReadTokens  int64     `json:"cache_read_tokens"`
	CacheWriteTokens int64     `json:"cache_write_tokens"`
	ReasoningTokens  int64     `json:"reasoning_tokens"`
	// CostMicro is the harness-reported cost in micro units, if any.
	CostMicro    *int64 `json:"cost_micro,omitempty"`
	CostCurrency string `json:"cost_currency,omitempty"`
	// Project is the base name of the git repository root containing the
	// session's working directory, or of the working directory itself when
	// it is not in a repository. Never a full path. Empty when unknown.
	Project string `json:"project,omitempty"`
}

// Meters is a batch of meter readings.
type Meters struct {
	Reports []MeterReport `json:"reports"`
}

// MeterReport is one reading with its meter definition. Agent-captured
// meters are defined by what the harness reports (Codex window lengths can
// change), so the definition travels with every reading.
type MeterReport struct {
	Account AccountRef    `json:"account"`
	Def     meter.Def     `json:"def"`
	Reading meter.Reading `json:"reading"`
}

// Status reports harness health, shown on the Machines page.
type Status struct {
	Harnesses []HarnessStatus `json:"harnesses"`
}

// HarnessStatus is one harness's last scan.
type HarnessStatus struct {
	ID         string    `json:"id"`
	Found      bool      `json:"found"`
	LastScanAt time.Time `json:"last_scan_at"`
	Error      string    `json:"error,omitempty"`
}

// SecretRequest asks for a plain API key the agent found, after the user
// chose to import it. The agent refuses for anything but AuthKind api_key.
type SecretRequest struct {
	RequestID string     `json:"request_id"`
	Account   AccountRef `json:"account"`
}

// SecretReply answers a SecretRequest. Exactly one of Value or Error is set.
type SecretReply struct {
	RequestID string `json:"request_id"`
	Value     string `json:"value,omitempty"`
	Error     string `json:"error,omitempty"`
}

// Ack confirms a message by its envelope id after the hub committed it.
// Error is set when the hub rejected the message for good; the agent drops
// it from the spool and logs the error instead of retrying.
type Ack struct {
	ID    string `json:"id"`
	Error string `json:"error,omitempty"`
}

// Error ends the connection. Code is stable snake_case.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Error codes.
const (
	CodeBadMessage       = "bad_message"
	CodeUnsupported      = "unsupported_version"
	CodeUnauthorized     = "unauthorized"
	CodeFingerprint      = "fingerprint_mismatch"
	CodeRevoked          = "machine_revoked"
	CodeBadHubSignature  = "bad_hub_signature"
	CodeInternal         = "internal"
	CodeTooLarge         = "too_large"
	CodeHandshakeTimeout = "handshake_timeout"
)

// Keys in DiscoveredAccount.Details the hub uses to fill in a subscription
// plan the user has not entered.
const (
	// DetailPlanTier is the provider catalog's tier id, for example "max_5x".
	DetailPlanTier = "plan_tier"
	// DetailSubscriptionStarted is when the paid subscription began (RFC 3339);
	// its day of month is the billing day.
	DetailSubscriptionStarted = "subscription_started"
	// DetailBillingMode is "api_key" when the harness on this machine is set
	// up to bill an API key instead of the subscription (for example an
	// ANTHROPIC_API_KEY in Claude Code's settings), otherwise "subscription".
	DetailBillingMode = "billing_mode"
	// DetailExtraUsage is "on" when the subscription bills paid usage past
	// its limits (Claude extra usage, Codex credits auto-reload), "off"
	// when it does not; absent when unknown.
	DetailExtraUsage = "extra_usage"
)
