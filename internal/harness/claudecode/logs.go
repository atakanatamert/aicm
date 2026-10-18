// SPDX-License-Identifier: Apache-2.0

package claudecode

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"time"

	"github.com/atakanatamert/aicm/internal/agentlink"
	"github.com/atakanatamert/aicm/internal/harness"
)

// Scan implements harness.Harness. It reads appended lines from every
// project log and from the statusline spool.
func (h *Harness) Scan(ctx context.Context, cursors harness.Cursors) (harness.Batch, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	batch := harness.Batch{Cursors: harness.Cursors{}}
	accounts := h.accounts(ctx)
	seen := map[string]int{}
	for _, acct := range accounts {
		if err := ctx.Err(); err != nil {
			return batch, fmt.Errorf("scanning Claude Code logs: %w", err)
		}
		pattern := filepath.Join(acct.dir.path, "projects", "*", "*.jsonl")
		files, err := filepath.Glob(pattern)
		if err != nil {
			return batch, fmt.Errorf("listing usage logs in %s: %w", acct.dir.path, err)
		}
		for _, path := range files {
			prev, ok := cursors[path]
			cursor, found, err := scanFile(path, cursorOrNil(prev, ok), func(line []byte) {
				event, valid := parseUsageLine(line, acct.ref)
				if !valid {
					return
				}
				if i, dup := seen[event.MessageID]; dup {
					batch.Usage[i] = event
					return
				}
				seen[event.MessageID] = len(batch.Usage)
				batch.Usage = append(batch.Usage, event)
			})
			if err != nil {
				return batch, fmt.Errorf("reading usage log %s: %w", path, err)
			}
			if found {
				batch.Cursors[path] = cursor
			}
		}
	}
	if err := h.scanStatusline(accounts, cursors, &batch); err != nil {
		return batch, err
	}
	return batch, nil
}

func cursorOrNil(c harness.Cursor, ok bool) *harness.Cursor {
	if !ok {
		return nil
	}
	return &c
}

type usageLine struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
	RequestID string `json:"requestId"`
	Message   *struct {
		ID    string `json:"id"`
		Model string `json:"model"`
		Usage *struct {
			InputTokens              int64 `json:"input_tokens"`
			OutputTokens             int64 `json:"output_tokens"`
			CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
			CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

// parseUsageLine turns one assistant JSONL line into a usage event. Lines
// without a message id, a request id, a timestamp or usage are skipped, as
// are synthetic messages.
func parseUsageLine(line []byte, ref agentlink.AccountRef) (agentlink.UsageEvent, bool) {
	var l usageLine
	if json.Unmarshal(line, &l) != nil || l.Type != "assistant" || l.Message == nil || l.Message.Usage == nil {
		return agentlink.UsageEvent{}, false
	}
	if l.Message.ID == "" || l.RequestID == "" || l.Message.Model == "<synthetic>" {
		return agentlink.UsageEvent{}, false
	}
	ts, err := time.Parse(time.RFC3339Nano, l.Timestamp)
	if err != nil {
		return agentlink.UsageEvent{}, false
	}
	return agentlink.UsageEvent{
		Account:          ref,
		Source:           ID,
		MessageID:        l.Message.ID + ":" + l.RequestID,
		OccurredAt:       ts.UTC(),
		Model:            l.Message.Model,
		InputTokens:      l.Message.Usage.InputTokens,
		OutputTokens:     l.Message.Usage.OutputTokens,
		CacheWriteTokens: l.Message.Usage.CacheCreationInputTokens,
		CacheReadTokens:  l.Message.Usage.CacheReadInputTokens,
	}, true
}

// scanFile reads complete lines appended since prev and returns the advanced
// cursor. It skips an unchanged file without opening it, restarts at zero when
// the inode changes or the file shrinks, and never consumes a final partial
// line. found is false when the file does not exist.
func scanFile(path string, prev *harness.Cursor, handle func([]byte)) (harness.Cursor, bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return harness.Cursor{}, false, nil
		}
		return harness.Cursor{}, false, fmt.Errorf("stating file: %w", err)
	}
	if info.IsDir() {
		return harness.Cursor{}, false, nil
	}
	inode := fileInode(info)
	start := int64(0)
	if prev != nil {
		sameFile := prev.Inode == 0 || inode == 0 || prev.Inode == inode
		if sameFile && info.Size() >= prev.Offset {
			if prev.Offset == info.Size() && prev.Size == info.Size() && prev.ModTime.Equal(info.ModTime()) {
				return *prev, true, nil
			}
			start = prev.Offset
		}
	}

	f, err := os.Open(path) //nolint:gosec // harness file path chosen by New or the data dir
	if err != nil {
		return harness.Cursor{}, false, fmt.Errorf("opening file: %w", err)
	}
	defer f.Close() //nolint:errcheck // read-only file
	if start > 0 {
		if _, err := f.Seek(start, io.SeekStart); err != nil {
			return harness.Cursor{}, false, fmt.Errorf("seeking file: %w", err)
		}
	}

	offset := start
	reader := bufio.NewReader(f)
	for {
		line, err := reader.ReadBytes('\n')
		switch err {
		case nil:
			handle(line)
			offset += int64(len(line))
		case io.EOF:
			// Any remainder is a partial last line; leave it for the next scan.
			goto done
		default:
			return harness.Cursor{}, false, fmt.Errorf("reading file: %w", err)
		}
	}
done:
	final, err := f.Stat()
	if err != nil {
		return harness.Cursor{}, false, fmt.Errorf("stating open file: %w", err)
	}
	return harness.Cursor{
		Inode:   inode,
		Offset:  offset,
		Size:    final.Size(),
		ModTime: final.ModTime(),
	}, true, nil
}

// fileInode returns a stable file identity without platform build tags.
func fileInode(info os.FileInfo) uint64 {
	sys := info.Sys()
	if sys == nil {
		return 0
	}
	v := reflect.ValueOf(sys)
	for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return 0
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return 0
	}
	if f := v.FieldByName("Ino"); f.IsValid() && f.CanUint() {
		return f.Uint()
	}
	hi := v.FieldByName("FileIndexHigh")
	lo := v.FieldByName("FileIndexLow")
	if hi.IsValid() && lo.IsValid() && hi.CanUint() && lo.CanUint() {
		return hi.Uint()<<32 | lo.Uint()
	}
	return 0
}
