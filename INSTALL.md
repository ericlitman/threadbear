# Installing ThreadBear

This guide is written for an installing agent. Explain the plan to the user, use the supported command, preserve the preview, and do not assert success until the verification commands pass.

## Preconditions

Confirm macOS 12 or newer, `arm64` or `x86_64`/`amd64` hardware, Codex Desktop with a compatible local `codex` executable/App Server surface, and HTTPS access to `threadbear.sh` and GitHub Releases. v1 is not Developer ID signed or notarized. Installation is user-local and must not use `sudo`.

## Guided installation dialogue

### 1. Start the canonical bootstrap

```sh
curl -fsSL https://threadbear.sh/install.sh | sh
```

The pipe supplies the script, not the answers. ThreadBear opens `/dev/tty` for prompts so script input cannot accidentally accept them.

The bootstrap fetches the selected release manifest, detects `darwin/arm64` or `darwin/amd64`, and downloads that architecture’s absolute `url` and `sha256_url` exactly as published. It uses a private temporary directory, verifies the checksum, runs `self-test --candidate`, verifies the embedded version, and only then delegates to `threadbear install`.

To install an exact release:

```sh
curl -fsSL https://threadbear.sh/install.sh | sh -s -- --version 1.2.0
```

Versions must be exact `N.N.N` values without a leading `v`. A missing checksum, mismatch, wrong embedded version, or failed bootstrap candidate self-test stops before a working installed binary is replaced.

### 2. Collect every preference

On first install, ask the prompts in this order. Pressing Return accepts the shown value.

| Prompt | Default | Noninteractive flag |
|---|---:|---|
| Heartbeat interval in seconds | `300` | `--heartbeat-seconds 300` |
| Automatically archive completed inactive tasks | `yes` | `--archive=true` |
| Archive inactivity interval in days | `14` | `--archive-after-days 14` |
| Automatically maintain status and next-action titles | `yes` | `--rename=true` |
| Show output tokens in managed titles | `start` | `--token-display=start` |
| Install managed AGENTS.md instructions | `yes` | `--agents=true` |
| Classifier model | `gpt-5.6-luna` | `--classifier-model gpt-5.6-luna` |
| Classifier effort | `medium` | `--classifier-effort medium` |
| Classifier context budget in bytes | `250000` | `--classifier-context-budget-bytes 250000` |

Boolean install/configure flags use `=` forms. Use `--archive=false`, `--rename=false`, or `--agents=false`; do not pass a separate `false` argument.

### 3. Review the single final preview

Before mutation, ThreadBear prints the deterministic-first classifier rule; binary and state paths; exact managed AGENTS.md and skill mutations; LaunchAgent path and staged/self-tested/enabled sequence; persistent control-task effect; and every selected preference.

The confirmation prompt is exactly:

```text
Apply exactly this preview? (yes/no) [yes]:
```

Answer `yes` only when every line matches the user's intent. `no` cancels before the confirmed mutation phase. Install and `configure` default to `yes`, because the operator has just answered every question and a bare Return that silently discards the whole session is the worse failure. `uninstall` keeps the default at `no`.

### 4. Expected final effects

A successful first install creates or adopts exactly these managed resources:

- `~/.local/bin/threadbear` — standalone executable, mode `0700`;
- `~/.local/share/threadbear/` — private state/config directory, mode `0700`; `config.json` and `state.json` are atomic mode-`0600` files;
- `~/Library/LaunchAgents/org.litman.threadbear.plist` — user LaunchAgent, mode `0600`;
- `~/.local/share/threadbear/logs/heartbeat.stdout.log` and `heartbeat.stderr.log` — LaunchAgent output paths;
- `${CODEX_HOME:-~/.codex}/AGENTS.md` — one identifiable mode-`0600` managed file when enabled, preserving all bytes outside that block;
- `${CODEX_HOME:-~/.codex}/skills/threadbear/SKILL.md` — one mode-`0600` managed ThreadBear skill block;
- one persistent Codex control task titled `🧵🐻 ThreadBear 🐻🧵`, opened with a welcome notice that lists the chosen settings and explains how to change them from that chat.

The LaunchAgent uses `StartInterval`, `ProcessType=Background`, and `KeepAlive=false`. The interval is approximate: macOS does not replay launches missed during sleep or while a previous heartbeat is still running.

## Noninteractive installation

Noninteractive installation requires both `--noninteractive` and the explicit confirmation assertion `--confirm`:

```sh
curl -fsSL https://threadbear.sh/install.sh | sh -s -- \
  --noninteractive \
  --confirm \
  --heartbeat-seconds 300 \
  --archive=true \
  --archive-after-days 14 \
  --rename=true \
  --token-display=start \
  --agents=true \
  --classifier-model gpt-5.6-luna \
  --classifier-effort medium \
  --classifier-context-budget-bytes 250000
```

The preview is written before mutation. Omitting `--confirm` is an error; noninteractive mode never infers consent.

Exit behavior:

- `0`: installation completed successfully;
- `2`: invalid bootstrap or CLI arguments, including an invalid exact version;
- `1`: unsupported platform, download/checksum/self-test failure, cancelled confirmation, or installation/lifecycle failure.

## Verify the installation

```sh
~/.local/bin/threadbear version
~/.local/bin/threadbear self-test
~/.local/bin/threadbear status
launchctl print "gui/$(id -u)/org.litman.threadbear"
```

Read-only diagnosis is also available:

```sh
~/.local/bin/threadbear heartbeat --dry-run
~/.local/bin/threadbear inspect TASK_ID
~/.local/bin/threadbear status --json
```

These commands do not invoke a model or mutate task titles/archives. Installed self-test reports condition-specific remedies for stale, unsafe, or inaccessible managed AGENTS.md and skill surfaces. The updater candidate self-test does not inspect installed managed files because update has not refreshed them yet; install internal staged-candidate verification still checks the managed surfaces after staging them. A normal unchanged `~/.local/bin/threadbear heartbeat` compares managed content directly, performs no managed-file write when it is current, and emits zero bytes when no update notice is due.

For optional bare invocations in the current shell, add the standard user-local bin directory and verify it explicitly:

```sh
export PATH="$HOME/.local/bin:$PATH"
command -v threadbear
```

## Reconfigure later

`~/.local/bin/threadbear configure` changes every onboarding preference and previews effects before confirmation:

```sh
~/.local/bin/threadbear configure --heartbeat-seconds 600 --archive-after-days 30
~/.local/bin/threadbear configure --archive=false --rename=true --agents=false
~/.local/bin/threadbear configure --token-display=end
~/.local/bin/threadbear configure --classifier-model gpt-5.6-luna --classifier-effort medium --classifier-context-budget-bytes 250000
~/.local/bin/threadbear configure --dry-run --heartbeat-seconds 900
```

For automation, add `--noninteractive --confirm`. Reconfiguration previews and reconciles both the AGENTS.md block and always-managed skill, including a stale skill when preferences are unchanged. It updates the existing job and managed blocks; it does not duplicate LaunchAgents, control tasks, state, or AGENTS.md blocks. Use `~/.local/bin/threadbear disable` to stop scheduled heartbeats without uninstalling, and `~/.local/bin/threadbear enable` to load the same job again.

`--token-display=off|start|end` controls the managed output-token figure. New installs default to `start`; configs created before this setting existed decode as `off` until explicitly changed. Start mode is compact (`🚨 1.6m Subject`), while end mode labels the metric (`🚨 Subject · out 1.6m`).

## ThreadWatch migration

When valid legacy ThreadWatch `state.json` is present and ThreadBear does not yet have a complete config/state pair, the installer previews a data migration. It preserves the control-task identity, title-derived classifications, retry IDs, captured activity, and a detectable legacy heartbeat interval. ThreadBear-only preferences are collected during onboarding. A legacy job or directory without valid migratable state can still be stopped for single-writer safety, but it is not represented as imported data.

Before ThreadBear activates, it stops and verifies the legacy job. Active resources then use `threadbear` and `org.litman.threadbear`; legacy state remains available as migration evidence. If installation fails after ThreadWatch is stopped, the error reports the explicit `launchctl` recovery command.

Rerunning the installer is an idempotent reinstall: it adopts the existing ThreadBear control task and updates managed resources rather than creating duplicates.

## Update or downgrade explicitly

Install the latest newer release:

```sh
~/.local/bin/threadbear update
```

Select an exact release, including an intentional downgrade:

```sh
~/.local/bin/threadbear update --version 1.2.0
```

Manual and automatic updates use the same updater pipeline. It downloads to a private temporary path, verifies the published checksum, embedded version, and updater candidate self-test, and asks the candidate binary to export its embedded managed AGENTS.md and skill content. It prevalidates changed managed surfaces and emits their preview before replacing the installed binary with one atomic rename, then refreshes the enabled AGENTS.md block and always-managed skill with the candidate content and verifies the result. A same-version manual update also reconciles stale managed surfaces from the current binary embedded assets while remaining a no-op when they are current. Managed files keep all bytes outside the single ThreadBear block. Symlinks to user-owned regular files are followed without replacing the link; malformed, dangling, non-file, or foreign-owned targets are refused. The binary and each managed file use safe individual replacement, but there is no cross-file atomicity. If managed refresh fails after binary replacement, the new binary remains installed, partial managed writes are rolled back best-effort, and the error is reported explicitly. A later heartbeat under the new binary is the convergence backstop that repairs residue. No binary rollback file is created or retained.

The updater deliberately does not rewrite `~/Library/LaunchAgents/org.litman.threadbear.plist`. Scheduler plist changes remain the responsibility of install/configure lifecycle operations. ThreadBear does not retain a release history. An explicit downgrade proceeds binary-only only when the older candidate reports the managed-asset command as `unknown_command`; malformed exports, empty assets, generic execution failures, and all other candidate errors stop the update. The binary-only result warns that AGENTS.md and the skill were not refreshed. Their newer managed content can remain as residue until a BEAR-27-or-newer `threadbear update` or `threadbear configure` reconciles it.

With auto-update enabled, the heartbeat checks for a newer release no more than once every 30 minutes and applies it through the same verified binary-and-managed-surface pipeline. The next heartbeat announces the completed version change once in the control task with up to three embedded changelog bullets and an opt-out hint. Manual and installer-driven updates use that same once-per-version announcement mechanism.

With auto-update disabled, the deterministic metadata check runs no more than once every 24 hours and remains silent when current. For each newer version it places one fixed notice in the control task:

```text
🧵🐻 ThreadBear VERSION is ready. Run threadbear update, or tell me “update ThreadBear.”
```

If `~/.local/bin` is not on `PATH`, run `~/.local/bin/threadbear update` instead of the bare command shown in the notice. From the control task, Codex uses its normal command approval panel for a manual update; choose one-time or Always approval there. ThreadBear does not request full-access defaults or bypass task permissions.

## Unsigned binary and Gatekeeper

v1 binaries are not Developer ID signed or notarized. The canonical installer verifies SHA-256 and the executable candidate before installation. If macOS blocks a manually downloaded copy, verify its checksum and source first, then use the standard Privacy & Security “Open Anyway” flow or remove quarantine only from that verified file:

```sh
xattr -d com.apple.quarantine ~/.local/bin/threadbear
```

Do not disable Gatekeeper globally.

## Uninstall

```sh
~/.local/bin/threadbear uninstall
```

ThreadBear separately asks whether to archive the control task (default `no`) and delete persistent state (default `no`), then previews the full removal and asks once for confirmation. It removes the loaded LaunchAgent/plist, binary, managed AGENTS.md block, managed skill block, and update-notice integration. Existing user text outside managed blocks is preserved, as are task titles and archives.

Noninteractive example:

```sh
~/.local/bin/threadbear uninstall --noninteractive --confirm --archive-control-task --delete-state
```

Omit `--archive-control-task` to leave the control task unarchived. Omit `--delete-state` to retain `~/.local/share/threadbear` for diagnosis or later reinstall.
