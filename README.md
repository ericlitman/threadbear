# ThreadBear

ThreadBear is a small local title decorator for Codex Desktop. Immediately before each ordinary final response, managed guidance runs one local command. ThreadBear keeps the exact user-owned subject and changes only the leading status icon.

| Mark | Meaning |
| --- | --- |
| 🚨 | blocked |
| 🙋 | needs input |
| 🤖 | healthy automation |
| ➡️ | next steps |
| ✅ | complete |
| 🐻 | existing task onboarded, status not yet known |

The visible shape is `<mark> <exact subject>`. Owners and actions stay in the response prose, not the title. ThreadBear never normalizes, strips, or truncates a safe subject. A title it cannot handle safely stays unchanged.

## Install

Open [INSTALL.md](INSTALL.md) in a Codex task and follow the guided preview and consent flow. There is no persistent ThreadBear task or controller. After installation, restart Codex so open tasks load the new managed guidance, then ask for **ThreadBear onboard** if you want existing local titles updated.

ThreadBear installs one Go binary, tiny private per-task subject records, one managed instruction block, one skill, and one daily update-only LaunchAgent. A consented reset from 2.2.1 deletes the exact old automation, unpins the exact former persistent task without renaming it, replaces managed artifacts, imports no old state, and does not guess at legacy title cleanup.

## Commands

```text
threadbear install
threadbear title --status complete
threadbear onboard --dry-run
threadbear onboard --noninteractive --confirm
threadbear status
threadbear self-test
threadbear update
threadbear uninstall
threadbear version
```

Every command accepts `--json`; the installed binary's `help` output is authoritative.

The terminal `title` command accepts exactly `complete`, `next_steps`, `needs_input`, `blocked`, or `automation`. The enum controls only the icon. The binary opens one short-lived official Codex App Server, reads the exact current title, resolves the safe subject, makes at most one `thread/name/set` request, and rereads the title. A failure or unconfirmed result stays local to that turn and never blocks the response or triggers a retry.

`onboard --dry-run --json` enumerates the complete unarchived App Server catalog before any write and reports a full plan. Explicit consent runs `onboard --noninteractive --confirm --json`, which processes every safe target serially with no arbitrary item cap. It rereads each target, skips drift or uncertainty, attempts one write, and counts it only after exact readback. The receipt accounts honestly for updated, unchanged, skipped, and unconfirmed tasks. A fresh rerun safely continues after an interruption.

## Boundaries

The short-lived official App Server is ThreadBear's only task read/write authority. ThreadBear does not open or edit Codex SQLite, Desktop caches, or task prose. It does not archive tasks, classify in the background, retry title writes, or maintain a queue, controller, repair pass, or persistent management task.

The daily LaunchAgent does one job: check for a verified official update. Network and candidate-verification failures leave the old install untouched. A later managed-surface write can produce a truthful rerunnable partial, with the binary written last. Successful updates report whether Codex must restart. Updater health is separate from title-core `ready`; it never reads tasks or changes titles.

See [architecture](docs/architecture.md), [compatibility](docs/compatibility.md), and the [status convention](docs/status-convention.md).
