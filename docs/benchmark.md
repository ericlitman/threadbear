# Aggregate classifier benchmark

ThreadBear was evaluated on a natural corpus of 89 cases plus an adversarial challenge set of 12 cases. False-complete was tracked separately because incorrectly hiding unfinished work is the dangerous error.

Only aggregate results are published here. The private evaluation corpus contains real user messages; raw messages, task text, task paths, per-case outputs, and private corpus files are not part of this repository.

## Model and effort comparison

| Model / effort | Accuracy | False-complete | Result |
|---|---:|---:|---|
| Luna low | 88/89 + 12/12 | 1 | Finalist |
| Luna medium | 87/89 + 12/12 | 0 | Finalist |
| Luna high | 86/89 + 12/12 | 0 | More conservative, no quality gain |
| Luna xhigh | Timed out at 240 seconds | — | Eliminated |
| Terra low | 87/89 + 12/12 | 1 | Eliminated |
| Terra medium | 85/89 + 12/12 | 1 | Eliminated; also omitted one input |

## Finalist hard-set stability

The finalists were rerun five times on 31 hard cases.

| Finalist | Stable hard cases | Mean hard-set accuracy | Runs with a dangerous false-complete |
|---|---:|---:|---:|
| Luna low | 27/31 | 29.0/31 | 4/5 |
| Luna medium | 27/31 | 29.6/31 | 1/5 |

## Cascade result

The evaluated deterministic-plus-Luna-medium cascade reached **89/89 natural labels and 12/12 challenge labels with zero false-completes**.

The product uses deterministic precedence and valid agent footers before semantic fallback, so these classifier results are not a claim that every heartbeat invokes Luna. Unchanged work costs zero model tokens, mechanically resolved changes bypass Luna, and only unresolved changed tasks enter fresh non-persisted classifier sessions.

The public synthetic regression fixtures separately cover all seven product states and assert that no false-next-steps result is accepted. This is repository test coverage, not an additional private-corpus metric.

No per-state breakdown or claim beyond the approved aggregate evidence is published here.

## First-sweep performance release gate

BEAR-87 separates the one-time legacy-history cost from ordinary status-guided
changes. Each benchmark replica carries an aggregate-only `threadbear-cohort.json` declaring `cohort`, `preparation`, and an expected 150–250 observations. Run both cohorts against isolated copied Codex homes and publish only
the cohort/mode-specific aggregate JSON emitted by `scripts/replica-rehearsal.sh`; the four successful runs must not overwrite one another. Never publish task
text, identifiers, titles, paths, flags, or classifier payloads.

| Cohort | Observations | Deterministic | Luna | First / previous requests | First progress | Total convergence | Retries / rate limits | Helper proof |
|---|---:|---:|---:|---:|---:|---:|---:|---|
| Legacy first adoption | pending live gate | pending | pending | pending | pending | pending | pending | pending |
| Status-guided changed work | pending live gate | pending | pending | pending | pending | pending | pending | pending |

Run each cohort with `THREADBEAR_FIRST_SWEEP_BENCHMARK=1`, once with `THREADBEAR_CLASSIFIER_MODE=serial` and once with
`THREADBEAR_CLASSIFIER_MODE=bounded`. The bounded mode may become the compiled
default only when it lowers total convergence time with no worse first-progress
latency, retries, rate limits, cancellation, row salvage, or restart recovery.
Capacity-sized packing may produce one request, in which case serial execution is
the measured result.

The product copy remains “ambiguity fallback” until the reviewed status-guided
cohort contains approximately 200 changed observations and sends no more than 5%
to Luna. Legacy-history results never qualify for the word “rare.”
