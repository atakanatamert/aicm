// SPDX-License-Identifier: SUL-1.0

package claudecode

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"

	"github.com/atakanatamert/aicm/internal/agentlink"
)

func compareGolden(t *testing.T, got any, name string) {
	t.Helper()
	data, err := json.MarshalIndent(got, "", "  ")
	if err != nil {
		t.Fatalf("marshaling golden: %v", err)
	}
	data = append(data, '\n')
	path := filepath.Join("testdata", name)
	if os.Getenv("UPDATE_GOLDEN") != "" {
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatalf("updating golden: %v", err)
		}
	}
	want, err := os.ReadFile(path) //nolint:gosec // testdata path
	if err != nil {
		t.Fatalf("reading golden %s: %v", path, err)
	}
	if !bytes.Equal(data, want) {
		t.Fatalf("golden %s mismatch\n got: %s\nwant: %s", path, data, want)
	}
}

func TestDiscoverGolden(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	home := t.TempDir()
	if err := os.CopyFS(home, os.DirFS(filepath.Join("testdata", "discover"))); err != nil {
		t.Fatalf("copying fixtures: %v", err)
	}
	h := New(home, t.TempDir())
	h.keychain = nil // keep the test off the real Keychain
	accounts, err := h.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	compareGolden(t, accounts, "discover.golden.json")

	raw, err := os.ReadFile("testdata/discover.golden.json")
	if err != nil {
		t.Fatalf("reading golden: %v", err)
	}
	for _, secret := range []string{"SYNTHETIC", "accessToken", "refreshToken", "sk-ant"} {
		if bytes.Contains(raw, []byte(secret)) {
			t.Fatalf("golden leaked credentials: found %q", secret)
		}
	}
}

func TestScanLogsGolden(t *testing.T) {
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS(filepath.Join("testdata", "logs"))); err != nil {
		t.Fatalf("copying fixtures: %v", err)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	h := New(t.TempDir(), t.TempDir())

	first, err := h.Scan(context.Background(), nil)
	if err != nil {
		t.Fatalf("first Scan: %v", err)
	}
	compareGolden(t, first.Usage, "logs.golden.json")
	if len(first.Meters) != 0 {
		t.Fatalf("unexpected meters: %d", len(first.Meters))
	}

	second, err := h.Scan(context.Background(), first.Cursors)
	if err != nil {
		t.Fatalf("second Scan: %v", err)
	}
	if len(second.Usage) != 0 || len(second.Meters) != 0 {
		t.Fatalf("rescan returned %d usage and %d meters, want none", len(second.Usage), len(second.Meters))
	}

	beta := filepath.Join(dir, "projects", "beta", "session-b.jsonl")
	appended := `{"type":"assistant","timestamp":"2026-10-02T08:00:00Z","requestId":"req_6","message":{"id":"msg_f","model":"claude-sonnet-4-5","usage":{"input_tokens":7,"output_tokens":3,"cache_creation_input_tokens":1,"cache_read_input_tokens":2}}}` + "\n"
	f, err := os.OpenFile(beta, os.O_WRONLY|os.O_APPEND, 0o600) //nolint:gosec // test fixture
	if err != nil {
		t.Fatalf("opening fixture: %v", err)
	}
	if _, err := f.WriteString(appended); err != nil {
		t.Fatalf("appending fixture: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("closing fixture: %v", err)
	}

	third, err := h.Scan(context.Background(), second.Cursors)
	if err != nil {
		t.Fatalf("Scan after append: %v", err)
	}
	if got := messageIDs(third.Usage); !reflect.DeepEqual(got, []string{"msg_f:req_6"}) {
		t.Fatalf("append scan events = %v, want msg_f:req_6", got)
	}

	if err := os.Truncate(beta, 0); err != nil {
		t.Fatalf("truncating fixture: %v", err)
	}
	f, err = os.OpenFile(beta, os.O_WRONLY|os.O_APPEND, 0o600) //nolint:gosec // test fixture
	if err != nil {
		t.Fatalf("reopening fixture: %v", err)
	}
	rewritten := `{"type":"assistant","timestamp":"2026-10-03T08:00:00Z","requestId":"req_7","message":{"id":"msg_g","model":"claude-sonnet-4-5","usage":{"input_tokens":4,"output_tokens":2}}}` + "\n"
	if _, err := f.WriteString(rewritten); err != nil {
		t.Fatalf("rewriting fixture: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("closing fixture: %v", err)
	}

	fourth, err := h.Scan(context.Background(), third.Cursors)
	if err != nil {
		t.Fatalf("Scan after truncate: %v", err)
	}
	if got := messageIDs(fourth.Usage); !reflect.DeepEqual(got, []string{"msg_g:req_7"}) {
		t.Fatalf("truncate scan events = %v, want msg_g:req_7", got)
	}
}

func messageIDs(events []agentlink.UsageEvent) []string {
	out := make([]string, 0, len(events))
	for _, event := range events {
		out = append(out, event.MessageID)
	}
	return out
}

func TestStatuslineCaptureGolden(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, "config")
	transcript := filepath.Join(configDir, "projects", "-tmp", "session.jsonl")
	if err := os.MkdirAll(filepath.Dir(transcript), 0o700); err != nil {
		t.Fatalf("creating project dir: %v", err)
	}
	for _, name := range []string{".claude.json", ".credentials.json"} {
		data, err := os.ReadFile(filepath.Join("testdata", "logs", name)) //nolint:gosec // testdata path
		if err != nil {
			t.Fatalf("reading fixture %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(configDir, name), data, 0o600); err != nil {
			t.Fatalf("writing fixture %s: %v", name, err)
		}
	}
	if err := os.WriteFile(transcript, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("writing transcript: %v", err)
	}

	dataDir := filepath.Join(dir, "data")
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		t.Fatalf("creating data dir: %v", err)
	}
	original, err := os.ReadFile(filepath.Join("testdata", "statusline", "statusline.json")) //nolint:gosec // testdata path
	if err != nil {
		t.Fatalf("reading statusline fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "statusline.json"), original, 0o600); err != nil {
		t.Fatalf("writing statusline config: %v", err)
	}

	t.Setenv("CLAUDE_CONFIG_DIR", configDir)
	input, err := os.ReadFile(filepath.Join("testdata", "statusline", "stdin.json")) //nolint:gosec // testdata path
	if err != nil {
		t.Fatalf("reading stdin fixture: %v", err)
	}
	// The path goes inside a JSON string; Windows backslashes need escaping.
	quoted, err := json.Marshal(transcript)
	if err != nil {
		t.Fatalf("encoding transcript path: %v", err)
	}
	transcriptJSON := quoted[1 : len(quoted)-1]
	input = bytes.ReplaceAll(input, []byte("/path/to/transcript.jsonl"), transcriptJSON)
	// The original's stdout passes through byte-exact. PowerShell, which
	// Claude Code uses on Windows without Git Bash, ends output lines in CRLF.
	want := "hi\n"
	if runtime.GOOS == "windows" && findGitBash() == "" {
		want = "hi\r\n"
	}
	var out bytes.Buffer
	if code := RunStatusline(bytes.NewReader(input), &out, dataDir); code != 0 {
		t.Fatalf("RunStatusline exit = %d, want 0", code)
	}
	if out.String() != want {
		t.Fatalf("stdout = %q, want %q", out.String(), want)
	}

	invalid, err := os.ReadFile(filepath.Join("testdata", "statusline", "stdin-invalid.json")) //nolint:gosec // testdata path
	if err != nil {
		t.Fatalf("reading invalid stdin fixture: %v", err)
	}
	invalid = bytes.ReplaceAll(invalid, []byte("/path/to/transcript.jsonl"), transcriptJSON)
	out.Reset()
	if code := RunStatusline(bytes.NewReader(invalid), &out, dataDir); code != 0 {
		t.Fatalf("RunStatusline invalid exit = %d, want 0", code)
	}
	if out.String() != want {
		t.Fatalf("stdout = %q, want %q", out.String(), want)
	}

	spoolPath := filepath.Join(dataDir, "claude-statusline.jsonl")
	spool, err := os.ReadFile(spoolPath) //nolint:gosec // test temp dir
	if err != nil {
		t.Fatalf("reading spool: %v", err)
	}
	lines := bytes.Split(bytes.TrimSpace(spool), []byte("\n"))
	if len(lines) != 1 {
		t.Fatalf("spool has %d lines, want 1: %s", len(lines), spool)
	}
	var captured capture
	if err := json.Unmarshal(lines[0], &captured); err != nil {
		t.Fatalf("parsing spool line: %v", err)
	}
	if !validCaptureWindow(captured.FiveHour) || !validCaptureWindow(captured.SevenDay) {
		t.Fatalf("spool line has invalid windows: %s", lines[0])
	}

	// Freeze captured_at so the meter golden is deterministic.
	fixed := time.Date(2025, 2, 1, 15, 0, 0, 0, time.UTC)
	var line map[string]json.RawMessage
	if err := json.Unmarshal(lines[0], &line); err != nil {
		t.Fatalf("parsing spool line: %v", err)
	}
	line["captured_at"], err = json.Marshal(fixed)
	if err != nil {
		t.Fatalf("encoding captured_at: %v", err)
	}
	normalized, err := json.Marshal(line)
	if err != nil {
		t.Fatalf("encoding spool line: %v", err)
	}
	if err := os.WriteFile(spoolPath, append(normalized, '\n'), 0o600); err != nil {
		t.Fatalf("normalizing spool: %v", err)
	}

	h := New(dir, dataDir)
	batch, err := h.Scan(context.Background(), nil)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	compareGolden(t, batch.Meters, "statusline.golden.json")
}

func TestInstallUninstall(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, "config")
	dataDir := filepath.Join(dir, "data")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatalf("creating config dir: %v", err)
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		t.Fatalf("creating data dir: %v", err)
	}
	original, err := os.ReadFile(filepath.Join("testdata", "install", "settings.json")) //nolint:gosec // testdata path
	if err != nil {
		t.Fatalf("reading settings fixture: %v", err)
	}
	settingsPath := filepath.Join(configDir, "settings.json")
	if err := os.WriteFile(settingsPath, original, 0o600); err != nil {
		t.Fatalf("writing settings fixture: %v", err)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", configDir)
	h := New(t.TempDir(), dataDir)
	executable := "/usr/local/bin/aicm-agent"
	wantCommand := executable + " statusline"
	if runtime.GOOS == "windows" {
		// Rooted but driveless, so Abs adds the current drive. Forward slashes
		// keep the command valid in both Git Bash and PowerShell.
		abs, err := filepath.Abs(executable)
		if err != nil {
			t.Fatalf("resolving executable: %v", err)
		}
		wantCommand = filepath.ToSlash(abs) + " statusline"
	}

	if err := h.Install(executable); err != nil {
		t.Fatalf("Install: %v", err)
	}
	got := readJSONMap(t, settingsPath)
	statusLine, ok := got["statusLine"].(map[string]any)
	if !ok {
		t.Fatalf("statusLine missing or not an object: %v", got["statusLine"])
	}
	if statusLine["type"] != "command" {
		t.Fatalf("statusLine type = %v", statusLine["type"])
	}
	if statusLine["command"] != wantCommand {
		t.Fatalf("statusLine command = %v, want %q", statusLine["command"], wantCommand)
	}
	if statusLine["padding"] != float64(2) {
		t.Fatalf("statusLine padding = %v, want 2", statusLine["padding"])
	}
	if got["theme"] != "dark" || got["model"] != "opus" {
		t.Fatalf("other keys changed: %v", got)
	}
	backups, err := filepath.Glob(settingsPath + ".aicm-backup-*")
	if err != nil || len(backups) != 1 {
		t.Fatalf("backups = %v (err %v), want 1", backups, err)
	}
	stored := readJSONMap(t, filepath.Join(dataDir, "statusline.json"))
	if stored["command"] != "echo mine" || stored["padding"] != float64(2) {
		t.Fatalf("stored original = %v", stored)
	}

	before, err := os.ReadFile(settingsPath) //nolint:gosec // test temp dir
	if err != nil {
		t.Fatalf("reading settings: %v", err)
	}
	if err := h.Install(executable); err != nil {
		t.Fatalf("second Install: %v", err)
	}
	after, err := os.ReadFile(settingsPath) //nolint:gosec // test temp dir
	if err != nil {
		t.Fatalf("reading settings: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("second Install changed settings:\nbefore: %s\nafter: %s", before, after)
	}

	// A wrapper from an earlier install or another path is pointed at this
	// executable; the stored original stays.
	stale := readJSONMapBytes(t, after)
	stale["statusLine"].(map[string]any)["command"] = `'/opt/old/aicm-agent' statusline`
	staleData, err := json.Marshal(stale)
	if err != nil {
		t.Fatalf("encoding stale settings: %v", err)
	}
	if err := os.WriteFile(settingsPath, staleData, 0o600); err != nil {
		t.Fatalf("writing stale settings: %v", err)
	}
	if err := h.Install(executable); err != nil {
		t.Fatalf("Install over a stale wrapper: %v", err)
	}
	refreshed := readJSONMap(t, settingsPath)["statusLine"].(map[string]any)
	if refreshed["command"] != wantCommand {
		t.Fatalf("refreshed command = %v, want %q", refreshed["command"], wantCommand)
	}
	if stored := readJSONMap(t, filepath.Join(dataDir, "statusline.json")); stored["command"] != "echo mine" {
		t.Fatalf("stored original after refresh = %v", stored)
	}

	if err := h.Uninstall(); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	restored := readJSONMap(t, settingsPath)
	want := readJSONMapBytes(t, original)
	if !reflect.DeepEqual(restored, want) {
		t.Fatalf("restored settings = %v, want %v", restored, want)
	}
}

func TestWindowsWrapperCommand(t *testing.T) {
	tests := []struct {
		name    string
		abs     string
		gitBash bool
		want    string
	}{
		{"plain path", `C:\Users\me\AppData\Local\aicm-agent\aicm-agent.exe`, true, "C:/Users/me/AppData/Local/aicm-agent/aicm-agent.exe statusline"},
		{"plain path without Git Bash", `C:\Users\me\AppData\Local\aicm-agent\aicm-agent.exe`, false, "C:/Users/me/AppData/Local/aicm-agent/aicm-agent.exe statusline"},
		{"space for Git Bash", `C:\Users\Jo O'Neil\aicm-agent.exe`, true, `'C:/Users/Jo O'\''Neil/aicm-agent.exe' statusline`},
		{"space for PowerShell", `C:\Users\Jo O'Neil\aicm-agent.exe`, false, `& 'C:/Users/Jo O''Neil/aicm-agent.exe' statusline`},
		{"typographic quote for PowerShell", `C:\Users\Jo O’Neil\aicm-agent.exe`, false, `& 'C:/Users/Jo O’’Neil/aicm-agent.exe' statusline`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := windowsWrapperCommand(tt.abs, tt.gitBash)
			if got != tt.want {
				t.Fatalf("windowsWrapperCommand = %q, want %q", got, tt.want)
			}
			if !isWrapperCommand(got) {
				t.Fatalf("isWrapperCommand(%q) = false", got)
			}
		})
	}

	for _, command := range []string{
		"/usr/local/bin/aicm-agent statusline",
		`'C:\Users\me\AppData\Local\aicm-agent\aicm-agent.exe' statusline`,
	} {
		if !isWrapperCommand(command) {
			t.Fatalf("isWrapperCommand(%q) = false for an earlier install's form", command)
		}
	}
	for _, command := range []string{"echo mine", "~/.claude/statusline.sh", "aicm-agent status"} {
		if isWrapperCommand(command) {
			t.Fatalf("isWrapperCommand(%q) = true", command)
		}
	}
}

func readJSONMap(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path) //nolint:gosec // test temp dir
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return readJSONMapBytes(t, data)
}

func readJSONMapBytes(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("parsing JSON: %v", err)
	}
	return out
}
