// SPDX-License-Identifier: Apache-2.0

package codex

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"time"

	"github.com/atakanatamert/aicm/internal/agentlink"
	"github.com/atakanatamert/aicm/internal/meter"
	"github.com/atakanatamert/aicm/internal/version"
)

const (
	pollInterval   = 15 * time.Minute
	pollTimeout    = 20 * time.Second
	appServerBin   = "codex"
	appServerBinID = "app-server"
)

var errCodexNotFound = errors.New("codex not found")

// PollInterval is the minimum gap between app-server reads.
func (h *Harness) PollInterval() time.Duration { return pollInterval }

// Poll reads fresh windows from codex app-server when the newest rollout
// reading is older than 30 minutes. It never handles credentials itself: the
// official binary uses its own auth.
func (h *Harness) Poll(ctx context.Context, _ agentlink.AgentConfig) ([]agentlink.MeterReport, error) {
	if _, err := exec.LookPath(appServerBin); err != nil {
		return nil, nil
	}

	recent, err := h.hasRecentRolloutReading(h.nowTime())
	if err != nil {
		return nil, err
	}
	if recent {
		return nil, nil
	}

	acct, err := h.accountForScan()
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, pollTimeout)
	defer cancel()

	reports, used, err := h.pollAppServer(ctx, acct)
	if err != nil {
		return nil, err
	}
	if used != nil {
		h.setAccount(used)
	}
	return reports, nil
}

func (h *Harness) pollAppServer(ctx context.Context, acct *agentlink.DiscoveredAccount) ([]agentlink.MeterReport, *agentlink.DiscoveredAccount, error) {
	client, err := startAppServer(ctx)
	if err != nil {
		if errors.Is(err, errCodexNotFound) {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	defer client.close()

	if err := client.initialize(ctx); err != nil {
		return nil, nil, err
	}

	limits, err := client.readRateLimits(ctx)
	if err != nil {
		return nil, nil, err
	}
	if acct == nil && limits.AccountID != "" {
		acct = discoveredAccount(limits.AccountID, "", "")
	}
	if acct == nil {
		return nil, nil, nil
	}
	return appServerReports(acct.Ref, limits.RateLimits, h.nowTime().UTC()), acct, nil
}

// appClient speaks newline-delimited JSON-RPC to one codex app-server process.
type appClient struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	nextID int
}

func startAppServer(ctx context.Context) (*appClient, error) {
	path, err := exec.LookPath(appServerBin)
	if err != nil {
		return nil, errCodexNotFound
	}
	// #nosec G204 -- path is the codex binary resolved from PATH.
	cmd := exec.CommandContext(ctx, path, appServerBinID)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("opening codex app-server stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("opening codex app-server stdout: %w", err)
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting codex app-server: %w", err)
	}
	return &appClient{
		cmd:    cmd,
		stdin:  stdin,
		stdout: bufio.NewReaderSize(stdout, 1<<20),
	}, nil
}

func (c *appClient) close() {
	_ = c.stdin.Close()
	if c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
	_ = c.cmd.Wait()
}

func (c *appClient) initialize(ctx context.Context) error {
	params := map[string]any{
		"clientInfo": map[string]any{
			"name":    "aicm-agent",
			"version": version.Version,
		},
		"capabilities": map[string]any{"experimentalApi": true},
	}
	if err := c.call(ctx, "initialize", params, nil); err != nil {
		return err
	}
	return c.notify(ctx, "initialized")
}

func (c *appClient) readAccount(ctx context.Context) (*agentlink.DiscoveredAccount, error) {
	var response struct {
		Account *struct {
			Type             string `json:"type"`
			Email            string `json:"email"`
			PlanType         string `json:"planType"`
			ChatGPTAccountID string `json:"chatgptAccountId"`
		} `json:"account"`
		WorkspaceRouting *struct {
			ChatGPTAccountID string `json:"chatgptAccountId"`
		} `json:"workspaceRouting"`
	}
	if err := c.call(ctx, "account/read", map[string]any{"refreshToken": false}, &response); err != nil {
		return nil, err
	}
	if response.Account == nil || response.Account.Type != "chatgpt" {
		return nil, nil
	}
	accountID := response.Account.ChatGPTAccountID
	if response.WorkspaceRouting != nil && response.WorkspaceRouting.ChatGPTAccountID != "" {
		accountID = response.WorkspaceRouting.ChatGPTAccountID
	}
	if accountID == "" {
		return nil, nil
	}
	return discoveredAccount(accountID, response.Account.Email, response.Account.PlanType), nil
}

type appRateLimitResponse struct {
	RateLimits          *appRateLimits           `json:"rateLimits"`
	RateLimitsByLimitID map[string]appRateLimits `json:"rateLimitsByLimitId"`
	AccountID           string                   `json:"accountId"`
}

type appRateLimits struct {
	Primary   *appRateLimitWindow `json:"primary"`
	Secondary *appRateLimitWindow `json:"secondary"`
	Credits   *appCredits         `json:"credits"`
}

type appRateLimitWindow struct {
	UsedPercent        float64 `json:"usedPercent"`
	WindowDurationMins *int    `json:"windowDurationMins"`
	ResetsAt           *int64  `json:"resetsAt"`
}

type appCredits struct {
	Unlimited bool    `json:"unlimited"`
	Balance   *string `json:"balance"`
}

func (c *appClient) readRateLimits(ctx context.Context) (*appRateLimitResponse, error) {
	var response appRateLimitResponse
	if err := c.call(ctx, "account/rateLimits/read", map[string]any{"excludeResetCreditDetails": true}, &response); err != nil {
		return nil, err
	}
	if response.RateLimits == nil {
		if limits, ok := response.RateLimitsByLimitID["codex"]; ok {
			response.RateLimits = &limits
		}
	}
	return &response, nil
}

func appServerReports(ref agentlink.AccountRef, limits *appRateLimits, capturedAt time.Time) []agentlink.MeterReport {
	if limits == nil {
		return nil
	}
	var reports []agentlink.MeterReport
	appendWindow := func(key string, window *appRateLimitWindow) {
		if window == nil {
			return
		}
		used := window.UsedPercent
		limit := 100.0
		var resetsAt *time.Time
		if window.ResetsAt != nil && *window.ResetsAt > 0 {
			t := time.Unix(*window.ResetsAt, 0).UTC()
			resetsAt = &t
		}
		reports = append(reports, agentlink.MeterReport{
			Account: ref,
			Def:     windowMeterDef(key, window.WindowDurationMins),
			Reading: meter.Reading{
				Key:        key,
				Used:       &used,
				Limit:      &limit,
				ResetsAt:   resetsAt,
				CapturedAt: capturedAt,
				Source:     meter.SourceAppServer,
			},
		})
	}
	appendWindow("primary", limits.Primary)
	appendWindow("secondary", limits.Secondary)

	if credits := limits.Credits; credits != nil && !credits.Unlimited && credits.Balance != nil {
		if balance, err := strconv.ParseFloat(*credits.Balance, 64); err == nil {
			reports = append(reports, agentlink.MeterReport{
				Account: ref,
				Def:     creditsMeterDef(),
				Reading: meter.Reading{
					Key:        "credits",
					Remaining:  &balance,
					CapturedAt: capturedAt,
					Source:     meter.SourceAppServer,
				},
			})
		}
	}
	return reports
}

func (c *appClient) call(ctx context.Context, method string, params any, result any) error {
	c.nextID++
	id := c.nextID
	request := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
	}
	if params != nil {
		request["params"] = params
	}
	if err := c.write(request); err != nil {
		return err
	}

	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("codex app-server %s: %w", method, err)
		}
		line, err := c.readLine()
		if err != nil {
			return fmt.Errorf("codex app-server %s: %w", method, err)
		}
		var response struct {
			ID     json.RawMessage `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(line, &response); err != nil {
			continue // notifications and non-JSON diagnostics are ignored
		}
		if !sameID(response.ID, id) {
			continue
		}
		if response.Error != nil {
			return fmt.Errorf("codex app-server %s: %s", method, response.Error.Message)
		}
		if result == nil {
			return nil
		}
		if len(response.Result) == 0 || string(response.Result) == "null" {
			return fmt.Errorf("codex app-server %s: empty result", method)
		}
		if err := json.Unmarshal(response.Result, result); err != nil {
			return fmt.Errorf("decoding codex app-server %s result: %w", method, err)
		}
		return nil
	}
}

func (c *appClient) notify(ctx context.Context, method string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("codex app-server %s: %w", method, err)
	}
	return c.write(map[string]any{"jsonrpc": "2.0", "method": method})
}

func (c *appClient) write(message any) error {
	encoded, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("encoding codex app-server request: %w", err)
	}
	encoded = append(encoded, '\n')
	if _, err := c.stdin.Write(encoded); err != nil {
		return fmt.Errorf("writing to codex app-server: %w", err)
	}
	return nil
}

func (c *appClient) readLine() ([]byte, error) {
	for {
		line, err := c.stdout.ReadBytes('\n')
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) > 0 {
			return trimmed, nil
		}
		if err != nil {
			return nil, fmt.Errorf("reading codex app-server output: %w", err)
		}
	}
}

func sameID(raw json.RawMessage, id int) bool {
	if len(raw) == 0 {
		return false
	}
	var number int
	if err := json.Unmarshal(raw, &number); err == nil {
		return number == id
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text == strconv.Itoa(id)
	}
	return false
}
