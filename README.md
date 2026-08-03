# ThreadBear

ThreadBear keeps Codex Desktop task titles useful in the turn that is doing the work. Managed guidance asks each ordinary turn to make two native title calls: one before work starts and one immediately before the final status footer. Two small hooks preserve the task's user-owned subject and expand those compact calls into the visible title.

| Mark | Meaning |
| --- | --- |
| ⏳ | running |
| 🚨 | blocked |
| 🙋 | needs input |
| 🤖 | healthy automation |
| ➡️ | next steps |
| ✅ | complete |
| ❔ | unknown legacy state |

The canonical shape is `<mark> <subject>[ → <action>]`. ThreadBear owns only decoration it previously committed. User renames are adopted intact, and every rendered title is bounded to Codex Desktop's 60 UTF-16-unit limit. When next steps do not fit, ThreadBear preserves the standalone subject display and truncates or omits only the action.

## Install

Open [INSTALL.md](INSTALL.md) in a new Codex task and follow the guided preview, consent, persistent-home setup, and supervised controller migration.

ThreadBear installs a standalone Go binary, one small private state file, managed guidance, two Codex hooks, and one consented hourly Luna heartbeat. The initiating task becomes the persistent `🧵🐻 ThreadBear 🐻🧵` home; one ephemeral controller owns installation migration so that home returns promptly, while the heartbeat later handles quiet housekeeping from that task.

## Commands

```text
threadbear install
threadbear inventory
threadbear migration
threadbear maintenance
threadbear update
threadbear status
threadbear self-test
threadbear uninstall
threadbear version
```

Every command accepts `--json`. `inventory` is read-only and includes every native-addressable unarchived local Codex Desktop or CLI task, including projectless tasks, excluding the persisted main and controller tasks. Rollout-only internal records and older signed-in ChatGPT chat-history rows that Codex's native title setter cannot enumerate or rename are excluded. Those chat-history rows may remain unchanged in the Desktop sidebar even after local migration completes. `status` reports `ready:true` only after `migration_complete`; the installed binary's `help` output is authoritative.

From the persistent ThreadBear task, ask to “strip title icons” or “check for updates now” at any time. The control task serially removes all leading ThreadBear status marks through the same native setter and exact Pre/Post verification used by ordinary turns. The same task's hourly Luna helper can archive only deterministically eligible, ThreadBear-owned complete user tasks after 14 quiet days, restore only archives recorded in its private ownership ledger, and run the deterministic verified update check last. Guided uninstall always pauses that helper and completes title cleanup before removing ThreadBear's local state, hooks, and owned automation.

## Boundaries

ThreadBear installs no daemon or LaunchAgent. One explicitly consented hourly Codex heartbeat runs maintenance from the persistent Luna-medium task and stays quiet on no-op runs. The CLI alone selects archive candidates, stages one operation, reconciles ownership, and chooses the exact Darwin asset from the official release manifest. Luna calls supported native controls and communicates typed results; it never edits private UI storage, interprets prose to add targets, or chooses/downloads/checksums a release. Updates refuse while archive work is pending, verify repository URLs, SHA-256, embedded version, candidate self-test, candidate install, and installed status, and never downgrade. ThreadBear adds no token counts, model call, or narration to ordinary turns. Installation uses one serial native-writing controller and adaptive waves of read-only Luna-medium workers only when genuinely ambiguous history cannot be classified deterministically; workers classify and never write titles.

See [architecture](docs/architecture.md), [compatibility](docs/compatibility.md), and the [status footer convention](docs/status-convention.md).
