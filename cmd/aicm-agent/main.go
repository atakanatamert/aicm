// SPDX-License-Identifier: Apache-2.0

// Command aicm-agent runs on each developer machine. It reads local harness
// data and pushes usage, discovered accounts and quota meters to the hub over
// an outbound WebSocket. Subscription tokens never leave the machine.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/atakanatamert/aicm/internal/agent"
	"github.com/atakanatamert/aicm/internal/harness"
	"github.com/atakanatamert/aicm/internal/harness/claudecode"
	"github.com/atakanatamert/aicm/internal/harness/codex"
	"github.com/atakanatamert/aicm/internal/harness/omp"
	"github.com/atakanatamert/aicm/internal/mcpserver"
	"github.com/atakanatamert/aicm/internal/version"
)

const usage = `Usage: ` + version.AgentBinary + ` <command> [flags]

Commands:
  enroll                enroll this machine with a hub
  run                   scan harnesses and keep the hub link open
  mcp                   serve the MCP tools over stdio
  update                update the agent to the latest release
  statusline            Claude Code statusline wrapper (called by Claude Code)
  statusline install    route Claude Code's statusline through the wrapper
  statusline uninstall  restore the original statusline
  version               print the version

Flags for enroll and run:
  --hub URL      hub base URL
  --org ID       organization id
  --key BASE64   hub public key
  --token TOKEN  enrollment token

Flags for mcp:
  --hub URL          hub base URL (default AICM_HUB_URL or the enrolled config)
  --token-file FILE  file holding the personal API token (mode 0600)
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	switch args[0] {
	case "version":
		fmt.Fprintln(stdout, version.AgentBinary, version.Version)
	case "-h", "--help", "help":
		fmt.Fprint(stdout, usage)
	case "enroll":
		return enroll(args[1:], stdout, stderr)
	case "run":
		return runAgent(args[1:], stdout, stderr)
	case "mcp":
		return serveMCP(args[1:], stderr)
	case "update":
		return update(stdout, stderr)
	case "statusline":
		return statusline(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n%s", args[0], usage)
		return 2
	}
	return 0
}

// enrollFlags are the four values in the install one-liners.
type enrollFlags struct {
	hub, org, key, token string
}

// parseEnrollFlags parses the shared enroll flags.
func parseEnrollFlags(name string, args []string, stderr io.Writer) (enrollFlags, bool) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	var f enrollFlags
	fs.StringVar(&f.hub, "hub", "", "hub base URL")
	fs.StringVar(&f.org, "org", "", "organization id")
	fs.StringVar(&f.key, "key", "", "hub public key, base64")
	fs.StringVar(&f.token, "token", "", "enrollment token")
	if err := fs.Parse(args); err != nil {
		return enrollFlags{}, false
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "unexpected arguments: %v\n", fs.Args())
		return enrollFlags{}, false
	}
	return f, true
}

// newAgent opens the agent state in the default data dir.
// harnesses returns every local harness importer, rooted at the user's home
// (AICM_AGENT_HOME in Docker) and the agent's data dir.
func harnesses(dataDir string) ([]harness.Harness, error) {
	home, err := agent.HomeDir()
	if err != nil {
		return nil, err
	}
	return []harness.Harness{
		claudecode.New(home, dataDir),
		codex.New(home),
		omp.New(home),
	}, nil
}

func newAgent(stderr io.Writer) (*agent.Agent, string, error) {
	dataDir, err := agent.DataDir()
	if err != nil {
		return nil, "", err
	}
	hs, err := harnesses(dataDir)
	if err != nil {
		return nil, "", err
	}
	log := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	a, err := agent.New(dataDir, hs, log)
	if err != nil {
		return nil, "", err
	}
	return a, dataDir, nil
}

// statusline is the Claude Code statusline wrapper and its installer. The
// wrapper must never fail Claude Code's statusline: errors only skip capture.
func statusline(args []string, stdout, stderr io.Writer) int {
	dataDir, err := agent.DataDir()
	if err != nil {
		if len(args) == 0 {
			return claudecode.RunStatusline(os.Stdin, stdout, "")
		}
		fmt.Fprintf(stderr, "statusline: %v\n", err)
		return 1
	}
	if len(args) == 0 {
		return claudecode.RunStatusline(os.Stdin, stdout, dataDir)
	}
	home, err := agent.HomeDir()
	if err != nil {
		fmt.Fprintf(stderr, "statusline: %v\n", err)
		return 1
	}
	h := claudecode.New(home, dataDir)
	switch args[0] {
	case "install":
		exe, err := os.Executable()
		if err == nil {
			exe, err = filepath.EvalSymlinks(exe)
		}
		if err == nil {
			err = h.Install(exe)
		}
		if err != nil {
			fmt.Fprintf(stderr, "statusline install: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, "Claude Code statusline now records rate limits; your own statusline still runs.")
	case "uninstall":
		if err := h.Uninstall(); err != nil {
			fmt.Fprintf(stderr, "statusline uninstall: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, "Claude Code statusline restored.")
	default:
		fmt.Fprintf(stderr, "unknown statusline command %q\n", args[0])
		return 2
	}
	return 0
}

// enroll pins the hub key and stores the machine token.
func enroll(args []string, stdout, stderr io.Writer) int {
	f, ok := parseEnrollFlags("enroll", args, stderr)
	if !ok {
		return 2
	}
	a, dataDir, err := newAgent(stderr)
	if err != nil {
		fmt.Fprintf(stderr, "enroll: %v\n", err)
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := a.Enroll(ctx, f.hub, f.org, f.key, f.token); err != nil {
		fmt.Fprintf(stderr, "enroll: %v\n", err)
		return 1
	}
	cfg, err := agent.Load(dataDir)
	if err != nil {
		fmt.Fprintf(stderr, "enroll: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "enrolled as machine %s\n", cfg.MachineID)
	return 0
}

// runAgent enrolls first when Docker-style flags are present and the machine
// has no token, then keeps the link open until interrupted.
func runAgent(args []string, _ io.Writer, stderr io.Writer) int {
	f, ok := parseEnrollFlags("run", args, stderr)
	if !ok {
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	a, _, err := newAgent(stderr)
	if err != nil {
		fmt.Fprintf(stderr, "run: %v\n", err)
		return 1
	}
	if !a.Enrolled() {
		if f.hub == "" || f.org == "" || f.key == "" || f.token == "" {
			fmt.Fprintln(stderr, "run: agent is not enrolled; run `aicm-agent enroll` or pass --hub, --org, --key and --token")
			return 1
		}
		if err := a.Enroll(ctx, f.hub, f.org, f.key, f.token); err != nil {
			fmt.Fprintf(stderr, "run: enroll: %v\n", err)
			return 1
		}
	}
	if err := a.Run(ctx); err != nil {
		fmt.Fprintf(stderr, "run: %v\n", err)
		return 1
	}
	return 0
}

// serveMCP speaks the MCP tool set over stdio until stdin closes. The token
// is a personal API token, so the hub applies its role and grants.
func serveMCP(args []string, stderr io.Writer) int {
	fs := flag.NewFlagSet("mcp", flag.ContinueOnError)
	fs.SetOutput(stderr)
	hubURL := fs.String("hub", "", "hub base URL (default AICM_HUB_URL or the enrolled config)")
	tokenFile := fs.String("token-file", "", "file holding the personal API token")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "unexpected arguments: %v\n", fs.Args())
		return 2
	}

	base := strings.TrimSpace(*hubURL)
	if base == "" {
		base = strings.TrimSpace(os.Getenv("AICM_HUB_URL"))
	}
	if base == "" {
		dataDir, err := agent.DataDir()
		if err != nil {
			fmt.Fprintf(stderr, "mcp: %v\n", err)
			return 1
		}
		cfg, err := agent.Load(dataDir)
		if err != nil {
			fmt.Fprintf(stderr, "mcp: hub URL: %v\n", err)
			return 1
		}
		base = cfg.HubURL
	}

	token := strings.TrimSpace(os.Getenv("AICM_TOKEN"))
	if *tokenFile != "" {
		tok, err := readTokenFile(*tokenFile)
		if err != nil {
			fmt.Fprintf(stderr, "mcp: %v\n", err)
			return 1
		}
		token = tok
	}
	if token == "" {
		fmt.Fprintln(stderr, "mcp: set AICM_TOKEN or pass --token-file")
		return 1
	}

	srv := mcpserver.New(base, func(context.Context) (string, error) { return token, nil })
	if err := srv.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		fmt.Fprintf(stderr, "mcp: %v\n", err)
		return 1
	}
	return 0
}

// readTokenFile reads the token once and refuses a world-readable file.
func readTokenFile(path string) (string, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("reading token file: %w", err)
	}
	if !fi.Mode().IsRegular() {
		return "", fmt.Errorf("%s is not a regular file", path)
	}
	if fi.Mode().Perm()&0o004 != 0 {
		return "", fmt.Errorf("%s is world-readable; chmod 600 it", path)
	}
	b, err := os.ReadFile(path) //nolint:gosec // path is an explicit CLI argument
	if err != nil {
		return "", fmt.Errorf("reading token file: %w", err)
	}
	token := strings.TrimSpace(string(b))
	if token == "" {
		return "", fmt.Errorf("%s is empty", path)
	}
	return token, nil
}

// update installs a newer release once.
func update(stdout, stderr io.Writer) int {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	latest, err := agent.Update(ctx, &http.Client{Timeout: 5 * time.Minute})
	if err != nil {
		fmt.Fprintf(stderr, "update: %v\n", err)
		return 1
	}
	if latest == "" {
		fmt.Fprintln(stdout, "already up to date")
		return 0
	}
	fmt.Fprintf(stdout, "updated to %s\n", latest)
	return 0
}
