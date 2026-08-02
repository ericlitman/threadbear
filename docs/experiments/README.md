# ThreadBear experiment registry

Read `registry.json` before changing title-path architecture or proposing a live
title experiment. It is the canonical record of what was tested, the runtime
in which it was tested, and which observations disagree.

The registry is public and privacy-safe. Evidence references may name issue,
pull request, commit, and Codex task identifiers, but must not contain prompts,
message bodies, screenshots, personal data, credentials, or absolute home
paths. Use `unknown: <reason>` instead of reconstructing a missing fingerprint.

## Preflight

Before a live experiment, add a `pending` preflight that states:

1. the capability being tested;
2. the experiment records consulted;
3. the exact remaining unknown;
4. one material variable that differs from the closest prior observation;
5. the outcomes that discriminate the competing explanations; and
6. the condition that stops the experiment.

If no material variable differs, reuse the prior result. One preflight permits
only its declared probe. Record the result and close the preflight before using
the evidence to change architecture or claim that ThreadBear works. The result
experiment must name the preflight in `preflight_id`; the closed preflight must
name that same experiment in `result_experiment_id`.

Run `python3 scripts/validate-experiments.py` before the issue reviews a
preflight and again after recording the result. The validator checks the
registry plus its negative fixtures. It deliberately cannot decide whether a
non-empty changed variable is scientifically meaningful; that remains an issue
and pull-request review responsibility.

## Evidence gates

Title-path work advances through three gates:

1. **Capability:** one minimal probe validates the load-bearing mechanism before
   architecture or multi-file implementation.
2. **Seam:** focused automated and live regressions cover the exact boundary
   while implementation is in progress.
3. **Release:** the complete rendered matrix in `../live-eval.md` runs against
   the installed exact candidate after a clean Codex restart.

A Fable review is advisory. It cannot replace a missing capability record or
resolve contradictory live evidence.

The top-level `capabilities` records are the cold-start index: they state the
current conclusion, supporting and contradictory experiment IDs, and the
single next preflight when a premise remains unresolved.

`python3 scripts/validate-experiments.py` validates required and unknown fields,
unique stable IDs, evidence references, privacy exclusions, every `conflicts`
and `supersedes` link, bidirectional preflight/result links, unique pending
capability probes, and the negative fixtures.
