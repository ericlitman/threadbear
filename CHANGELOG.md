# Changelog

## Unreleased

### Fixed

- Decoupled guided-install health from background historical convergence, restored context-sized semantic packing, added aggregate first-sweep progress, and isolated each heartbeat's classifier in one private minimal-auth App Server process reused across batches.
- Confirmed the Codex 0.146 production isolation canary and four 200-observation rehearsals; ordinary status-guided work was fully deterministic, while serial remains the default because bounded first progress regressed.

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
