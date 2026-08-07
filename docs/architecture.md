# Architecture

ThreadBear is one Go executable, private per-task subject records, one managed instruction block, one installed skill, and one daily update-only LaunchAgent. It has no persistent management task, controller, classifier, archive system, detached writer, queue, or global migration state.

## Ordinary turn

1. The task completes its work and writes the substantive response. Any owner or next action stays in that prose.
2. Immediately before the final response, managed guidance runs exactly one local command: `threadbear title --status <enum> --json`.
3. The binary requires the current task ID from Codex's environment, starts one bounded official App Server process, reads the exact current title, resolves the exact safe subject, and renders one status icon plus that subject.
4. If the title already matches, the command returns unchanged. Otherwise it sends at most one `thread/name/set` request and rereads the title. Only exact readback is confirmed.
5. The App Server process exits. Failure, timeout, or uncertain acknowledgement is reported locally; the task does not poll, retry, reconcile, or delay its response.

The enum controls only the icon. The command carries no owner, action, or rewritten task description.

## Subject ownership

State is keyed by task ID and stores only the exact subject needed to recognize ThreadBear's renderings. There is no stored status, action, original title, pending proposal, controller phase, global failure, or repair marker.

For one title command:

1. If a subject is stored and the current title byte-matches a valid ThreadBear icon plus that subject, reuse it.
2. Otherwise, treat the exact current title as a user rename when it is safe.
3. Reject blank, multiline, control-bearing, raw internal-envelope, ambiguous unowned legacy-prefixed, or overlong text. Rejection leaves that title unchanged.
4. Persist the chosen exact subject and render one icon plus that subject.

Subjects are never normalized, stripped, or truncated. User-authored leading emoji and arrows survive as subject bytes. ThreadBear owns only its exact rendering.

Codex provides no compare-and-swap title primitive. ThreadBear narrows the race with an immediate read/write/readback sequence and no retries. A later safe user rename is adopted on the next turn. If live canaries show practical corruption or response blocking, rewriting is disabled rather than wrapped in reconciliation machinery.

## App Server boundary

The official `codex app-server --stdio` process is the only task read/write authority. ThreadBear initializes one short-lived client, correlates JSON-RPC response IDs while tolerating notifications, and closes it after the bounded operation.

Current-task writing reads the exact native name, sends at most one `thread/name/set`, and performs exact readback. Onboarding first follows every unarchived `thread/list` page, deduplicates task IDs, and returns no plan unless enumeration completes. Native `name` is the user-facing title. A null or blank name is raw and unowned; `preview` is never adopted, persisted, or rendered.

ThreadBear does not open Codex SQLite, edit Desktop caches, run an App Server daemon, keep a shared client, use a model, or fall back to another title source.

## Onboarding

`onboard --dry-run --json` is the only read-only onboarding mode. It enumerates the complete catalog before any mutation and reports `total`, `safe`, `needs_update`, and per-item reasons. Enumeration or protocol failure means zero writes.

Mutation requires exact explicit consent through `onboard --noninteractive --confirm --json`. The binary starts from a fresh complete snapshot, skips the active caller and unsafe rows, and handles every safe target serially with no cap or waves. Immediately before a possible write it rereads the target and requires its byte-exact snapshot title. Missing, unreadable, drifted, ambiguous, or overlong targets are skipped. Each target receives at most one neutral `🐻 <exact subject>` write and is counted as updated only after exact readback. An acknowledgement without exact readback is unconfirmed and is never retried.

An interrupted pass may leave valid partial decoration. A rerun takes a fresh complete snapshot and continues without a controller, worker task, pending queue, or hidden resume state.

## Installation, reset, and uninstall

Fresh installation writes only the current core artifacts and updater. Codex must restart before open tasks load the new managed guidance. Installation offers onboarding; it never creates a ThreadBear task.

Version 2.2.1 is a clean reset, not a state migration. The preview exposes the old main-task ID and complete automation fingerprint. After explicit consent, the guide verifies and deletes only that automation, then unpins and verifies the exact former persistent task without renaming it. Any mismatch aborts before filesystem reset. The reset removes only exact obsolete ThreadBear Pre/Post title-interception entries, preserves foreign entries and order, imports no old state, and performs no heuristic title cleanup.

Uninstall removes ThreadBear-owned files, managed guidance, subject records, and LaunchAgent without waiting for titles to converge. Historical icons may remain. Once removal commits, the task does not run the title command and asks for a Codex restart.

## Verified updates

The daily LaunchAgent runs only `threadbear update`. The updater selects the Darwin architecture from the official release manifest and validates release origin, checksum, embedded version, and candidate self-test before local installation. Network and verification failures happen before writes and leave the old install untouched. Managed surfaces are written individually, with the binary last; a local failure may truthfully report a rerunnable partial. Successful update JSON includes `restart_required`. It never reads tasks or changes titles.

`status` computes title-core `ready` from the binary, subject store, managed guidance, and skill. It reports the LaunchAgent separately. Missing automatic updates do not globally fail ordinary title handling.
