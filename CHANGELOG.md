# Changelog

## Unreleased

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
