# ThreadBear

ThreadBear watches local Codex tasks and keeps their Desktop titles useful. Its
heartbeat is deterministic first: it reads the Codex task index and fixed
rollout tails, accepts exact status footers, and uses runtime state for active
tasks. Luna medium sees only legacy evidence that remains genuinely ambiguous
across two passes.

The title marks are deliberately small:

| Mark | Meaning |
| --- | --- |
| ⏳ | running |
| 🚨 | blocked |
| 🙋 | needs input |
| 🤖 | healthy automation |
| ➡️ | next steps |
| ✅ | complete |
| ❔ | unknown |

ThreadBear preserves the user-owned subject after the mark and bounds every
visible title to 60 UTF-16 units, matching Codex Desktop. Heartbeats stage
guarded decisions without writing titles. The retained control task drains
those plans through Codex Desktop's supported native setter, the only path
proved to repaint the rendered UI.

## Install

Open [INSTALL.md](INSTALL.md) in a Codex task and follow the guided flow. The
installer verifies the release checksum and candidate self-test before it
changes anything.

## Commands

```text
threadbear install
threadbear heartbeat
threadbear status
threadbear self-test
threadbear uninstall
threadbear version
```

Every command accepts `--json`; `heartbeat --dry-run` reads the complete current
inventory without writing state, opening Luna, or changing titles. `title-plan`
is a hidden compatibility surface used only by the installed native handoff.

## Deliberate scope

ThreadBear owns observation, status resolution, guarded title staging, native
handoff, and enough private state to make those operations repeatable. It does not
archive tasks, decorate titles with token counts, update itself in the
background, expose a configuration framework, or migrate the pre-reset state
schema. Installing this generation starts from `core.json` and conservatively
adopts each existing non-status title remainder as user-owned text.

See [architecture](docs/architecture.md), [compatibility](docs/compatibility.md),
and the [status footer convention](docs/status-convention.md).
