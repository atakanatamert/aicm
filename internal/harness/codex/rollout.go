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
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/atakanatamert/aicm/internal/agentlink"
	"github.com/atakanatamert/aicm/internal/harness"
	"github.com/atakanatamert/aicm/internal/meter"
)

const compressedRolloutsWarning = "compressed rollouts not supported yet"

// recentReadingWindow is how old the newest rollout reading may be before
// Poll asks the app-server for fresh windows.
const recentReadingWindow = 30 * time.Minute

// tailReadBytes bounds how much of a recently modified rollout Poll inspects.
const tailReadBytes = 1 << 20

func (h *Harness) scanRollouts(ctx context.Context, ref agentlink.AccountRef, cursors harness.Cursors) (harness.Batch, error) {
	out := harness.Batch{Cursors: copyCursors(cursors)}

	files, compressed, err := rolloutFiles(h.dir)
	if err != nil {
		return out, err
	}

	type pick struct {
		report agentlink.MeterReport
		at     time.Time
	}
	newest := make(map[string]pick)

	for _, path := range files {
		if err := ctx.Err(); err != nil {
			return out, fmt.Errorf("scanning codex rollouts: %w", err)
		}
		next, usage, reports, err := readRollout(ctx, path, out.Cursors[path], ref)
		if err != nil {
			return out, fmt.Errorf("reading rollout %s: %w", path, err)
		}
		out.Cursors[path] = next
		out.Usage = append(out.Usage, usage...)

		for _, report := range reports {
			prev, ok := newest[report.Def.Key]
			if !ok || report.Reading.CapturedAt.After(prev.at) {
				newest[report.Def.Key] = pick{report: report, at: report.Reading.CapturedAt}
			}
		}
	}

	// Stable order: the two windows first, then credits.
	for _, key := range []string{"primary", "secondary", "credits"} {
		if p, ok := newest[key]; ok {
			out.Meters = append(out.Meters, p.report)
		}
	}

	if compressed > 0 {
		return out, fmt.Errorf("%d %s", compressed, compressedRolloutsWarning)
	}
	return out, nil
}

// readRollout reads complete lines appended since cur. A trailing partial line
// stays unread until a later scan completes it.
func readRollout(ctx context.Context, path string, cur harness.Cursor, ref agentlink.AccountRef) (harness.Cursor, []agentlink.UsageEvent, []agentlink.MeterReport, error) {
	// Skip files that have not changed without opening them.
	info, err := os.Stat(path)
	if err != nil {
		return cur, nil, nil, fmt.Errorf("statting: %w", err)
	}
	if cur.Inode == harness.Inode(info) && cur.Size == info.Size() && cur.ModTime.Equal(info.ModTime()) {
		return cur, nil, nil, nil
	}

	// #nosec G304 -- path comes from the Codex sessions directory.
	f, err := os.Open(path)
	if err != nil {
		return cur, nil, nil, fmt.Errorf("opening: %w", err)
	}
	defer f.Close()

	info, err = f.Stat()
	if err != nil {
		return cur, nil, nil, fmt.Errorf("statting: %w", err)
	}
	inode := harness.Inode(info)
	offset := cur.Offset
	if cur.Inode != inode || info.Size() < offset {
		offset = 0
	}

	next := harness.Cursor{
		Inode:   inode,
		Offset:  offset,
		Size:    info.Size(),
		ModTime: info.ModTime(),
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return next, nil, nil, fmt.Errorf("seeking to %d: %w", offset, err)
	}

	st := rolloutState{sessionID: sessionIDFromPath(path), cwd: sessionCwd(path)}
	reader := bufio.NewReaderSize(f, 64*1024)
	var usage []agentlink.UsageEvent
	var reports []agentlink.MeterReport
	for {
		line, readErr := reader.ReadString('\n')
		if strings.HasSuffix(line, "\n") {
			if err := ctx.Err(); err != nil {
				return next, usage, reports, fmt.Errorf("scanning: %w", err)
			}
			next.Offset += int64(len(line))
			events, meters := parseRolloutLine([]byte(strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")), &st, ref)
			usage = append(usage, events...)
			reports = append(reports, meters...)
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			return next, usage, reports, fmt.Errorf("reading: %w", readErr)
		}
	}

	// The file may have grown while it was read. Keep the cursor consistent
	// with the bytes actually consumed so the next scan does not restart.
	if end, err := f.Stat(); err == nil {
		next.Size = end.Size()
		next.ModTime = end.ModTime()
		if next.Size < next.Offset {
			next.Size = next.Offset
		}
	}
	return next, usage, reports, nil
}

type rolloutState struct {
	sessionID string
	model     string
	cwd       string
}

type rolloutLine struct {
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

type rolloutTokenInfo struct {
	LastTokenUsage *tokenUsage `json:"last_token_usage"`
}

type tokenUsage struct {
	InputTokens        int64 `json:"input_tokens"`
	CachedInputTokens  int64 `json:"cached_input_tokens"`
	OutputTokens       int64 `json:"output_tokens"`
	ReasoningOutputTok int64 `json:"reasoning_output_tokens"`
}

type rolloutTokenCount struct {
	Info       *rolloutTokenInfo  `json:"info"`
	RateLimits *rolloutRateLimits `json:"rate_limits"`
}

type rolloutRateLimits struct {
	Primary   *rolloutRateLimitWindow `json:"primary"`
	Secondary *rolloutRateLimitWindow `json:"secondary"`
	Credits   *rolloutCredits         `json:"credits"`
}

type rolloutRateLimitWindow struct {
	UsedPercent     float64 `json:"used_percent"`
	WindowMinutes   *int    `json:"window_minutes"`
	ResetsAt        *int64  `json:"resets_at"`
	ResetsInSeconds *int64  `json:"resets_in_seconds"`
}

// rolloutCredits is Codex's add-on credit balance. Accounts without
// purchased credits report has_credits false and balance "0"; that is not an
// exhausted meter, so only accounts with credits get a reading.
type rolloutCredits struct {
	HasCredits bool    `json:"has_credits"`
	Unlimited  bool    `json:"unlimited"`
	Balance    *string `json:"balance"`
}

func parseRolloutLine(raw []byte, st *rolloutState, ref agentlink.AccountRef) ([]agentlink.UsageEvent, []agentlink.MeterReport) {
	var line rolloutLine
	if err := json.Unmarshal(raw, &line); err != nil {
		return nil, nil
	}

	switch line.Type {
	case "session_meta":
		var meta struct {
			SessionID string `json:"session_id"`
			ID        string `json:"id"`
			Cwd       string `json:"cwd"`
		}
		if err := json.Unmarshal(line.Payload, &meta); err != nil {
			return nil, nil
		}
		if meta.SessionID != "" {
			st.sessionID = meta.SessionID
		} else if meta.ID != "" {
			st.sessionID = meta.ID
		}
		if meta.Cwd != "" {
			st.cwd = meta.Cwd
		}
		return nil, nil
	case "turn_context":
		var ctx struct {
			Model string `json:"model"`
		}
		if err := json.Unmarshal(line.Payload, &ctx); err != nil {
			return nil, nil
		}
		if ctx.Model != "" {
			st.model = ctx.Model
		}
		return nil, nil
	case "event_msg":
	default:
		return nil, nil
	}

	var payload struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(line.Payload, &payload); err != nil || payload.Type != "token_count" {
		return nil, nil
	}
	var event rolloutTokenCount
	if err := json.Unmarshal(line.Payload, &event); err != nil {
		return nil, nil
	}

	occurredAt, err := time.Parse(time.RFC3339Nano, line.Timestamp)
	if err != nil {
		return nil, nil
	}
	occurredAt = occurredAt.UTC()

	var usage []agentlink.UsageEvent
	if event.Info != nil && event.Info.LastTokenUsage != nil {
		t := event.Info.LastTokenUsage
		input := t.InputTokens - t.CachedInputTokens
		if input < 0 {
			input = 0
		}
		usage = append(usage, agentlink.UsageEvent{
			Account:         ref,
			Source:          ID,
			MessageID:       st.sessionID + ":" + line.Timestamp,
			OccurredAt:      occurredAt,
			Model:           st.model,
			InputTokens:     input,
			OutputTokens:    t.OutputTokens,
			CacheReadTokens: t.CachedInputTokens,
			ReasoningTokens: t.ReasoningOutputTok,
			Project:         harness.Project(st.cwd),
		})
	}

	return usage, rolloutMeterReports(ref, event.RateLimits, occurredAt)
}

func rolloutMeterReports(ref agentlink.AccountRef, limits *rolloutRateLimits, capturedAt time.Time) []agentlink.MeterReport {
	if limits == nil {
		return nil
	}
	var reports []agentlink.MeterReport
	appendWindow := func(key string, window *rolloutRateLimitWindow) {
		if window == nil {
			return
		}
		used := window.UsedPercent
		limit := 100.0
		reports = append(reports, agentlink.MeterReport{
			Account: ref,
			Def:     windowMeterDef(key, window.WindowMinutes),
			Reading: meter.Reading{
				Key:        key,
				Used:       &used,
				Limit:      &limit,
				ResetsAt:   windowReset(window, capturedAt),
				CapturedAt: capturedAt,
				Source:     meter.SourceRollout,
			},
		})
	}
	appendWindow("primary", limits.Primary)
	appendWindow("secondary", limits.Secondary)

	if c := limits.Credits; c != nil && c.HasCredits && !c.Unlimited && c.Balance != nil {
		if balance, err := strconv.ParseFloat(*c.Balance, 64); err == nil {
			reports = append(reports, agentlink.MeterReport{
				Account: ref,
				Def:     creditsMeterDef(),
				Reading: meter.Reading{
					Key:        "credits",
					Remaining:  &balance,
					CapturedAt: capturedAt,
					Source:     meter.SourceRollout,
				},
			})
		}
	}
	return reports
}

func windowReset(window *rolloutRateLimitWindow, capturedAt time.Time) *time.Time {
	if window.ResetsAt != nil && *window.ResetsAt > 0 {
		t := time.Unix(*window.ResetsAt, 0).UTC()
		return &t
	}
	if window.ResetsInSeconds != nil {
		t := capturedAt.Add(time.Duration(*window.ResetsInSeconds) * time.Second).UTC()
		return &t
	}
	return nil
}

// windowMeterDef copies the cataloging definition for a Codex window and
// takes the window length and label from the provider's own reading.
func windowMeterDef(key string, minutes *int) meter.Def {
	def := meter.Def{Key: key, Unit: meter.UnitPercent, ResetKind: meter.ResetFixed}
	switch key {
	case "primary":
		def.Label = "5-hour limit"
		def.WindowMinutes = 300
	case "secondary":
		def.Label = "Weekly limit"
		def.WindowMinutes = 10080
	}
	if minutes == nil {
		return def
	}
	def.WindowMinutes = *minutes
	switch *minutes {
	case 300:
		def.Label = "5-hour limit"
	case 10080:
		def.Label = "Weekly limit"
	default:
		def.Label = fmt.Sprintf("%d-minute window", *minutes)
	}
	return def
}

func creditsMeterDef() meter.Def {
	return meter.Def{
		Key:          "credits",
		Label:        "Codex credits",
		Unit:         meter.UnitCredits,
		ResetKind:    meter.ResetNever,
		Supplemental: true,
	}
}

func rolloutFiles(codexHome string) ([]string, int, error) {
	root := filepath.Join(codexHome, "sessions")
	if _, err := os.Stat(root); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, 0, nil
		}
		return nil, 0, fmt.Errorf("statting codex sessions: %w", err)
	}

	var files []string
	compressed := 0
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		name := entry.Name()
		if !strings.HasPrefix(name, "rollout-") {
			return nil
		}
		switch {
		case strings.HasSuffix(name, ".jsonl.zst"):
			compressed++
		case strings.HasSuffix(name, ".jsonl"):
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, 0, fmt.Errorf("walking codex sessions: %w", err)
	}
	sort.Strings(files)
	return files, compressed, nil
}

func copyCursors(in harness.Cursors) harness.Cursors {
	out := make(harness.Cursors, len(in))
	for path, cursor := range in {
		out[path] = cursor
	}
	return out
}

// hasRecentRolloutReading reports whether the newest meter reading in the
// rollouts is younger than recentReadingWindow. Only recently modified files
// can contain such a reading, so untouched sessions are not read.
func (h *Harness) hasRecentRolloutReading(now time.Time) (bool, error) {
	files, _, err := rolloutFiles(h.dir)
	if err != nil {
		return false, err
	}
	cutoff := now.Add(-recentReadingWindow)
	for _, path := range files {
		info, err := os.Stat(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return false, fmt.Errorf("statting rollout %s: %w", path, err)
		}
		if info.ModTime().Before(cutoff) {
			continue
		}
		if at, ok := lastRateLimitReading(path, info.Size()); ok && at.After(cutoff) {
			return true, nil
		}
	}
	return false, nil
}

func lastRateLimitReading(path string, size int64) (time.Time, bool) {
	// #nosec G304 -- path comes from the Codex sessions directory.
	f, err := os.Open(path)
	if err != nil {
		return time.Time{}, false
	}
	defer f.Close()

	start := size - tailReadBytes
	if start < 0 {
		start = 0
	}
	buf := make([]byte, size-start)
	if _, err := f.ReadAt(buf, start); err != nil && !errors.Is(err, io.EOF) {
		return time.Time{}, false
	}
	lines := bytes.Split(buf, []byte("\n"))
	if start > 0 && len(lines) > 0 {
		lines = lines[1:] // the first chunk may start mid-line
	}
	for i := len(lines) - 1; i >= 0; i-- {
		line := bytes.TrimSpace(lines[i])
		if len(line) == 0 {
			continue
		}
		var entry rolloutLine
		if err := json.Unmarshal(line, &entry); err != nil || entry.Type != "event_msg" {
			continue
		}
		var payload struct {
			Type       string             `json:"type"`
			RateLimits *rolloutRateLimits `json:"rate_limits"`
		}
		if err := json.Unmarshal(entry.Payload, &payload); err != nil || payload.Type != "token_count" {
			continue
		}
		if !hasReadings(payload.RateLimits) {
			continue
		}
		at, err := time.Parse(time.RFC3339Nano, entry.Timestamp)
		if err != nil {
			continue
		}
		return at.UTC(), true
	}
	return time.Time{}, false
}

func hasReadings(limits *rolloutRateLimits) bool {
	if limits == nil {
		return false
	}
	if limits.Primary != nil || limits.Secondary != nil {
		return true
	}
	return limits.Credits != nil && limits.Credits.HasCredits && !limits.Credits.Unlimited && limits.Credits.Balance != nil
}

// sessionCwd returns the session's working directory from the session_meta
// line at the top of a rollout. A resumed scan starts mid-file, so the cwd
// cannot be taken from the lines being read.
func sessionCwd(path string) string {
	f, err := os.Open(path) //nolint:gosec // path comes from the Codex sessions directory
	if err != nil {
		return ""
	}
	defer f.Close() //nolint:errcheck // read-only file
	reader := bufio.NewReaderSize(f, 64*1024)
	for range 10 {
		line, err := reader.ReadBytes('\n')
		if trimmed := bytes.TrimSpace(line); len(trimmed) > 0 {
			var entry rolloutLine
			if json.Unmarshal(trimmed, &entry) == nil && entry.Type == "session_meta" {
				var meta struct {
					Cwd string `json:"cwd"`
				}
				if json.Unmarshal(entry.Payload, &meta) == nil {
					return meta.Cwd
				}
				return ""
			}
		}
		if err != nil {
			break
		}
	}
	return ""
}

// sessionIDFromPath extracts the thread id from
// rollout-<timestamp>-<thread id>[_<rollout id>].jsonl.
func sessionIDFromPath(path string) string {
	base := strings.TrimSuffix(filepath.Base(path), ".jsonl")
	base = strings.TrimPrefix(base, "rollout-")
	if len(base) < 21 || base[19] != '-' {
		return ""
	}
	rest := base[20:]
	if i := strings.IndexByte(rest, '_'); i >= 0 {
		rest = rest[:i]
	}
	return rest
}
