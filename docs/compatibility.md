# Compatibility and support

## Supported platform

ThreadBear v1 supports macOS 12 Monterey or newer on Apple silicon (`darwin/arm64`) and Intel (`darwin/amd64`), with Codex Desktop and compatible local index/App Server surfaces validated by `~/.local/bin/threadbear self-test`.

Release binaries are standalone pure-Go executables built with `CGO_ENABLED=0`. End users do not need Go, cgo, Python, Node.js, or another runtime. Windows and Linux are not v1 targets.

## Unsigned distribution

v1 binaries are not Developer ID signed or notarized. The supported installer verifies the published SHA-256 checksum, candidate executable health, and embedded version before installation. See [INSTALL.md](../INSTALL.md#unsigned-binary-and-gatekeeper) for the narrow Gatekeeper recovery path; do not disable Gatekeeper globally.

## Codex surfaces

ThreadBear depends on compatibility-detectable local Codex capabilities for complete read-only inventory; persistent title, archive, and unarchive operations; fresh non-persisted classifier sessions with model/effort overrides; and a zero-model notice insertion into the control task.

`~/.local/bin/threadbear self-test` checks the supported platform/architecture, binary, pinned Codex executable, local Codex home, state/config integrity, managed files, and LaunchAgent health. Candidate self-test is read-only and does not mutate titles or archives. If a required surface changes or config/state uses an unsupported schema, ThreadBear fails conservatively instead of guessing.

## Sidebar expectations

A successful persistent title update does not guarantee immediate repaint in an already-open Codex Desktop sidebar. ThreadBear verifies persistent task state, never edits `.codex-global-state.json`, never uses private cache invalidation, and never clicks through the UI. The sidebar may converge after later task activity or a supported app refresh.

## LaunchAgent timing and environment

The `org.litman.threadbear` user LaunchAgent uses `StartInterval`, `ProcessType=Background`, and `KeepAlive=false`. The configured interval is approximate: launches missed during sleep or while a prior run is active are not replayed.

The job receives explicit `HOME`, `CODEX_HOME`, sanitized `PATH`, and `LC_ALL=C` values. ThreadBear does not inherit or log the caller's full environment.

## Network behavior

Ordinary inventory and deterministic classification are local. Network/model activity occurs only for configured-classifier calls on unresolved changed tasks, a release metadata check when due, or an explicit install/update. An unchanged heartbeat with no due update check starts no App Server or classifier and writes no output.

## Troubleshooting order

Use read-only commands first:

```sh
~/.local/bin/threadbear version
~/.local/bin/threadbear status
~/.local/bin/threadbear self-test
~/.local/bin/threadbear heartbeat --dry-run
~/.local/bin/threadbear inspect TASK_ID
```

`~/.local/bin/threadbear status` reports installed version, LaunchAgent health, last completed heartbeat, control-task identity, preferences, pending retries, and last update check without invoking a model.

If scheduling is intentionally paused, run `~/.local/bin/threadbear enable`. Inspect the job with `launchctl print "gui/$(id -u)/org.litman.threadbear"`. For a legacy migration failure after ThreadWatch was stopped, follow the exact recovery command printed by the installer; do not run both automation jobs concurrently.

## Updates, downgrades, and removal

`~/.local/bin/threadbear update` installs only after checksum, version, and candidate self-test validation. `~/.local/bin/threadbear update --version N.N.N` selects an exact release and is also the explicit downgrade mechanism. ThreadBear has no automatic rollback or local release history.

`~/.local/bin/threadbear uninstall` removes managed executable/scheduler/guidance resources after preview and confirmation. Control-task archival and persistent-state deletion are separate choices, both defaulting to retention. Existing task titles and archives are left alone.
