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
> ThreadBear keeps each Codex task's exact subject and adds one useful status icon at the end of a turn. Its small local command reads and updates the title through Codex's official App Server.
>
> I'll check this Mac, show you exactly what will change, and ask before installing anything. Afterward, Codex needs one restart. Then you can say **ThreadBear onboard** in any task to update every safe existing local task—there is no 50-task cap or persistent ThreadBear task.

Codex collapses commentary after a turn finishes, so the final answer that asks for consent must repeat the orientation, readiness result, complete recommendation, and question. If a check fails, report it plainly and do not ask for install consent.

## 1. Check without changing anything

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

> ## Recommended setup
>
> - One status icon in each task title, updated once immediately before the final response.
> - Your exact subject stays intact; owners and actions remain in response prose.
> - Unsafe, ambiguous, active, drifted, or overlong titles are left alone.
> - Existing tasks can be previewed completely and onboarded serially, with no item cap.
> - Small local footprint: one binary, tiny subject records, one skill, and one managed instruction block.
> - One daily LaunchAgent installs only verified official updates and never reads tasks or changes titles.
> - No persistent ThreadBear task, controller, classifier, archive automation, queue, or background repair.
> - Codex restarts once after install so open tasks load the new guidance.
>
> Install ThreadBear with this recommended setup?

For a 2.2.1 reset, add one sentence: the exact old maintenance automation will be deleted, its exact former persistent task will be unpinned but not renamed, managed artifacts will be replaced, old state will not be imported, and ambiguous historical icons may remain.

A clear yes to the unchanged recommendation is consent. Ask again only if the effect changes or the answer is ambiguous. If the user does not want historical onboarding, accept that preference and add `--no-onboard` to the confirmed install.

## 3. Install after consent

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

## 4. Restart and onboard

After a successful install say:

> ThreadBear is installed. Restart Codex so open tasks load the new managed guidance.
>
> After restart, open any task and say: **ThreadBear onboard**

When that request arrives, read the installed skill and follow this protocol:

1. Run `~/.local/bin/threadbear status --json`, then `~/.local/bin/threadbear onboard --dry-run --json`.
2. Require `ready:true`, `plan_complete:true`, and `read_only:true`. The preview enumerates and deduplicates the entire unarchived App Server catalog before any write. If enumeration fails, make zero writes.
3. Explain `total`, `safe`, and `needs_update`. The active caller, null or blank names, unsafe or overlong subjects, and ambiguous legacy titles stay unchanged. Preview text is never a title source.
4. Ask for explicit consent unless unchanged install consent covered this first pass.
5. After consent, run exactly:

```sh
~/.local/bin/threadbear onboard --noninteractive --confirm --json
```

The confirmed command starts from a fresh complete catalog and handles every safe target serially with no item cap, waves, worker tasks, or resume machinery. It rereads each target before a possible write; drift, absence, ambiguity, or unreadability is skipped. It attempts each write once and counts `updated` only after exact readback. An acknowledgement without exact readback is `unconfirmed` and is never retried.

Report `updated`, `unchanged`, `skipped`, and `unconfirmed` honestly and account for the returned target set. Call onboarding complete only when `plan_complete:true` and `onboarding_complete:true`. An interruption may leave valid partial decoration; a later **ThreadBear onboard** starts a fresh plan and safely continues.

## Commands and updater

```sh
~/.local/bin/threadbear help
~/.local/bin/threadbear status --json
~/.local/bin/threadbear title --status complete --json
~/.local/bin/threadbear onboard --dry-run --json
~/.local/bin/threadbear update --json
```

The managed guidance runs `title --status <complete|next_steps|needs_input|blocked|automation> --json` exactly once immediately before an ordinary final response. The enum changes only the icon. The binary reads the exact current title, preserves the safe subject, makes at most one App Server name update, and performs exact readback. If the command fails or does not finish in its bounded terminal moment, deliver the response without polling, retrying, or reconciling.

`update` verifies the official manifest, release URLs, architecture, checksum, embedded version, and candidate self-test before replacement. Network or verification failure leaves the old installation untouched. A later managed-surface write can truthfully leave a rerunnable partial; the binary is written last. Every successful update reports `restart_required`. The daily LaunchAgent runs only this command and never reads tasks or changes titles.

## Uninstall

Preview first:

```sh
~/.local/bin/threadbear uninstall --dry-run --json
```

Explain:

> Want me to uninstall ThreadBear? I'll remove only its binary, private subject records, managed guidance, installed skill, and daily updater. Existing title icons may remain until those tasks are renamed. Other Codex settings and files stay untouched. When removal finishes, you'll restart Codex.

After consent:

```sh
~/.local/bin/threadbear uninstall --noninteractive --confirm --json
```

Require committed removal and verify unrelated AGENTS content, skills, settings, files, and LaunchAgents remain byte-for-byte intact. After commit, do not run the title command. Ask the user to restart Codex so open tasks stop using snapshotted guidance.

## Release proof

Before release, run unit and integration tests, race tests, both Darwin builds, shell checks, experiment validation, installer/guide parity, and the focused fixture smoke.

Release acceptance additionally requires one reviewed candidate live-tested end to end in Codex Desktop:

- the terminal `title` command changes only the status icon and preserves the exact subject;
- App Server acknowledgement and exact readback agree;
- the rendered sidebar shows the expected title before and after a clean restart;
- a full onboarding preview enumerates every local task, and a consented serial pass updates every safe target with honest counts;
- failure and unconfirmed cases never block the substantive response or trigger retries;
- automatic update and uninstall preserve neighboring user content.

If the direct writer causes practical title corruption or response blocking, disable rewriting instead of adding reconciliation machinery.
