// SPDX-License-Identifier: SUL-1.0

package claudecode

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/atakanatamert/aicm/internal/agentlink"
	"github.com/atakanatamert/aicm/internal/harness"
	"github.com/atakanatamert/aicm/internal/meter"
	"github.com/atakanatamert/aicm/internal/version"
)

var (
	fiveHourDef = meter.Def{Key: "five_hour", Label: "5-hour limit", Unit: meter.UnitPercent, ResetKind: meter.ResetFixed, WindowMinutes: 300}
	sevenDayDef = meter.Def{Key: "seven_day", Label: "Weekly limit", Unit: meter.UnitPercent, ResetKind: meter.ResetFixed, WindowMinutes: 10080}
)

type captureWindow struct {
	UsedPercentage *float64 `json:"used_percentage"`
	ResetsAt       int64    `json:"resets_at"`
}

type statusInput struct {
	TranscriptPath string `json:"transcript_path"`
	RateLimits     *struct {
		FiveHour *captureWindow `json:"five_hour"`
		SevenDay *captureWindow `json:"seven_day"`
	} `json:"rate_limits"`
}

type capture struct {
	CapturedAt     time.Time      `json:"captured_at"`
	TranscriptPath string         `json:"transcript_path"`
	FiveHour       *captureWindow `json:"five_hour"`
	SevenDay       *captureWindow `json:"seven_day"`
}

type originalStatusline struct {
	Command string `json:"command"`
}

// RunStatusline is the command Claude Code runs: capture rate limits, then
// run the user's previous statusline with the same stdin and copy its stdout
// and exit code. It never touches the network.
func RunStatusline(stdin io.Reader, stdout io.Writer, dataDir string) int {
	input, err := io.ReadAll(stdin)
	if err != nil {
		input = nil
	}
	captureStatusline(dataDir, input)

	command := readOriginalCommand(dataDir)
	if command == "" {
		return 0
	}
	cmd := shellCommand(command)
	cmd.Stdin = bytes.NewReader(input)
	cmd.Stdout = stdout
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return exit.ExitCode()
		}
		return 1
	}
	return 0
}

// shellCommand runs a statusline command the way Claude Code does: sh on
// Unix; on Windows Git Bash when it is installed, otherwise PowerShell.
func shellCommand(command string) *exec.Cmd {
	if runtime.GOOS != "windows" {
		return exec.CommandContext(context.Background(), "sh", "-c", command) //nolint:gosec // the user's configured statusline command
	}
	if bash := findGitBash(); bash != "" {
		return exec.CommandContext(context.Background(), bash, "-c", command) //nolint:gosec // the user's configured statusline command
	}
	args := []string{"-NoProfile", "-NonInteractive"}
	if os.Getenv("CLAUDE_CODE_POWERSHELL_RESPECT_EXECUTION_POLICY") != "1" {
		args = append(args, "-ExecutionPolicy", "Bypass")
	}
	args = append(args, "-Command", command)
	return exec.CommandContext(context.Background(), findPowerShell(), args...) //nolint:gosec // the user's configured statusline command
}

// findGitBash mirrors how Claude Code locates Git Bash on Windows:
// CLAUDE_CODE_GIT_BASH_PATH when it names an existing bash or sh, then the
// default Git install dirs, then bin\bash.exe of the Git install behind the
// git on PATH. A git inside the working directory is ignored, as Claude Code
// refuses executables a project placed there. It returns "" when none exists.
func findGitBash() string {
	if p := os.Getenv("CLAUDE_CODE_GIT_BASH_PATH"); p != "" && isRegularFile(p) {
		switch strings.ToLower(filepath.Base(p)) {
		case "bash.exe", "sh.exe", "bash", "sh":
			return p
		}
	}
	for _, root := range []string{`C:\Program Files\Git`, `C:\Program Files (x86)\Git`} {
		if p := filepath.Join(root, "bin", "bash.exe"); isRegularFile(p) {
			return p
		}
	}
	git, err := exec.LookPath("git")
	if err != nil {
		return ""
	}
	if git, err = filepath.Abs(git); err != nil {
		return ""
	}
	if wd, err := os.Getwd(); err == nil {
		if rel, err := filepath.Rel(wd, git); err == nil && rel != ".." &&
			!strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return ""
		}
	}
	// git.exe lives in <Git>\cmd or <Git>\bin; bash.exe in <Git>\bin.
	if p := filepath.Join(filepath.Dir(filepath.Dir(git)), "bin", "bash.exe"); isRegularFile(p) {
		return p
	}
	return ""
}

// findPowerShell prefers PowerShell 7 and falls back to Windows PowerShell,
// as Claude Code does.
func findPowerShell() string {
	for _, name := range []string{"pwsh.exe", "powershell.exe"} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	return "powershell.exe"
}

func isRegularFile(path string) bool {
	info, err := os.Stat(path) //nolint:gosec // fixed Git install paths, PATH lookups or the user's CLAUDE_CODE_GIT_BASH_PATH
	return err == nil && info.Mode().IsRegular()
}

// captureStatusline appends one capture line when stdin carries a valid
// rate-limit window. Every failure is silent: a statusline must never break
// the session.
func captureStatusline(dataDir string, input []byte) {
	var in statusInput
	if json.Unmarshal(input, &in) != nil || in.RateLimits == nil {
		return
	}
	c := capture{CapturedAt: time.Now().UTC(), TranscriptPath: in.TranscriptPath}
	if validCaptureWindow(in.RateLimits.FiveHour) {
		c.FiveHour = in.RateLimits.FiveHour
	}
	if validCaptureWindow(in.RateLimits.SevenDay) {
		c.SevenDay = in.RateLimits.SevenDay
	}
	if c.FiveHour == nil && c.SevenDay == nil {
		return
	}
	line, err := json.Marshal(c)
	if err != nil {
		return
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return
	}
	f, err := os.OpenFile( //nolint:gosec // the agent's own data dir
		filepath.Join(dataDir, "claude-statusline.jsonl"),
		os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer f.Close() //nolint:errcheck // best-effort append
	_, _ = f.Write(append(line, '\n'))
}

func validCaptureWindow(w *captureWindow) bool {
	return w != nil && w.UsedPercentage != nil && *w.UsedPercentage >= 0 &&
		*w.UsedPercentage <= 100 && w.ResetsAt > 0
}

func readOriginalCommand(dataDir string) string {
	data, err := os.ReadFile(filepath.Join(dataDir, "statusline.json")) //nolint:gosec // the agent's own data dir
	if err != nil {
		return ""
	}
	var original originalStatusline
	if json.Unmarshal(data, &original) != nil {
		return ""
	}
	return original.Command
}

// Install wraps each config dir's statusline with the aicm-agent wrapper,
// keeping the previous statusLine object in the data dir.
func (h *Harness) Install(executable string) error {
	abs, err := filepath.Abs(executable)
	if err != nil {
		return fmt.Errorf("resolving agent executable: %w", err)
	}
	wrapper := wrapperCommand(abs)
	var original json.RawMessage
	haveOriginal := false
	changed := false
	for _, dir := range h.dirs {
		path := filepath.Join(dir.path, "settings.json")
		data, err := os.ReadFile(path) //nolint:gosec // a harness config dir chosen by New
		existed := true
		if err != nil {
			if !os.IsNotExist(err) {
				return fmt.Errorf("reading %s: %w", path, err)
			}
			existed = false
			data = []byte("{}")
		}
		settings := map[string]json.RawMessage{}
		if len(bytes.TrimSpace(data)) > 0 {
			if err := json.Unmarshal(data, &settings); err != nil {
				return fmt.Errorf("parsing %s: %w", path, err)
			}
		}
		if settings == nil {
			settings = map[string]json.RawMessage{}
		}

		var current map[string]json.RawMessage
		if raw, ok := settings["statusLine"]; ok && !isJSONNull(raw) {
			if err := json.Unmarshal(raw, &current); err != nil {
				return fmt.Errorf("parsing statusLine in %s: %w", path, err)
			}
		}
		var existingCommand string
		if raw, ok := current["command"]; ok {
			_ = json.Unmarshal(raw, &existingCommand)
		}
		// A wrapper from an earlier install is refreshed when it differs from
		// the current one (moved binary, other shell); the stored original is
		// kept.
		wrapped := isWrapperCommand(existingCommand)
		if wrapped && strings.TrimSpace(existingCommand) == wrapper {
			continue
		}

		if existed {
			backup := path + ".aicm-backup-" + strconv.FormatInt(time.Now().Unix(), 10)
			if err := os.WriteFile(backup, data, 0o600); err != nil { //nolint:gosec // backup beside the harness settings file
				return fmt.Errorf("backing up %s: %w", path, err)
			}
		}
		if !haveOriginal && current != nil && !wrapped {
			original = append(json.RawMessage(nil), settings["statusLine"]...)
			haveOriginal = true
		}

		next := map[string]json.RawMessage{}
		for key, value := range current {
			next[key] = value
		}
		next["type"] = json.RawMessage(`"command"`)
		next["command"], err = json.Marshal(wrapper)
		if err != nil {
			return fmt.Errorf("encoding statusLine for %s: %w", path, err)
		}
		encoded, err := json.Marshal(next)
		if err != nil {
			return fmt.Errorf("encoding statusLine for %s: %w", path, err)
		}
		settings["statusLine"] = encoded
		out, err := json.MarshalIndent(settings, "", "  ")
		if err != nil {
			return fmt.Errorf("encoding %s: %w", path, err)
		}
		out = append(out, '\n')
		mode := os.FileMode(0o600)
		if info, statErr := os.Stat(path); statErr == nil {
			mode = info.Mode().Perm()
		}
		if err := os.WriteFile(path, out, mode); err != nil {
			return fmt.Errorf("writing %s: %w", path, err)
		}
		changed = changed || !wrapped
	}
	if !changed {
		return nil
	}
	if err := os.MkdirAll(h.dataDir, 0o700); err != nil {
		return fmt.Errorf("creating data dir: %w", err)
	}
	payload := []byte("null")
	if haveOriginal {
		payload = original
	}
	if err := os.WriteFile(filepath.Join(h.dataDir, "statusline.json"), payload, 0o600); err != nil {
		return fmt.Errorf("storing original statusline: %w", err)
	}
	return nil
}

// Uninstall restores the statusLine object Install stored, or removes the key
// when there was none.
func (h *Harness) Uninstall() error {
	data, err := os.ReadFile(filepath.Join(h.dataDir, "statusline.json")) //nolint:gosec // the agent's own data dir
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading stored statusline: %w", err)
	}
	trimmed := bytes.TrimSpace(data)
	var original json.RawMessage
	if len(trimmed) > 0 && !isJSONNull(trimmed) {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(trimmed, &obj); err != nil {
			return fmt.Errorf("parsing stored statusline: %w", err)
		}
		original = append(json.RawMessage(nil), trimmed...)
	}
	for _, dir := range h.dirs {
		path := filepath.Join(dir.path, "settings.json")
		data, err := os.ReadFile(path) //nolint:gosec // a harness config dir chosen by New
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("reading %s: %w", path, err)
		}
		settings := map[string]json.RawMessage{}
		if len(bytes.TrimSpace(data)) > 0 {
			if err := json.Unmarshal(data, &settings); err != nil {
				return fmt.Errorf("parsing %s: %w", path, err)
			}
		}
		if settings == nil {
			settings = map[string]json.RawMessage{}
		}
		if original == nil {
			delete(settings, "statusLine")
		} else {
			settings["statusLine"] = original
		}
		out, err := json.MarshalIndent(settings, "", "  ")
		if err != nil {
			return fmt.Errorf("encoding %s: %w", path, err)
		}
		out = append(out, '\n')
		mode := os.FileMode(0o600)
		if info, statErr := os.Stat(path); statErr == nil {
			mode = info.Mode().Perm()
		}
		if err := os.WriteFile(path, out, mode); err != nil {
			return fmt.Errorf("writing %s: %w", path, err)
		}
	}
	return nil
}

func isJSONNull(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

func isWrapperCommand(command string) bool {
	exe, ok := strings.CutSuffix(strings.TrimSpace(command), " statusline")
	if !ok {
		return false
	}
	// Drop PowerShell's call operator and either shell's quotes.
	exe = strings.Trim(strings.TrimSpace(strings.TrimPrefix(exe, "&")), `"'`)
	base := exe[strings.LastIndexAny(exe, `/\`)+1:]
	return base == version.AgentBinary || base == version.AgentBinary+".exe"
}

// wrapperCommand is the statusLine command Install writes for the agent at
// abs.
func wrapperCommand(abs string) string {
	if runtime.GOOS != "windows" {
		return shellQuote(abs) + " statusline"
	}
	return windowsWrapperCommand(abs, findGitBash() != "")
}

// windowsShellSpecial holds the characters that keep a forward-slash path
// from being a plain word in both Git Bash and PowerShell. PowerShell also
// treats typographic quotes as quotes.
const windowsShellSpecial = " \t\r\n'\"`$!*?()[]{};&|<>#~@,%^=+‘’‚‛“”„"

// windowsWrapperCommand writes the path with forward slashes, which Git Bash
// and PowerShell both run unquoted. A path that needs quoting is quoted for
// the shell Claude Code picks: Git Bash when installed, else PowerShell.
func windowsWrapperCommand(abs string, gitBash bool) string {
	path := strings.ReplaceAll(abs, `\`, "/")
	switch {
	case !strings.ContainsAny(path, windowsShellSpecial):
		return path + " statusline"
	case gitBash:
		return "'" + strings.ReplaceAll(path, "'", `'\''`) + "' statusline"
	default:
		return "& " + powerShellQuote(path) + " statusline"
	}
}

// powerShellQuote single-quotes s for PowerShell, doubling every character
// PowerShell reads as a single quote.
func powerShellQuote(s string) string {
	var b strings.Builder
	b.WriteByte('\'')
	for _, r := range s {
		b.WriteRune(r)
		if strings.ContainsRune("'‘’‚‛", r) {
			b.WriteRune(r)
		}
	}
	b.WriteByte('\'')
	return b.String()
}

// shellQuote quotes an executable path for sh.
func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	if !strings.ContainsAny(s, " \t\n'\"\\$`!*?()[]{};&|<>#~") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// scanStatusline emits meter reports for new spool lines.
func (h *Harness) scanStatusline(accounts []dirAccount, cursors harness.Cursors, batch *harness.Batch) error {
	path := h.spoolPath()
	prev, ok := cursors[path]
	cursor, found, err := scanFile(path, cursorOrNil(prev, ok), func(line []byte) {
		var c capture
		if json.Unmarshal(line, &c) != nil || c.CapturedAt.IsZero() {
			return
		}
		ref, ok := accountForTranscript(accounts, c.TranscriptPath)
		if !ok {
			return
		}
		if report, ok := meterReportFor(ref, fiveHourDef, c.CapturedAt, c.FiveHour); ok {
			batch.Meters = append(batch.Meters, report)
		}
		if report, ok := meterReportFor(ref, sevenDayDef, c.CapturedAt, c.SevenDay); ok {
			batch.Meters = append(batch.Meters, report)
		}
	})
	if err != nil {
		return fmt.Errorf("reading statusline spool: %w", err)
	}
	if found {
		batch.Cursors[path] = cursor
	}
	return nil
}

func accountForTranscript(accounts []dirAccount, transcript string) (agentlink.AccountRef, bool) {
	if transcript == "" {
		return agentlink.AccountRef{}, false
	}
	clean := filepath.Clean(transcript)
	var ref agentlink.AccountRef
	best := -1
	for _, acct := range accounts {
		rel, err := filepath.Rel(acct.dir.path, clean)
		if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		if len(acct.dir.path) > best {
			best = len(acct.dir.path)
			ref = acct.ref
		}
	}
	return ref, best >= 0
}

func meterReportFor(ref agentlink.AccountRef, def meter.Def, capturedAt time.Time, w *captureWindow) (agentlink.MeterReport, bool) {
	if !validCaptureWindow(w) || w.UsedPercentage == nil {
		return agentlink.MeterReport{}, false
	}
	used := *w.UsedPercentage
	limit := 100.0
	resets := time.Unix(w.ResetsAt, 0).UTC()
	reading := meter.Reading{
		Key:        def.Key,
		Used:       &used,
		Limit:      &limit,
		ResetsAt:   &resets,
		CapturedAt: capturedAt,
		Source:     meter.SourceStatusline,
	}
	if err := reading.Validate(def); err != nil {
		return agentlink.MeterReport{}, false
	}
	return agentlink.MeterReport{Account: ref, Def: def, Reading: reading}, true
}
