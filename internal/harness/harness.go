// SPDX-License-Identifier: SUL-1.0

// Package harness defines local harness importers used by the agent. Each
// harness (Claude Code, Codex, omp) is one package under internal/harness/<id>/.
//
// Rules every harness follows:
//   - Read-only on every harness file. Never write, lock or rename them.
//   - Never refresh an OAuth token and never return one. Subscription tokens
//     stay on the machine; the agent may only read a token to identify the plan.
//   - Respect path overrides (CLAUDE_CONFIG_DIR, CODEX_HOME, PI_CODING_AGENT_DIR).
//   - Return plain API keys only through KeyReader, only for accounts
//     reported with AuthKind "api_key", and only when the hub asks.
package harness

import (
	"context"
	"time"

	"github.com/atakanatamert/aicm/internal/agentlink"
)

// Harness discovers accounts and reads new usage and meter data.
type Harness interface {
	// ID is the harness id used as UsageEvent.Source: "claudecode", "codex", "omp".
	ID() string
	// Discover lists accounts configured on this machine. A harness that is
	// not installed returns (nil, nil).
	Discover(ctx context.Context) ([]agentlink.DiscoveredAccount, error)
	// Scan reads everything appended since cursors and returns it with the
	// advanced cursors. It must be resumable: scanning again with the
	// returned cursors yields only newer data. The agent spools the batch
	// before saving the cursors, so a crash can repeat data (the hub
	// dedupes) but never loses it.
	Scan(ctx context.Context, cursors Cursors) (Batch, error)
}

// Poller is implemented by harnesses that actively fetch meters, such as
// Codex's app-server read or the opt-in Claude usage poll.
type Poller interface {
	// PollInterval is the minimum gap between polls. The agent never calls
	// Poll more often, and backs off further after errors.
	PollInterval() time.Duration
	// Poll fetches meters for the accounts in cfg it is allowed to poll
	// (opt-in polls check AccountConfig.AllowUnofficial).
	Poll(ctx context.Context, cfg agentlink.AgentConfig) ([]agentlink.MeterReport, error)
}

// KeyReader is implemented by harnesses that hold plain provider API keys.
type KeyReader interface {
	// ReadAPIKey returns the plain API key for an account this harness
	// reported with AuthKind "api_key". It refuses anything else.
	ReadAPIKey(ctx context.Context, ref agentlink.AccountRef) (string, error)
}

// Batch is the result of one Scan.
type Batch struct {
	Usage   []agentlink.UsageEvent
	Meters  []agentlink.MeterReport
	Cursors Cursors
}

// Cursors maps a file path to how far it has been read.
type Cursors map[string]Cursor

// Cursor is the read position in one file. When the file's inode changes or
// its size drops below Offset, the file was replaced or truncated and is
// read again from the start.
type Cursor struct {
	Inode  uint64 `json:"inode"`
	Offset int64  `json:"offset"`
	// Size and ModTime let Scan skip unchanged files without opening them.
	Size    int64     `json:"size"`
	ModTime time.Time `json:"mod_time"`
}
