// SPDX-License-Identifier: Apache-2.0

// Command aicm-agent runs on each developer machine. It reads local harness
// data and pushes usage, discovered accounts and quota meters to the hub over
// an outbound WebSocket. Subscription tokens never leave the machine.
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/atakanatamert/aicm/internal/version"
)

const usage = `Usage: ` + version.AgentBinary + ` <command> [flags]

Commands:
  version   print the version
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
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n%s", args[0], usage)
		return 2
	}
	return 0
}
