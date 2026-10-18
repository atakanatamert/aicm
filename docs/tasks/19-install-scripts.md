# 19 – Install scripts: one pasted command per machine, user-level service

**Writer:** DeepSeek Flash
**Depends on:** 18, 18a
**License of touched packages:** AGPL-3.0-or-later (hub-served scripts)

## Goal

The commands `POST /api/enroll-tokens` returns work: `curl -fsSL {hub}/install.sh | sh -s -- --hub … --org … --key … --token …` on macOS and Linux, and the PowerShell one-liner on Windows, download the latest `aicm-agent` release, verify its checksum, enroll, and install a **user-level** service that starts at login and restarts on failure. The hub serves both scripts.

## Files to touch

- `deploy/install/install.sh` (new)
- `deploy/install/install.ps1` (new)
- `deploy/install/embed.go` (new): package `install`, embeds both scripts, `Handler() http.Handler` serving `GET /install.sh` (`text/x-shellscript`) and `GET /install.ps1` (`text/plain`)
- `internal/httpapi/httpapi.go` (edit: mount the two paths on the base mux when `Deps.Install` is set; they are public)
- `cmd/aicm/main.go` (edit: pass `install.Handler()`)

Touch nothing else.

## Read first

- `CLAUDE.md`, `docs/PLAN.md` "Collector agent"
- `internal/machine/machine.go` (`Commands`: the exact flags passed)
- `internal/agent/update.go` (release asset naming and checksums format: reuse exactly), `cmd/aicm-agent/main.go` (`enroll`, `run`, `update`)
- `internal/version/version.go` (binary name)

## Contract

### `install.sh` (POSIX sh, `set -eu`, shellcheck-clean)

1. Parse `--hub --org --key --token`, optional `--version vX.Y.Z` (default latest) and `--uninstall`. Refuse to run as root (`id -u` = 0) with a message: the agent must run as the user whose `~/.claude` and `~/.codex` it reads.
2. Detect OS (`Darwin`/`Linux`) and arch (`x86_64`→`amd64`, `arm64`/`aarch64`→`arm64`); anything else → clear error.
3. Download from `${AICM_AGENT_DOWNLOAD_BASE:-https://github.com/atakanatamert/aicm/releases}` (`/latest/download/` or `/download/<version>/`) the asset `aicm-agent_<version>_<os>_<arch>.tar.gz` and `checksums.txt` with `curl -fsSL` (fallback `wget -qO-`). For "latest", resolve the version from the redirect of `/latest` (or the GitHub API) because the asset name contains it. Verify SHA-256 with `shasum -a 256` or `sha256sum`; mismatch → abort.
4. Install the binary to `$HOME/.local/bin/aicm-agent` (create the dir; `chmod 755`). Warn if that dir is not on `PATH`.
5. Run `aicm-agent enroll --hub … --org … --key … --token …`.
6. Service:
   - macOS: `~/Library/LaunchAgents/dev.aicm.agent.plist` with `ProgramArguments` [binary, `run`], `RunAtLoad` true, `KeepAlive` true, `StandardOutPath`/`StandardErrorPath` `~/Library/Logs/aicm-agent.log`; `launchctl bootout` an old one, then `launchctl bootstrap gui/$(id -u) <plist>`.
   - Linux: `~/.config/systemd/user/aicm-agent.service` (`ExecStart=<binary> run`, `Restart=always`, `RestartSec=10`, `WantedBy=default.target`); `systemctl --user daemon-reload && systemctl --user enable --now aicm-agent`. If `systemctl --user` is unavailable, print how to run `aicm-agent run` manually. Print the `loginctl enable-linger $USER` hint for headless machines.
7. Print a short success message with the log location.
8. `--uninstall`: stop and remove the service, remove the binary; keep the data dir and print its path.

The repo path and binary name each appear once as variables at the top.

### `install.ps1`

Parameters `-Hub -Org -Key -Token [-Version] [-Uninstall]`. Download the `windows_<arch>.zip` asset and `checksums.txt`, verify with `Get-FileHash -Algorithm SHA256`, install to `$env:LOCALAPPDATA\aicm-agent\aicm-agent.exe`, run `enroll`, register a scheduled task `aicm-agent` at logon for the current user (`Register-ScheduledTask` with `New-ScheduledTaskTrigger -AtLogOn -User $env:USERNAME`, `-RunLevel Limited`, restart on failure 3 times every minute) and start it. `-Uninstall` reverses it.

## Done when

- [ ] `shellcheck deploy/install/install.sh` clean (install shellcheck with brew if missing) and `sh -n` passes.
- [ ] Local end-to-end on this Mac without GitHub: build the agent (`go build -o /tmp/rel/aicm-agent ./cmd/aicm-agent`), package it as `/tmp/rel/download/v0.0.0-test/aicm-agent_0.0.0-test_darwin_arm64.tar.gz` with a matching `checksums.txt`, serve `/tmp/rel` with `python3 -m http.server`, start a local hub, create an enroll token, then run the hub-served script with `AICM_AGENT_DOWNLOAD_BASE=http://127.0.0.1:<port>` and `--version v0.0.0-test`: the agent installs, enrolls, the LaunchAgent is loaded, and `GET /api/machines` shows the machine online. Then `--uninstall` removes it cleanly. Report each step's output.
- [ ] `install.ps1`: no Windows here; check it with `pwsh -NoProfile -Command "[ScriptBlock]::Create((Get-Content -Raw deploy/install/install.ps1)) | Out-Null"` if `pwsh` is installed, else say so.
- [ ] `curl -fsS http://localhost:<port>/install.sh` returns the script without logging in.
- [ ] `go build ./... && go vet ./... && ~/go/bin/golangci-lint run` clean; `./scripts/check-licenses.sh` passes; `reuse lint` passes.

## Outcome (filled in by Opus)
