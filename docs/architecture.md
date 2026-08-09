# Architecture

ThreadBear is one Go executable, one managed instruction block, one installed skill, small private lifecycle/update state, and one daily update-only LaunchAgent. It has no per-task database, persistent management task, controller, classifier, archive system, detached writer, queue, or global failure state.

## Ordinary turn

1. The task writes its substantive response. Any owner or next action stays in that prose.
2. Immediately before the final response, managed guidance runs one terminal JavaScript cell containing `threadbear title --status <enum> --json`.
3. That stateless command validates the status and current task ID, then returns the fixed icon and safety policy. It starts no App Server and writes no state.
4. The mounted Codex app reads the exact calling task. The cell rejects a wrong ID, blank or unsafe title, raw internal text, an ambiguous old ThreadBear prefix, or a title that cannot fit intact.
5. The cell strips at most one of ThreadBear's five current status prefixes or the obsolete neutral bear prefix, preserves every other subject byte, and renders the selected status icon. If the title already matches, it stops. Otherwise it calls mounted `set_thread_title` once with no explicit task ID.
6. Success requires the exact returned task ID and title. A throw, malformed response, or mismatch stays local. The task never starts another cell, polls the title, retries, or reconciles.

The enum controls only the icon and can emit exactly `✅ `, `➡️ `, `🙋 `, `🚨 `, or `🤖 `. ThreadBear also recognizes neutral `🐻 ` as a removable legacy prefix but never emits it. A title beginning with one of those exact prefixes is deliberately ambiguous. The obsolete `➡ `, `⏳ `, `❔ `, and `🧵🐻` forms are also ambiguous after a clean v2 reset, so ThreadBear leaves the complete title unchanged rather than guessing whether its leading emoji is user-authored. Every other leading emoji remains user text.

Codex has no compare-and-swap title primitive. The mounted read and possible write remain in one terminal cell, and uninstall cleanup rereads each historical target immediately before its one possible write. If live canaries show practical corruption or response blocking, rewriting is disabled instead of wrapped in reconciliation machinery.

## Native boundaries

Mounted Codex tools are the only ordinary title reader and the sole title writer. Current-task writes omit `threadId`; uninstall cleanup writes carry one explicit target. Tool results normally arrive as raw JSON text, so managed cells decode one layer while retaining object compatibility. Exact returned ID/title is the acknowledgement; release acceptance still verifies the mounted header and sidebar.

The official `codex app-server --stdio` process is used only for complete-catalog uninstall cleanup. ThreadBear launches it from a fixed Codex Desktop path, never ambient repository `PATH`, initializes one bounded client, follows every unarchived `thread/list` page, tolerates notifications, deduplicates IDs, and closes it. Native `name` is the user-facing title. Null or blank names stay raw; `preview` is never adopted.

Ordinary turns therefore work under Codex's default workspace permissions. Uninstall cleanup asks for one explicit command permission because App Server maintains Codex's own local state outside the workspace. ThreadBear never opens Codex SQLite, edits Desktop caches, runs an App Server daemon, keeps a shared client, uses a model, or falls back to another title source.

## Uninstall title cleanup

`uninstall --dry-run --json` enumerates the complete catalog without mutation and reports `total`, `needs_cleanup`, `unchanged`, `skipped`, and per-item reasons alongside the artifact preview. Enumeration or protocol failure means zero title or filesystem writes.

After separate consent, `uninstall --prepare --noninteractive --confirm --json` takes one fresh complete snapshot and returns one `prepared` action with snapshot `title` and undecorated `desired_title` for every safe owned prefix. The initiating task is last. Preparation stores no titles, performs no per-target App Server read, and writes no Codex title.

The installed skill resumes the same preparation process if it yields. It then serially reads each prepared target through the mounted app immediately before any write. A missing task, wrong ID, or drift blocks teardown. An exact match receives at most one setter call. Only exact returned target ID/title counts as `updated`; every other setter result is `unconfirmed` and also blocks teardown. If every prepared row succeeds, the same cell runs `uninstall --commit --noninteractive --confirm --json`. A bare confirmed uninstall is refused, so the ordinary CLI path cannot skip preparation. A failure gets one fresh-rerun action—there is no retry, final inventory scan, controller, queue, marker, or resume state.

## Installation, reset, updates, and uninstall

Fresh installation writes the executable, lifecycle/update state, managed guidance, skill, and daily updater. Candidate self-test requires macOS and Codex Desktop 0.146.0 or newer from a fixed supported path. Codex restarts once so open tasks load the guidance.

Version 2.2.1 uses an explicit clean reset. The guide verifies and removes only the exact old automation, unpins the exact former persistent task without renaming it, removes exact obsolete hook entries, imports no state, and performs no heuristic title cleanup.

The daily LaunchAgent runs only `threadbear update`. It validates origin, architecture, checksum, version, Codex compatibility, and candidate self-test before replacement. Network and verification failures leave the old install untouched; later local failures report a rerunnable partial, with the binary written last. The updater never reads tasks or changes titles.

Uninstall first removes one current owned prefix from every prepared safe unarchived title through the mounted writer. Plain, unsafe, ambiguous, user-owned, and archived titles remain unchanged. Only after every prepared write returns the exact target and title does it remove the executable, lifecycle/update state, managed guidance, skill, and LaunchAgent, preserving neighbors and removing the binary last. Once removal commits, the task asks for a Codex restart and makes no title call.
