# Installing ThreadBear

This guide is written for an installing agent. Explain the plan to the user, use the supported command, preserve the preview, and do not assert success until the verification commands pass.

## Preconditions

Confirm macOS 12 or newer, `arm64` or `x86_64`/`amd64` hardware, Codex Desktop with a compatible local `codex` executable/App Server surface, and HTTPS access to `threadbear.dev` and GitHub Releases. v1 is not Developer ID signed or notarized. Installation is user-local and must not use `sudo`.

## Guided installation dialogue

### 1. Start the canonical bootstrap

```sh
curl -fsSL https://threadbear.dev/install.sh | sh
```

The pipe supplies the script, not the answers. ThreadBear opens `/dev/tty` for prompts so script input cannot accidentally accept them.

The bootstrap selects the latest stable release, detects `darwin/arm64` or `darwin/amd64`, downloads the binary and SHA-256 file to a private temporary directory, verifies the checksum, runs `self-test --candidate`, verifies the embedded version, and only then delegates to `threadbear install`.

To install an exact release:

```sh
curl -fsSL https://threadbear.dev/install.sh | sh -s -- --version 1.2.0
```

Versions must be exact `N.N.N` values without a leading `v`. A missing checksum, mismatch, wrong embedded version, or failed candidate self-test stops before a working installed binary is replaced.

### 2. Collect every preference

On first install, ask the prompts in this order. Pressing Return accepts the shown value.

| Prompt | Default | Noninteractive flag |
|---|---:|---|
| Heartbeat interval in seconds | `300` | `--heartbeat-seconds 300` |
| Automatically archive completed inactive tasks | `yes` | `--archive=true` |
| Archive inactivity interval in days | `14` | `--archive-after-days 14` |
| Automatically maintain status and next-action titles | `yes` | `--rename=true` |
| Install managed AGENTS.md instructions | `yes` | `--agents=true` |
| Classifier model | `gpt-5.6-luna` | `--classifier-model gpt-5.6-luna` |
| Classifier effort | `medium` | `--classifier-effort medium` |
| Classifier context budget in bytes | `250000` | `--classifier-context-budget-bytes 250000` |

Boolean install/configure flags use `=` forms. Use `--archive=false`, `--rename=false`, or `--agents=false`; do not pass a separate `false` argument.

### 3. Review the single final preview

Before mutation, ThreadBear prints the deterministic-first classifier rule; binary and state paths; exact managed AGENTS.md and skill mutations; LaunchAgent path and staged/self-tested/enabled sequence; persistent control-task effect; and every selected preference.

The confirmation prompt is exactly:

```text
Apply exactly this preview? (yes/no) [no]:
```

Answer `yes` only when every line matches the user's intent. `no` cancels before the confirmed mutation phase.

### 4. Expected final effects

A successful first install creates or adopts exactly these managed resources:

- `~/.local/bin/threadbear` — standalone executable, mode `0700`;
- `~/.local/share/threadbear/` — private state/config directory, mode `0700`; `config.json` and `state.json` are atomic mode-`0600` files;
- `~/Library/LaunchAgents/org.litman.threadbear.plist` — user LaunchAgent, mode `0600`;
- `~/.local/share/threadbear/logs/heartbeat.stdout.log` and `heartbeat.stderr.log` — LaunchAgent output paths;
- `${CODEX_HOME:-~/.codex}/AGENTS.md` — one identifiable mode-`0600` managed file when enabled, preserving all bytes outside that block;
- `${CODEX_HOME:-~/.codex}/skills/threadbear/SKILL.md` — one mode-`0600` managed ThreadBear skill block;
- one persistent Codex control task titled `🧵🐻 ThreadBear 🐻🧵`.

The LaunchAgent uses `StartInterval`, `ProcessType=Background`, and `KeepAlive=false`. The interval is approximate: macOS does not replay launches missed during sleep or while a previous heartbeat is still running.

## Noninteractive installation

Noninteractive installation requires both `--noninteractive` and the explicit confirmation assertion `--confirm`:

```sh
curl -fsSL https://threadbear.dev/install.sh | sh -s -- \
  --noninteractive \
  --confirm \
  --heartbeat-seconds 300 \
  --archive=true \
  --archive-after-days 14 \
  --rename=true \
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
threadbear heartbeat --dry-run
threadbear inspect TASK_ID
threadbear status --json
```

These commands do not invoke a model or mutate task titles/archives. A normal unchanged `threadbear heartbeat` emits zero bytes when no update notice is due.

## Reconfigure later

`threadbear configure` changes every onboarding preference and previews effects before confirmation:

```sh
threadbear configure --heartbeat-seconds 600 --archive-after-days 30
threadbear configure --archive=false --rename=true --agents=false
threadbear configure --classifier-model gpt-5.6-luna --classifier-effort medium --classifier-context-budget-bytes 250000
threadbear configure --dry-run --heartbeat-seconds 900
```

For automation, add `--noninteractive --confirm`. Reconfiguration updates the existing job and managed block; it does not duplicate LaunchAgents, control tasks, state, or AGENTS.md blocks. Use `threadbear disable` to stop scheduled heartbeats without uninstalling, and `threadbear enable` to load the same job again.

## ThreadWatch migration

When valid legacy ThreadWatch `state.json` is present and ThreadBear does not yet have a complete config/state pair, the installer previews a data migration. It preserves the control-task identity, title-derived classifications, retry IDs, captured activity, and a detectable legacy heartbeat interval. ThreadBear-only preferences are collected during onboarding. A legacy job or directory without valid migratable state can still be stopped for single-writer safety, but it is not represented as imported data.

Before ThreadBear activates, it stops and verifies the legacy job. Active resources then use `threadbear` and `org.litman.threadbear`; legacy state remains available as migration evidence. If installation fails after ThreadWatch is stopped, the error reports the explicit `launchctl` recovery command.

Rerunning the installer is an idempotent reinstall: it adopts the existing ThreadBear control task and updates managed resources rather than creating duplicates.

## Update or downgrade explicitly

Install the latest newer release:

```sh
threadbear update
```

Select an exact release, including an intentional downgrade:

```sh
threadbear update --version 1.2.0
```

The updater downloads to a private temporary path, verifies the published checksum, embedded version, and candidate self-test, then atomically replaces the installed binary. Any pre-replacement failure removes the candidate and leaves the current binary untouched. ThreadBear does not create rollback state or retain a local release history; an explicit older version is the downgrade path and still must pass compatibility checks.

A daily deterministic metadata check is silent when current. For each newer version it places one notice in the control task:

```text
🧵🐻 ThreadBear VERSION is ready. Run threadbear update, or tell me “update ThreadBear.”
```

No binary changes until the user invokes an update. From the control task, Codex uses its normal command approval panel; choose one-time or Always approval there. ThreadBear does not request full-access defaults or bypass task permissions.

## Unsigned binary and Gatekeeper

v1 binaries are not Developer ID signed or notarized. The canonical installer verifies SHA-256 and the executable candidate before installation. If macOS blocks a manually downloaded copy, verify its checksum and source first, then use the standard Privacy & Security “Open Anyway” flow or remove quarantine only from that verified file:

```sh
xattr -d com.apple.quarantine ~/.local/bin/threadbear
```

Do not disable Gatekeeper globally.

## Uninstall

```sh
threadbear uninstall
```

ThreadBear separately asks whether to archive the control task (default `no`) and delete persistent state (default `no`), then previews the full removal and asks once for confirmation. It removes the loaded LaunchAgent/plist, binary, managed AGENTS.md block, managed skill block, and update-notice integration. Existing user text outside managed blocks is preserved, as are task titles and archives.

Noninteractive example:

```sh
threadbear uninstall --noninteractive --confirm --archive-control-task --delete-state
```

Omit `--archive-control-task` to leave the control task unarchived. Omit `--delete-state` to retain `~/.local/share/threadbear` for diagnosis or later reinstall.
