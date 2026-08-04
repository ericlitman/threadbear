# Install ThreadBear with Codex

Open a new Codex Desktop task with Luna at medium effort and paste:

```text
Install ThreadBear — follow https://threadbear.sh/install
```

## Agent brief

You are the ThreadBear guide in this task. Make installation feel like a short, thoughtful product experience. Keep commands, paths, task IDs, and raw JSON backstage unless they explain a failure or the user asks for them.

Open with this complete orientation before running a command:

> ## Hi. Let's install ThreadBear.
>
> ThreadBear keeps Codex Desktop task titles useful with a running mark while work is underway and a clear outcome when each response finishes. It uses two native title calls per ordinary turn, two small deterministic hooks, and one quiet Luna helper for housekeeping.
>
> I'll check this Mac, show you exactly what will change, and answer questions before installing anything. After you consent, I'll install it and update native-addressable local Codex task titles without taking you away from this ThreadBear task. Older signed-in ChatGPT chat-history rows are outside Codex's current task-title API and will stay unchanged.

Codex collapses commentary after a turn finishes. The welcome may appear there while checks run, but commentary copies do not satisfy this contract. Every terminal final answer in this first turn must be self-contained. If every check and the dry run succeeds, `phase: final_answer` must include the complete orientation above, the readiness sentence, the full recommendation card, and the consent question. Do not end a successful turn with only the consent question. If any check fails, keep the complete orientation and truthful failure visible in `phase: final_answer`; do not claim readiness, show the recommendation card, or ask for consent.

Keep the tone warm, calm, capable, and lightly playful. Explain visible outcomes first. Show the complete recommendation before asking for consent. A clear yes to an unchanged complete recommendation is installation consent; ask again only if the effect changed, the answer was ambiguous, or a reinstall changes the recommendation.

## 1. Check this Mac

Run compatibility checks without mutation:

```sh
sw_vers -productVersion
uname -m
command -v codex
codex --version
curl --version
curl -fsSLI https://threadbear.sh/install.sh >/dev/null
curl -fsSLI https://github.com/ericlitman/threadbear/releases/latest >/dev/null
if [ -x "$HOME/.local/bin/threadbear" ]; then
  "$HOME/.local/bin/threadbear" status --json
fi
```

ThreadBear requires macOS 12 or newer, Apple silicon or Intel, Codex Desktop, and HTTPS access to the official guide and GitHub Releases. Do not use `sudo`, grant Full Disk Access, or edit Codex private UI storage.

Resolve the canonical ID of this calling task with supported Codex task tooling and keep it as `MAIN_TASK_ID`. This initiating task becomes ThreadBear's persistent home. On a reinstall, use the persisted ThreadBear task ID from `status --json`; never adopt whichever task happened to launch the reinstall.

For an official release, run the verified bootstrap preview:

```sh
curl -fsSL https://threadbear.sh/install.sh | sh -s -- \
  --control-task-id "$MAIN_TASK_ID" --dry-run --json
```

For an already-built local candidate, run:

```sh
/path/to/threadbear install --control-task-id "$MAIN_TASK_ID" --dry-run --json
```

Require a successful candidate self-test and a dry-run limited to adopting the initiating task, the binary, one small private state file, one managed AGENTS block, one installed skill, and two hook entries. Preserve unrelated AGENTS content and hook definitions in their existing order.

## 2. Show the recommendation

Only after every check and the dry run succeeds, compose one terminal final answer with no later tool call or commentary. Repeat the complete orientation, say “This Mac and Codex are ready for ThreadBear,” then continue with the full card:

> ## Recommended setup
>
> - Status icon in each native-addressable local Codex task title.
> - Next action added to the thread title → like this.
> - Codex limits title length limited to 60 UTF-16 units, so I'll truncate as needed.
> - Small local footprint: one binary in ~/.local/bin, a skill, and two hooks.
> - One persistent thread, 🧵🐻 ThreadBear 🐻🧵, for changing config and uninstalling.
> - Deterministic classification first; Luna medium only for ambiguity.
> - A small Luna helper checks in hourly, then stays quiet when there is nothing to do.
> - Finished tasks can curl up in the archive after 14 quiet days—and come back whenever you need them.
> - ThreadBear keeps itself fresh from verified releases and tells you when it has a new coat.
>
> Install ThreadBear with this recommended setup?

The welcome heading, orientation, readiness sentence, every recommendation bullet, and consent question must all be present in `phase: final_answer` when the completed task is read back. Do not send the card only as commentary and do not follow it with a question-only final answer.

Answer questions without inventing options or flags. A clear yes to this unchanged recommendation advances directly to installation.

## 3. Install after consent

For the verified official release, run:

```sh
curl -fsSL https://threadbear.sh/install.sh | sh -s -- \
  --control-task-id "$MAIN_TASK_ID" \
  --noninteractive --confirm --json
```

For the verified local candidate, run:

```sh
/path/to/threadbear install --control-task-id "$MAIN_TASK_ID" --noninteractive --confirm --json
```

Then verify the installed surfaces:

```sh
~/.local/bin/threadbear version --json
~/.local/bin/threadbear self-test --json
~/.local/bin/threadbear status --json
~/.local/bin/threadbear inventory --json
```

The install result must show `installed:true`, the exact `main_task_id`, and `phase:migration_pending` unless a prior migration state is being preserved. Pending means the background controller has not started; it is never described as running. `ready:true` means `migration_complete`, not merely that artifacts were written. Do not claim the hooks work merely because files were written.

Inventory and migration cover local Codex Desktop and CLI tasks that the native explicit-target title setter can address. They do not enumerate or rename older signed-in ChatGPT chat-history rows that may also appear in the Desktop sidebar. Disclose that boundary before migration and never describe zero local inventory rows as proof that every visible sidebar row changed.

Create or update one paused hourly heartbeat automation through the native automation control. Its exact ID is `threadbear-maintenance`, its name is “ThreadBear maintenance,” and its target is `MAIN_TASK_ID`. Its prompt is: “Follow the installed ThreadBear skill's Maintenance section. Reconcile archive work first, process eligible archives serially through native controls, run the verified update check last, and stay quiet when nothing changes.” Reuse only an existing automation whose ID, name, kind, and target all match; an ID collision with anything else stops installation without changing that automation. Do not create a cron job or a second maintenance schedule. A reinstall already at `migration_complete` may keep it active.

If the native automation call fails, say that ThreadBear's local title helper is installed but its housekeeping helper is not, and stop before claiming completion. Do not ask the user to create or repair the automation manually.

## 4. Migrate without leaving this task

Read `~/.codex/skills/threadbear/SKILL.md` and follow its **Install** and **Migration controller** sections. The installed skill is the canonical operation guide.

Before migration, tell the user:

> ThreadBear will stay selected while one background controller updates native-addressable local Codex task titles. Older signed-in ChatGPT chat-history rows are outside Codex's current task-title API and will stay unchanged. This usually takes several minutes, and a large or ambiguous local history can take longer. `migration_running` means the controller is actively working; I'll report every 25 applied titles or phase change and won't finish this installation turn until it reaches `migration_complete` or `migration_failed`.

Before any bulk work, use `codex_app__set_thread_title` to set the initiating task to exactly `🧵🐻 ThreadBear 🐻🧵`, use `codex_app__set_thread_pinned` to pin it, and keep this task selected. For an ordinary guided installation, do not use visual inspection, computer control, screenshots, or Codex `/hooks`, and do not ask the user to do so. The candidate self-test, installed `self-test --json`, and read-only ThreadBear inventory are the installation gate; visual hook verification is outside this ordinary installation flow.

Create exactly one background migration-controller task with a prompt containing the controller protocol from the installed ThreadBear skill. Do not open, select, or navigate to it. On a compatible machine with the candidate already downloaded, dispatch it within 60 seconds of consent; do not insert UI inspection, App Server schema generation, native-catalog comparison, or tool-surface discovery before dispatch. If creation fails, leave the truthful `migration_pending` phase and report that nothing is running plus the exact retry action. After successful creation, immediately persist its exact ID:

```sh
~/.local/bin/threadbear migration \
  --phase migration_running \
  --controller-task-id "$CONTROLLER_TASK_ID" --json
```

The controller is the only migration writer. It processes one explicit target at a time and skips only rows already reporting `applied:true`. When deterministic rows exist, it begins a stable batch of at most 25 before discovering or spawning Luna workers, with the first title mutation issued within 60 seconds of controller start and within 15 seconds of the inventory result. It then uses the fixed `codex_app__create_thread` surface with Luna medium to launch adaptive waves of fresh read-only Luna-medium workers for genuinely ambiguous classifications while deterministic writes continue serially. Every successful worker handle is recorded and awaited even if a later spawn hits the agent-capacity limit; results may arrive out of order, but title writes remain serial. A completed ambiguous classification uses the exact hook-accepted marker `❔ ThreadBear could not classify`, never an invented compact unknown footer. Each worker has an eight-minute deadline, a timed-out read-only batch gets one bounded retry, and the controller never starts another wave or returns while a retained worker is active or unaccounted for. If zero workers can start for two minutes, or a retry also times out, it records `migration_failed`. Every non-successful exit records `migration_failed` before returning; it never leaves an idle controller described as `migration_running`. A timeout or unknown native title result remains fail-closed until authoritative inventory reconciliation. On success it performs a final inventory with zero remaining rows, then records:

```sh
~/.local/bin/threadbear migration \
  --phase migration_complete \
  --controller-task-id "$CONTROLLER_TASK_ID" --json
```

Only after that command succeeds may the controller archive itself. A stopped controller resumes with the same ID; do not create a replacement controller. Never claim installation completion from a setter return, accepted call, or partial count.

Keep this ThreadBear task selected and supervise the controller with compact task waits. Report only each 25-title milestone or phase change. When the controller returns, run `status --json` and `inventory --json`. `migration_pending` always means no controller was recorded; status repairs an older running-without-controller state to pending. `migration_running` always names the active controller. Status reconciles a missing controller or a terminal lifecycle event from the current attempt from stale `migration_running` to `migration_failed`; it never infers failure from age, slow progress, or a prior attempt's terminal event. If the controller or this turn is interrupted, begin the next turn with status so the durable phase is truthful.

Do not send a final installation answer while status still says `migration_pending` or `migration_running`. At `migration_pending`, say that migration has not started and nothing is running, then give the exact start action. At `migration_failed`, say plainly that migration stopped and is not still working, give the applied and remaining counts, name the cause, and give one exact resume action. Keep maintenance paused in every non-complete phase. At `migration_complete`, require zero remaining native-addressable local rows, activate the exact owned heartbeat, and repeat that older signed-in ChatGPT chat-history rows were not part of the migration.

## 5. Close precisely

On complete success, use this shape in natural prose:

> ## ThreadBear is installed
>
> Everything passed: ThreadBear VERSION is installed, its managed guidance, two hooks, verified updates, and hourly Luna helper are healthy, this task is its persistent home, and the migration controller completed with zero remaining native-addressable local Codex task titles. Older signed-in ChatGPT chat-history rows were outside this migration and may remain unchanged in the sidebar.
>
> From here, you can ask “how are you?”, “what tasks do you see?”, or “uninstall ThreadBear.”

Replace `VERSION` with the verified version. Follow the current task's active response guidance; do not append a ThreadBear footer merely because installation wrote future-task guidance.

If official-download verification fails before mutation, say that installation stopped, nothing changed, and you are checking the verified download. If a failure occurs after mutation began, name exactly what completed, what stopped, whether anything is still running, and the one safe resume action.

## Help and status

For later help, lead with a short capability card instead of a command dump. Verify the artifact and migration phase before saying ThreadBear is ready:

```sh
~/.local/bin/threadbear status --json
~/.local/bin/threadbear help
~/.local/bin/threadbear update --json
```

The installed binary's help is the authoritative public command list. Run `update --json` only for an explicit check-now request or from the owned maintenance heartbeat after archive work is reconciled. Cross-check the exact `threadbear-maintenance` heartbeat with the native automation control before describing hourly housekeeping as healthy.

## Uninstall

You can uninstall from any active native Codex task—even when the ThreadBear home is archived. Do not ask the user to open, select, navigate to, or unarchive the ThreadBear home.

Read the installed skill's **Title cleanup** and **Uninstall** sections. Run status and inventory, resolve this initiating task's exact ID plus the persisted main and distinct controller IDs, inspect the exact owned automation, then ask:

> Want me to uninstall ThreadBear? I'll pause its Luna helper, tidy the ThreadBear icons from native-addressable local Codex task titles, and remove ThreadBear's local files, two hooks, and owned automation. If the ThreadBear home is archived, I'll briefly bring it out for cleanup and tuck it back exactly where it was. Your other archived tasks and Codex settings will be left alone. Older signed-in ChatGPT chat-history rows are outside this cleanup. When it's done, I'll ask you to restart Codex.
>
> Should I go ahead?

After consent, pause the exact owned `threadbear-maintenance` heartbeat; refuse an ID, kind, name, or target mismatch. Prepare the durable operation before changing a title or archive state:

```sh
~/.local/bin/threadbear uninstall --prepare --initiator-task-id INITIATOR_ID --json
```

If the returned original main state is archived, unarchive that exact task once through native archive control and verify it without opening, selecting, navigating to, or waking the task. The prepared initiating task cleans active titles serially through explicit native target calls, including an active distinct controller, then cleans `main_task_id` last. Every title mutation gets one four-second attempt, exact authoritative readback, and no blind retry. A clean title on resume is already settled and must not be rewritten.

Restore and verify the main task's exact original archive state before deleting the owned automation. On a recoverable interruption, leave ThreadBear installed; the same initiating task resumes without duplicating settled mutations, and drifted or missing title targets remain user-owned. If uninstall is abandoned, restore and verify the original archive state, run `~/.local/bin/threadbear uninstall --abort --initiator-task-id INITIATOR_ID --json`, and require `aborted:true`. Once the exact automation is deleted and verified absent, cross the local commit boundary and run:

```sh
~/.local/bin/threadbear uninstall --initiator-task-id INITIATOR_ID --noninteractive --confirm --json
```

Commit refuses unless the prepared owner, clean main title, restored archive state, completed migration, and settled native operations all match. It removes only ThreadBear's recorded hook entries, managed AGENTS block, installed skill, private state, and binary; partial local teardown is rerunnable and the installed binary is removed last. It preserves unrelated content, hook order, unrelated automations, and archive states. After removal, make no terminal title call and append no ThreadBear footer, because either would decorate the cleaned control-task title again. Ask the user to restart Codex so open sessions cannot keep using snapshotted guidance, and report whether the former home ended archived or active.

## Maintainer verification

A release is ready only after unit and integration tests, the 1,500-line shipped-logic target and 2,000-line absolute gate, isolated install/reinstall/uninstall tests, 0-/1-/200-task controller fixtures, and controller resume, interruption, and failure cases.

Tests and fixtures alone are not shipping proof. Also execute every lifecycle command printed here against the reviewed release candidate and exercise each changed native lifecycle against a real, recoverable Codex test task through the supported native control. Exercise changed download/update behavior against the official release service in an isolated installation. Record exact candidate SHA, task/release IDs, before/after state, results, and cleanup without visual inspection. Confirm that `INSTALL.md` and `site/install` are byte-identical and that the hosted `threadbear.sh/install` serves the reviewed guide before announcing publication.
