// SPDX-License-Identifier: Apache-2.0

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

func shellCommand(command string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		return exec.CommandContext(context.Background(), "cmd", "/C", command) //nolint:gosec // the user's configured statusline command
	}
	return exec.CommandContext(context.Background(), "sh", "-c", command) //nolint:gosec // the user's configured statusline command
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
	wrapper := shellQuote(abs) + " statusline"
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
		if isWrapperCommand(existingCommand) {
			continue
		}

		if existed {
			backup := path + ".aicm-backup-" + strconv.FormatInt(time.Now().Unix(), 10)
			if err := os.WriteFile(backup, data, 0o600); err != nil { //nolint:gosec // backup beside the harness settings file
				return fmt.Errorf("backing up %s: %w", path, err)
			}
		}
		if !haveOriginal && current != nil {
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
		changed = true
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
	command = strings.TrimSpace(command)
	if !strings.HasSuffix(command, " statusline") {
		return false
	}
	exe := strings.Trim(strings.TrimSuffix(command, " statusline"), `"'`)
	base := filepath.Base(exe)
	return base == version.AgentBinary || base == version.AgentBinary+".exe"
}

// shellQuote quotes an executable path for the shell Claude Code uses.
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
