# Compatibility and support

## Supported platform

ThreadBear v1 supports macOS 12 Monterey or newer on Apple silicon (`darwin/arm64`) and Intel (`darwin/amd64`), with Codex Desktop and compatible local index/App Server surfaces validated by `~/.local/bin/threadbear self-test`.

Release binaries are standalone pure-Go executables built with `CGO_ENABLED=0`. End users do not need Go, cgo, Python, Node.js, or another runtime. Windows and Linux are not v1 targets.

## Unsigned distribution

v1 binaries are not Developer ID signed or notarized. The supported installer verifies the published SHA-256 checksum, candidate executable health, and embedded version before installation. See [INSTALL.md](../INSTALL.md#unsigned-binary-and-gatekeeper) for the narrow Gatekeeper recovery path; do not disable Gatekeeper globally.

## Codex surfaces

ThreadBear depends on compatibility-detectable local Codex capabilities for complete read-only inventory; persistent title, archive, and unarchive operations; fresh non-persisted classifier sessions with model/effort overrides; and a zero-model notice insertion into the control task.

`~/.local/bin/threadbear self-test` checks the supported platform/architecture, binary, pinned Codex executable, local Codex home, state/config integrity, managed files, and LaunchAgent health. A failed installed AGENTS.md or skill check identifies whether the surface is stale, unsafe, or inaccessible and gives a condition-specific remedy without exposing its path. The updater candidate self-test is read-only, does not check installed managed files, and does not mutate titles or archives because update refreshes those files only after candidate validation. Install internal staged-candidate verification still checks managed surfaces after install stages them. Symlinks to user-owned regular managed files are followed without replacing the link; malformed, dangling, non-file, and foreign-owned targets fail conservatively. If a required surface changes or config/state uses an unsupported schema, ThreadBear fails instead of guessing.

## Sidebar expectations

A successful persistent title update does not guarantee immediate repaint in an already-open Codex Desktop sidebar. ThreadBear verifies persistent task state, never edits `.codex-global-state.json`, never uses private cache invalidation, and never clicks through the UI. The sidebar may converge after later task activity or a supported app refresh.

## LaunchAgent timing and environment

The `org.litman.threadbear` user LaunchAgent uses `StartInterval`, `ProcessType=Background`, and `KeepAlive=false`. The configured interval is approximate: launches missed during sleep or while a prior run is active are not replayed.

The job receives explicit `HOME`, `CODEX_HOME`, sanitized `PATH`, and `LC_ALL=C` values. ThreadBear does not inherit or log the caller's full environment.

## Network behavior

Ordinary inventory and deterministic classification are local. At the start of each non-dry-run heartbeat, under the existing shared lock, ThreadBear compares the enabled AGENTS.md block and always-managed skill with its embedded content and repairs drift before inventory or classification. A clean comparison performs no write, starts no model, and produces no output. If repair fails, the heartbeat reports a stable diagnostic while unchanged handling, update notices or announcements, and checkpoint commit continue without a control-task failure post. Network/model activity occurs only for configured-classifier calls on unresolved changed tasks, a release metadata check at the configured mode's cadence, or an install/update. Automatic and manual updates use the same verified updater and managed-surface refresh pipeline.

## Troubleshooting order

Use read-only commands first:

```sh
~/.local/bin/threadbear version
~/.local/bin/threadbear status
~/.local/bin/threadbear self-test
~/.local/bin/threadbear heartbeat --dry-run
~/.local/bin/threadbear inspect TASK_ID
```

`~/.local/bin/threadbear status` reports installed version, LaunchAgent health, last completed heartbeat, control-task identity, preferences, pending retries, and last update check without invoking a model. `~/.local/bin/threadbear inspect TASK_ID` reports the task's configured and applied token-display positions, managed figure, and token-usage availability without exposing rollout paths or task prose.

If scheduling is intentionally paused, run `~/.local/bin/threadbear enable`. Inspect the job with `launchctl print "gui/$(id -u)/org.litman.threadbear"`. For a legacy migration failure after ThreadWatch was stopped, follow the exact recovery command printed by the installer; do not run both automation jobs concurrently.

## Updates, downgrades, and removal

Manual `~/.local/bin/threadbear update` and auto-update share one pipeline: checksum, version, updater candidate self-test, and changed managed-mutation prevalidation all pass before the binary's single atomic rename; managed previews are emitted before replacement. A successful update then refreshes managed AGENTS.md content when enabled and the always-managed skill from candidate-embedded assets and verifies them. A same-version manual update reconciles stale surfaces from the current binary embedded assets and is silent when everything is current. Binary and managed-file replacements are individually safe but are not cross-file atomic. If managed refresh fails after replacement, the new binary remains installed, partial managed writes are rolled back best-effort, and ThreadBear returns an explicit error; a subsequent heartbeat from the new binary reconciles residue. No binary rollback file or local release history is retained. Update does not refresh the LaunchAgent plist.

With auto-update enabled, release checks run no more than once every 30 minutes and newer releases use that shared pipeline. The next heartbeat posts one version-change announcement with up to three embedded changelog bullets and an opt-out hint; manual and installer-driven updates use the same announcement mechanism. With auto-update disabled, checks run no more than once every 24 hours and retain the fixed once-per-version ready notice instead of applying the update.

`~/.local/bin/threadbear update --version N.N.N` selects an exact release and is also the explicit downgrade mechanism. A downgrade is installed binary-only with an explicit warning only when the older candidate returns an ErrorResult with `operation=dispatch` and `error_code=unknown_command` for managed-asset export. Malformed JSON, empty assets, generic execution failures, and other candidate errors fail the downgrade. Newer AGENTS.md/skill content may remain until update or configure under a newer binary. ThreadBear has no local release history.

`~/.local/bin/threadbear uninstall` removes managed executable/scheduler/guidance resources after preview and confirmation. Control-task archival and persistent-state deletion are separate choices, both defaulting to retention. Existing task titles and archives are left alone.
