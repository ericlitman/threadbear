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

The canonical shape is `<mark> <subject>[ → <action>]`. ThreadBear owns only decoration it previously committed. User renames are adopted intact, and every rendered title is bounded to Codex Desktop's 60 UTF-16-unit limit.

## Install

Open [INSTALL.md](INSTALL.md) in a new Codex task and follow the guided preview, consent, install, migration, and rendered Desktop verification.

ThreadBear installs a standalone Go binary, one small private state file, managed guidance, and two Codex hooks.

## Commands

```text
threadbear install
threadbear inventory
threadbear status
threadbear self-test
threadbear uninstall
threadbear version
```

Every command accepts `--json`. `inventory` is read-only and includes every local unarchived task across source shapes, including projectless tasks. The installed binary's `help` output is authoritative.

## Boundaries

ThreadBear does not archive tasks, add token counts, update itself in the background, or edit Codex private UI storage. It adds no model call or narration to ordinary turns. Installation may use at most eight ephemeral Luna-medium workers only when genuinely ambiguous v2 history cannot be classified deterministically; those workers classify and never write titles.

See [architecture](docs/architecture.md), [compatibility](docs/compatibility.md), and the [status footer convention](docs/status-convention.md).
