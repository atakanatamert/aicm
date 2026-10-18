#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Checks that every Go file carries the SPDX header for its directory and that
# the Apache-2.0 agent binary links no AGPL package.
set -eu

module=$(go list -m)
apache_re='^(cmd/aicm-agent|internal/(agentlink|mcpserver|harness|meter|version|apiclient|agent))(/|$)'
fail=0
spdx='// SPDX-License''-Identifier:'
for f in $(git ls-files --cached --others --exclude-standard '*.go'); do
	# Generated files (sqlc, oapi) get their license from REUSE.toml.
	if grep -Eq '^// Code generated .* DO NOT EDIT\.$' "$f"; then
		continue
	fi
	dir=$(dirname "$f")
	if echo "$dir" | grep -Eq "$apache_re"; then
		want="$spdx Apache-2.0"
	else
		want="$spdx AGPL-3.0-or-later"
	fi
	if [ "$(head -n 1 "$f")" != "$want" ]; then
		echo "$f: first line must be: $want"
		fail=1
	fi
done

for pkg in $(go list -deps "./cmd/aicm-agent" | grep "^$module/" || true); do
	rel=${pkg#"$module"/}
	if ! echo "$rel" | grep -Eq "$apache_re"; then
		echo "cmd/aicm-agent links AGPL package $rel; agent dependencies must be Apache-2.0"
		fail=1
	fi
done

exit $fail
