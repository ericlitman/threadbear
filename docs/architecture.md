# Architecture

ThreadBear is one small Go executable, one private atomic JSON file, one managed instruction block, one installed skill, and two Codex hook entries. It has no LaunchAgent or background writer.

## Ordinary turn

1. Managed guidance makes the native current-task title setter the turn's first action with `⏳ ThreadBear is working: <concise subject>` and no explicit task ID. The same model already answering the user supplies the subject; ThreadBear adds no model call.
2. `PreToolUse` reads the current title, explicit name, and first message from the local Codex index. It preserves an explicit name, a generated short title, exact prior ownership, or a later user rename. Only when an unowned title is still the raw or truncated first message does it adopt the reserved subject handoff. A missing or malformed handoff fails closed.
3. `PostToolUse` accepts only the exact returned task ID and rewritten title, then commits the subject and rendering.
4. Immediately before the final response, the task calls the same native setter with its exact ThreadBear footer. The hooks expand and commit the matching terminal title; the response ends with that footer.

Each title moment may be retried once. There is no Stop hook: an interrupted turn keeps its running title until the next ordinary turn naturally replaces it.

## Ownership and state

The canonical title is `<status icon> <user-owned subject>[ → <managed action>]`. ThreadBear owns only a leading status and action suffix from its last exact committed rendering. Any different current title is a user rename and becomes the complete subject, even if it contains an icon or arrow. A first-call seed is ignored after ownership exists.

State is keyed by task ID and contains the persistent main-task ID, the single migration-controller ID, one migration phase, the canonical subject, the last verified rendering, and at most one pending proposal. A pending proposal lets a later call recognize setter success when Post was lost. State is private, locked, and atomically replaced. Ordinary title proposals are never queued for later repair.

The visible title is limited to 60 UTF-16 units. Rendering truncates displayed subject before displayed action without changing canonical state.

## Installation and migration

Installation writes the binary, state, guidance, skill, and two hook entries while preserving unrelated managed files and hook order. The initiating task is recorded as the persistent ThreadBear home, then a fresh-task canary proves the native boundary. That task creates exactly one ephemeral migration controller and returns promptly; the controller inventories native-addressable unarchived Desktop and CLI tasks, excludes rollout-only internal records plus the main/controller IDs, classifies exact footers deterministically, and uses Luna medium only for genuinely ambiguous history. Immediately before each explicit-target native write, the hook re-reads the target and adopts any newer rename.

Migration is rerunnable from the same controller ID and skips only inventory rows proven `applied: true` from exact committed ownership state. Native writes are serial. A timeout or unknown result records `migration_failed` until authoritative reconciliation; only a final zero-remaining inventory may record `migration_complete`. The persistent task never performs, awaits, or polls the migration. Rendered active-header and sidebar verification belongs in release QA.
