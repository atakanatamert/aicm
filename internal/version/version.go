// SPDX-License-Identifier: SUL-1.0

// Package version holds the product name and build version shared by the hub
// and the agent. The product name lives only here and in go.mod, so renaming
// the project is one search-and-replace.
package version

// Name is the placeholder product name. Binaries are Name and Name+"-agent".
const Name = "aicm"

// Version is set at build time with -ldflags "-X github.com/atakanatamert/aicm/internal/version.Version=v1.2.3".
var Version = "dev"

// AgentBinary is the agent's executable name.
const AgentBinary = Name + "-agent"
