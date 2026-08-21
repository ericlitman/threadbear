# Architecture

ThreadBear is one Go executable, one managed instruction block, one installed skill, small private lifecycle/update state, and one daily update-only LaunchAgent. It has no per-task database, persistent management task, controller, recurring classifier, archive system, detached writer, queue, or global failure state.

## Ordinary turn

1. The task writes its substantive response. Any owner or next action stays in that prose.
2. Immediately before the final response, managed guidance runs one small terminal JavaScript loader containing `threadbear title-script --status <enum>`.
3. That stateless command validates the status and current task ID, binds the fixed icon and safety policy into the reviewed JavaScript program embedded in the verified binary, and prints only that program. It starts no App Server and writes no state.
4. Codex evaluates the emitted program inside the current in-app tool context. The mounted app reads the exact calling task; the program rejects a wrong ID, blank or unsafe title, raw internal text, an ambiguous old ThreadBear prefix, or a title that cannot fit intact.
5. The program strips at most one of ThreadBear's five current status prefixes, five inferred `✦` prefixes, or the obsolete neutral bear prefix, preserves every other subject byte, and renders the selected plain status icon. If the title already matches, it stops. Otherwise it calls mounted `set_thread_title` once with no explicit task ID.
6. Success requires the exact returned task ID and title. A command failure, throw, malformed response, or mismatch stays local. The task never starts another cell, caches program source, polls the title, retries, or reconciles.

The enum controls only the icon and can emit exactly `✅ `, `➡️ `, `🙋 `, `🚨 `, or `🤖 `. ThreadBear also recognizes the five `✦` first-read prefixes and neutral `🐻 ` as removable decorations but never emits either during an ordinary turn. A title beginning with one of those exact prefixes is deliberately reserved. The obsolete `➡ `, `⏳ `, `❔ `, and `🧵🐻` forms are ambiguous after a clean v2 reset, so ThreadBear leaves the complete title unchanged rather than guessing whether its leading emoji is user-authored. Every other leading emoji remains user text.

## Existing-task first read

After installation verification, one in-app JavaScript cell emits a validated handoff and yields so the user receives the friendly installed recap before the pass finishes. That recap explains the one permission request that follows and makes icon progress conditional on approval. The same cell requests complete-catalog read permission and starts one hidden read-only `onboard-stream` process. Declining leaves existing titles untouched without changing installation health. The process fully paginates the unarchived catalog, excludes the installing task and every decorated, raw, blank, unsafe, or ambiguous title, and requests only the newest turn. It uses only bounded user and final-assistant text; all other turn items are ignored.

An exact final-line historical ThreadBear footer maps directly to a plain status. Remaining completed turns carry bounded latest user and final-assistant text into fixed batches of eight, processed sequentially by ephemeral `gpt-5.6-luna` at medium reasoning. User config and rules are ignored; every hosted feature is disabled; request-input is disabled; shell and the code-mode host are disabled. The JSON event trace must contain one final assistant message matching the schema output and no tool activity or runtime error. The schema returns only task ID and one of five statuses or `unknown`. Wrong, missing, duplicate, reordered, malformed, tool-attempting, failed, or timed-out batch output makes that entire batch unknown without retry. Incomplete turns and missing evidence are unknown before the model.

The helper emits ordered JSON Lines with snapshot ID, title, status, and exact/inferred provenance; it never writes a title. The yielded cell strictly validates the stream, rereads each candidate through mounted Codex, and makes at most one explicit-target mounted setter call for a still-matching semantic row. Exact provenance renders a plain prefix, inferred provenance renders the corresponding `✦` prefix, and unknown renders nothing. Drift and unconfirmed writes stay local to one row. The cell and processes exit after the pass, persisting no task state; a later install simply skips titles already decorated.

Codex has no compare-and-swap title primitive. The mounted read and possible write remain in one terminal cell, and uninstall cleanup rereads each historical target immediately before its one possible write. If live canaries show practical corruption or response blocking, rewriting is disabled instead of wrapped in reconciliation machinery.

## Native boundaries

Mounted Codex tools are the only ordinary title reader and the sole title writer. Current-task writes omit `threadId`; uninstall cleanup writes carry one explicit target. Tool results normally arrive as raw JSON text, so managed cells decode one layer while retaining object compatibility. Exact returned ID/title is the acknowledgement; release acceptance still verifies the mounted header and sidebar.

The official `codex app-server --stdio` process is used for the installation first read and complete-catalog uninstall cleanup. ThreadBear launches it from a fixed Codex Desktop path, never ambient repository `PATH`, initializes one bounded client, follows every unarchived `thread/list` page, tolerates notifications, deduplicates IDs, and closes it. Onboarding additionally reads only `thread/turns/list` with a one-turn descending limit. Native `name` is the user-facing title. Null or blank names stay raw; `preview` is never adopted.

Ordinary turns therefore work under Codex's default workspace permissions. The local Go process cannot access mounted app tools; emitting the embedded program keeps the actual title read and write inside Codex without making the model reproduce that program on every turn. Uninstall cleanup asks for one explicit command permission because App Server maintains Codex's own local state outside the workspace. ThreadBear never opens Codex SQLite, edits Desktop caches, runs an App Server daemon, keeps a shared client, caches executable policy, retries a model call, or falls back to another title source.

## Uninstall title cleanup

`uninstall --dry-run --json` enumerates the complete catalog without mutation and reports `total`, `needs_cleanup`, `unchanged`, `skipped`, and per-item reasons alongside the artifact preview. Enumeration or protocol failure means zero title or filesystem writes.

After separate consent, `uninstall --prepare --noninteractive --confirm --json` takes one fresh complete snapshot and returns one `prepared` action with snapshot `title` and undecorated `desired_title` for every safe owned prefix. The initiating task is last. Preparation stores no titles, performs no per-target App Server read, and writes no Codex title.

The installed skill resumes the same preparation process if it yields. It then serially reads each prepared target through the mounted app immediately before any write. A missing task, wrong ID, or drift blocks teardown. An exact match receives at most one setter call. Only exact returned target ID/title counts as `updated`; every other setter result is `unconfirmed` and also blocks teardown. If every prepared row succeeds, the same cell runs `uninstall --commit --noninteractive --confirm --json`. A bare confirmed uninstall is refused, so the ordinary CLI path cannot skip preparation. A failure gets one fresh-rerun action—there is no retry, final inventory scan, controller, queue, marker, or resume state.

The installed skill is the trusted local lifecycle orchestrator; `--commit` is its explicit internal phase attestation, not a security boundary against deliberate local CLI misuse. Cleanup guarantees exact handling of the prepared snapshot, not a freeze against later user or concurrent Codex title writes. Process handshakes, persisted proof, and cross-task writer coordination are outside this lifecycle.

## Installation, reset, updates, and uninstall

Fresh installation writes the executable, lifecycle/update state, managed guidance, skill, and daily updater. Candidate self-test requires macOS and Codex Desktop 0.147.0 or newer from a fixed supported path. After verification and the friendly handoff, the one existing-task first read runs in the background; it is not part of core readiness. Codex restarts once so open tasks load the guidance.

Version 2.2.1 uses an explicit clean reset. The guide verifies and removes only the exact old automation, unpins the exact former persistent task without renaming it, removes exact obsolete hook entries, imports no state, and performs no heuristic title cleanup.

The daily LaunchAgent runs only `threadbear update`. It validates origin, architecture, checksum, version, Codex compatibility, and candidate self-test before replacement. Network and verification failures leave the old install untouched; later local failures report a rerunnable partial, with the binary written last. The updater never reads tasks or changes titles.

Uninstall first removes one current owned prefix from every prepared safe unarchived title through the mounted writer. Plain, unsafe, ambiguous, user-owned, and archived titles remain unchanged. Only after every prepared write returns the exact target and title does it remove the executable, lifecycle/update state, managed guidance, skill, and LaunchAgent, preserving neighbors and removing the binary last. Once removal commits, the task asks for a Codex restart and makes no title call.
