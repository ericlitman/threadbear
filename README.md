# 🧵🐻 ThreadBear

ThreadBear is a token-conscious Codex task manager for macOS. A standalone Go binary and user LaunchAgent keep managed, unarchived Codex Desktop tasks classified, usefully titled, and safely archived—without waking a model when deterministic evidence already settles the answer.

An unchanged heartbeat reads the complete local inventory, sees that nothing moved, and exits with **zero model tokens and zero output**. Changed work is resolved from runtime state, structured errors, automation state, or an optional agent status footer before ThreadBear asks the configured semantic classifier (Luna medium by default) for help.

## What the bear does

- Maintains one persistent control task titled `🧵🐻 ThreadBear 🐻🧵`.
- Prefixes managed task titles with one of seven states: `⏳` `🚨` `🙋` `🤖` `➡️` `✅` `❔`.
- Optionally places a compact cumulative output-token figure at the start or end of managed titles.
- Preserves a durable task subject and adds a concise next action when warranted.
- Automatically archives only completed, inactive tasks; unfinished work remains visible.
- With auto-update enabled by default, checks for a newer release at most every 30 minutes, installs it through the verified replacement path, and posts one control-task announcement with up to three changelog bullets. Opting out restores the once-daily notice-only behavior.
- Offers read-only status, inspection, dry-run, and self-test commands that do not invoke a model.

ThreadBear does not edit `.codex-global-state.json`, click through Codex Desktop, invalidate private sidebar caches, organize Desktop projects, or promise an immediate repaint in an already-open sidebar. Persisted task state is authoritative; the supported UI may converge on a later task activity or refresh.

## Install

Open a new Codex task and paste:

```text
Install ThreadBear — follow https://threadbear.sh/install
```

Codex preflights the Mac, asks only about preferences that matter, shows the complete dry-run preview for approval, installs ThreadBear, and verifies the result in the same conversation.

ThreadBear v1 supports macOS 12 or newer on Apple silicon and Intel Macs. Release binaries are pure Go, require no end-user runtime, and install without `sudo`.

> **Deprecated for one release — installer machinery only:** `curl -fsSL https://threadbear.sh/install.sh | sh` remains available for installing agents, but it is not the supported human install path.

For exact-version, noninteractive, Gatekeeper, update/downgrade, and uninstall details, read [INSTALL.md](INSTALL.md).

## Commands

| Command | Purpose | Model-free |
|---|---|---:|
| `~/.local/bin/threadbear status` | Show version, LaunchAgent health, heartbeat, control task, preferences, retries, and update check | yes |
| `~/.local/bin/threadbear inspect TASK_ID` | Explain saved state, evidence source, next action, managed token figure, retry, and archive eligibility | yes |
| `~/.local/bin/threadbear heartbeat --dry-run` | Preview due classification/archive/update-check work without models or mutation | yes |
| `~/.local/bin/threadbear self-test` | Validate platform, Codex surfaces, state, config, binary, managed files, and LaunchAgent | yes |
| `~/.local/bin/threadbear configure …` | Preview and change onboarding preferences | yes |
| `~/.local/bin/threadbear enable` / `~/.local/bin/threadbear disable` | Enable or disable the user LaunchAgent | yes |
| `~/.local/bin/threadbear restore TASK_ID` | Restore a task archived by ThreadBear and restart its inactivity grace period | yes |
| `~/.local/bin/threadbear update [--version N.N.N]` | Stage, verify, and explicitly install a release | yes |
| `~/.local/bin/threadbear uninstall` | Clean active managed titles, preview removal, choose control-task archival, and delete state | yes |
| `~/.local/bin/threadbear version` | Show installed version and website | yes |
| `~/.local/bin/threadbear heartbeat` | Run the normal classification and lifecycle cycle | only if semantic fallback is needed |

All commands accept `--json` for stable machine-readable output where applicable.

## How the token model works

ThreadBear pays attention before it pays for inference:

1. Read every managed unarchived task from local Codex surfaces.
2. Exit silently if the inventory is unchanged and no update check or version-change follow-up is due.
3. Resolve changed tasks mechanically from stronger structured evidence and valid status footers.
4. Send only unresolved changed tasks to fresh, non-persisted classifier sessions in context-safe batches.
5. Revalidate each task before changing its title or archive state so newer user activity wins.

Classifier sessions never append to the control task and never leave sidebar tasks behind. See [Architecture](docs/architecture.md) and the aggregate [Benchmark](docs/benchmark.md).

## Installed footprint

- Binary: `~/.local/bin/threadbear`
- State and config: `~/.local/share/threadbear/`
- LaunchAgent: `~/Library/LaunchAgents/org.litman.threadbear.plist`
- Logs: `~/.local/share/threadbear/logs/`
- Optional managed guidance: `${CODEX_HOME:-~/.codex}/AGENTS.md`
- Managed skill: `${CODEX_HOME:-~/.codex}/skills/threadbear/SKILL.md`

ThreadBear uses private user-local files and a minimal explicit subprocess environment. Logs contain task IDs, counts, state transitions, and errors—not message bodies, inherited environment dumps, or classifier payloads.

## Documentation

- [Install and lifecycle guide](INSTALL.md)
- [Architecture and privacy model](docs/architecture.md)
- [Status footer and title convention](docs/status-convention.md)
- [Compatibility and troubleshooting](docs/compatibility.md)
- [Aggregate benchmark evidence](docs/benchmark.md)

## License

ThreadBear is available under the [MIT License](LICENSE).
