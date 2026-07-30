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

The product uses deterministic precedence and valid agent footers before semantic fallback, so these classifier results are not a claim that every heartbeat invokes Luna. Unchanged work costs zero model tokens, mechanically resolved changes bypass Luna, and only unresolved changed tasks enter one private non-persisted classifier process per heartbeat, reused across capacity-sized batches.

The public synthetic regression fixtures separately cover all seven product states and assert that no false-next-steps result is accepted. This is repository test coverage, not an additional private-corpus metric.

No per-state breakdown or claim beyond the approved aggregate evidence is published here.

## First-sweep performance release gate

BEAR-87 separates the one-time legacy-history cost from ordinary status-guided
changes. Each benchmark replica carries an aggregate-only `threadbear-cohort.json` declaring `cohort`, `preparation`, and an expected 150–250 observations. Run both cohorts against isolated copied Codex homes and publish only
the cohort/mode-specific aggregate JSON emitted by `scripts/replica-rehearsal.sh`; the four successful runs must not overwrite one another. Never publish task
text, identifiers, titles, paths, flags, or classifier payloads.

| Cohort | Mode | Observations | Deterministic | Luna | First / previous batches | First progress | Convergence | Retries / rate limits | Isolation and recovery gates |
|---|---|---:|---:|---:|---:|---:|---:|---:|---|
| Legacy first adoption | serial | 200 | 67 | 133 | 2 / 1 | 6.869s | 238.710s | 3 / 0 | passed |
| Legacy first adoption | bounded | 200 | 67 | 133 | 2 / 1 | 9.517s | 127.374s | 3 / 0 | passed |
| Status-guided changed work | serial | 200 | 200 | 0 | 0 / 0 | 4.855s | 20.813s | 0 / 0 | passed |
| Status-guided changed work | bounded | 200 | 200 | 0 | 0 / 0 | 6.452s | 27.431s | 0 / 0 | passed |

On exact draft head `7d0fbc7e69dbfc442e36f84b650ec401c13ef403`, the production isolation canary passed on Codex CLI 0.146.0, macOS 26.4 arm64, in 9.77 seconds. Configured OpenKnowledge and node helper sentinels did not start, no matching descendant appeared, and the private classifier root was removed. Every cohort run also passed helper proof, cancellation recovery, row salvage, and the rate-limit gate; installed, self-tested, loaded the isolated LaunchAgent, uninstalled, and removed state successfully.

Run each cohort with `THREADBEAR_FIRST_SWEEP_BENCHMARK=1`, once with `THREADBEAR_CLASSIFIER_MODE=serial` and once with
`THREADBEAR_CLASSIFIER_MODE=bounded`. The bounded mode may become the compiled
default only when it lowers total convergence time with no worse first-progress
latency, retries, rate limits, cancellation, row salvage, or restart recovery.
Capacity-sized packing may produce one request, in which case serial execution is
the measured result.

The reviewed status-guided cohort sent 0 of 200 changed observations to Luna.
Durable product copy may therefore call Luna medium a rare ambiguity fallback in
ordinary status-guided use while retaining the exact boundary: unchanged tasks
use zero model calls and straightforward status-guided changes resolve
deterministically. Legacy history remains a separate one-time pre-guidance case
and never qualifies for the word “rare.” Serial remains the compiled default:
bounded legacy convergence was faster, but first progress regressed from 6.869s
to 9.517s, and bounded status-guided work was slower.
