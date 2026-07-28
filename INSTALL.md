# Install ThreadBear with Codex

Open Codex, paste this guide or request, and I’ll take it from there.

## Agent brief

You are installing ThreadBear for the person in this Codex task. Keep them informed, ask only for choices that matter, use the exact commands below, and do not claim success until every verification step passes.

ThreadBear is a user-local macOS utility that classifies changed Codex tasks, keeps useful titles, and conservatively archives completed inactive work. Unchanged heartbeats use no model tokens. Installation adopts the calling Codex task as the control task; it never creates a persistent task.

Do not use `sudo`. Do not edit README, the website, or the installed skill during this procedure. Do not rename or pin the calling task until installation succeeds. The welcome notice written after adoption records all selected settings, so do not repeat the preference interview in that task later.

## 1. Permission prebrief

Before commands, give the person this full prebrief:

- macOS privacy prompts may name **`threadbear`**, but they originate from the spawned Codex App Server that ThreadBear uses to read task data.
- **Documents** access may be requested because Codex workspaces live under `~/Documents/Codex`.
- **Automation** may be requested because Codex App Server reaches for Codex Computer Use.
- ThreadBear needs neither Documents access nor Automation permission. Declining both is safe and does not affect ThreadBear's function. There is no supported spawn-side fix in Codex `0.145.0`.
- ThreadBear does not need **Full Disk Access**. Do not grant it.
- HTTPS access to `threadbear.sh` and GitHub Releases is needed for the bootstrap, checksum, and release metadata.

If a panel appears, stop and let the person decide. Never click a privacy panel on their behalf.

## 2. Preflight

Run read-only checks and summarize failures plainly:

```sh
sw_vers -productVersion
uname -m
command -v codex
codex --version
curl --version
curl -fsSLI https://threadbear.sh/install.sh >/dev/null
curl -fsSLI https://github.com/ericlitman/threadbear/releases/latest >/dev/null
```

Requirements:

- macOS 12 or newer;
- Apple silicon `arm64` or Intel `x86_64`;
- Codex Desktop and a working local `codex` executable/App Server;
- HTTPS access to `https://threadbear.sh` and GitHub Releases;
- no `sudo` and no non-macOS install attempt.

ThreadBear v1 is not Developer ID signed or notarized. The supported bootstrap verifies SHA-256, candidate health, and embedded version before delegating to the candidate.

## 3. Feature-detect the calling task before mutation

Before downloading, installing, renaming, pinning, or changing any managed resource, feature-detect the available Codex task tooling:

1. Resolve the canonical ID of the **calling task**, meaning this exact task in which the person asked you to follow the guide. Record it as `CONTROL_TASK_ID`. Do not ask the person to copy an ID if the task tooling can resolve it.
2. Prove that the task tooling supports renaming this task, using a capability/read-only check that does not rename it yet.

If canonical calling-task ID resolution is unavailable, ambiguous, or noncanonical, or if task rename is unsupported, stop and tell the person: **“This Codex version has an unsupported install path for ThreadBear because calling-task identification and rename support are required.”** Do not mutate anything and do not direct them to another task, app, or support channel.

Use only supported task tooling. Do not read private Codex state and do not use UI automation. Do not rename or pin yet. A first install without `--control-task-id`, or a reinstall whose persisted control task is unreadable without a replacement ID, exits `2` without changing files or the scheduler. Installation validates the selected task through the App Server before filesystem or scheduler mutation.

On reinstall, a normal readable persisted control task wins even when the calling task supplies a different ID; the result reports `stayed_home`. An unreadable persisted task can be replaced by the supplied calling task. A persisted archived task is unarchived during reinstall. A supplied archived task is rejected.

## 4. Ask for preferences in plain language

Say: “The defaults are a five-minute heartbeat, automatic verified updates, archive completed tasks after 14 quiet days, keep titles current, show output tokens at the start of titles, install the managed AGENTS guidance, and use the default classifier. Keep all defaults?”

If yes, use the fast path flags below. If no, ask only about the settings they want changed:

| Preference | Default | Flag |
|---|---:|---|
| Heartbeat | 300 seconds | `--heartbeat-seconds 300` |
| Automatic verified updates | on | `--auto-update=true` |
| Archive completed inactive tasks | on | `--archive=true` |
| Quiet days before archive | 14 | `--archive-after-days 14` |
| Maintain status/next-action titles | on | `--rename=true` |
| Output-token figure | start | `--token-display=start` |
| Managed AGENTS guidance | on | `--agents=true` |
| Classifier model | `gpt-5.6-luna` | `--classifier-model gpt-5.6-luna` |
| Classifier effort | medium | `--classifier-effort medium` |
| Classifier context budget | 250000 bytes | `--classifier-context-budget-bytes 250000` |

Boolean values use `=`. Examples: `--archive=false`, `--rename=false`, `--agents=false`.

## 5. Understand the bootstrap

The canonical bootstrap is:

```sh
curl -fsSL https://threadbear.sh/install.sh | sh
```

It resolves the latest release manifest, selects the `darwin/arm64` or `darwin/amd64` candidate, downloads the candidate and its published checksum into a private temporary directory, verifies SHA-256, runs candidate self-test, checks the candidate's embedded version, and only then delegates to `threadbear install`.

An exact version is selected with `--version N.N.N` without a leading `v`:

```sh
curl -fsSL https://threadbear.sh/install.sh | sh -s -- --version 1.2.0
```

A missing checksum, mismatch, malformed candidate, wrong embedded version, or failed candidate self-test stops before replacing a working binary. The candidate is temporary; ThreadBear does not retain version directories or rollback copies.

## 6. Produce the dry-run preview

Construct one flag list and keep it unchanged for the confirmed run. Defaults fast path:

```sh
CONTROL_TASK_ID='paste-id-here'
INSTALL_FLAGS="--control-task-id $CONTROL_TASK_ID --heartbeat-seconds 300 --auto-update=true --archive=true --archive-after-days 14 --rename=true --token-display=start --agents=true --classifier-model gpt-5.6-luna --classifier-effort medium --classifier-context-budget-bytes 250000"
curl -fsSL https://threadbear.sh/install.sh | sh -s -- --dry-run $INSTALL_FLAGS
```

For an exact release, append `--version N.N.N` to both preview and confirmed commands.

Dry-run requires no confirmation, acquires no mutating lock, and causes zero mutation. It validates the adoption task and prints the complete deterministic `PreviewResult` in human form. Add `--json` when machine-readable output is useful.

Show the full preview to the person in chat. Explain the control-task disposition:

- `adopted`: first adoption;
- `retained`: the persisted readable task remains home;
- `stayed_home`: a different supplied task was ignored because the persisted task is readable;
- `replaced`: an unreadable persisted task will be replaced;
- `repaired`: the one exact BEAR-60 controller repair will run;
- `will_unarchive_control_task=true`: the persisted home will be unarchived by the confirmed install.

Ask: “Apply exactly this preview?” Continue only after an explicit yes.

## 7. Run the identical confirmed flags

Use the same flags, adding only noninteractive confirmation:

```sh
curl -fsSL https://threadbear.sh/install.sh | sh -s -- --noninteractive --confirm $INSTALL_FLAGS
```

Do not substitute a different task ID or preference after approval. The installer recomputes the same preview before mutation. If the world changed, it stops and asks for a fresh preview.

The install validates the control task before any filesystem or scheduler mutation, stages and self-tests managed resources, writes private config/state, enables the LaunchAgent, and posts the unchanged welcome notice exactly once for first adoption, unreadable replacement, or the exact repair. It does not call persistent `thread/start`, retitle the control task, pin it, or deliberately kickstart a heartbeat while the install lock is held.

After the installer returns successfully and has released its lock, inspect `control_task_disposition`. Only for `adopted`, `replaced`, or `repaired`, use the supported rename tool detected earlier to rename the calling task exactly `🧵🐻 ThreadBear 🐻🧵`, then pin it once if supported; otherwise tell the person how to pin it manually and continue. For `retained` or `stayed_home`, do not rename, pin, or reassert anything on any task. A user's later rename or unpin is respected and must not be restored. In particular, `stayed_home` from another calling task never renames or pins that calling task. Do not use private Codex state or UI automation.

## 8. Verify, heartbeat once when required, and report here

Run:

```sh
~/.local/bin/threadbear version
~/.local/bin/threadbear self-test
~/.local/bin/threadbear status
~/.local/bin/threadbear status --json
launchctl print "gui/$(id -u)/org.litman.threadbear"
```

Inspect `last_completed_heartbeat` in `status --json`. If it is `null`, run exactly one explicit heartbeat now, after the installer has returned and released its lock:

```sh
~/.local/bin/threadbear heartbeat
~/.local/bin/threadbear status --json
```

That heartbeat is mandatory when the field is null, not optional. Do not request a second user approval beyond normal command-tool approval. Do not run more than one explicit heartbeat during installation verification. If it fails, report and troubleshoot the failure in this same task.

Capture the heartbeat result. When it emits JSON, report the counts of `changed`, `archived_ids`, and `retries`; when it emits no record because there was no work, report those counts as zero. After the heartbeat, rerun `status --json` and report `pending_retries`. Also report install resource and warning counts, installed version, self-test pass/fail, LaunchAgent status, control-task disposition, whether the task was unarchived, and the final `last_completed_heartbeat`. Finish in a friendly bear voice. Never direct the person elsewhere to complete, verify, or troubleshoot the installation.

Expected managed resources are:

- `~/.local/bin/threadbear`, mode `0700`;
- `~/.local/share/threadbear/`, mode `0700`, with private atomic config/state files;
- `~/Library/LaunchAgents/org.litman.threadbear.plist`, mode `0600`;
- logs below `~/.local/share/threadbear/logs/`;
- `${CODEX_HOME:-~/.codex}/AGENTS.md`, one managed block when enabled;
- `${CODEX_HOME:-~/.codex}/skills/threadbear/SKILL.md`, one managed block;
- for an `adopted`, `replaced`, or `repaired` result only, the calling task renamed after success exactly `🧵🐻 ThreadBear 🐻🧵` and pinned once when supported; `retained` and `stayed_home` leave task title and pin state untouched.

## Living with the bear

Read-only diagnosis:

```sh
~/.local/bin/threadbear status
~/.local/bin/threadbear status --json
~/.local/bin/threadbear self-test
~/.local/bin/threadbear heartbeat --dry-run
~/.local/bin/threadbear inspect TASK_ID
```

Pause and resume scheduling:

```sh
~/.local/bin/threadbear disable
~/.local/bin/threadbear enable
```

Reconfigure with a preview first:

```sh
~/.local/bin/threadbear configure --dry-run --heartbeat-seconds 600
~/.local/bin/threadbear configure --heartbeat-seconds 600
~/.local/bin/threadbear configure --auto-update=false --archive=false
```

For noninteractive configuration use `--noninteractive --confirm`. The welcome notice already records installation settings and plain-language examples, so the agent in the control task should read it instead of repeating onboarding.

## Update or downgrade

```sh
~/.local/bin/threadbear update
~/.local/bin/threadbear update --version 1.2.0
```

Manual and automatic updates share checksum, embedded-version, candidate self-test, managed-surface prevalidation, and atomic binary replacement. Exact version selection is also the downgrade mechanism. No local release history or automatic rollback copy is retained.

## Troubleshooting

1. Run `version`, `status --json`, and `self-test` before mutating anything.
2. Confirm the persisted control task is readable in Codex. If it is gone, choose a readable unarchived replacement and rerun install with `--control-task-id`.
3. If a supplied task is archived, unarchive it in Codex and rerun the dry-run.
4. Inspect the job with `launchctl print "gui/$(id -u)/org.litman.threadbear"`.
5. Confirm `${CODEX_HOME:-~/.codex}` and the pinned Codex executable are available.
6. Do not edit config/state by hand unless a maintainer gives a recovery procedure.

### Unsigned binary and Gatekeeper

The supported installer verifies the checksum and candidate. If macOS blocks a manually downloaded verified copy, use Privacy & Security **Open Anyway**, or remove quarantine only from that verified file:

```sh
xattr -d com.apple.quarantine ~/.local/bin/threadbear
```

Never disable Gatekeeper globally.

## Exit codes and noninteractive reference

- `0`: preview or installation completed successfully;
- `2`: invalid arguments or required control task ID missing; no install mutation;
- `1`: platform, network, checksum, App Server, candidate, confirmation, or lifecycle failure.

Full noninteractive defaults:

```sh
~/.local/bin/threadbear install \
  --control-task-id TASK_ID \
  --noninteractive --confirm \
  --heartbeat-seconds 300 \
  --auto-update=true \
  --archive=true \
  --archive-after-days 14 \
  --rename=true \
  --token-display=start \
  --agents=true \
  --classifier-model gpt-5.6-luna \
  --classifier-effort medium \
  --classifier-context-budget-bytes 250000
```

Dry-run uses the identical flags with `--dry-run` and without `--confirm`.

## Uninstall

```sh
~/.local/bin/threadbear uninstall
```

Interactive uninstall thanks the person, defaults control-task archival and final confirmation to yes, removes the binary, LaunchAgent, managed blocks, and persistent ThreadBear state, and leaves unrelated task titles and archives alone. Noninteractive form:

```sh
~/.local/bin/threadbear uninstall --noninteractive --confirm --archive-control-task
```

Omit `--archive-control-task` to leave the task unarchived. `--delete-state` remains a deprecated no-op for one release; state is deleted either way.
