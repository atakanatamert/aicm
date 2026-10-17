// SPDX-License-Identifier: AGPL-3.0-or-later

// Package migrations embeds the goose SQL migrations into the hub binary.
package migrations

import "embed"

// FS holds every *.sql migration, applied in file-name order.
//
//go:embed *.sql
var FS embed.FS
