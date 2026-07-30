# Release checklist

Complete this checklist for every stable ThreadBear release.

## Before tagging

- BEAR-87/90 evidence passed on runtime head `7d0fbc7e69dbfc442e36f84b650ec401c13ef403`: the Codex 0.146.0 production-factory isolation canary, bounded ten-second install handoff, and all four 200-observation serial/bounded rehearsals were green. The status-guided cohort was 200/200 deterministic with zero Luna calls; keep serial as the compiled default because bounded first progress regressed. See [the aggregate benchmark](benchmark.md#first-sweep-performance-release-gate). Keep the PR draft for final operator review.


1. Prepare the intended `vN.N.N` section in `CHANGELOG.md` and leave a fresh `Unreleased` section.
2. Make a quiesced, disposable copy of the operator's real Codex home. The copy must contain its `state_N.sqlite`, rollout files, auth needed by Codex, and 50+ real-shape tasks (emoji-only titles are reported when present, not required). Keep the copy outside this repository.
3. Choose a readable, unarchived control task in the copy and confirm no ThreadBear LaunchAgent is loaded for the operator account.
4. From the release commit, run:

   ```sh
   scripts/replica-rehearsal.sh \
     --version N.N.N \
     --control-task-id CONTROL_TASK_ID \
     --replica /absolute/path/to/copied-codex-home \
     --codex /absolute/path/to/codex
   ```

   The script requires a clean release checkout; refuses the genuine Codex home, overlapping paths, and links back into it; verifies 50+ real-shape tasks and reports the emoji-only title count; copies the supplied replica again into an isolated temporary `HOME`; stages the current-architecture release artifacts outside the repository; executes the bootstrap; completes the first heartbeat when required; verifies the LaunchAgent; and uninstalls on every exit path.
5. A first heartbeat over a real corpus may legitimately report `partial_failure` with per-task retries — that is the row-salvage contract, and the rehearsal accepts it; any other heartbeat error code fails. If a rehearsal fails, rerun with `THREADBEAR_REHEARSAL_DIAGNOSTICS=/absolute/dir` to preserve its step outputs for diagnosis; they can contain task data, so delete them after review.
6. Do not tag unless the rehearsal is green. Retain only its count/status summary with the commit SHA, intended version, macOS and architecture, Codex version, install/self-test/LaunchAgent results, heartbeat aggregate counts, pending retry count, uninstall result, and `isolation=temporary_copy`. Do not retain task IDs, titles, messages, rollout paths, replica state, or raw App Server output.

## Publish and verify

1. Push the stable-shaped `vN.N.N` tag and confirm the release workflow publishes the GitHub release.
2. Confirm the `Release smoke` workflow triggered by publication passes. It executes the live `https://threadbear.sh/install.sh` bootstrap against that exact release on a GitHub-hosted macOS runner and verifies the manifest, checksum, binary, self-test, fixture control-task plumbing, LaunchAgent, and uninstall chain.
3. Treat a red release smoke as a failed release requiring operator action and a fix-forward decision. Check Pages/CDN deployment timing because the live bootstrap can briefly lag the release commit; the smoke does not delete, demote, or automatically retry a published release.
4. For an already-published stable-shaped release, including one marked prerelease on GitHub, rerun `Release smoke` with manual dispatch and its `vN.N.N` tag. SemVer `-rc` tags are not supported by the bootstrap or release workflow.

The hosted smoke explicitly does not prove real Codex auth, real App Server side effects, classification heartbeat behavior, title/archive effects, or an architecture other than its runner. The required replica rehearsal covers the Codex-touching composition before tagging.

## Native title-convergence canaries

Before a release that changes title handling, use a real Codex catalog and verify:

1. Detached `thread/name/set` canonical persistence is recorded separately and is not claimed as mounted Desktop convergence.
2. The retained control task resolves its canonical runtime identity exactly, drains a multi-title native batch, and repaints mounted Recents and saved-project rows without reload or restart.
3. Every operation is revalidated immediately before the native setter; unowned, malformed, stale, and drifted rows are skipped and reported only by operation ID.
4. Native success, canonical verification, checkpoint recovery, partial failure, duplicate reports, and bounded canonical retries settle deterministically before same-task archive.
5. The current source title matches the exact terminal ThreadBear footer, and a changed/missing final footer recomputes rather than accepting the staged status.
6. One unchanged heartbeat and retained source turn perform zero classifier turns and zero title operations.
7. A disposable 50+ task replica exercises the fixed aggregate raw-V8 path and retains only aggregate counts.

Do not use private IPC, caches, daemons, UI automation, restarts, or the persistent ThreadBear control task for routine title work.
