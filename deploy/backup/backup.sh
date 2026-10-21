#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Nightly backup sidecar for the hub: a pg_dump of the database and a copy of
# the master key, kept side by side in /backups. Without the master key the
# stored API keys in a dump cannot be decrypted, so restore both together.
# Keeps the newest AICM_BACKUP_KEEP dumps (default 14). `backup.sh now` writes
# one backup and exits.
set -eu

: "${PGPASSWORD:?PGPASSWORD must be set}"
export PGHOST="${PGHOST:-db}" PGUSER="${PGUSER:-aicm}" PGDATABASE="${PGDATABASE:-aicm}"
keep="${AICM_BACKUP_KEEP:-14}"

backup() {
	stamp=$(date -u +%Y%m%dT%H%M%SZ)
	tmp="/backups/.aicm-$stamp.dump.partial"
	pg_dump --format=custom --no-owner --file="$tmp"
	mv "$tmp" "/backups/aicm-$stamp.dump"
	if [ -f /hub-data/master.key ]; then
		cp /hub-data/master.key /backups/master.key.tmp
		chmod 600 /backups/master.key.tmp
		mv /backups/master.key.tmp /backups/master.key
	fi
	echo "backup written: /backups/aicm-$stamp.dump"
	ls -1t /backups/aicm-*.dump 2>/dev/null | tail -n "+$((keep + 1))" | while read -r old; do
		rm -f "$old"
	done
}

umask 077
if [ "${1:-}" = "now" ]; then
	backup
	exit 0
fi

while true; do
	backup || echo "backup failed" >&2
	sleep 86400
done
