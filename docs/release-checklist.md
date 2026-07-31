# Release checklist

Before tagging a stable release:

1. Rename `Unreleased` to `vN.N.N - YYYY-MM-DD` and add a fresh
   `Unreleased` section.
2. Run `go test ./...`, `go vet ./...`, both Darwin cross-builds, shell syntax,
   plist lint, and the public-site parity checks.
3. On a disposable private Codex home with at least 50 real-shape tasks, verify
   a mutation-free first scan, a later ambiguity-only Luna pass, guarded direct
   title staging, uninstall that preserves current titles, and no retained task
   data in the report.
4. On the real Codex Desktop, stage one controlled retained-task plan, apply it
   through the supported native setter, verify the rendered title in the
   accessibility tree, capture a privacy-safe screenshot, and restore the
   original title through the same guarded path.
5. Record aggregate task, deterministic, ambiguous, Luna, staged, scan-time,
   native accepted, and native failed values. Never retain task IDs, titles,
   messages, rollout paths, model payloads, or copied state.

After tagging, confirm the release workflow publishes both architectures,
checksums, and the manifest, then prove the live bootstrap verifies the
checksum, candidate self-test, install, LaunchAgent, and uninstall chain.
