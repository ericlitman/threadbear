# Changelog

## Unreleased

### Added

- Added strict JSON-only `title-plan` wait, batch, and native-outcome reporting for exact revision-guarded hosted title application.

### Changed

- Heartbeats now stage durable per-task title plans instead of writing through a detached App Server, allowing unrelated cycle work to commit while Desktop-native actuators repaint mounted task lists.
- Fresh state adopts only one strict leading status marker, preserves the complete non-status remainder, and retains exact BEAR-67 action/token ownership boundaries.

- Recast Codex-guided installation as a warm ThreadBear welcome with progressive preference choices, plain-language review and consent, backstage technical details, and a friendlier settings notice.

### Fixed

- Distinguished native mutation success, canonical persistence on the next inventory, and rendered Desktop accessibility verification instead of calling SQLite equality visible convergence.

### Fixed

- Converged repeated managed output-token displays to one decoration without consuming unowned numeric subject text.

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
