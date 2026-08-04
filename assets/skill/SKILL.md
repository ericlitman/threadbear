---
name: threadbear
description: Install, inspect, migrate, maintain, verify, or uninstall the local ThreadBear title manager for Codex Desktop on macOS.
---

# ThreadBear

Be warm, brief, and lightly bear-themed. Explain visible outcomes before commands, keep task IDs and raw JSON backstage, and never claim a rendered title from state or command success alone.

## Help

For a help-shaped request, start with a short capability card: ThreadBear keeps Codex Desktop titles useful through two native title calls per ordinary turn, while its hooks deterministically preserve each task's subject. One hourly Luna helper can quietly tuck away owned, completed tasks after 14 inactive days and install verified ThreadBear releases. ThreadBear adds no extra model call or narration to ordinary turns.

Run `~/.local/bin/threadbear status --json` before saying ThreadBear is installed or healthy. Use `~/.local/bin/threadbear help` as the authoritative public command reference.

Show a command before running it. Ask for explicit consent before any lifecycle mutation.

| Plain-language request | Command |
| --- | --- |
| "How are you?" | `~/.local/bin/threadbear status --json` |
| "What tasks do you see?" | `~/.local/bin/threadbear inventory --json` |
| "Run maintenance now" | Follow **Maintenance** below. |
| "Check for updates now" | Run the update-last step in **Maintenance**. |
| "Bring back archived task TASK_ID" | Follow the restore path in **Maintenance**. |
| "Strip title icons" | Follow **Title cleanup** below from the persisted ThreadBear task. |
| "Install ThreadBear" | Follow **Install** below. |
| "Uninstall ThreadBear" | Follow **Uninstall** below. |

## Install

1. Read the current install guide and the candidate's help output. Check macOS, architecture, Codex, HTTPS access, and candidate self-test without changing the machine. Resolve the exact current task ID with supported Codex task tooling.
2. Run the exact dry run with `--control-task-id CURRENT_TASK_ID`. Explain the complete effect: adopting that task as the persistent home, the local binary, one small private state file, one managed AGENTS block, this skill, two hook entries, and one owned hourly Luna heartbeat. Explain that migration covers native-addressable local Codex tasks; older signed-in ChatGPT chat-history rows are outside Codex's current task-title API and stay unchanged.
3. Show the recommended setup and include: “A small Luna helper checks in hourly, then stays quiet when there is nothing to do.”, “Finished tasks can curl up in the archive after 14 quiet days—and come back whenever you need them.”, and “ThreadBear keeps itself fresh from verified releases and tells you when it has a new coat.” Ask once for consent. A clear yes to the unchanged complete recommendation is installation consent. Ask again only if the recommendation changed, the answer was ambiguous, or this is a reinstall with a different effect.
4. Run the confirmed install with the same ID and verify `version`, `self-test`, and `inventory`. A fresh result is `migration_pending`: no controller has started and nothing is running yet. On reinstall, omit the flag only when `status --json` already reports the persisted main task; never replace it with the launching task.
5. Before any migration, use `codex_app__set_thread_title` to set the initiating task to exactly `🧵🐻 ThreadBear 🐻🧵`, use `codex_app__set_thread_pinned` to pin it, and keep that task selected.
6. Create or update one paused hourly heartbeat through the native automation control. Its ID is `threadbear-maintenance`, name is “ThreadBear maintenance,” target is the persisted main task, and prompt is: “Follow the installed ThreadBear skill's Maintenance section. Reconcile archive work first, process eligible archives serially through native controls, run the verified update check last, and stay quiet when nothing changes.” Reuse only an exact ID/name/kind/target match. A collision stops installation without changing the other automation; never create a cron job or duplicate. If the call fails, report the partial install and stop instead of asking the user to repair it manually. On a reinstall that already reports `migration_complete`, it may be active immediately.
7. For an ordinary guided installation, never use visual inspection, computer control, screenshots, or Codex `/hooks`, and never ask the user to do so. The candidate self-test, installed `self-test --json`, and read-only ThreadBear inventory are the installation gate. Visual hook verification belongs only to **Debug canaries** when the install result explicitly contains `debug_canaries:true`.
8. Create exactly one background migration controller without opening, selecting, or navigating to it, then immediately record it with `migration --phase migration_running`. On a compatible machine with the candidate already downloaded, dispatch it within 60 seconds of consent; do not insert UI inspection, App Server schema generation, native-catalog comparison, or tool-surface discovery before dispatch. If creation fails, leave `migration_pending` unchanged and report that nothing is running plus the exact retry action.
9. Give the controller the **Migration controller** protocol below. Supervise it from the persistent task with compact task waits, reporting only each 25-title milestone or phase change. Do not end the installation turn while durable status is `migration_pending` or `migration_running`.
10. After the controller returns, run `status --json` and `inventory --json`, then verify and activate the exact owned heartbeat with native automation controls. Claim success only at `migration_complete` with zero remaining native-addressable local rows and one active healthy maintenance automation, and repeat that older signed-in ChatGPT chat-history rows were not part of the migration. Leave the heartbeat paused at `migration_pending`, `migration_running`, or `migration_failed`. At `migration_failed`, say migration stopped and is not still working, report applied and remaining counts, name the cause, and give one exact resume action using the same controller ID.

For a large existing workspace, say this before migration:

> ThreadBear will stay selected while one background controller updates native-addressable local Codex task titles. Older signed-in ChatGPT chat-history rows are outside Codex's current task-title API and will stay unchanged. This usually takes several minutes, and a large or ambiguous local history can take longer. `migration_running` means the controller is actively working; I'll report every 25 applied titles or phase change and won't finish this installation turn until it reaches `migration_complete` or `migration_failed`.

Do not claim success from installed files, a native setter return, or partial counts.

## Maintenance

The owned `threadbear-maintenance` heartbeat runs this section from the persistent ThreadBear task. Luna orchestrates; the CLI alone chooses archive eligibility and records ownership. Never inspect task prose to add a target, and never edit Codex's SQLite archive field.

1. Run `~/.local/bin/threadbear maintenance --json`. If it reports a pending operation, reconcile that operation before any new archive. If it reports no pending operation and no candidates, stay silent.
2. For each candidate in stable order, run `~/.local/bin/threadbear maintenance --archive TASK_ID --json` immediately before mutation and require that exact ID with `action:"archive"` and `pending:true`. Call native `codex_app__set_thread_archived` once with that task ID and `archived:true`, without opening or selecting the task. Rerun the exact maintenance command and require `reconciled:true` before continuing.
3. If the native result is unknown or the reconciliation still says pending, do not repeat the mutation. Read the task with native task controls, rerun maintenance, and stop with the pending transaction intact unless the CLI authoritatively reconciles it. If the native call returned a definite failure and a native read confirms the original archive state, run `maintenance --cancel TASK_ID --json` to clear that known-unapplied operation. Never cancel an unknown or in-flight result. Title, footer, activity, kind, identity, or archive drift makes the CLI fail closed. If the CLI reports that an applied archive drifted, it remains pending and unowned: do not adopt it. Report the task for manual recovery; only after the user restores it and a native read confirms it is unarchived may the exact guarded cancel clear the operation.
4. Process one native archive operation at a time. Finish with a no-target maintenance pass and require no pending operation. Report only archived task subjects/counts or an error; do not narrate healthy no-op runs.
5. Only after the closing archive pass proves there is no pending native operation, run `~/.local/bin/threadbear update --json` last. The command alone fetches the exact official manifest, chooses the Darwin architecture, verifies HTTPS repository URLs and SHA-256, checks the embedded version and candidate self-test, and invokes the candidate's existing install path. Luna never browses for, chooses, downloads, checksums, or approves an asset.
6. Stay silent for `current:true`. For `updated:true`, report the old and new versions once in the persistent ThreadBear task. For `repaired:true`, say that the same verified version repaired managed files. On failure, report its typed `stage` and error once; never bypass verification, run a remote script, downgrade, or retry an unknown partial install blindly.

For a user-requested restore, verify the request from the persistent ThreadBear task, then run `maintenance --restore TASK_ID --json`. Continue only for `action:"restore"`, call the native archive control once with `archived:false`, and rerun the same command until `reconciled:true`. The CLI accepts only ThreadBear-owned archives and restarts that task's 14-day quiet clock. A user-archived task is never adopted. A task manually restored through Codex is detected on the next pass, removed from the ownership ledger, and receives the same fresh quiet clock.

To change the quiet window, pass the requested positive `--archive-after-days N` consistently to planning and staging and update the owned automation prompt. To disable archival, update the owned heartbeat prompt to omit this archive protocol while preserving the update-last step. To pause updates, omit only the update-last step while preserving enabled archival. A check-now request runs the same deterministic update command after proving no pending archive work; there is no separate scheduler or release channel.

## Status and inventory

`status --json` checks the installed binary, managed files, hooks, and state readability. It reports the expected `maintenance_automation_id`, pending native archive state, and owned archive count, but the agent must cross-check the exact heartbeat through native automation controls before calling it healthy. It reports `installed:true` while artifacts are present, but `ready:true` only for `phase:migration_complete`. It does not mutate titles. `migration_pending` means no controller was recorded and returns the exact start action; status also repairs an older running-without-controller state to pending. `migration_running` always names the active controller. If that controller is missing or has a terminal lifecycle event from the current attempt, status atomically records `migration_failed`, explains that the controller stopped, and returns the exact resume action. It never infers failure from age, slow progress, or a prior attempt's terminal event.

`inventory --json` reads every native-addressable, unarchived local Codex Desktop or CLI task, including projectless tasks, excluding the persisted main and controller IDs. It excludes rollout-only internal records and older signed-in ChatGPT chat-history rows that Codex's native title setter cannot enumerate or rename. Treat its deterministic classifications, `status`, `action`, and `applied` evidence as authoritative only for that local catalog. Never describe zero inventory rows as proof that every visible sidebar row changed, and do not infer ThreadBear ownership from an icon or arrow alone.

## Migration controller

The controller is the only installation-migration writer and is rerunnable under one persisted controller ID:

1. Run `~/.local/bin/threadbear inventory --json` and use its `status`, `action`, `task_id`, and `applied` fields. The main and controller tasks are already excluded. If any unapplied deterministic rows exist, immediately process the first stable batch of at most 25 through the serial title path before resolving or spawning any Luna worker. Issue the first in-scope title mutation within 60 seconds of controller start and within 15 seconds of the inventory result.
2. Accept exact historical footers deterministically. Split only genuinely ambiguous rows into stable batches of at most 20 tasks. Workers classify and never write titles. When a completed classification remains ambiguous, call the native setter with title exactly `❔ ThreadBear could not classify`; never invent a compact unknown footer.
3. Only after the first deterministic batch has applied, or when no deterministic row exists, classify ambiguous batches with `codex_app__create_thread` using `model:"gpt-5.6-luna"`, `thinking:"medium"`, and a projectless background target. Do not inspect or compare alternative agent surfaces at runtime. Run adaptive waves of fresh, read-only Luna-medium workers, with at most eight workers active. After every successful spawn, immediately record its exact handle and assigned task IDs before attempting another spawn. At the first agent-capacity error, stop launching that wave. Never reinterpret that error as zero workers when earlier spawns succeeded.
4. Account for every retained worker even when results arrive out of order. Wait in bounded snapshots and give each worker eight minutes from spawn. When a worker completes, validate and record its result by handle, then process that batch in stable order through the serial title path while other read-only workers may continue. If a worker reaches its deadline, stop it, discard only that batch's uncommitted classifications, finish accounting for the rest of the wave, and retry the timed-out batch once in the next wave. A second timeout records `migration_failed`.
5. Process one native title write at a time. Convert each row's `status` and `action` to the exact compact ThreadBear footer grammar, using the exact unknown marker from step 2, then call the native setter with the explicit `threadId`. After the first fast batch, continue deterministic rows serially while Luna workers classify; never wait for ambiguous classification before exhausting deterministic progress. The Pre hook immediately re-reads the target and preserves any newer user rename; Post accepts only the exact target/title result and commits the proposal. Re-run inventory after each write or bounded batch and skip only rows reporting `applied:true`. A similar-looking but unowned title still passes through the native Pre/Post boundary.
6. Do not start another wave or return while a retained worker is still active or unaccounted for. If zero workers can start, wait 30 seconds and retry for at most two minutes; then record `migration_failed`. Do not degrade unclassified rows to unknown merely because worker capacity is temporarily unavailable.
7. A timeout, unknown native result, hook rejection, or unreconcilable target is not blindly retried except for the one bounded read-only classifier retry above. Record `migration_failed` with the same controller ID before every non-successful return and leave the controller visible. Resume only after authoritative inventory establishes whether a prior native write applied.
8. Report progress after each 25 newly applied rows or phase change. Finish only when a final inventory reports zero remaining rows, then run:

   ```sh
   ~/.local/bin/threadbear migration --phase migration_complete \
     --controller-task-id CONTROLLER_TASK_ID --json
   ```

   Archive the controller only after that command succeeds. A successful transition is the durable completion evidence.

The native setter has no compare-and-set argument. Do not claim it can prevent a rename that races the setter itself. If the controller is interrupted before it can record a terminal phase, the next `status --json` reconciles the definitively stopped task to `migration_failed`.

## Debug canaries

Run this section only when the immediately preceding install result contains `debug_canaries:true`. Never run it during an ordinary guided installation.

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

1. Run `status --json`. For ordinary cleanup, verify this task's exact ID equals `main_task_id`; no other task may request it. During uninstall, the exact prepared uninstall task may request cleanup while its persisted operation is active.
2. Run `inventory --json`. Add the active persisted controller task, if any, to the target set; the inventory intentionally excludes it and the main task.
3. Select every active title beginning with one or more exact ThreadBear status icons: `⏳`, `🚨`, `🙋`, `🤖`, `➡️`, `✅`, or `❔`. Ordinary leading emoji are not decoration.
4. In stable order, re-read one target and require its exact planned title. Call the native title setter with that explicit `threadId` and title exactly `🧵🐻 strip title icons`. The Pre hook re-reads the target, strips every leading ThreadBear status icon, uses `Untitled task` only when no subject remains, and stages the result through normal ownership state. Require the exact returned task ID/title and re-read the live title before continuing. Never retry an unknown result blindly.
5. Re-run the complete inventory plus controller read. Finish only when every selected title has no leading ThreadBear status icon and every native result reconciles. On drift, mismatch, or inaccessible state, stop with artifacts and private ownership state intact so the same control task can safely resume.

For ordinary on-demand cleanup, do not target the active control task: its required terminal call will leave exactly one current status icon. For uninstall, target the control task last, immediately before removing ThreadBear, and use the uninstall-turn exception in the managed guidance.

## Uninstall

You can uninstall from any active native Codex task—even when the ThreadBear home is archived. Do not ask the user to open, select, navigate to, or unarchive the ThreadBear home.

1. Run `help`, `status --json`, `inventory --json`, and inspect the exact owned `threadbear-maintenance` heartbeat. Resolve the exact current task ID, `main_task_id`, and distinct `controller_task_id`. If migration or archive work is pending, reconcile it first. Refuse any automation ID/name/kind/target mismatch.
2. Ask: “Want me to uninstall ThreadBear? I'll pause its Luna helper, tidy the ThreadBear icons from native-addressable local Codex task titles, and remove ThreadBear's local files, two hooks, and owned automation. If the ThreadBear home is archived, I'll briefly bring it out of the archive for cleanup and tuck it back exactly where it was. Your other archived tasks and Codex settings will be left alone. Older signed-in ChatGPT chat-history rows are outside this cleanup. When it's done, I'll ask you to restart Codex. Should I go ahead?” Continue only after a clear yes.
3. Pause the exact owned heartbeat so it cannot race cleanup. Run `~/.local/bin/threadbear uninstall --prepare --initiator-task-id INITIATOR_ID --json`. Require the returned IDs to match the current task, persisted main task, and distinct controller snapshot. Re-running this command from the same initiating task resumes the same operation; a different task must stop.
4. If the returned original main state is archived, call `codex_app__set_thread_archived` once with that exact `threadId` and `archived:false`, then verify the task through `codex_app__read_thread` without opening it or sending it a turn. The prepared uninstall task remains the only writer.
5. Follow **Title cleanup** serially for every decorated active regular task and an active distinct controller. Re-read and clean `main_task_id` last through one explicit-target native call from the prepared uninstall task. Skip only a target whose authoritative title is already clean. Every mutation gets one four-second attempt, exact readback, and no blind retry after an unknown result.
6. Require the persisted main title to be clean. Restore its exact original archive state with one `codex_app__set_thread_archived` call and verify it through `codex_app__read_thread` before continuing. On a recoverable interruption, leave the automation and local artifacts intact; the same task resumes through step 3 without repeating settled mutations. If uninstall is abandoned, restore and verify the original archive state, run `~/.local/bin/threadbear uninstall --abort --initiator-task-id INITIATOR_ID --json`, verify `aborted:true`, and leave ThreadBear installed. Drifted or missing title targets are reported in `drifted_titles` and left user-owned instead of being rewritten.
7. Delete the exact owned heartbeat and verify it is absent. This is the commit boundary: make no later native task or automation mutation. Run `~/.local/bin/threadbear uninstall --initiator-task-id INITIATOR_ID --noninteractive --confirm --json`. It revalidates the clean main title and restored archive state, tolerates already-removed owned local entries, preserves unrelated config order, and removes the installed binary last.
8. From successful prepare through commit, make no running-title or terminal-title call and append no ThreadBear footer. Ask the user to restart Codex so already-open sessions cannot keep using snapshotted guidance, and report whether the former home ended archived or active.

Thank the user and invite optional feedback at `eric@litman.org`. Never remove artifacts before title cleanup has completed.
