// SPDX-License-Identifier: Apache-2.0

package omp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
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
)

// Scan implements harness.Harness. It reads every session file appended since
// cursors and reports assistant messages that carry usage. A message is
// attributed only when its provider has a discovered plain API key; OAuth
// providers and providers without a hub catalog id are skipped.
func (h *Harness) Scan(ctx context.Context, cursors harness.Cursors) (harness.Batch, error) {
	if err := ctx.Err(); err != nil {
		return harness.Batch{}, fmt.Errorf("scanning omp sessions: %w", err)
	}

	batch := harness.Batch{Cursors: make(harness.Cursors, len(cursors))}
	for path, cursor := range cursors {
		batch.Cursors[path] = cursor
	}

	for _, dir := range h.dirs {
		entries, err := readAPIKeys(dir)
		if err != nil {
			return batch, fmt.Errorf("reading omp auth in %s: %w", dir, err)
		}
		byProvider := make(map[string]keyEntry, len(entries))
		for _, entry := range entries {
			if _, ok := byProvider[entry.Name]; !ok {
				byProvider[entry.Name] = entry
			}
		}

		files, err := sessionFiles(filepath.Join(dir, "sessions"))
		if err != nil {
			return batch, fmt.Errorf("listing omp sessions in %s: %w", dir, err)
		}
		for _, path := range files {
			events, cursor, err := scanSessionFile(ctx, path, cursors[path], byProvider)
			if err != nil {
				return batch, fmt.Errorf("scanning omp session %s: %w", path, err)
			}
			batch.Usage = append(batch.Usage, events...)
			batch.Cursors[path] = cursor
		}
	}
	return batch, nil
}

// sessionLine is the subset of one JSONL line this importer reads.
type sessionLine struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	Timestamp string `json:"timestamp"`
	Message   struct {
		Role      string        `json:"role"`
		Provider  string        `json:"provider"`
		Model     string        `json:"model"`
		Timestamp int64         `json:"timestamp"`
		Usage     *sessionUsage `json:"usage"`
	} `json:"message"`
}

type sessionUsage struct {
	Input      json.Number  `json:"input"`
	Output     json.Number  `json:"output"`
	CacheRead  json.Number  `json:"cacheRead"`
	CacheWrite json.Number  `json:"cacheWrite"`
	Cost       *sessionCost `json:"cost"`
}

type sessionCost struct {
	Total json.Number `json:"total"`
}

// scanSessionFile reads complete lines after prev and returns the events they
// describe plus the advanced cursor. A trailing line without a newline is left
// for the next scan. Unchanged files are skipped from the cursor metadata.
func scanSessionFile(ctx context.Context, path string, prev harness.Cursor, keys map[string]keyEntry) ([]agentlink.UsageEvent, harness.Cursor, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, prev, fmt.Errorf("stat: %w", err)
	}
	inode := harness.Inode(info)
	if prev.Inode != 0 && prev.Inode == inode && prev.Size == info.Size() &&
		prev.ModTime.Equal(info.ModTime()) {
		return nil, prev, nil
	}

	offset := prev.Offset
	if prev.Inode != inode || info.Size() < offset {
		offset = 0
	}

	f, err := os.Open(path) //nolint:gosec // session path listed from the harness directory
	if err != nil {
		return nil, prev, fmt.Errorf("open: %w", err)
	}
	defer f.Close() //nolint:errcheck // read-only file

	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, prev, fmt.Errorf("seek: %w", err)
	}

	sessionID := readSessionID(path)
	reader := bufio.NewReader(f)
	var events []agentlink.UsageEvent
	for {
		if err := ctx.Err(); err != nil {
			return nil, prev, fmt.Errorf("reading session: %w", err)
		}
		line, err := reader.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, prev, fmt.Errorf("reading line: %w", err)
		}
		offset += int64(len(line))
		if event, ok := parseSessionLine(line, sessionID, keys); ok {
			events = append(events, event)
		}
	}

	return events, harness.Cursor{
		Inode:   inode,
		Offset:  offset,
		Size:    info.Size(),
		ModTime: info.ModTime(),
	}, nil
}

// parseSessionLine converts one complete line to a usage event.
func parseSessionLine(line []byte, sessionID string, keys map[string]keyEntry) (agentlink.UsageEvent, bool) {
	var parsed sessionLine
	if err := json.Unmarshal(line, &parsed); err != nil {
		return agentlink.UsageEvent{}, false
	}
	if parsed.Type != "message" {
		return agentlink.UsageEvent{}, false
	}
	message := parsed.Message
	if message.Role != "assistant" || message.Usage == nil {
		return agentlink.UsageEvent{}, false
	}
	key, ok := keys[strings.ToLower(message.Provider)]
	if !ok {
		return agentlink.UsageEvent{}, false
	}

	messageID := parsed.ID
	if messageID == "" {
		messageID = parsed.Timestamp
	}
	if messageID == "" && message.Timestamp != 0 {
		messageID = strconv.FormatInt(message.Timestamp, 10)
	}
	if messageID == "" {
		return agentlink.UsageEvent{}, false
	}

	occurredAt := parseTime(parsed.Timestamp)
	if occurredAt.IsZero() && message.Timestamp != 0 {
		occurredAt = time.UnixMilli(message.Timestamp).UTC()
	}

	event := agentlink.UsageEvent{
		Account:          key.Ref,
		Source:           ID,
		MessageID:        sessionID + ":" + messageID,
		OccurredAt:       occurredAt,
		Model:            message.Model,
		InputTokens:      tokenCount(message.Usage.Input),
		OutputTokens:     tokenCount(message.Usage.Output),
		CacheReadTokens:  tokenCount(message.Usage.CacheRead),
		CacheWriteTokens: tokenCount(message.Usage.CacheWrite),
	}
	if cost, ok := costMicro(message.Usage.Cost); ok {
		event.CostMicro = &cost
		event.CostCurrency = "USD"
	}
	return event, true
}

func parseTime(value string) time.Time {
	if value == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}
	}
	return parsed.UTC()
}

func tokenCount(number json.Number) int64 {
	value, err := strconv.ParseInt(number.String(), 10, 64)
	if err != nil {
		return 0
	}
	return value
}

// costMicro converts omp's USD decimal to integer micro USD. The JSON number's
// text is parsed with strconv and scaled by 10^6 with integer arithmetic (no
// float multiplication), rounding halves away from zero.
func costMicro(cost *sessionCost) (int64, bool) {
	if cost == nil {
		return 0, false
	}
	text := strings.TrimSpace(cost.Total.String())
	if text == "" {
		return 0, false
	}

	negative := false
	switch text[0] {
	case '-':
		negative = true
		text = text[1:]
	case '+':
		text = text[1:]
	}
	if text == "" {
		return 0, false
	}

	exponent := 0
	if index := strings.IndexAny(text, "eE"); index >= 0 {
		parsed, err := strconv.Atoi(text[index+1:])
		if err != nil {
			return 0, false
		}
		exponent = parsed
		text = text[:index]
	}

	integerPart, fractionPart := text, ""
	if index := strings.IndexByte(text, '.'); index >= 0 {
		integerPart, fractionPart = text[:index], text[index+1:]
	}
	digits := integerPart + fractionPart
	if digits == "" {
		return 0, false
	}
	magnitude, err := strconv.ParseUint(digits, 10, 64)
	if err != nil {
		return 0, false
	}

	// value = magnitude × 10^(exponent − len(fractionPart)) USD, so micro USD
	// scales the exponent by six more decimal places.
	shift := exponent - len(fractionPart) + 6
	var micro uint64
	if shift >= 0 {
		if shift > 19 {
			return 0, false
		}
		factor := uint64(1)
		for range shift {
			if factor > ^uint64(0)/10 {
				return 0, false
			}
			factor *= 10
		}
		if magnitude > ^uint64(0)/factor {
			return 0, false
		}
		micro = magnitude * factor
	} else {
		divisorShift := -shift
		var divisor uint64
		if divisorShift > 19 {
			// Anything representable in uint64 is less than half of 10^20.
			divisor = 0
		} else {
			divisor = 1
			for range divisorShift {
				divisor *= 10
			}
		}
		if divisor == 0 {
			micro = 0
		} else {
			micro = magnitude / divisor
			remainder := magnitude % divisor
			if remainder >= divisor-remainder {
				micro++
			}
		}
	}

	if negative {
		if micro > 1<<63 {
			return 0, false
		}
		return -int64(micro), true
	}
	if micro > 1<<63-1 {
		return 0, false
	}
	return int64(micro), true
}

// readSessionID reads the session header so resumed and subagent files use the
// same id on every scan. Files without a header fall back to their file name.
func readSessionID(path string) string {
	if f, err := os.Open(path); err == nil { //nolint:gosec // session path from the harness directory
		reader := bufio.NewReader(f)
		for range 10 {
			line, err := reader.ReadBytes('\n')
			if len(line) > 0 {
				var header struct {
					Type string `json:"type"`
					ID   string `json:"id"`
				}
				if json.Unmarshal(bytes.TrimSpace(line), &header) == nil && header.Type == "session" && header.ID != "" {
					_ = f.Close()
					return header.ID
				}
			}
			if err != nil {
				break
			}
		}
		_ = f.Close()
	}
	return sessionIDFromPath(path)
}

func sessionIDFromPath(path string) string {
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	if index := strings.LastIndex(base, "_"); index >= 0 && index+1 < len(base) {
		return base[index+1:]
	}
	return base
}

// sessionFiles lists sessions/**/*.jsonl under root. A missing directory is
// an installation without omp sessions, not an error.
func sessionFiles(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			if path == root && os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".jsonl") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking sessions: %w", err)
	}
	sort.Strings(files)
	return files, nil
}
