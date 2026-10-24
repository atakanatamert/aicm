#!/bin/sh
# SPDX-License-Identifier: SUL-1.0
#
# Checks that every hand-written Go file starts with the project's SPDX header.
set -eu

fail=0
want='// SPDX-License''-Identifier: SUL-1.0'
for f in $(git ls-files --cached --others --exclude-standard '*.go'); do
	# Generated files (sqlc, oapi) get their license from REUSE.toml.
	if grep -Eq '^// Code generated .* DO NOT EDIT\.$' "$f"; then
		continue
	fi
	if [ "$(head -n 1 "$f")" != "$want" ]; then
		echo "$f: first line must be: $want"
		fail=1
	fi
done

exit $fail
