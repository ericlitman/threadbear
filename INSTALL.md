# Install ThreadBear with Codex

Open a new Codex Desktop task and paste:

```text
Install ThreadBear — follow https://threadbear.sh/install
```

## Agent brief

You are the ThreadBear guide in this task. Keep the experience short, warm, and concrete. Explain visible outcomes before commands and keep raw JSON, paths, and task IDs backstage unless they explain a failure. Do not use screenshots, computer control, or ask the user to inspect private files during an ordinary install.

Open with this orientation:

> ## Hi. Let's install ThreadBear.
>
> ThreadBear keeps each Codex task's exact subject and adds one useful status icon at the end of a turn. It prepares one safe title, then Codex applies it once.
>
> I'll check this Mac, show you exactly what will change, and ask before installing anything. Afterward, Codex needs one restart. Then you can say **ThreadBear onboard** in any task to update every safe existing local task—there is no 50-task cap or persistent ThreadBear task.

Codex collapses commentary after a turn finishes, so the final answer that asks for consent must repeat the orientation, readiness result, complete recommendation, and question. If a check fails, report it plainly and do not ask for install consent.

For every lifecycle action, write the lasting summary after all tool calls. End the final response with **ThreadBear recap 🐻** and include the result, counts or uncertainty, what stayed untouched, and the next action. Never leave that recap only in commentary, progress notices, notifications, or raw tool output; those can disappear when Codex summarizes the turn.

Keep that recap user-facing: do not copy raw fields or list internal files and components. Translate them into helper, title memory, instructions, and automatic updates. Group safe skips as “left unchanged” unless the user needs to act. An unconfirmed title write means “I couldn't confirm whether this title changed,” never “it stayed unchanged.”

## 1. Check without changing anything

Say: “First I'll check that this Mac is ready and preview the exact ThreadBear setup. Nothing changes in this step.”

Run:

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

ThreadBear requires macOS 12 or newer, Apple silicon or Intel, Codex Desktop, and HTTPS access to the official guide and GitHub Releases. It needs no `sudo` or Full Disk Access. It never opens Codex SQLite or edits Desktop storage.

For an official release, run the verified bootstrap preview:

```sh
curl -fsSL https://threadbear.sh/install.sh | sh -s -- --dry-run --json
```

For an already-built local candidate, run:

```sh
/path/to/threadbear install --dry-run --json
```

The preview must pass candidate self-test and be limited to the binary, private subject records, managed AGENTS block, installed skill, and one daily update-only LaunchAgent. It must preserve unrelated AGENTS content, skills, settings, files, and LaunchAgents.

If the preview returns `legacy_reset_required:true`, require `legacy_main_task_id` plus `legacy_automation_id`, `legacy_automation_name`, `legacy_automation_kind`, and `legacy_automation_target_thread_id`. The target must equal the main-task ID. This is a clean 2.2.1 reset, not an in-place migration. Through supported native controls, verify the exact automation and former persistent task before proposing mutation. A collision, missing target, or uncertain owner stops the reset. The reset also removes only exact obsolete ThreadBear Pre/Post title-interception entries and preserves every foreign entry and its order. Import no old state and reinterpret no legacy title.

## 2. Show the recommendation

Only after the checks and dry run succeed, present this complete card in the same final answer as the consent question:

> ## Here's what will happen
>
> - ThreadBear adds one helpful status icon without rewriting your task's subject or emoji.
> - Existing tasks stay unchanged until you preview onboarding and approve it separately.
> - A small local helper, Codex instructions, and private title memory are added.
> - Once a day, ThreadBear checks for and installs only verified official releases. Updates never read tasks or change titles.
> - Unclear or unsafe titles are left alone, and there is no persistent ThreadBear task.
> - Other Codex settings and files stay untouched.
> - Codex restarts once so open tasks load the new instructions.
>
> Install ThreadBear?

For a 2.2.1 reset, add: “I'll remove only the verified old ThreadBear automation, unpin its former task without renaming it, and install the simpler version fresh. Old title history will not be guessed or imported, so some existing icons may remain.”

A clear yes to the unchanged recommendation is consent. Ask again only if the effect changes or the answer is ambiguous. If the user does not want historical onboarding, accept that preference and add `--no-onboard` to the confirmed install.

## 3. Install after consent

Say: “Thanks—I'll install ThreadBear now, then check that it is healthy. Existing task titles will not change in this step.”

Before a 2.2.1 reset, delete the exact fingerprinted `threadbear-maintenance` automation through supported native control and verify it is absent. Then unpin the preview's exact legacy main-task ID and verify the returned and reread task ID match with `pinned:false`. Do not rename that task. Any mismatch aborts before filesystem reset. The confirmed candidate command must include `--reset`.

For the official release, run:

```sh
curl -fsSL https://threadbear.sh/install.sh | sh -s -- \
  --noninteractive --confirm --json
```

For a local candidate, run:

```sh
/path/to/threadbear install --noninteractive --confirm --json
```

Add `--no-onboard` only when the user opted out. Add `--reset` only after the exact legacy cleanup is verified. Then run:

```sh
~/.local/bin/threadbear version --json
~/.local/bin/threadbear self-test --json
~/.local/bin/threadbear status --json
```

Core `ready` is healthy when the installed binary, private subject state, managed guidance, and skill match the candidate. Report the daily updater separately; missing automatic updates do not make title handling globally unready. Core readiness does not depend on historical title counts.

No controller, worker, migration phase, persistent task, or hidden onboarding job should exist after installation. If installation fails after mutation starts, report `partial:true`, the failed stage, whether restart is required, and the one safe rerun action. `planned_changes` is a plan, not a claim that every item ran.

After the checks finish, end the final response with this plain-language receipt, filled with the real result:

> ## ThreadBear recap 🐻
>
> - ThreadBear is installed and automatic updates are [ready / need attention].
> - Existing tasks have not been changed yet, and unrelated Codex settings stayed untouched.
> - Next: restart Codex, then open any task and say **ThreadBear onboard**.

## 4. Restart and onboard

Say: “Installation is finished. One restart loads the new instructions; onboarding stays a separate previewed choice.”

After a successful install say:

> ThreadBear is installed. Restart Codex so open tasks load the new managed guidance.
>
> After restart, open any task and say: **ThreadBear onboard**

When that request arrives, read the installed skill and follow this protocol:

1. Run `~/.local/bin/threadbear status --json`, then `~/.local/bin/threadbear onboard --dry-run --json`.
2. Require `ready:true`, `plan_complete:true`, and `read_only:true`. The preview enumerates and deduplicates the entire unarchived App Server catalog before any preparation or title write. If enumeration fails, make zero changes.
3. Explain `total`, `safe`, and `needs_update` with this card:

> ## Here's what will happen
>
> - I found N existing tasks. X have safe titles, and Y need a ThreadBear icon.
> - The rest stay untouched.
> - I'll check each task again immediately before its one possible title change.
> - If a title changed before its turn, I'll leave it alone.
> - If a change cannot be confirmed, I won't retry it and I'll tell you.
>
> Update these existing tasks now?

The active caller, null or blank names, unsafe or overlong subjects, and ambiguous legacy titles stay unchanged. Preview text is never a title source.
4. Ask for explicit consent unless unchanged install consent covered this first pass.
5. After consent, follow the installed skill's single onboarding JavaScript cell. Its first action runs exactly:

```sh
~/.local/bin/threadbear onboard --noninteractive --confirm --json
```

The confirmed command takes a fresh complete catalog snapshot, stores each safe subject, and returns one `prepared` action containing the snapshot title and desired title. It makes no Codex title writes. If preparation yields, the same JavaScript cell resumes that exact process through `tools.write_stdin`; it never starts another command. For every prepared item, call `tools.codex_app__read_thread({threadId:item.task_id,includeOutputs:false,turnLimit:1,maxOutputCharsPerItem:1})` immediately before a possible write. A missing, unreadable, wrong-ID, or changed-title response is skipped. Only an exact task ID and snapshot title may receive one serial `tools.codex_app__set_thread_title({threadId:item.task_id,title:item.desired_title})` call. Lightweight progress appears during preparation and every 25 outcomes. There is no item cap, wave, worker task, or resume state. Count only an exact returned task ID/title as `updated`; a throw, malformed response, or mismatch is `unconfirmed` and is never retried.

Codex can keep an already-mounted historical row cached after an exact native write. Do not retry or add refresh machinery. The persisted title appears when its project is reopened or Codex restarts; say this plainly in the onboarding summary.

Report `updated`, `skipped`, `unchanged`, and `unconfirmed`. Every prepared item must reach exactly one outcome. ThreadBear is ready only when all are accounted for and `unconfirmed` is zero. An interruption may leave valid partial decoration; a later **ThreadBear onboard** starts a fresh plan.

End with:

> ## ThreadBear recap 🐻
>
> - Checked N existing tasks: updated X, left Y unchanged, and could not confirm Z.
> - No uncertain task was retried. Older sidebar rows may refresh when their project reopens or Codex restarts.
> - Next: [ThreadBear is ready / rerun **ThreadBear onboard** after resolving the named problem].

## Commands and updater

```sh
~/.local/bin/threadbear help
~/.local/bin/threadbear status --json
~/.local/bin/threadbear title --status complete --json
~/.local/bin/threadbear onboard --dry-run --json
~/.local/bin/threadbear update --json
```

The managed guidance runs one injection-safe terminal JavaScript cell immediately before an ordinary final response. Replace only the status enum; the parsed `plan.desired_title` variable passes directly to the native tool and is never re-embedded by the model. The cell runs `title --status <complete|next_steps|needs_input|blocked|automation> --json` exactly once. The binary reads the exact current title through the App Server, preserves the safe subject, and returns a plan without writing a title. When `write_required` is true, the cell calls `tools.codex_app__set_thread_title({title:plan.desired_title})` exactly once with `threadId` omitted, and accepts only the exact returned planned task ID and title. If the outer cell yields after 30 seconds, wait only for that same cell; the yield does not cancel a slow native call, which may delay the final response. Never retry, start another cell, poll the title, or reconcile.

`update` verifies the official manifest, release URLs, architecture, checksum, embedded version, and candidate self-test before replacement. Network or verification failure leaves the old installation untouched. A later managed-surface write can truthfully leave a rerunnable partial; the binary is written last. Every successful update reports `restart_required`. The daily LaunchAgent runs only this command and never reads tasks or changes titles.

For a manual update, preview first and end the consent turn with:

> ## Here's what will happen
>
> - ThreadBear will download the official update, verify it, and replace its local helper only after the checks pass.
> - The update check does not read tasks or change titles.
> - I'll tell you whether Codex needs a restart.
>
> Update ThreadBear now?

Afterward, end with:

> ## ThreadBear recap 🐻
>
> - ThreadBear is now version [version], and automatic updates are [ready / need attention].
> - Codex [does / does not] need a restart.
> - Next: [nothing—you're up to date / the one safe rerun for a partial update].

## Uninstall

Preview first:

```sh
~/.local/bin/threadbear uninstall --dry-run --json
```

End the consent turn with:

> ## Here's what will happen
>
> - I'll remove ThreadBear's local helper, private title memory, Codex instructions, skill, and automatic updates.
> - Your tasks, other Codex settings, and unrelated files stay untouched.
> - Existing title icons may remain until those tasks are renamed.
> - After removal, you'll restart Codex once.
>
> Uninstall ThreadBear now?

After consent:

```sh
~/.local/bin/threadbear uninstall --noninteractive --confirm --json
```

Require committed removal and verify unrelated AGENTS content, skills, settings, files, and LaunchAgents remain byte-for-byte intact. After commit, do not run the title command. Ask the user to restart Codex so open tasks stop using snapshotted guidance.

The final response after committed removal is:

> ## ThreadBear recap 🐻
>
> - ThreadBear and its automatic updates were removed.
> - Your tasks and unrelated Codex content stayed untouched; old title icons may remain.
> - Next: restart Codex so open tasks drop the old instructions.

## Release proof

Before release, run unit and integration tests, race tests, both Darwin builds, shell checks, experiment validation, installer/guide parity, and the focused fixture smoke.

Release acceptance additionally requires one reviewed candidate live-tested end to end in Codex Desktop:

- the terminal planner changes no Codex title, preserves the exact subject, and prepares only the status icon change;
- the mounted app-native setter receives no explicit current-task ID and returns the exact planned task ID and title;
- the rendered sidebar shows the expected title before and after a clean restart;
- a full onboarding preview enumerates every local task, confirmed preparation writes no title, and the consented serial app-native pass accounts for every prepared target while skipping title drift before any write;
- failures and unconfirmed results are reported locally without retries or global failure state;
- automatic update and uninstall preserve neighboring user content.

If the mounted app-native writer causes practical title corruption or response blocking, disable rewriting instead of adding reconciliation machinery.
