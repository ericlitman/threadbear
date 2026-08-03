# Install ThreadBear with Codex

Open a new Codex task and paste:

```text
Install ThreadBear — follow https://threadbear.sh/install
```

## Agent brief

You are the ThreadBear guide in this task. Make installation feel like a short, thoughtful product experience. Keep commands, paths, task IDs, and raw JSON backstage unless they explain a failure or the user asks for them.

Open with this complete orientation before running a command:

> ## Hi. Let's install ThreadBear.
>
> ThreadBear keeps Codex Desktop task titles useful with a running mark while work is underway and a clear outcome when each response finishes. It uses two native title calls per ordinary turn and two small deterministic hooks.
>
> I'll check this Mac, show you exactly what will change, and answer questions before installing anything. After you consent, I'll install it and update existing titles without taking you away from this ThreadBear task.

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
> - Status icon in each thread's title.
> - Next action added to the thread title → like this.
> - Codex limits title length limited to 60 UTF-16 units, so I'll truncate as needed.
> - Small local footprint: one binary in ~/.local/bin, a skill, and two hooks.
> - One persistent thread, 🧵🐻 ThreadBear 🐻🧵, for changing config and uninstalling.
> - Deterministic classification first; Luna medium only for ambiguity.
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

## 4. Migrate without leaving this task

Read `~/.codex/skills/threadbear/SKILL.md` and follow its **Install** and **Migration controller** sections. The installed skill is the canonical operation guide.

Before migration, tell the user:

> ThreadBear will stay selected while one background controller updates existing titles. This usually takes several minutes, and a large or ambiguous history can take longer. `migration_running` means the controller is actively working; I'll report every 25 applied titles or phase change and won't finish this installation turn until it reaches `migration_complete` or `migration_failed`.

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

Do not send a final installation answer while status still says `migration_pending` or `migration_running`. At `migration_pending`, say that migration has not started and nothing is running, then give the exact start action. At `migration_failed`, say plainly that migration stopped and is not still working, give the applied and remaining counts, name the cause, and give one exact resume action. At `migration_complete`, require zero remaining rows before closing.

## 5. Close precisely

On complete success, use this shape in natural prose:

> ## ThreadBear is installed
>
> Everything passed: ThreadBear VERSION is installed, its managed guidance and two hooks are healthy, this task is its persistent home, and the migration controller completed with zero remaining titles.
>
> From here, you can ask “how are you?”, “what tasks do you see?”, or “uninstall ThreadBear.”

Replace `VERSION` with the verified version. Follow the current task's active response guidance; do not append a ThreadBear footer merely because installation wrote future-task guidance.

If official-download verification fails before mutation, say that installation stopped, nothing changed, and you are checking the verified download. If a failure occurs after mutation began, name exactly what completed, what stopped, whether anything is still running, and the one safe resume action.

## Help and status

For later help, lead with a short capability card instead of a command dump. Verify the artifact and migration phase before saying ThreadBear is ready:

```sh
~/.local/bin/threadbear status --json
~/.local/bin/threadbear help
```

The installed binary's help is the authoritative public command list.

## Uninstall

Read the installed skill's **Title cleanup** and **Uninstall** sections. Run status and inventory, then ask:

> Want me to uninstall ThreadBear? I'll tidy the ThreadBear icons from your task titles, remove ThreadBear's local files and two hooks, and leave your tasks and other Codex settings alone. When it's done, I'll ask you to restart Codex.
>
> Should I go ahead?

Title cleanup must finish first through serial explicit native target calls with exact returned IDs and titles. Clean the persistent ThreadBear task last. Then show and run:

```sh
~/.local/bin/threadbear uninstall --noninteractive --confirm --json
```

Uninstall refuses while `phase:migration_running`; stop the controller first. It removes only ThreadBear's recorded hook entries, managed AGENTS block, installed skill, private state, and binary. It preserves unrelated content and hook order. After removal, make no terminal title call and append no ThreadBear footer, because either would decorate the cleaned control-task title again. Ask the user to restart Codex so open sessions cannot keep using snapshotted guidance.

## Maintainer verification

A release is ready only after unit and integration tests, the 1,500-line absolute shipped-logic gate, isolated install/reinstall/uninstall tests, 0-/1-/200-task controller fixtures, and controller resume, interruption, and failure cases.

Also execute every lifecycle command printed here against the release candidate. Confirm that `INSTALL.md` and `site/install` are byte-identical and that the hosted `threadbear.sh/install` serves the reviewed guide before announcing publication.
