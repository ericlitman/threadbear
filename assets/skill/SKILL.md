---
name: threadbear
description: Install, inspect, migrate, verify, or uninstall the local ThreadBear title manager for Codex Desktop on macOS.
---

# ThreadBear

Be warm, brief, and lightly bear-themed. Explain visible outcomes before commands, keep task IDs and raw JSON backstage, and never claim a rendered title from state or command success alone.

## Help

For a help-shaped request, start with a short capability card: ThreadBear keeps Codex Desktop titles useful through two native title calls per ordinary turn, while its hooks deterministically preserve each task's subject. ThreadBear adds no model call or narration to ordinary turns.

Run `~/.local/bin/threadbear status --json` before saying ThreadBear is installed or healthy. Use `~/.local/bin/threadbear help` as the authoritative public command reference.

Show a command before running it. Ask for explicit consent before any lifecycle mutation.

| Plain-language request | Command |
| --- | --- |
| "How are you?" | `~/.local/bin/threadbear status --json` |
| "What tasks do you see?" | `~/.local/bin/threadbear inventory --json` |
| "Install ThreadBear" | Follow **Install** below. |
| "Uninstall ThreadBear" | Follow **Uninstall** below. |

## Install

1. Read the current install guide and the candidate's help output. Check macOS, architecture, Codex, HTTPS access, and candidate self-test without changing the machine.
2. Run the exact dry run. Explain the complete effect: the local binary, one small private state file, one managed AGENTS block, this skill, and two hook entries.
3. Show the recommended setup and ask once for consent. A clear yes to the unchanged complete recommendation is installation consent. Ask again only if the recommendation changed, the answer was ambiguous, or this is a reinstall with a different effect.
4. Run the confirmed install and verify `version`, `self-test`, and `status`.
5. Use Codex `/hooks` to inspect and trust the two installed definitions. Then open a genuinely fresh Codex Desktop task and prove the first native call, the terminal native call, and the exact hook results before changing existing titles. Existing sessions may retain the hook snapshot they started with.
6. Follow **Bulk migration** for the current inventory, then prove a rendered active header and sidebar row in Codex Desktop.

For a large existing workspace, say this before migration:

> The deterministic scan is already done and highly token-efficient. A large workspace can spend about three to five minutes in the native Desktop handoff. I'll report only real progress. Luna medium runs only for genuinely ambiguous legacy history.

Do not claim success until the installed checks, fresh-task canary, exact native results, and rendered Desktop proof all pass.

## Status and inventory

`status --json` checks the installed binary, managed files, hooks, and state readability. It does not mutate titles.

`inventory --json` reads every local, unarchived Codex task across source shapes, including projectless tasks. Treat its deterministic classifications and ownership evidence as authoritative. Do not infer ThreadBear ownership from an icon or arrow alone.

## Bulk migration

Bulk migration is foreground and rerunnable:

1. Run `inventory --json` and report only counts and changed phase progress.
2. Accept exact historical ThreadBear footers deterministically. Send only genuinely ambiguous legacy items to fresh, read-only Luna-medium workers, with at most eight workers active. Each worker must inspect its assigned task through Codex's read-only task reader; workers classify and never write titles. Use `❔` when ambiguity remains.
3. For each unapplied item, call the native title setter with its explicit task ID and a compact marker: the exact valid footer for the classified status/action, or `❔ ThreadBear could not classify` for unknown. The Pre hook immediately re-reads that target, preserves any newer user rename, records the proposal, and rewrites the marker to the full desired title.
4. Require the native result to contain the exact target ID and returned title. The Post hook accepts only that exact pair and commits it. After the batch, rerun inventory and require every row to report `applied: true` before claiming success.
5. If interrupted or if any item fails, rerun inventory from scratch. Skip only rows already proven `applied: true`; a syntactically similar but unowned title must still pass through the native Pre/Post boundary.

The native setter has no compare-and-set argument. Do not claim it can prevent a rename that races the setter itself.

## Rendered Desktop verification

Command success, state, and `read_thread` are not visual proof. In Codex Desktop, verify that:

- a fresh foreground task shows the expanded running title in both the active header and sidebar before the response finishes;
- the terminal title appears in both places before the footer;
- one explicit-target migration repaints only the intended mounted sidebar row;
- a stopped turn leaves the running title and creates no additional ThreadBear turn.

Capture privacy-safe evidence when preparing a release.

## Uninstall

1. Run `help`, `status --json`, and `inventory --json`.
2. Ask whether to clean ThreadBear-owned decoration to subject-only titles or keep current titles. Preview the chosen effect and obtain a clear yes.
   Keeping titles makes those decorated strings user-owned; a later reinstall preserves them literally.
3. For cleanup, re-read and update each explicit task through the native setter before removing any artifacts. Require exact returned IDs and titles. A changed title that is not the last committed ThreadBear rendering is user-owned and must not be cleaned.
4. Run the confirmed uninstall. It removes only ThreadBear's recorded hook entries, managed AGENTS block, installed skill, private state, and binary while preserving unrelated content and hook order.
5. Ask the user to restart Codex so already-open sessions cannot keep using snapshotted guidance.

Thank the user and invite optional feedback at `eric@litman.org`. Never remove artifacts before requested title cleanup has completed.
