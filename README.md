# ThreadBear

ThreadBear is a small local title decorator for Codex Desktop. Immediately before each ordinary final response, managed guidance runs one terminal cell: a stateless local helper returns the fixed icon policy, then the mounted Codex app reads and applies the title. ThreadBear changes only the leading status icon.

| Mark | Meaning |
| --- | --- |
| 🚨 | blocked |
| 🙋 | needs input |
| 🤖 | healthy automation |
| ➡️ | next steps |
| ✅ | complete |
| 🐻 | existing task onboarded, status not yet known |

The visible shape is `<mark> <exact subject>`. ThreadBear reserves those six exact leading icon prefixes; every other safe leading emoji and the rest of the subject stay byte-exact. Owners and actions stay in response prose. A title it cannot handle safely stays unchanged rather than truncated.

## Install

Open [INSTALL.md](INSTALL.md) in a Codex task and follow the guided preview and consent flow. There is no persistent ThreadBear task or controller. After installation, restart Codex so open tasks load the new managed guidance, then ask for **ThreadBear onboard** if you want existing local titles updated.

ThreadBear installs one Go binary, one managed instruction block, one skill, and one daily update-only LaunchAgent. It keeps no per-task title database. A consented reset from 2.2.1 deletes the exact old automation, unpins the exact former persistent task without renaming it, replaces managed artifacts, imports no old state, and does not guess at legacy title cleanup.

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

The terminal `title` command accepts exactly `complete`, `next_steps`, `needs_input`, `blocked`, or `automation`. It returns the calling task ID and fixed icon/safety policy without reading Codex or writing state. In the same cell, the mounted app reads the exact current title, safely derives the subject, and—only when needed—applies one title to the calling task. The exact returned task ID and title must match. A failure stays local and is never retried.

`onboard --dry-run --json` asks once for permission to enumerate the complete unarchived App Server catalog and reports a read-only plan. After separate consent, `onboard --noninteractive --confirm --json` takes a fresh complete snapshot and returns prepared actions with no arbitrary item cap or local title records. For each action the mounted app rereads the exact task immediately, skips drift, and makes at most one native write. The receipt accounts honestly for updated, skipped, unchanged, and unconfirmed tasks.

## Boundaries

Ordinary turns use only mounted Codex reads and writes, so they work under Codex's default workspace permissions. The short-lived official App Server is used only for explicitly approved, complete-catalog onboarding and is launched from a fixed Desktop executable path, never repository `PATH`. ThreadBear does not open Codex SQLite, edit Desktop caches or task prose, archive tasks, retry title writes, or maintain a database, queue, controller, repair pass, or persistent management task.

The daily LaunchAgent does one job: check for a verified official update. Network and candidate-verification failures leave the old install untouched. A later managed-surface write can produce a truthful rerunnable partial, with the binary written last. Successful updates report whether Codex must restart. Updater health is separate from title-core `ready`; it never reads tasks or changes titles.

See [architecture](docs/architecture.md), [compatibility](docs/compatibility.md), and the [status convention](docs/status-convention.md).
