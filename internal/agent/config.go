// SPDX-License-Identifier: SUL-1.0

// Package agent is the aicm agent: it enrolls one machine with a hub, keeps
// an outbound WebSocket open, scans local harnesses and spools every outgoing
// message until the hub acknowledges it. It never sends subscription OAuth
// tokens anywhere.
package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/atakanatamert/aicm/internal/agentlink"
	"github.com/atakanatamert/aicm/internal/version"
)

const (
	configFile    = "config.json"
	machineToken  = "machine.token"
	machineIDFile = "machine-id"
	dirMode       = 0o700
	fileMode      = 0o600
	dataDirEnv    = "AICM_AGENT_DATA"
)

// Config is the pinned hub identity of an enrolled machine.
type Config struct {
	// HubURL is the hub base URL as given to enroll.
	HubURL string `json:"hub_url"`
	OrgID  string `json:"org_id"`
	// HubPublicKey is the pinned Ed25519 public key, base64.
	HubPublicKey string `json:"hub_public_key"`
	// MachineID is the id the hub assigned to this machine.
	MachineID string `json:"machine_id"`
}

// Load reads the config from dataDir.
func Load(dataDir string) (Config, error) {
	b, err := os.ReadFile(filepath.Join(dataDir, configFile)) //nolint:gosec // path is the agent's own data dir
	if err != nil {
		return Config{}, fmt.Errorf("reading agent config: %w", err)
	}
	var c Config
	if err := json.Unmarshal(b, &c); err != nil {
		return Config{}, fmt.Errorf("decoding agent config: %w", err)
	}
	if c.HubURL == "" || c.OrgID == "" || c.HubPublicKey == "" {
		return Config{}, errors.New("agent config is incomplete; run enroll again")
	}
	return c, nil
}

// Save writes the config atomically to dataDir.
func (c Config) Save(dataDir string) error {
	if err := os.MkdirAll(dataDir, dirMode); err != nil {
		return fmt.Errorf("creating data dir: %w", err)
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding agent config: %w", err)
	}
	if err := writeFileAtomic(filepath.Join(dataDir, configFile), append(b, '\n'), fileMode); err != nil {
		return fmt.Errorf("saving agent config: %w", err)
	}
	return nil
}

// DataDir returns the agent's data directory, creating it when needed.
// AICM_AGENT_DATA overrides the platform config directory.
func DataDir() (string, error) {
	if dir := os.Getenv(dataDirEnv); dir != "" {
		if err := os.MkdirAll(dir, dirMode); err != nil { //nolint:gosec // operator-chosen path from the environment
			return "", fmt.Errorf("creating %s: %w", dataDirEnv, err)
		}
		return dir, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("finding user config dir: %w", err)
	}
	dir := filepath.Join(base, version.AgentBinary)
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return "", fmt.Errorf("creating data dir: %w", err)
	}
	return dir, nil
}

// HomeDir is the home directory harnesses read. In Docker the host home is
// mounted here through AICM_AGENT_HOME.
func HomeDir() (string, error) {
	if dir := os.Getenv("AICM_AGENT_HOME"); dir != "" {
		return dir, nil
	}
	dir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("finding home dir: %w", err)
	}
	return dir, nil
}

// Fingerprint derives the machine fingerprint from the OS's stable machine
// id, generating and storing a random one when the OS has none.
func Fingerprint() (string, error) {
	id, err := machineID()
	if err != nil {
		return "", err
	}
	return agentlink.Fingerprint(id), nil
}

var platformUUID = regexp.MustCompile(`"IOPlatformUUID"\s*=\s*"([^"]+)"`)

// machineID returns the OS machine id, or a random id stored in the data dir.
func machineID() (string, error) {
	switch runtime.GOOS {
	case "linux":
		for _, path := range []string{"/etc/machine-id", "/var/lib/dbus/machine-id"} {
			if b, err := os.ReadFile(path); err == nil { //nolint:gosec // fixed system paths
				if id := strings.TrimSpace(string(b)); id != "" {
					return id, nil
				}
			}
		}
	case "darwin":
		out, err := exec.CommandContext(context.Background(), "ioreg", "-rd1", "-c", "IOPlatformExpertDevice").Output()
		if err == nil {
			if m := platformUUID.FindSubmatch(out); m != nil {
				return string(m[1]), nil
			}
		}
	case "windows":
		out, err := exec.CommandContext(context.Background(), "reg", "query", `HKLM\SOFTWARE\Microsoft\Cryptography`, "/v", "MachineGuid").Output()
		if err == nil {
			fields := strings.Fields(string(out))
			if len(fields) > 0 {
				return fields[len(fields)-1], nil
			}
		}
	}
	return storedMachineID()
}

// storedMachineID reads the fallback id, creating it once.
func storedMachineID() (string, error) {
	dir, err := DataDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, machineIDFile)
	b, err := os.ReadFile(path) //nolint:gosec // path is the agent's own data dir
	switch {
	case err == nil:
		if id := strings.TrimSpace(string(b)); id != "" {
			return id, nil
		}
	case !errors.Is(err, fs.ErrNotExist):
		return "", fmt.Errorf("reading machine id: %w", err)
	}
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generating machine id: %w", err)
	}
	id := hex.EncodeToString(raw[:])
	if err := writeFileAtomic(path, []byte(id+"\n"), fileMode); err != nil {
		return "", fmt.Errorf("saving machine id: %w", err)
	}
	return id, nil
}

// loadMachineToken reads the machine credential. A missing file is reported
// as fs.ErrNotExist.
func loadMachineToken(dataDir string) (string, error) {
	b, err := os.ReadFile(filepath.Join(dataDir, machineToken)) //nolint:gosec // path is the agent's own data dir
	if err != nil {
		return "", fmt.Errorf("reading machine token: %w", err)
	}
	return strings.TrimSpace(string(b)), nil
}

// saveMachineToken writes the machine credential atomically.
func saveMachineToken(dataDir, token string) error {
	if token == "" {
		return errors.New("machine token is empty")
	}
	if err := writeFileAtomic(filepath.Join(dataDir, machineToken), []byte(token+"\n"), fileMode); err != nil {
		return fmt.Errorf("saving machine token: %w", err)
	}
	return nil
}

// writeFileAtomic writes data to path through a synced temporary file in the
// same directory, then renames it into place.
func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+"-*")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmp := f.Name()
	cleanup := func() {
		_ = f.Close()
		_ = os.Remove(tmp)
	}
	if err := f.Chmod(mode); err != nil {
		cleanup()
		return fmt.Errorf("setting temp file mode: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		cleanup()
		return fmt.Errorf("writing temp file: %w", err)
	}
	if err := f.Sync(); err != nil {
		cleanup()
		return fmt.Errorf("syncing temp file: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("closing temp file: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("renaming temp file: %w", err)
	}
	// A rename is durable only after the directory is synced too.
	if d, err := os.Open(dir); err == nil { //nolint:gosec // dir is ours
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}
