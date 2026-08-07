# Live evaluation

Run release QA with the exact reviewed candidate in fresh Codex Desktop tasks after a clean restart. Unit tests, App Server responses, and local state are supporting evidence; verify the rendered active header and sidebar users actually see.

Record candidate checksum, Codex version, task IDs, inputs, App Server results, rendered results, restart results, and cleanup. Use recoverable test tasks and privacy-safe screenshots only for release QA, never ordinary installation.

## Terminal title writer

Exercise `complete`, `next_steps`, `needs_input`, `blocked`, and `automation`. Include one tool-free turn and one tool-using turn. For each, prove:

- there was no running title update;
- the one local `threadbear title --status ENUM --json` command was the last tool action before the final response;
- the enum changed only the icon while the exact subject survived;
- owners and actions remained in response prose;
- the command exited within its bound and was never polled, retried, or recovered;
- App Server acknowledgement and exact readback agreed;
- the active header and sidebar showed the exact expected title.

Exercise a generated short title, continued task, user rename, leading user emoji, user arrow, duplicate subject, maximum fitting subject, overlong subject, multiline or control text, and raw delegated envelope. Safe renames must survive byte-for-byte. Unsafe input must leave only that title unchanged without blocking the response.

Force App Server start, initialize, current-read, set, readback, and exit failures; missing or malformed current task ID; null and blank `name`; acknowledgement without readback; timeout; and a rename concurrent with a delayed write. Require at most one `thread/name/set` call and no blind retry, alternate source, repair command, pending proposal, or global failure. If this seam causes practical corruption or response blocking, disable rewriting rather than add reconciliation.

Restart Codex after a successful write. Confirm the exact title remains in the sidebar and the next terminal turn still preserves the subject.

## Onboarding

For `onboard --dry-run --json`, prove the exact App Server handshake and cursor protocol. Include more than 100 tasks so the catalog is larger than 50 and necessarily multi-page; inject notifications and a duplicate ID. Prove complete deduplication, no arbitrary cap, no model or SQLite access, and zero mutation. Null and blank names remain raw even when `preview` looks safe. Fail a later page and prove zero writes because no partial plan escaped.

After explicit consent, run exact `onboard --noninteractive --confirm --json`. Prove every safe target is handled serially and every returned item is accounted for as updated, unchanged, skipped, or unconfirmed. Cover the active caller, null and blank names, unreadable and drifted tasks, ambiguous old status prefixes, overlong text, user emoji, already-onboarded titles, setter failure, and acknowledgement without exact readback. Each safe target receives at most one neutral `🐻 <exact subject>` write after fresh readback. Interrupt a pass, rerun from another task, and prove completed titles are not doubled.

Live-test the complete real local catalog with no artificial first-50 subset. Verify the rendered sidebar before and after a clean restart.

## Lifecycle

Prove fresh install, reinstall, and a consented exact 2.2.1 reset. The preview exposes the legacy main-task ID and complete automation fingerprint. Verify collision and missing-target dry runs mutate nothing. After consent, delete and verify only the exact automation, unpin and verify only the exact former persistent task, and do not rename it. Either native failure aborts before filesystem reset. The completed reset imports no old state, leaves ambiguous legacy titles untouched, installs one daily updater, and requires restart.

Exercise dry-run preflight against modified managed guidance, skill, LaunchAgent, and filesystem collisions. Exercise install/update and update/uninstall lock races; each loser reports busy without corrupting either lifecycle.

Exercise manual and scheduled updates against an isolated official-release service. Origin, platform, checksum, version, and self-test failures must happen before writes and preserve the old install. Inject each local managed-surface failure and require `partial:true`, the failed stage, restart implication, and one safe rerun while the prior binary remains active. Successful update JSON includes `restart_required`; the LaunchAgent invokes only update; missing updater health does not change title-core `ready`.

Uninstall from an ordinary task. Prove preview and commit JSON are complete and exact. Preserve unrelated AGENTS content, skills, settings, files, and LaunchAgents, and remove the binary last. Historical title cleanup is not a gate and icons may remain. After committed removal, do not run the title command. Restart Codex and prove the managed protocol is gone.
