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
| "Strip title icons" | Follow **Title cleanup** below from the persisted ThreadBear task. |
| "Install ThreadBear" | Follow **Install** below. |
| "Uninstall ThreadBear" | Follow **Uninstall** below. |

## Install

1. Read the current install guide and the candidate's help output. Check macOS, architecture, Codex, HTTPS access, and candidate self-test without changing the machine. Resolve the exact current task ID with supported Codex task tooling.
2. Run the exact dry run with `--control-task-id CURRENT_TASK_ID`. Explain the complete effect: adopting that task as the persistent home, the local binary, one small private state file, one managed AGENTS block, this skill, and two hook entries.
3. Show the recommended setup and ask once for consent. A clear yes to the unchanged complete recommendation is installation consent. Ask again only if the recommendation changed, the answer was ambiguous, or this is a reinstall with a different effect.
4. Run the confirmed install with the same ID and verify `version`, `self-test`, and `status`. On reinstall, omit the flag only when `status --json` already reports the persisted main task; never replace it with the launching task.
5. Before any migration, use `codex_app__set_thread_title` to set the initiating task to exactly `🧵🐻 ThreadBear 🐻🧵`, use `codex_app__set_thread_pinned` to pin it, and prove that exact title in the active header and mounted sidebar.
6. Use Codex `/hooks` to inspect and trust the two installed definitions. Then open a genuinely fresh Codex Desktop task and prove its first native call carries the reserved `⏳ ThreadBear is working: <concise subject>` handoff, the terminal native call matches its footer, and both exact hook results pass before changing existing titles. Existing sessions may retain the hook snapshot they started with.
7. Confirm the read-only ThreadBear inventory count matches Codex's native task catalog on the verified Codex version. A mismatch stops migration; do not add runtime repair or a second writer.
8. After the canary passes, create exactly one ephemeral migration controller, record it with `migration --phase migration_running`, and end the persistent task promptly. The persistent task never performs, awaits, or polls bulk migration.
9. Give the controller the **Migration controller** protocol below. Do not claim completion until it records `migration_complete` after final inventory convergence.

For a large existing workspace, say this before migration:

> The deterministic scan is already done and highly token-efficient. A large workspace can spend about three to five minutes in the native Desktop handoff. I'll report only real progress. Luna medium runs only for genuinely ambiguous legacy history.

Do not claim success until the installed checks, fresh-task canary, exact native results, and rendered Desktop proof all pass.

## Status and inventory

`status --json` checks the installed binary, managed files, hooks, and state readability. It reports `installed:true` while artifacts are present, but `ready:true` only for `phase:migration_complete`. It does not mutate titles.

`inventory --json` reads every native-addressable, unarchived Desktop or CLI task, including projectless tasks, excluding the persisted main and controller IDs. It excludes rollout-only internal records that Codex's native title setter cannot rename. Treat its deterministic classifications, `status`, `action`, and `applied` evidence as authoritative. Do not infer ThreadBear ownership from an icon or arrow alone.

## Migration controller

The controller is the only installation-migration writer and is rerunnable under one persisted controller ID:

1. Run `~/.local/bin/threadbear inventory --json` and use its `status`, `action`, `task_id`, and `applied` fields. The main and controller tasks are already excluded.
2. Accept exact historical footers deterministically. Send only genuinely ambiguous items to fresh, read-only Luna-medium workers, with at most eight workers active. Workers classify and never write titles; use the unknown marker when ambiguity remains.
3. Process targets in stable order, one native title write at a time. Convert each row's `status` and `action` to the exact compact ThreadBear footer grammar, then call the native setter with the explicit `threadId`. The Pre hook immediately re-reads the target and preserves any newer user rename; Post accepts only the exact target/title result and commits the proposal.
4. Re-run inventory after each write or bounded batch. Skip only rows reporting `applied:true`. A similar-looking but unowned title still passes through the native Pre/Post boundary.
5. A timeout, unknown native result, hook rejection, or unreconcilable target is not blindly retried. Record `migration_failed` with the same controller ID and leave the controller visible. Resume only after authoritative inventory establishes whether the prior write applied.
6. Finish only when a final inventory reports zero remaining rows, then run:

   ```sh
   ~/.local/bin/threadbear migration --phase migration_complete \
     --controller-task-id CONTROLLER_TASK_ID --json
   ```

   Archive the controller only after that command succeeds. A successful transition is the durable completion evidence.

The native setter has no compare-and-set argument. Do not claim it can prevent a rename that races the setter itself.

## Rendered Desktop verification

Command success, state, and `read_thread` are not visual proof. In Codex Desktop, verify that:

- a fresh foreground task shows the expanded running title in both the active header and sidebar before the response finishes;
- the terminal title appears in both places before the footer;
- one explicit-target migration repaints only the intended mounted sidebar row;
- a stopped turn leaves the running title and creates no additional ThreadBear turn.
- the persistent task remains exactly `🧵🐻 ThreadBear 🐻🧵` and pinned before migration begins;
- a completed controller leaves `status --json` at `ready:true`, `phase:migration_complete`, with no remaining inventory rows.

Capture privacy-safe evidence when preparing a release.

## Title cleanup

Title cleanup is an on-demand, idempotent control-task operation. It removes every consecutive leading ThreadBear status icon while preserving ordinary emoji and every remaining title byte. A later ordinary turn may add one current status icon again; cleanup prevents old decoration from becoming part of the durable subject.

1. Run `status --json` and verify this task's exact ID equals `main_task_id`. If it does not, continue in the persisted ThreadBear task; no other task may request cleanup.
2. Run `inventory --json`. Add the active persisted controller task, if any, to the target set; the inventory intentionally excludes it and the main task.
3. Select every active title beginning with one or more exact ThreadBear status icons: `⏳`, `🚨`, `🙋`, `🤖`, `➡️`, `✅`, or `❔`. Ordinary leading emoji are not decoration.
4. In stable order, re-read one target and require its exact planned title. Call the native title setter with that explicit `threadId` and title exactly `🧵🐻 strip title icons`. The Pre hook re-reads the target, strips every leading ThreadBear status icon, uses `Untitled task` only when no subject remains, and stages the result through normal ownership state. Require the exact returned task ID/title and re-read the live title before continuing. Never retry an unknown result blindly.
5. Re-run the complete inventory plus controller read. Finish only when every selected title has no leading ThreadBear status icon and every native result reconciles. On drift, mismatch, or inaccessible state, stop with artifacts and private ownership state intact so the same control task can safely resume.

For ordinary on-demand cleanup, do not target the active control task: its required terminal call will leave exactly one current status icon. For uninstall, target the control task last, immediately before removing ThreadBear, and use the uninstall-turn exception in the managed guidance.

## Uninstall

1. Run `help`, `status --json`, and `inventory --json`. If `phase:migration_running`, stop the controller and let it record `migration_failed` before uninstalling.
2. Explain that uninstall first strips all leading ThreadBear status icons from active titles, then removes the local artifacts. Preview the complete effect and obtain a clear yes.
3. Follow **Title cleanup** for every regular and controller task. Re-read and clean this persisted ThreadBear task last. Any drift, unknown result, or remaining decorated title stops uninstall before artifacts or ownership state are removed.
4. Run the confirmed uninstall. It refuses while migration is running, then removes only ThreadBear's recorded hook entries, managed AGENTS block, installed skill, private state, and binary while preserving unrelated content and hook order.
5. Use the managed uninstall-turn exception: make no terminal title call and append no ThreadBear footer. Ask the user to restart Codex so already-open sessions cannot keep using snapshotted guidance.

Thank the user and invite optional feedback at `eric@litman.org`. Never remove artifacts before title cleanup has completed.
