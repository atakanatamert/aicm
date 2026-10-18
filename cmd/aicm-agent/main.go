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
	"syscall"
	"time"

	"github.com/atakanatamert/aicm/internal/agent"
	"github.com/atakanatamert/aicm/internal/version"
)

const usage = `Usage: ` + version.AgentBinary + ` <command> [flags]

Commands:
  enroll    enroll this machine with a hub
  run       scan harnesses and keep the hub link open
  update    update the agent to the latest release
  version   print the version

Flags for enroll and run:
  --hub URL      hub base URL
  --org ID       organization id
  --key BASE64   hub public key
  --token TOKEN  enrollment token
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
	case "update":
		return update(stdout, stderr)
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
func newAgent(stderr io.Writer) (*agent.Agent, string, error) {
	dataDir, err := agent.DataDir()
	if err != nil {
		return nil, "", err
	}
	log := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	a, err := agent.New(dataDir, nil, log)
	if err != nil {
		return nil, "", err
	}
	return a, dataDir, nil
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
