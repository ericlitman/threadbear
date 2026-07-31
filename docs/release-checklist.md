# Release checklist

Before tagging a stable release:

1. Rename `Unreleased` to `vN.N.N - YYYY-MM-DD` and add a fresh `Unreleased` section.
2. Run `gofmt`, `go test ./...`, `go vet ./...`, both Darwin cross-builds, shell syntax checks, and installer/guide parity checks. Count every tracked non-test `.go` file and require at most 1,000 physical production lines.
3. In isolated homes, prove install, reinstall, status, inventory, and both uninstall title choices while preserving unrelated AGENTS content and hook definitions in order.
4. Exercise 0-, 1-, and 200-task foreground migrations. Prove deterministic exact-footer classification, ambiguity-only Luna medium with at most eight workers, explicit native results, concurrent rename/archive handling, interruption, and clean rerun.
5. In fresh Codex Desktop tasks, run the matrix in `docs/live-eval.md`. Verify the rendered active header and sidebar, capture privacy-safe screenshots, and restore controlled canary titles through the supported native path.

After tagging, confirm the release workflow publishes both Darwin architectures, checksums, and the manifest. Then run the hosted smoke test through `threadbear.sh`, including checksum verification, candidate self-test, install, status, inventory, and uninstall.
