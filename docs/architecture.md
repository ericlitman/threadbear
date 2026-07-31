# Architecture

ThreadBear is one small Go executable, one private atomic JSON file, one managed instruction block, one installed skill, and two Codex hook entries.

## Ordinary turn

1. Managed guidance makes the native current-task title setter the turn's first action with the compact input `⏳ ThreadBear is working` and no explicit task ID.
2. `PreToolUse` reads the calling task's current title from the local Codex index, preserves the stable user-owned subject, records one pending proposal, and rewrites the title input to `⏳ <subject>`.
3. `PostToolUse` accepts only the exact returned task ID and rewritten title, then commits the subject and rendering.
4. Immediately before the final response, the task calls the same native setter with its exact ThreadBear footer. The hooks expand and commit the matching terminal title; the response ends with that footer.

Each title moment may be retried once. There is no Stop hook: an interrupted turn keeps its running title until the next ordinary turn naturally replaces it.

## Ownership and state

The canonical title is `<status icon> <user-owned subject>[ → <managed action>]`. ThreadBear owns only a leading status and action suffix from its last exact committed rendering. Any different current title is a user rename and becomes the complete subject, even if it contains an icon or arrow.

State is keyed by task ID and contains the canonical subject, last verified rendering, and at most one pending proposal. A pending proposal lets a later call recognize setter success when Post was lost. State is private, locked, and atomically replaced.

The visible title is limited to 60 UTF-16 units. Rendering truncates displayed subject before displayed action without changing canonical state.

## Installation and migration

Installation writes the binary, state, guidance, skill, and two hook entries while preserving unrelated managed files and hook order. A foreground Codex task inventories all local unarchived tasks, classifies exact footers deterministically, and uses Luna medium only for genuinely ambiguous v2 history. Immediately before each explicit-target native write, the helper re-reads the target and adopts any newer rename.

Migration is rerunnable from scratch and skips only inventory rows proven `applied: true` from exact committed ownership state. It persists no migration workflow. Rendered active-header and sidebar verification belongs in release QA.
