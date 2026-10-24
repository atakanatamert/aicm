#!/bin/sh
# SPDX-License-Identifier: SUL-1.0
#
# Installs the aicm agent for the current user: downloads the release,
# verifies its checksum, enrolls the machine and installs a user-level
# service that starts at login and restarts on failure.
set -eu

REPO="atakanatamert/aicm"
BINARY="aicm-agent"
BASE="${AICM_AGENT_DOWNLOAD_BASE:-https://github.com/$REPO/releases}"

usage() {
	cat <<EOF
Usage: install.sh --hub URL --org ID --key KEY --token TOKEN [--version vX.Y.Z] [--no-statusline]
       install.sh --uninstall

The hub command already fills in the four enrollment values.
EOF
}

die() {
	echo "install: $*" >&2
	exit 1
}

warn() {
	echo "install: $*" >&2
}

fetch() {
	if command -v curl >/dev/null 2>&1; then
		curl -fsSL "$1" -o "$2"
	elif command -v wget >/dev/null 2>&1; then
		wget -qO "$2" "$1"
	else
		warn "curl or wget is required"
		return 1
	fi
}

resolve_latest() {
	if command -v curl >/dev/null 2>&1; then
		effective=$(curl -fsSLI -o /dev/null -w '%{url_effective}' "$BASE/latest") || effective=""
		tag=${effective##*/}
	elif command -v wget >/dev/null 2>&1; then
		tag=$(wget -qO- "https://api.github.com/repos/$REPO/releases/latest" 2>/dev/null |
			sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')
	else
		warn "curl or wget is required"
		return 1
	fi
	case "$tag" in
	"" | latest | releases) newest_release ;;
	*) printf '%s\n' "$tag" ;;
	esac
}

# newest_release prints the most recently published non-draft tag including
# pre-releases, for repos that have no stable release yet. The API's list order
# is not publication order, so pick the greatest published_at (ISO 8601 UTC
# strings sort lexicographically). Splitting on , { } puts each key on its own
# line; tag_name and draft precede published_at within a release object.
newest_release() {
	tag=$(fetch "https://api.github.com/repos/$REPO/releases?per_page=100" - 2>/dev/null |
		tr ',{}' '\n\n\n' |
		awk -F'"' '
			$2 == "tag_name" { t = $4; d = 0 }
			$2 == "draft" && $3 ~ /true/ { d = 1 }
			$2 == "published_at" && $4 != "" && !d && $4 > best { best = $4; tag = t }
			END { if (tag != "") print tag }')
	[ -n "$tag" ] || return 1
	printf '%s\n' "$tag"
}

xml_escape() {
	printf '%s' "$1" | sed -e 's/&/\&amp;/g' -e 's/</\&lt;/g' -e 's/>/\&gt;/g'
}

install_service_macos() {
	plist_dir="$HOME/Library/LaunchAgents"
	log_dir="$HOME/Library/Logs"
	plist="$plist_dir/dev.aicm.agent.plist"
	log_file="$log_dir/$BINARY.log"
	mkdir -p "$plist_dir" "$log_dir"

	bin_xml=$(xml_escape "$BIN_DIR/$BINARY")
	log_xml=$(xml_escape "$log_file")
	path_xml=$(xml_escape "$SERVICE_PATH")
	cat >"$plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>dev.aicm.agent</string>
	<key>ProgramArguments</key>
	<array>
		<string>$bin_xml</string>
		<string>run</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<true/>
	<key>StandardOutPath</key>
	<string>$log_xml</string>
	<key>StandardErrorPath</key>
	<string>$log_xml</string>
	<key>EnvironmentVariables</key>
	<dict>
		<key>PATH</key>
		<string>$path_xml</string>
	</dict>
</dict>
</plist>
EOF

	launchctl bootout "gui/$(id -u)" "$plist" 2>/dev/null || true
	launchctl bootstrap "gui/$(id -u)" "$plist" || die "launchctl could not load $plist"
	LOG_HINT="tail -f $log_file"
}

install_service_linux() {
	unit_dir="$HOME/.config/systemd/user"
	unit="$unit_dir/$BINARY.service"
	mkdir -p "$unit_dir"

	cat >"$unit" <<EOF
[Unit]
Description=aicm agent
After=network-online.target

[Service]
ExecStart="$BIN_DIR/$BINARY" run
Environment="PATH=$(printf '%s' "$SERVICE_PATH" | sed -e 's/%/%%/g' -e 's/"/\\"/g')"
Restart=always
RestartSec=10

[Install]
WantedBy=default.target
EOF

	if command -v systemctl >/dev/null 2>&1 && systemctl --user daemon-reload 2>/dev/null; then
		if systemctl --user enable --now "$BINARY.service"; then
			echo "hint: on headless machines run: loginctl enable-linger $USER"
			LOG_HINT="journalctl --user -u $BINARY"
		else
			warn "systemd could not start the agent; run it manually with: $BIN_DIR/$BINARY run"
			LOG_HINT="the terminal where you run it"
		fi
	else
		warn "systemd --user is unavailable; run the agent manually with: $BIN_DIR/$BINARY run"
		LOG_HINT="the terminal where you run it"
	fi
}

uninstall() {
	if [ "$OS" = "Darwin" ]; then
		launchctl bootout "gui/$(id -u)" "$plist" 2>/dev/null || true
		rm -f "$plist"
	elif [ "$OS" = "Linux" ]; then
		if command -v systemctl >/dev/null 2>&1; then
			systemctl --user disable --now "$BINARY.service" 2>/dev/null || true
			systemctl --user daemon-reload 2>/dev/null || true
		fi
		rm -f "$unit"
	fi
	if [ -x "$BIN_DIR/$BINARY" ]; then
		"$BIN_DIR/$BINARY" statusline uninstall 2>/dev/null || true
	fi
	rm -f "$BIN_DIR/$BINARY"
	echo "Uninstalled $BINARY. Data kept at $DATA_DIR"
}

HUB=""
ORG=""
KEY=""
TOKEN=""
VERSION=""
UNINSTALL=0
NO_STATUSLINE=0

while [ $# -gt 0 ]; do
	case "$1" in
	--hub)
		[ $# -ge 2 ] || die "--hub needs a value"
		HUB=$2
		shift 2
		;;
	--org)
		[ $# -ge 2 ] || die "--org needs a value"
		ORG=$2
		shift 2
		;;
	--key)
		[ $# -ge 2 ] || die "--key needs a value"
		KEY=$2
		shift 2
		;;
	--token)
		[ $# -ge 2 ] || die "--token needs a value"
		TOKEN=$2
		shift 2
		;;
	--version)
		[ $# -ge 2 ] || die "--version needs a value"
		VERSION=$2
		shift 2
		;;
	--uninstall)
		UNINSTALL=1
		shift
		;;
	--no-statusline)
		NO_STATUSLINE=1
		shift
		;;
	-h | --help)
		usage
		exit 0
		;;
	*)
		die "unknown argument: $1"
		;;
	esac
done

if [ "$(id -u)" -eq 0 ]; then
	die "do not run as root: the agent must run as the user whose ~/.claude and ~/.codex it reads"
fi

case "$(uname -s)" in
Darwin) OS="Darwin" ;;
Linux) OS="Linux" ;;
*) die "unsupported operating system: $(uname -s)" ;;
esac

BIN_DIR="$HOME/.local/bin"
# The service sees the installing shell's PATH, so the agent finds harness
# binaries (codex for its app-server) installed in user directories.
SERVICE_PATH="$BIN_DIR:${PATH:-/usr/local/bin:/usr/bin:/bin}"
if [ "$OS" = "Darwin" ]; then
	DATA_DIR="${AICM_AGENT_DATA:-$HOME/Library/Application Support/$BINARY}"
	plist="$HOME/Library/LaunchAgents/dev.aicm.agent.plist"
else
	DATA_DIR="${AICM_AGENT_DATA:-${XDG_CONFIG_HOME:-$HOME/.config}/$BINARY}"
	unit="$HOME/.config/systemd/user/$BINARY.service"
fi

if [ "$UNINSTALL" -eq 1 ]; then
	uninstall
	exit 0
fi

[ -n "$HUB" ] || die "--hub is required"
[ -n "$ORG" ] || die "--org is required"
[ -n "$KEY" ] || die "--key is required"
[ -n "$TOKEN" ] || die "--token is required"

case "$(uname -m)" in
x86_64 | amd64) ARCH="amd64" ;;
arm64 | aarch64) ARCH="arm64" ;;
*) die "unsupported architecture: $(uname -m)" ;;
esac

case "$OS" in
Darwin) os="darwin" ;;
Linux) os="linux" ;;
*) die "unsupported operating system: $OS" ;;
esac

if [ -n "$VERSION" ]; then
	case "$VERSION" in
	v*) TAG=$VERSION ;;
	*) TAG="v$VERSION" ;;
	esac
	release_dir="$BASE/download/$TAG"
else
	TAG=$(resolve_latest) || die "could not resolve the latest release under $BASE"
	release_dir="$BASE/download/$TAG"
fi
asset_version=${TAG#v}
asset="${BINARY}_${asset_version}_${os}_${ARCH}.tar.gz"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

echo "Downloading $asset"
fetch "$release_dir/$asset" "$tmp/$asset" || die "could not download $release_dir/$asset"
fetch "$release_dir/checksums.txt" "$tmp/checksums.txt" || die "could not download $release_dir/checksums.txt"

if command -v shasum >/dev/null 2>&1; then
	actual=$(shasum -a 256 "$tmp/$asset")
elif command -v sha256sum >/dev/null 2>&1; then
	actual=$(sha256sum "$tmp/$asset")
else
	die "shasum or sha256sum is required"
fi
actual=${actual%% *}
want=$(awk -v name="$asset" '{ n = $2; sub(/^\*/, "", n); if (n == name) { print $1; exit } }' "$tmp/checksums.txt")
[ -n "$want" ] || die "checksums.txt has no entry for $asset"
actual=$(printf '%s' "$actual" | tr 'A-F' 'a-f')
want=$(printf '%s' "$want" | tr 'A-F' 'a-f')
[ "$actual" = "$want" ] || die "checksum mismatch for $asset (expected $want, got $actual)"

tar -xzf "$tmp/$asset" -C "$tmp"
[ -f "$tmp/$BINARY" ] || die "$asset does not contain $BINARY"

mkdir -p "$BIN_DIR"
cp "$tmp/$BINARY" "$BIN_DIR/$BINARY"
chmod 755 "$BIN_DIR/$BINARY"

case ":$PATH:" in
*":$BIN_DIR:"*) ;;
*) warn "$BIN_DIR is not on PATH; add it to your shell profile" ;;
esac

echo "Enrolling with $HUB"
"$BIN_DIR/$BINARY" enroll --hub "$HUB" --org "$ORG" --key "$KEY" --token "$TOKEN"

# Claude Code shows its 5-hour and weekly limits only to its statusline
# command; the wrapper records them and then runs the previous statusline.
if [ "$NO_STATUSLINE" -eq 0 ] && [ -d "${CLAUDE_CONFIG_DIR:-$HOME/.claude}" ]; then
	if "$BIN_DIR/$BINARY" statusline install; then
		echo "Claude Code limits: statusline wrapped (undo: $BINARY statusline uninstall)"
	else
		warn "could not wrap the Claude Code statusline; Claude limits stay empty until: $BINARY statusline install"
	fi
fi

if [ "$OS" = "Darwin" ]; then
	install_service_macos
else
	install_service_linux
fi

echo "Installed $BINARY and started the user service."
echo "Logs: $LOG_HINT"
