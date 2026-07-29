# Release checklist

Complete this checklist for every stable ThreadBear release.

## Before tagging

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

## BEAR-59 Desktop title canaries

Before a release that changes title planning or managed guidance, use an already-mounted Codex Desktop catalog and prove these boundaries separately without reload or restart:

1. Prove the non-control source made an actual `functions.exec` identity/status guard call before dispatch; a prose claim does not count. Confirm it creates exactly one projectless `gpt-5.6-luna` / `medium` actuator through delegated source identity and that the persistent control task creates no worker.
2. Read the actuator turn and confirm one model pass and one `functions.exec`: exact `title-plan --wait`, per-operation `title-plan --operation`, fulfilled native `await tools.codex_app__set_thread_title({threadId: TASK_ID, title: DESIRED_TITLE})`, one strict report with boolean `native_success`, exact accepted-ID set equality, then self-archive through `await tools.codex_app__set_thread_archived({archived: true})` without `threadId`. Treat the resulting `interrupted` state as expected. Confirm the program did not enumerate, inspect, or look up available tools or schemas, and that no implementation inspection, second command, retry, transcript, or title manifest entered model context.
3. Exercise the zero-plan success path without an empty report. Exercise a drift or native/report failure and confirm the actuator remains visible with one stable `title_actuation_failed` error and no recovery loop.
4. Record native-call success, accepted helper report, canonical `read_thread` and `list_threads` persistence, and the already-mounted Desktop accessibility label as four separate facts without reload or restart. None substitutes for another.
5. Record actuator model, effort, raw input, cached input, output, and reasoning-output tokens as release-canary evidence only. Require one Luna/medium model pass near the measured 24,070 raw-input baseline, allowing bounded contract growth.
6. Run one unchanged heartbeat after the canary and verify zero classifier turns and zero title RPCs.

Do not add production token telemetry, use the persistent ThreadBear master for routine work, automate accessibility/UI inspection in product code, or use private IPC, caches, daemons, or restarts.
