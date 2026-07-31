# Install ThreadBear

Hi. Let's install ThreadBear. 🧵🐻

This guide is meant to be followed inside the Codex task that should remain the
ThreadBear control task. The installer adopts that task; it never creates a
hidden persistent conversation.

## What you are installing

ThreadBear scans local Codex task metadata and rollout tails every five minutes,
decides exact statuses, and keeps Desktop titles current. The scan is highly
token-efficient and mostly deterministic: exact footers and live runtime state
do not open a model. Luna medium is reserved for legacy history that remains
ambiguous across two unchanged passes.

The deterministic scan of a large local inventory should finish in seconds.
The separate native Desktop handoff can take roughly three to five minutes when
a large existing workspace actually has many titles to repaint. Progress should
describe completed work, never unexplained waiting.

It writes:

- `~/.local/bin/threadbear`
- `~/.local/share/threadbear/core.json` and private logs
- `~/Library/LaunchAgents/org.litman.threadbear.plist`
- one managed block in `~/.codex/AGENTS.md`
- one managed skill at `~/.codex/skills/threadbear/SKILL.md`

It reads the local Codex SQLite index and rollout files, and uses App Server
only to read current runtime state when a rollout is ambiguous. Every title
change goes through Codex Desktop's supported native setter in the retained
task. No `sudo` is used.

## Recommended setup

- Heartbeat: every five minutes
- Titles: enabled, bounded to 60 UTF-16 units
- Luna: `gpt-5.6-luna`, medium, ambiguity only
- Automatic updates, task archiving, and token decoration: not part of this
  minimal generation

Install ThreadBear with this recommended setup?

Treat a clear **yes** to that unchanged recommendation as installation consent.
If the recommendation changes, the answer is ambiguous, or this is a reinstall,
explain the changed effects and ask again before mutating anything.

## Preview and install

First build or download the verified candidate, then show its exact preview:

```sh
~/.local/bin/threadbear install \
  --control-task-id "$CODEX_THREAD_ID" \
  --dry-run --json
```

For a published release, the bootstrap verifies the manifest checksum and
candidate self-test:

```sh
curl -fsSL https://threadbear.sh/install.sh | sh -s -- \
  --control-task-id "$CODEX_THREAD_ID" \
  --noninteractive --confirm
```

For a local candidate:

```sh
/path/to/threadbear install \
  --control-task-id "$CODEX_THREAD_ID" \
  --noninteractive --confirm --json
```

After installation, run:

```sh
~/.local/bin/threadbear status --json
~/.local/bin/threadbear heartbeat --dry-run --json
```

Then perform the retained native handoff described by the installed ThreadBear
skill. Its operation guard is rechecked immediately before every native setter,
and a report is accepted only when Codex exposes the exact desired title.

## Uninstall

Preview the effect in chat, obtain explicit approval, and run:

```sh
~/.local/bin/threadbear uninstall --noninteractive --confirm --json
```

Uninstall removes ThreadBear's state, binary, LaunchAgent, and managed guidance
while leaving every current Codex task title unchanged.

## Verification expectations

A release is ready only after unit and integration tests pass, a real inventory
scan is timed separately from title application, Luna calls are counted, and a
controlled Codex Desktop canary proves the rendered title with screenshot
evidence. State writes and command exit codes alone are not visual proof.
