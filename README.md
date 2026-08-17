# ThreadBear

ThreadBear is a small local title decorator for Codex Desktop. Immediately before each ordinary final response, managed guidance runs one terminal cell: a stateless local helper returns the fixed icon policy, then the mounted Codex app reads and applies the title. ThreadBear changes only the leading status icon.

| Mark | Meaning |
| --- | --- |
| 🚨 | blocked |
| 🙋 | needs input |
| 🤖 | healthy automation |
| ➡️ | next steps |
| ✅ | complete |

The ordinary visible shape is `<mark> <exact subject>`. ThreadBear writes those five status prefixes. During the first read after installation, a conservative historical inference uses `<mark>✦ <exact subject>`; the sparkle disappears when that task next takes a real turn. ThreadBear also recognizes the obsolete neutral `🐻 ` prefix so it can remove that decoration without ever emitting it. Every other safe leading emoji and subject byte stays exact, except an ambiguous old ThreadBear prefix is deliberately left unchanged rather than guessed. Owners and actions stay in response prose. A title it cannot handle safely stays unchanged rather than truncated.

## Install

Open [INSTALL.md](INSTALL.md) in a Codex task and follow the guided preview and consent flow. There is no persistent ThreadBear task or controller. After installation reports success, Codex asks once for complete-catalog read permission. If allowed, one yielded in-app cell progressively reads bounded latest-turn history and applies exact or conservative `✦` status icons through mounted Codex title tools; declining leaves existing titles unchanged. Unknown tasks stay unchanged, interruption loses only unfinished work, and no onboarding state is persisted. Restart Codex so open tasks load the new managed guidance.

ThreadBear installs one Go binary, one managed instruction block, one skill, and one daily update-only LaunchAgent. It keeps no per-task title database. A consented reset from 2.2.1 deletes the exact old automation, unpins the exact former persistent task without renaming it, replaces managed artifacts, imports no old state, and does not guess at legacy title cleanup.

## Commands

```text
threadbear install
threadbear title --status complete
threadbear status
threadbear self-test
threadbear update
threadbear uninstall
threadbear version
```

Every command accepts `--json`; the installed binary's `help` output is authoritative.

The terminal `title` command accepts exactly `complete`, `next_steps`, `needs_input`, `blocked`, or `automation`. It returns the calling task ID and fixed icon/safety policy without reading Codex or writing state. In the same cell, the mounted app reads the exact current title, safely derives the subject, and—only when needed—applies one title to the calling task. The exact returned task ID and title must match. A failure stays local and is never retried.

`uninstall --dry-run --json` asks once for permission to enumerate the complete unarchived App Server catalog and reports which exact ThreadBear prefixes will be removed before managed artifacts. After consent, `uninstall --prepare --noninteractive --confirm --json` takes one fresh complete snapshot. The mounted app rereads each prepared task and makes at most one exact prefix-removal write, with the initiating task last. Any drift or unconfirmed result stops before artifact removal; a fresh rerun makes a new plan. Successful cleanup is followed by `uninstall --commit --noninteractive --confirm --json`, with no final rescan or title call. A bare confirmed uninstall is refused.

## Boundaries

Ordinary turns use only mounted Codex reads and writes, so they work under Codex's default workspace permissions. Installation's explicitly permissioned ephemeral first read and explicitly approved uninstall cleanup use short-lived official Codex processes launched from a fixed Desktop executable path, never repository `PATH`. The first read uses deterministic footer parsing before fixed sequential Luna-medium batches; every hosted classifier capability is disabled and its event trace rejects tool activity. Its local helper remains read-only and mounted Codex tools perform every title write. ThreadBear does not open Codex SQLite, edit Desktop caches or task prose, archive tasks, retry title writes, or maintain a database, queue, controller, repair pass, or persistent management task.

The daily LaunchAgent does one job: check for a verified official update. Network and candidate-verification failures leave the old install untouched. A later managed-surface write can produce a truthful rerunnable partial, with the binary written last. Successful updates report whether Codex must restart. Updater health is separate from title-core `ready`; it never reads tasks or changes titles.

See [architecture](docs/architecture.md), [compatibility](docs/compatibility.md), and the [status convention](docs/status-convention.md).
