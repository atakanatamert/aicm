// SPDX-License-Identifier: SUL-1.0

// Package migrations embeds the goose SQL migrations into the hub binary.
package migrations

import "embed"

// FS holds every *.sql migration, applied in file-name order.
//
//go:embed *.sql
var FS embed.FS
