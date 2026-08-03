# Architecture

ThreadBear is one small Go executable, one private atomic JSON file, one managed instruction block, one installed skill, and two Codex hook entries. It has no LaunchAgent or background writer.

## Ordinary turn

1. Managed guidance makes one bounded native current-task title attempt the turn's first action with `⏳ ThreadBear is working: <concise subject>` and no explicit task ID. The same model already answering the user supplies the subject; ThreadBear adds no model call. A four-second outer timer bounds the complete call while each installed hook has a one-second process limit.
2. `PreToolUse` reads the current title, explicit name, and first message from the local Codex index. It preserves an explicit name, a generated short title, exact prior ownership, or a later user rename. Only when an unowned title is still the raw or truncated first message does it adopt the reserved subject handoff. A missing or malformed handoff fails closed.
3. `PostToolUse` accepts only the exact returned task ID and rewritten title, then commits the subject and rendering.
4. Immediately before the final response, the task makes the same bounded native attempt with its exact ThreadBear footer. The hooks expand and commit the matching terminal title; the response ends with that footer.

Each title moment makes exactly one native attempt. A timeout leaves the write result unknown, so the turn does not retry or await the abandoned promise. There is no Stop hook: an interrupted turn keeps its running title until the next ordinary turn naturally replaces it.

## Ownership and state

The canonical title is `<status icon> <user-owned subject>[ → <managed action>]`. ThreadBear owns only a leading status and action suffix from its last exact committed rendering. Any different current title is a user rename and becomes the complete subject, even if it contains an icon or arrow. A first-call seed is ignored after ownership exists.

State is keyed by task ID and contains the persistent main-task ID, the single migration-controller ID, one migration phase, the canonical subject, the last verified rendering, and at most one pending proposal. A pending proposal lets a later call recognize setter success when Post was lost. State is private, locked, and atomically replaced. Ordinary title proposals are never queued for later repair.

The visible title is limited to 60 UTF-16 units. Rendering first computes the bounded standalone status-and-subject display, then truncates or omits only the appended action without changing canonical state.

The persisted main task may request one reserved cleanup marker for an explicit target. The same Pre/Post transaction re-reads the target, removes every consecutive leading ThreadBear status mark, stages the subject-only title, validates the exact native result, and repairs ownership state. The marker is denied for every other caller. Guided uninstall uses this serial operation for all active titles before deleting the hooks or ownership state; the same operation is available on demand from the persistent task.

## Installation and migration

Installation writes the binary, state, guidance, skill, and two hook entries while preserving unrelated managed files and hook order. It records `migration_pending` until a real controller ID is persisted, so an interrupted pre-controller install is never described as running. Ordinary guided installation trusts deterministic self-test and inventory evidence; Desktop visual inspection and computer control are debug/release-canary tools, never an end-user gate. The initiating task is recorded as the persistent ThreadBear home and stays selected while it creates and supervises exactly one background migration controller to a terminal phase. The controller inventories native-addressable unarchived Desktop and CLI tasks, excludes rollout-only internal records plus the main/controller IDs, and immediately begins a bounded deterministic title batch before fixed-surface Luna worker creation. It classifies exact footers deterministically and uses adaptive waves of read-only Luna-medium workers only for genuinely ambiguous history, with `❔ ThreadBear could not classify` as the exact hook-accepted unknown proposal. It retains and awaits every successfully spawned worker even when a later spawn reaches collaboration capacity; worker results may complete out of order, but explicit native title writes remain serial and deterministic work never waits on ambiguous classification. Immediately before each write, the hook re-reads the target and adopts any newer rename. When an ownerless migration title begins with prior ThreadBear status marks, the controller boundary removes those marks before rendering so reinstall cannot compound decoration.

Migration is rerunnable from the same controller ID and skips only inventory rows proven `applied: true` from exact committed ownership state. Native writes are serial. A timeout or unknown result records `migration_failed` until authoritative reconciliation; only a final zero-remaining inventory may record `migration_complete`. The persistent task supervises progress without becoming a second writer. Status repairs an older running-without-controller state to pending. Each real running transition records its attempt start; status reconciles a missing controller or a later terminal lifecycle event from stale `migration_running` to `migration_failed` without using age or a prior attempt as a failure signal. Rendered active-header and sidebar verification belongs in opt-in release QA.
