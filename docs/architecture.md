# Architecture

ThreadBear is one Go executable, private per-task subject records, one managed instruction block, one installed skill, and one daily update-only LaunchAgent. It has no persistent management task, controller, classifier, archive system, detached writer, queue, or global migration state.

## Ordinary turn

1. The task completes its work and writes the substantive response. Any owner or next action stays in that prose.
2. Immediately before the final response, managed guidance runs one terminal JavaScript cell containing `threadbear title --status <enum> --json`.
3. The binary requires the current task ID, starts one bounded official App Server process, reads the exact native title, resolves and records the safe subject, and returns `desired_title` plus `write_required`. It never writes a task title.
4. If no write is required, the cell exits. Otherwise it calls the mounted Codex app's native `set_thread_title` once with the prepared title and no explicit task ID, so only the calling task can be targeted.
5. The cell accepts success only when the native result returns the planner's exact task ID and desired title. A returned failure, malformed output, or mismatch stays local. If the outer cell yields after 30 seconds, the task waits only for that same running cell; the yield is not cancellation, so a slow native call can delay the response. The task never starts another cell, polls the title, retries, or reconciles.

The enum controls only the icon. Neither the planner nor the native call carries an owner, action, or rewritten task description.

## Subject ownership

State is keyed by task ID and stores only the exact subject needed to recognize ThreadBear's renderings. There is no stored status, action, original title, pending proposal, controller phase, global failure, or repair marker.

For one title plan:

1. If a subject is stored and the current title byte-matches a valid ThreadBear icon plus that subject, reuse it.
2. Otherwise, treat the exact current title as a user rename when it is safe.
3. Reject blank, multiline, control-bearing, raw internal-envelope, ambiguous unowned legacy-prefixed, or overlong text. Rejection leaves that title unchanged.
4. Persist the chosen exact subject and render one icon plus that subject.

Subjects are never normalized, stripped, or truncated. User-authored leading emoji and arrows survive as subject bytes. ThreadBear owns only its exact rendering.

Codex provides no compare-and-swap title primitive. ThreadBear keeps the planner-to-native-call interval to one terminal cell and never retries. A later safe user rename is adopted on the next turn. If live canaries show practical corruption or response blocking, rewriting is disabled instead of wrapped in reconciliation machinery.

## Native boundaries

The official `codex app-server --stdio` process is ThreadBear's read and planning authority only. The binary initializes one short-lived client, correlates JSON-RPC response IDs while tolerating notifications, and closes it before returning a plan. It contains no `thread/name/set` path.

The mounted Codex app's native `set_thread_title` tool is the sole title writer. Current-task calls omit `threadId`; onboarding calls carry one explicit prepared target. Mounted tool results normally arrive as raw JSON text; managed cells decode that text once, also accept already-decoded objects, and reject malformed or non-object results. Exact returned task ID and title are the acknowledgement. Release acceptance still requires the mounted header and sidebar to render that title.

Onboarding follows every unarchived `thread/list` page and deduplicates task IDs before any preparation. Native `name` is the user-facing title. A null or blank name is raw and unowned; `preview` is never adopted, persisted, or rendered.

ThreadBear does not open Codex SQLite, edit Desktop caches, run an App Server daemon, keep a shared client, use a model, or fall back to another title source.

## Onboarding

`onboard --dry-run --json` enumerates the complete catalog without mutation and reports `total`, `safe`, `needs_update`, and per-item reasons. Enumeration or protocol failure means zero writes.

Mutation requires explicit consent. `onboard --noninteractive --confirm --json` takes a fresh complete snapshot, skips the active caller and unsafe rows, stores the safe subject, and returns one `prepared` action containing the snapshot `title` and `desired_title` for each eligible target. The binary performs no per-target app read and writes no Codex title.

The installed skill runs preparation and the native pass in one managed JavaScript cell. If the preparation process yields, the cell resumes that same process with `write_stdin`; it never starts another command. Immediately before each possible write, the cell serially reads the prepared target through the mounted app and requires the returned task ID and current title to equal the prepared ID and snapshot `title`. A read failure, wrong ID, or drift is `skipped` and receives no write. An exact match receives at most one native title call. Only an exact returned target ID and desired title counts as `updated`; a throw, undecodable or non-object response, or mismatch is `unconfirmed` and is never retried.

The final receipt reports the complete catalog and `updated`, `skipped`, `unchanged`, and `unconfirmed` counts. Every prepared item must reach exactly one deliberate outcome. ThreadBear is ready only when all prepared items are accounted for and no call is unconfirmed; skipped tasks are honestly left unchanged.

An interrupted pass may leave valid partial decoration. A rerun takes a fresh complete snapshot and continues without a controller, worker task, pending queue, or hidden resume state.

## Installation, reset, and uninstall

Fresh installation writes only the current core artifacts and updater. Codex must restart before open tasks load the new managed guidance. Installation offers onboarding; it never creates a ThreadBear task.

Version 2.2.1 is a clean reset, not a state migration. The preview exposes the old main-task ID and complete automation fingerprint. After explicit consent, the guide verifies and deletes only that automation, then unpins and verifies the exact former persistent task without renaming it. Any mismatch aborts before filesystem reset. The reset removes only exact obsolete ThreadBear title-hook entries, preserves foreign entries and order, imports no old state, and performs no heuristic title cleanup.

Uninstall removes ThreadBear-owned files, managed guidance, subject records, and LaunchAgent without waiting for titles to converge. Historical icons may remain. Once removal commits, the task does not run the title cell and asks for a Codex restart.

## Verified updates

The daily LaunchAgent runs only `threadbear update`. It validates release origin, architecture, checksum, version, and candidate self-test before local installation. Network and verification failures happen before writes and leave the old install untouched. Managed surfaces are written individually, with the binary last; a local failure may truthfully report a rerunnable partial. Successful update JSON includes `restart_required`. The updater never reads tasks or changes titles.

`status` computes title-core `ready` from the binary, subject store, managed guidance, and skill. It reports the LaunchAgent separately. Missing automatic updates do not globally fail ordinary title handling.
