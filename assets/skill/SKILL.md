---
name: threadbear
description: Install, inspect, onboard, update, or uninstall the local ThreadBear title decorator for Codex Desktop on macOS.
---

# ThreadBear

Be brief, warm, and lightly bear-themed. Explain effects before commands and get explicit consent before install, reset, historical title mutation, manual update, or uninstall.

ThreadBear's managed guidance runs one local `title` command immediately before an ordinary final response. The status enum changes only the icon; the exact safe subject remains intact and any owner or action stays in response prose. There is no persistent task, running call, controller, classifier, archive system, or repair job.

## Help and status

Run `~/.local/bin/threadbear status --json` before calling ThreadBear installed or healthy. `threadbear help` is authoritative.

- “How is ThreadBear?” — run `status --json`.
- “ThreadBear onboard” — follow **Onboard existing tasks**.
- “Check for updates” — run `update --json` after consent.
- “Uninstall ThreadBear” — follow **Uninstall**.

`ready` covers the title core. Report updater health separately; a missing updater does not make title handling globally unready. Onboarding has its own receipt.

## Install or reset

Follow `https://threadbear.sh/install` and the candidate help. Run checks, self-test, and dry run first. Explain the binary, private subject state, managed block, skill, and daily updater.

For a 2.2.1 reset, require the preview's legacy main-task ID and complete automation fingerprint. Consent covers deleting only that exact automation, unpinning that exact former ThreadBear task without renaming it, and removing only exact obsolete ThreadBear Pre/Post title-interception entries. Preserve foreign entries and order. Verify both native results before `install --reset`. Any mismatch stops. Import no old state and guess at no legacy title.

After consent, install and verify `version --json`, `self-test --json`, and `status --json`. Ask for a Codex restart. Unless the user opted out, give this exact request:

> Open any task after restart and say: **ThreadBear onboard**

## Onboard existing tasks

1. Run `status --json`, then `onboard --dry-run --json`. The preview must enumerate and deduplicate the full unarchived App Server catalog before any write. Enumeration failure means zero writes.
2. Explain `total`, `safe`, and `needs_update`. The active caller, null or blank names, unsafe or overlong subjects, and ambiguous legacy titles stay unchanged. Never adopt preview text. Ask for explicit consent unless unchanged install consent already covered this first pass.
3. After consent, run exactly `~/.local/bin/threadbear onboard --noninteractive --confirm --json`. It processes the complete plan serially with no item cap. Each candidate is reread; missing, unreadable, drifted, or uncertain tasks are skipped. Each write is attempted once and counted only after exact readback. Never retry an unconfirmed result.
4. Report the returned totals: `Updated X of N existing tasks; Y were unchanged or skipped; Z could not be confirmed.` Do not claim completion unless `plan_complete` and `onboarding_complete` are true and every target is accounted for.

An interruption may leave valid partial decoration. A later **ThreadBear onboard** starts a fresh complete plan and safely continues. Never create a controller, queue, hidden resume state, product cap, or persistent ThreadBear task.

## Update

The daily LaunchAgent runs only `threadbear update`. Network and candidate-verification failures leave the old installation untouched. A later local managed-surface write can leave a truthful rerunnable partial; the binary is written last. The updater never reads tasks or changes titles.

For a requested check, show `~/.local/bin/threadbear update --json`, get consent, run it, and report `restart_required`. If true, say open tasks keep their snapshot until Codex restarts.

## Uninstall

Run `status --json` and show the uninstall dry run. Explain the owned artifacts removed and that historical icons may remain. Ask for consent.

After consent, run the confirmed uninstall. Preserve unrelated AGENTS content, skills, settings, files, and LaunchAgents. Once removal commits, do not run the title command. Ask the user to restart Codex so open tasks stop using snapshotted guidance.
