# ThreadBear

@docs/README.md

ThreadBear is a playful, token-conscious Codex title manager for macOS: one small Go binary uses the official local App Server to keep visible Desktop titles current without a daemon or model call.

- Current work is tracked in live Linear issues. The evergreen product contract is `README.md` plus `docs/architecture.md`; dated files in `docs/plans/` are historical evidence, not current architecture.
- Before title-path architecture or live experiments, run `python3 scripts/validate-experiments.py`, consult `docs/experiments/registry.json`, and satisfy the preflight in `docs/experiments/README.md`. Contradictory records remain conditional until one changed variable is isolated. Automation proves mechanical integrity; the active issue and pull-request review judge whether the unknown and changed variable are meaningful.
- Private eval corpus: `ericlitman/threadbear-eval` (real user messages — must never enter this public tree).
- Voice: playful, bear-themed, never at the expense of operational clarity.
- The only scheduler is the daily `sh.threadbear.update` LaunchAgent. It may run only the verified update command; it never reads tasks, invokes a model, onboards titles, or archives anything. Do not add another schedule, persistent ThreadBear task, pending-title queue, detached title writer, or background classifier.
- Changelog: every PR with user-visible changes must append a concise entry under `CHANGELOG.md`'s `Unreleased` section. Release preparation renames that section to `vN.N.N - YYYY-MM-DD` and adds a fresh `Unreleased` section; the release workflow rejects stable tags without the matching version section.
- Shipping evidence: unit and fixture tests are necessary but never sufficient. Before calling a native lifecycle or release path shippable, exercise the reviewed candidate end to end against the real supported native control or official release service in an isolated, recoverable canary, record exact candidate/ref/results, and clean up the canary without visual inspection.
