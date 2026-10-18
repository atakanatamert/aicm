#!/bin/sh
# SPDX-License-Identifier: Apache-2.0
# Synthetic codex app-server: canned JSON-RPC responses for tests.
while IFS= read -r line; do
	case "$line" in
	*'"method":"initialize"'*)
		printf '%s\n' '{"jsonrpc":"2.0","id":1,"result":{"userAgent":"fake-codex","codexHome":"/tmp","platformFamily":"unix","platformOs":"macos"}}'
		;;
	*'"method":"account/rateLimits/read"'*)
		printf '%s\n' '{"jsonrpc":"2.0","id":2,"result":{"rateLimits":{"primary":{"usedPercent":22,"windowDurationMins":300,"resetsAt":1772636400},"secondary":{"usedPercent":55,"windowDurationMins":10080,"resetsAt":1773223200},"credits":{"hasCredits":true,"unlimited":false,"balance":"7.25"},"planType":"plus"},"accountId":"acct_abc123"}}'
		;;
	esac
done
