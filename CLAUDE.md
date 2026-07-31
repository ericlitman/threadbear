# ThreadBear

@docs/README.md

ThreadBear is a playful, token-conscious Codex title manager for macOS: one Go binary and one LaunchAgent observe Codex tasks, decide seven statuses, and safely update visible Desktop titles.

- Current work item: BEAR-100. The evergreen contract is `README.md` plus `docs/architecture.md`; dated files in `docs/plans/` are historical evidence, not current architecture.
- Private eval corpus: `ericlitman/threadbear-eval` (real user messages — must never enter this public tree).
- Voice: playful, bear-themed, never at the expense of operational clarity.
- Keep the complete production executable surface below 2,000 source lines and prefer below 1,000. Do not compress code to game the count.
- Control-task adoption is explicit through `threadbear install --control-task-id`; installation never creates a persistent conversation.
- Changelog: every PR with user-visible changes must append a concise entry under `CHANGELOG.md`'s `Unreleased` section. Release preparation renames that section to `vN.N.N - YYYY-MM-DD` and adds a fresh `Unreleased` section; the release workflow rejects stable tags without the matching version section.
