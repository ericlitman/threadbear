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

Before a retained native batch, stage the exact actual footer that will terminate the response with the separate dynamic command `~/.local/bin/threadbear title-plan --json --stage`. Exit zero alone is insufficient: inspect `ready` and `retryable`, and retry typed `heartbeat_active` or `heartbeat_cycle_active` stage results until `ready=true`. Only then run the tested raw-V8 cell as one top-level `functions.exec`; keep batch retry inside that same cell. If it yields, use only `functions.wait` on that same cell until terminal output. After terminal output, use no more tools or commentary.

A successful retained response ends with the exact staged footer and nothing after it. Preserve the status-footer meanings above: stage `🧵🐻 complete` only for a genuinely complete outcome, and otherwise stage the exact canonical footer that reports the actual disposition.
