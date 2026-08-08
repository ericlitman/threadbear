# Changelog

## Unreleased

## v3.0.0 - 2026-08-08

### Changed

- Reset ThreadBear to one terminal local title command that preserves exact subjects, changes only the status icon, keeps actions in response prose, and leaves unsafe titles unchanged.
- Made one short-lived official Codex App Server the read/planning authority and the mounted app-native setter the sole title writer: one prepared current-task title, one-time decoding of native JSON-text results, at most one native call, and exact returned ID/title validation, with no SQLite access, detached write, fallback, or retry.
- Replaced controller migration with explicit, uncapped onboarding that prepares a fresh complete snapshot, immediately checks each app-native task ID and title before its one possible write, reports honest updated, skipped, unchanged, and unconfirmed counts, and discloses that cached historical rows may redraw on project reopen or restart.
- Removed title interception and configuration dependencies; installation now manages only the binary, subject records, guidance, skill, and daily update-only LaunchAgent.
- Separated title-core readiness from updater health, made successful updates report restart requirements, made uninstall wait for any in-flight updater before teardown, and made queued updater processes reject a replaced installed binary; pre-install failures preserve the old install while later local write failures report a rerunnable partial with the binary last.
- Made 2.2.1 upgrades an explicit reset that previews the exact automation and former persistent task, verifies deletion and unpinning before filesystem mutation, imports no state, and performs no heuristic title cleanup.
- Made install, onboarding, manual update, and uninstall guidance friendly and plain: each action now previews what changes and what stays untouched, clearly discloses verified automatic updates, distinguishes safe skips from uncertain writes, and leaves a durable final-response recap with the result and next step.

### Removed

- Removed the running-title call, Post hook, persistent ThreadBear task, migration/controller/classifier machinery, archive maintenance, pending title transactions, repair flows, and global title-failure states.

## v2.2.1 - 2026-08-07

### Fixed

- Completed guided title migration in bounded concurrent waves without client-created native-call timeouts, while keeping exact inventory reconciliation and read-only Luna classifiers isolated to temporary installation tasks.
- Prevented delayed/older title callbacks and no-op proposals from racing replacement or uninstall teardown with a locked state-format fence, and registered the controller's actual native runtime ID from its marked home delegation instead of trusting Codex's provisional creation handle.
- Required that marked controller registration also come from Codex's native `subagent` source, preventing an ordinary user task from forging the delegation text and claiming migration authority.
- Made uninstall resumable from any active native task after a completed migration, a stopped failed migration, or a quiescent pre-controller install, with a canonical undecorated `ThreadBear` home, durable initiator ownership, exact archive restoration, preservation of user-created files beside the managed skill, and binary-last local teardown.
- Kept ambiguity checks on Luna medium, accepted nonempty one-word actions, ignored surplus `action` fields only for statuses that do not consume them, and retained fail-closed settlement for every unknown native title result, including canonical-home no-ops.

## v2.2.0 - 2026-08-03

### Added

- Restored conservative 14-day automatic archiving through one consented hourly Luna heartbeat, deterministic candidate selection, native archive controls, interruption-safe ownership, and ownership-only restore.
- Restored verified automatic updates through the same heartbeat, with bounded official release selection, checksum and candidate validation, same-version repair, and one version-change announcement.

## v2.1.7 - 2026-08-03

### Fixed

- Scoped installation completion to native-addressable local Codex tasks and disclosed that older signed-in ChatGPT chat-history rows are outside the current native title API, preventing a zero-local-inventory result from being presented as complete sidebar migration.

## v2.1.6 - 2026-08-03

### Fixed

- Removed visual and computer-control checks from ordinary guided installation, dispatched migration before UI or catalog archaeology, began deterministic title writes before Luna worker discovery, and documented the hook's exact unknown-state marker.

## v2.1.5 - 2026-08-03

### Changed

- Kept guided installation focused on the persistent ThreadBear task, moved Desktop canaries behind an explicit debug flag, and clarified installation and uninstall language.

### Fixed

- Kept pre-controller installs truthful with `migration_pending`, reconciled stopped controllers to `migration_failed`, and made guided migration retain and await adaptive Luna worker waves through an explicit terminal result while keeping title writes serial.

## v2.1.4 - 2026-08-03

### Fixed

- Bounded each ordinary native title moment to one four-second attempt and reduced the installed PreToolUse and PostToolUse limits to one second.

## v2.1.3 - 2026-08-02

### Fixed

- Prevented uninstall/reinstall migration from compounding leading status icons, added on-demand control-task icon cleanup, and preserved durable subjects by truncating only appended next-step actions.

## v2.1.2 - 2026-08-02

### Fixed

- Made the published release smoke honor the distinct first-install home and migration-controller contracts before verifying migration and native hook finalization.

## v2.1.1 - 2026-08-02

### Changed

- Made the initiating task ThreadBear's persistent home and moved installation migration to one resumable, serial native-title controller with honest phase reporting.

### Fixed

- Prevented fresh tasks from freezing a raw first message or delegated envelope as their stable subject by carrying a concise seed in the mandatory first native title call.
- Kept the guided installer welcome, readiness result, complete recommendation, and consent question visible together after the first turn finishes.

## v2.1.0 - 2026-07-31

### Changed

- Replaced the v2 heartbeat, retained control task, title queue, and LaunchAgent with two native current-task title calls per ordinary turn, expanded and verified by two deterministic hooks.
- Made installation and v2 title migration foreground, consented, rerunnable flows with fresh-task and rendered Desktop verification.

### Removed

- Removed background title processing, runtime Luna classification, Stop repair, durable title plans, and App Server title writes.

## v2.0.0 - 2026-07-31

### Changed

- Restored the pre-reset public homepage and guided-install experience while keeping the minimal runtime and current hosted bootstrap intact.
- Replaced the accumulated runtime with one small deterministic state machine: fixed-boundary rollout scanning, exact evidence guards, durable title staging, and one guarded native Desktop handoff.
- Reserved ephemeral Luna medium calls for unchanged legacy ambiguity and separated sub-second inventory scan measurements from the three-to-five-minute worst-case native handoff.
- Reset persistence to one private, atomically written `core.json` format and conservatively adopted existing non-status title text as user-owned.

### Removed

- Removed task archiving, token-count title decoration, automatic updates, configuration matrices, migration/checkpoint machinery, background title writes, replica orchestration, and the public commands that existed only for those features.

## v1.5.1 - 2026-07-30

### Fixed

- Preserved the complete active task inventory when promoting residual native-title plans, while continuing to process changed deterministic and Luna-classified siblings and prune removed rows.
- Deferred ambiguous token-shaped title subjects from the deterministic handoff to the later semantic pass, then removed confirmed managed-token contamination without consuming legitimate numeric subjects or divergent user edits.

## v1.5.0 - 2026-07-30

### Changed

- Bounded retained native title handoff during install/reconcile: drain guarded plans in the Desktop task, call Luna only for typed ambiguity, then resume direct title writes on later heartbeats.
- Bounded latest-turn and fresh rollout-tail reads to eight workers with deterministic result ordering, and updated guided install to drain native plans immediately after bounded background start.

### Fixed

- Opened guided installs with a rendered ThreadBear heading.
- Removed the duplicate review and confirmation when a person accepts the unchanged recommended guided-install setup.
- Decoupled guided-install health from background historical convergence, restored context-sized semantic packing, added aggregate first-sweep progress, and isolated each heartbeat's classifier in one private minimal-auth App Server process reused across batches.
- Confirmed the Codex 0.146 production isolation canary and four 200-observation rehearsals; ordinary status-guided work was fully deterministic, while serial remains the default because bounded first progress regressed.
- Enforced rename opt-out across native staging and operation reads, preserved setter-visible plans until their native report, and bounded stage readiness at 120 seconds plus the immediate continuation at five minutes.
- Kept the continuation signal backward-compatible with older installed actuators and made the retained cell stage the exact success or retry footer that matches its verified outcome.
- Made the replica rehearsal recognize the retained Desktop handoff, verify exact native-plan accounting and replay coverage, and keep semantic convergence explicitly deferred.

## v1.4.0 - 2026-07-29

### Changed

- Restored direct, checkpointed App Server title writes with revision/title revalidation, persisted applying/applied/verified stages, inventory verification, and title-before-archive ordering.
- Drains schema-v2 pending title plans once into ordinary checkpoint operations without evidence, transcript, token, or classifier reads; missing and drifted plans return to normal comparison.
- Retired the executable child title actuator architecture while retaining one hidden fail-closed `title-plan --json --dispatch` compatibility response.
- Bounded managed titles to 60 UTF-16 units without splitting surrogate pairs and standardized end token suffixes as ` · VALUE`, including legacy suffix convergence.

### Fixed

- Cleaned ThreadBear-owned status icons and recorded token counts from active managed task titles before uninstall removes runtime or state.

- Preserved crash recovery for applying/applied title operations, forced one direct setter call for same-title migrated refreshes, retained failed migrated plans, and saved verified title guards before same-task archives.

- Made uninstall wait for App Server title persistence and accept exact or Codex-shortened restored titles, so title cleanup converges in one invocation.

## v1.3.0 - 2026-07-28

### Added

- Added hosted post-publish install smoke coverage and a required pre-tag replica rehearsal checklist.

### Changed

- Added conversational ThreadBear help, plain-language command mapping, and a safe in-thread uninstall playbook.
- Made Codex-guided installation the supported end-user path and deprecated direct terminal bootstrap use while retaining it as installing-agent machinery.
- Made install adopt an explicitly selected readable Codex task with mutation-free dry-run previews, stable adoption diagnostics, and no legacy ThreadWatch cutover.

### Fixed

- Reconciled legacy titles containing only a status emoji without retrying for a missing subject.
- Made uninstall wait briefly for an executing heartbeat and report a clear retry-safe cause if the heartbeat remains in flight.
- Salvaged valid classifier rows individually and capped classifier batches at twenty tasks, so one mangled ID no longer drops a whole batch and retries stay bear-sized.
- Matched the replica-rehearsal preconditions to the live corpus: 50+ real-shape tasks required, emoji-only titles reported rather than required.
- Made the replica rehearsal reuse the host Go module cache so its temporary home always cleans up completely.
- Let the replica rehearsal accept partial-failure first heartbeats — per-task retries are the row-salvage contract — and preserve failure diagnostics on request.

## v1.2.0 - 2026-07-27

### Added

- Added friendly top-level and per-command CLI help generated from the parser's command and flag registrations.

### Changed

- Made uninstall thank the operator, default confirmations to yes, always delete persistent state, and finish with a friendly success message.

## v1.1.0 - 2026-07-27

### Fixed

- Refined thread classification of closing messages that describe a problem or prediction without a stated follow-up: the over-broad rule forcing them to complete is removed, matching the relabeled evidence corpus.

### Added

- Added opt-out automatic updates on a 30-minute check cadence, with verified self-replacement and one changelog-backed control-thread announcement after each version change.

## v1.0.0 - 2026-07-23

### Added

- Added deterministic-first classification and seven managed task states, with semantic fallback only for unresolved changed work.
- Added transactional heartbeats that maintain task titles, safely archive completed inactive tasks, and stay silent with zero model tokens when idle.
- Added guided user-local install, configuration, status, restore, update, downgrade, and uninstall flows with checksum, embedded-version, and candidate self-test verification.
- Added a persistent control task, managed AGENTS and skill guidance, and standalone Apple silicon and Intel release binaries.

### Known limitations

- Intel release artifacts were cross-compiled and checksum-verified but had not yet been executed on Intel hardware.
- Uninstall could require a retry when a heartbeat was already running.
- Updating the binary did not refresh managed AGENTS and skill content in v1.0.0.
