# 30 – Release pipeline

**Writer:** Opus
**Depends on:** 19
**License of touched packages:** n/a (build config)

## Files

`.goreleaser.yaml`, `deploy/release/hub.Dockerfile`, `deploy/release/agent.Dockerfile`, `.github/workflows/release.yml`, cross-compile step in `.github/workflows/ci.yml`.

## Done when

- [x] `goreleaser release --snapshot --clean --skip=docker,publish,sign` builds hub (linux amd64/arm64) and agent (darwin, linux, windows × amd64/arm64) archives named `aicm-agent_<version>_<os>_<arch>`, `checksums.txt`, SBOMs and the Homebrew formula.
- [x] Snapshot release installs end to end: the hub-served `install.sh` installed the snapshot archive, the LaunchAgent ran, the machine came online and pushed data; uninstall was clean.
- [ ] Docker images (GHCR) are built only in the release workflow: this machine has no `docker buildx`.

## Outcome (filled in by Opus)

Merged in `37af4c7`. The snapshot exposed a Windows build break (inode via `syscall.Stat_t`), fixed with one build-tagged `harness.Inode` helper; CI now cross-compiles every target. GoReleaser marks `brews` deprecated in favour of casks, but `brew services` needs a formula, so the formula stays. Needs before the first tag: a GitHub remote, a `homebrew-tap` repo and a `HOMEBREW_TAP_TOKEN` secret.
