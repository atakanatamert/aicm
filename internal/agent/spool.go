// SPDX-License-Identifier: SUL-1.0

package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/atakanatamert/aicm/internal/agentlink"
)

// maxSpoolFiles caps the outbox. Beyond it the oldest messages are dropped;
// the hub dedupes anyway, so losing the oldest is better than an unbounded
// disk.
const maxSpoolFiles = 10_000

// Spool is a durable outbox: one file per message in dataDir/spool, named
// "<20-digit sequence>-<envelope id>.json" and holding {type, id, payload}.
type Spool struct {
	dir  string
	mu   sync.Mutex
	next uint64
}

// OpenSpool opens or creates the spool directory and resumes the sequence.
func OpenSpool(dir string) (*Spool, error) {
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return nil, fmt.Errorf("creating spool dir: %w", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading spool dir: %w", err)
	}
	var next uint64
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if seq, ok := sequence(e.Name()); ok && seq >= next {
			next = seq + 1
		}
	}
	if next == 0 {
		next = 1
	}
	return &Spool{dir: dir, next: next}, nil
}

// Add writes one message to the spool and returns its envelope id. The file
// and its directory are synced before Add returns.
func (s *Spool) Add(typ agentlink.Type, payload any) (string, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encoding %s payload: %w", typ, err)
	}
	id := agentlink.NewID()
	body, err := json.Marshal(agentlink.Envelope{Type: typ, ID: id, Payload: raw})
	if err != nil {
		return "", fmt.Errorf("encoding %s envelope: %w", typ, err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.next == 0 {
		s.next = 1
	}
	name := fmt.Sprintf("%020d-%s.json", s.next, id)
	if err := writeFileAtomic(filepath.Join(s.dir, name), body, fileMode); err != nil {
		return "", fmt.Errorf("spooling %s: %w", typ, err)
	}
	s.next++
	s.enforceCap()
	return id, nil
}

// Pending returns every spooled message in sequence order.
func (s *Spool) Pending() ([]agentlink.Envelope, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, fmt.Errorf("reading spool dir: %w", err)
	}
	names := jsonNames(entries)
	out := make([]agentlink.Envelope, 0, len(names))
	for _, name := range names {
		b, err := os.ReadFile(filepath.Join(s.dir, name)) //nolint:gosec // names come from the agent's own spool dir
		if errors.Is(err, fs.ErrNotExist) {
			continue // acked between listing and reading
		}
		if err != nil {
			return nil, fmt.Errorf("reading spool file %s: %w", name, err)
		}
		var e agentlink.Envelope
		if err := json.Unmarshal(b, &e); err != nil || e.Type == "" || e.ID == "" {
			slog.Default().Warn("skipping corrupt spool file", "file", name)
			continue
		}
		out = append(out, e)
	}
	return out, nil
}

// Ack removes the message with id. An unknown id is not an error.
func (s *Spool) Ack(id string) error {
	if id == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return fmt.Errorf("reading spool dir: %w", err)
	}
	suffix := "-" + id + ".json"
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), suffix) {
			continue
		}
		if err := os.Remove(filepath.Join(s.dir, e.Name())); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("removing spool file: %w", err)
		}
	}
	return nil
}

// enforceCap drops the oldest files when the spool is over its limit. The
// caller holds s.mu.
func (s *Spool) enforceCap() {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return
	}
	names := jsonNames(entries)
	if len(names) <= maxSpoolFiles {
		return
	}
	names = names[:len(names)-maxSpoolFiles]
	for _, name := range names {
		_ = os.Remove(filepath.Join(s.dir, name))
	}
	slog.Default().Warn("spool over cap, dropped oldest messages", "dropped", len(names), "cap", maxSpoolFiles)
}

// jsonNames returns the spool's message files, oldest first.
func jsonNames(entries []os.DirEntry) []string {
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names
}

// sequence parses the sequence prefix of a spool file name.
func sequence(name string) (uint64, bool) {
	prefix, _, ok := strings.Cut(name, "-")
	if !ok || len(prefix) != 20 {
		return 0, false
	}
	seq, err := strconv.ParseUint(prefix, 10, 64)
	return seq, err == nil
}
