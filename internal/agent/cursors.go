// SPDX-License-Identifier: Apache-2.0

package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"github.com/atakanatamert/aicm/internal/harness"
)

const cursorsFile = "cursors.json"

// Cursors is the on-disk map from harness id to that harness's read
// positions.
type Cursors struct {
	dir string
	mu  sync.Mutex
}

// OpenCursors returns a cursor store rooted at the agent data dir.
func OpenCursors(dir string) *Cursors {
	return &Cursors{dir: dir}
}

// Load returns every harness's cursors. Missing state is an empty map.
func (c *Cursors) Load() (map[string]harness.Cursors, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.load()
}

// Save stores one harness's cursors, keeping the others.
func (c *Cursors) Save(id string, cur harness.Cursors) error {
	if id == "" {
		return errors.New("harness id is required")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	all, err := c.load()
	if err != nil {
		return err
	}
	all[id] = cur
	b, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding cursors: %w", err)
	}
	if err := writeFileAtomic(filepath.Join(c.dir, cursorsFile), append(b, '\n'), fileMode); err != nil {
		return fmt.Errorf("saving cursors: %w", err)
	}
	return nil
}

// load reads the cursor file. The caller holds c.mu.
func (c *Cursors) load() (map[string]harness.Cursors, error) {
	b, err := os.ReadFile(filepath.Join(c.dir, cursorsFile))
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]harness.Cursors{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading cursors: %w", err)
	}
	var all map[string]harness.Cursors
	if err := json.Unmarshal(b, &all); err != nil {
		return nil, fmt.Errorf("decoding cursors: %w", err)
	}
	if all == nil {
		all = map[string]harness.Cursors{}
	}
	return all, nil
}
