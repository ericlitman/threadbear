# Live evaluation

Run release QA with the exact reviewed candidate in fresh Codex Desktop tasks after a clean restart. Unit tests, App Server responses, and local state are supporting evidence; verify the rendered active header and sidebar users actually see.

Record candidate checksum, Codex version, task IDs, planner results, app-native results, rendered results, restart results, and cleanup. Use recoverable test tasks and privacy-safe screenshots only for release QA, never ordinary installation.

## Terminal title cell

Exercise `complete`, `next_steps`, `needs_input`, `blocked`, and `automation`. Include one tool-free turn and one tool-using turn. For each, prove:

- there was no running title update;
- one terminal JavaScript cell was the last tool action before the final response;
- that cell ran exactly one local `threadbear title --status ENUM --json` planner and parsed its complete JSON only after exit zero;
- the enum changed only the icon while the exact subject survived;
- owners and actions remained in response prose;
- the planner wrote no Codex title;
- when `write_required` was true, the cell made exactly one mounted app-native call with `threadId` omitted and accepted only the exact planned task ID and title;
- if the outer cell yielded after 30 seconds, the agent waited only for that same running cell; it never started another cell, polled the title, retried, or reconciled;
- the active header and sidebar showed the exact expected title.

Exercise a generated short title, continued task, user rename, leading user emoji, user arrow, duplicate subject, maximum fitting subject, overlong subject, multiline or control text, and raw delegated envelope. Safe renames must survive byte-for-byte. Unsafe input must leave only that title unchanged without blocking the response.

Force planner App Server start, initialize, current-read, and exit failures; missing or malformed current task ID; null and blank `name`; malformed planner JSON; and a planner-to-native-call rename race. For the mounted writer, cover a throw, returned error string, malformed object, wrong task ID, wrong title, and a slow call that outlasts the initial 30-second outer yield before returning. Require the yielded case to resume only the same running cell. Require zero binary `thread/name/set` calls, at most one app-native call, and no blind retry, alternate source, repair command, pending proposal, or global failure. If this seam causes practical corruption or response blocking, disable rewriting rather than add reconciliation.

Restart Codex after a successful write. Confirm the exact title remains in the sidebar and the next terminal turn still preserves the subject.

## Onboarding

For `onboard --dry-run --json`, prove the exact App Server handshake and cursor protocol. Include more than 100 tasks so the catalog is larger than 50 and necessarily multi-page; inject notifications and a duplicate ID. Prove complete deduplication, no arbitrary cap, no model or SQLite access, and zero mutation. Null and blank names remain raw even when `preview` looks safe. Fail a later page and prove zero preparation or native calls because no partial plan escaped.

After explicit consent, run exact `onboard --noninteractive --confirm --json`. Prove it starts from a fresh complete snapshot, stores subjects only for safe snapshot titles, emits `prepared` actions containing snapshot `title` and `desired_title`, performs no per-target app read, and makes zero Codex title writes. Cover the active caller, null and blank names, ambiguous old status prefixes, overlong text, user emoji, and already-onboarded titles. Force the preparation command to yield and prove the exact embedded JavaScript resumes that same process through `write_stdin` without starting a second command.

Run the installed skill's one serial native loop. Immediately before each possible write, require one mounted-app `read_thread` call with `includeOutputs:false`, `turnLimit:1`, and `maxOutputCharsPerItem:1`. A missing or unreadable task or a title that differs from the prepared snapshot is `skipped` and receives no setter call. Every exact match receives at most one explicit-target setter call for `🐻 <exact subject>`. Validate the exact returned ID/title and cover a throw, error string, malformed response, wrong target, and wrong title. Count every non-exact setter result as `unconfirmed` without retry. Interrupt a pass, rerun from another task, and prove completed titles are not doubled. Require serial read-before-write ordering, progress during preparation and every 25 outcomes, and a final receipt where every prepared row is exactly one of `updated`, `skipped`, or `unconfirmed`. Report `unchanged` honestly and report ready only when all prepared rows are accounted for and `unconfirmed` is zero.

Live-test the complete real local catalog with no artificial first-50 subset. Verify the rendered sidebar before and after a clean restart.

## Lifecycle

Prove fresh install, reinstall, and a consented exact 2.2.1 reset. The preview exposes the legacy main-task ID and complete automation fingerprint. Verify collision and missing-target dry runs mutate nothing. After consent, delete and verify only the exact automation, unpin and verify only the exact former persistent task, and do not rename it. Either native failure aborts before filesystem reset. The completed reset imports no old state, leaves ambiguous legacy titles untouched, installs one daily updater, and requires restart.

Exercise dry-run preflight against modified managed guidance, skill, LaunchAgent, and filesystem collisions. Exercise install/update and update/uninstall lock races; each loser reports busy without corrupting either lifecycle.

Exercise manual and scheduled updates against an isolated official-release service. Origin, platform, checksum, version, and self-test failures must happen before writes and preserve the old install. Inject each local managed-surface failure and require `partial:true`, the failed stage, restart implication, and one safe rerun while the prior binary remains active. Successful update JSON includes `restart_required`; the LaunchAgent invokes only update; missing updater health does not change title-core `ready`.

Uninstall from an ordinary task. Prove preview and commit JSON are complete and exact. Preserve unrelated AGENTS content, skills, settings, files, and LaunchAgents, and remove the binary last. Historical title cleanup is not a gate and icons may remain. After committed removal, do not run the title command. Restart Codex and prove the managed protocol is gone.
