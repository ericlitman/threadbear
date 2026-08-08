# Architecture

ThreadBear is one Go executable, one managed instruction block, one installed skill, small private lifecycle/update state, and one daily update-only LaunchAgent. It has no per-task database, persistent management task, controller, classifier, archive system, detached writer, queue, or global failure state.

## Ordinary turn

1. The task writes its substantive response. Any owner or next action stays in that prose.
2. Immediately before the final response, managed guidance runs one terminal JavaScript cell containing `threadbear title --status <enum> --json`.
3. That stateless command validates the status and current task ID, then returns the fixed icon and safety policy. It starts no App Server and writes no state.
4. The mounted Codex app reads the exact calling task. The cell rejects a wrong ID, blank or unsafe title, raw internal text, an ambiguous old ThreadBear prefix, or a title that cannot fit intact.
5. The cell strips at most one of ThreadBear's six reserved leading icon prefixes, preserves every other subject byte, and renders the selected icon. If the title already matches, it stops. Otherwise it calls mounted `set_thread_title` once with no explicit task ID.
6. Success requires the exact returned task ID and title. A throw, malformed response, or mismatch stays local. The task never starts another cell, polls the title, retries, or reconciles.

The enum controls only the icon. ThreadBear reserves `✅ `, `➡️ `, `🙋 `, `🚨 `, `🤖 `, and neutral onboarding `🐻 ` as its visible ownership boundary. A title beginning with one of those current prefixes is deliberately ambiguous. The obsolete `➡ `, `⏳ `, `❔ `, and `🧵🐻` forms are also ambiguous after a clean v2 reset, so ThreadBear leaves the complete title unchanged rather than guessing whether its leading emoji is user-authored. Every other leading emoji remains user text.

Codex has no compare-and-swap title primitive. The mounted read and possible write remain in one terminal cell, and onboarding rereads each historical target immediately before its one possible write. If live canaries show practical corruption or response blocking, rewriting is disabled instead of wrapped in reconciliation machinery.

## Native boundaries

Mounted Codex tools are the only ordinary title reader and the sole title writer. Current-task writes omit `threadId`; onboarding writes carry one explicit target. Tool results normally arrive as raw JSON text, so managed cells decode one layer while retaining object compatibility. Exact returned ID/title is the acknowledgement; release acceptance still verifies the mounted header and sidebar.

The official `codex app-server --stdio` process is used only for complete-catalog onboarding. ThreadBear launches it from a fixed Codex Desktop path, never ambient repository `PATH`, initializes one bounded client, follows every unarchived `thread/list` page, tolerates notifications, deduplicates IDs, and closes it. Native `name` is the user-facing title. Null or blank names stay raw; `preview` is never adopted.

Ordinary turns therefore work under Codex's default workspace permissions. Onboarding asks for one explicit command permission because App Server maintains Codex's own local state outside the workspace. ThreadBear never opens Codex SQLite, edits Desktop caches, runs an App Server daemon, keeps a shared client, uses a model, or falls back to another title source.

## Onboarding

`onboard --dry-run --json` enumerates the complete catalog without ThreadBear mutation and reports `total`, `safe`, `needs_update`, and per-item reasons. Enumeration or protocol failure means zero title calls.

After separate consent, `onboard --noninteractive --confirm --json` takes a fresh complete snapshot, skips the active caller and unsafe rows, and returns one `prepared` action with snapshot `title` and `desired_title` for every eligible target. It stores no titles, performs no per-target App Server read, and writes no Codex title.

The installed skill resumes the same preparation process if it yields. It then serially reads each prepared target through the mounted app immediately before any write. A missing task, wrong ID, or drift is `skipped`. An exact match receives at most one setter call. Only exact returned target ID/title counts as `updated`; every other setter result is `unconfirmed` and is never retried. A rerun takes a fresh complete snapshot—there is no controller, wave, queue, or resume state.

## Installation, reset, updates, and uninstall

Fresh installation writes the executable, lifecycle/update state, managed guidance, skill, and daily updater. Candidate self-test requires macOS and Codex Desktop 0.146.0 or newer from a fixed supported path. Codex restarts once so open tasks load the guidance.

Version 2.2.1 uses an explicit clean reset. The guide verifies and removes only the exact old automation, unpins the exact former persistent task without renaming it, removes exact obsolete hook entries, imports no state, and performs no heuristic title cleanup.

The daily LaunchAgent runs only `threadbear update`. It validates origin, architecture, checksum, version, Codex compatibility, and candidate self-test before replacement. Network and verification failures leave the old install untouched; later local failures report a rerunnable partial, with the binary written last. The updater never reads tasks or changes titles.

Uninstall removes the executable, lifecycle/update state, managed guidance, skill, and LaunchAgent without waiting for titles to converge. It also removes any obsolete v3.0.0 subject records it owns while preserving neighbors. Historical icons may remain. Once removal commits, the task asks for a Codex restart and does not run the title cell.
