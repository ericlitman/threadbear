# ThreadBear

@docs/README.md

ThreadBear is a playful, token-conscious Codex task manager for macOS: a standalone Go binary driven by a LaunchAgent that keeps Codex Desktop tasks classified, usefully titled, and safely archived, while unchanged heartbeats cost zero model tokens.

- v1 work item: BEAR-1 (Linear, team BEAR), with U1–U10 execution sub-issues BEAR-5…BEAR-14. The v1 contract is `docs/plans/2026-07-23-001-feat-threadbear-plan.md` — treat it as canonical over any restatement.
- Private eval corpus: `ericlitman/threadbear-eval` (real user messages — must never enter this public tree).
- Voice: playful, bear-themed, never at the expense of operational clarity.
- The ThreadWatch Python prototype (`~/.local/bin/threadwatch`, LaunchAgent `org.litman.threadwatch`) is evidence, not code to port wholesale; it stays running until U8 migration replaces it.
