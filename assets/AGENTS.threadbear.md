# ThreadBear

## Status footer

End each terminal response with exactly one compact status line. Use the matching literal example as its shape:

- Finished with no warranted follow-up: `🧵🐻 complete`
- Finished with one concrete action for the user: `🧵🐻 next steps (you): approve the release plan`
- Finished with one concrete action for the agent: `🧵🐻 next steps (agent): implement the approved plan`
- Finished with one concrete action for someone or something external: `🧵🐻 next steps (external): review the security exception`
- Waiting for required user input: `🧵🐻 needs input (you): choose the release region`
- Unable to continue because of an external condition: `🧵🐻 blocked (external): restore the signing service`
- Healthy scheduled or automated work with nothing pending: `🧵🐻 automation`

Never write the literal placeholder words `STATUS`, `OWNER`, or `ACTION` in the line. Start with `🧵🐻` — the full mark, both characters — and one of the lowercase states shown above.

Report the turn's actual disposition; do not invent or recommend work to populate this line. Use `complete` unless the substantive response already ends with one clear, concrete, warranted next step. Generic offers, speculative possibilities, and mentions of recorded work do not qualify.

## Retained native title drain

Before a retained native batch, stage the fixed safety footer `🧵🐻 next steps (agent): retry the first title handoff` with the separate dynamic command `~/.local/bin/threadbear title-plan --json --stage`. Exit zero alone is insufficient: inspect `ready` and `retryable`, and retry typed `heartbeat_active` or `heartbeat_cycle_active` stage results once per second for at most 120 attempts until `ready=true`. Only then run the exact raw-V8 cell embedded in the installed ThreadBear skill as one top-level `functions.exec`. That cell drains the first guarded batch, kickstarts exactly one immediate LaunchAgent continuation only when `continuation_due=true`, gives every command and native setter one shared five-minute elapsed-time and 300-attempt budget, drains all remaining guarded plans, and stages plus drains the final footer that matches its `complete` result. Normal handoff work stops at the five-minute deadline. If the result becomes incomplete before that deadline, it restores the retry footer. If the deadline expires before success finalization begins, it makes no more tool calls and reports `complete=false`, leaving the pre-staged retry footer as the conservative fallback. If and only if the deadline expires after success finalization has begun, it performs one bounded compensating retry-footer restoration through the existing guarded plan bridge, allows at most ten one-second typed-busy waits, and then reports `complete=false`. If it yields, use only `functions.wait` on that same cell until terminal output. After terminal output, use no more tools or commentary.

End with `🧵🐻 complete` only when the cell reports `complete=true`; otherwise end with `🧵🐻 next steps (agent): retry the first title handoff`. The cell already completed all permitted footer work, including the bounded compensation when applicable, so write nothing after that footer.
