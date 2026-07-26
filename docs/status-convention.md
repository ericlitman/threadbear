# ThreadBear status convention

When managed global guidance is enabled, ThreadBear asks terminal agents to end each response with exactly one compact line. The managed block leads with concrete forms:

```text
🧵🐻 complete
🧵🐻 next steps (you): approve the release plan
🧵🐻 next steps (agent): implement the approved plan
🧵🐻 next steps (external): review the security exception
🧵🐻 needs input (you): choose the release region
🧵🐻 blocked (external): restore the signing service
🧵🐻 automation
```

The grammar has two shapes: the mark followed by one lowercase state on its own (`complete` and `automation`, which carry no owner or action), or the mark, the state, an owner in parentheses, and an action after the colon (`next steps`, `needs input`, `blocked`). The five states are `complete`, `next steps`, `needs input`, `blocked`, and `automation`; the four owners are `you`, `agent`, `external`, and `none`.

The governing instruction is:

> Report the turn's actual disposition; do not invent or recommend work to populate this line. Use `complete` unless the substantive response already ends with one clear, concrete, warranted next step. Generic offers, speculative possibilities, and mentions of recorded work do not qualify.

Agents must never emit the literal placeholder words `STATUS`, `OWNER`, or `ACTION`. The examples above are complete footer lines, not fill-in-the-blank templates.

A valid footer is a deterministic signal, so ThreadBear can update the title without another classifier call. The footer stays out of the title by construction: managed titles are composed from the durable subject and the footer's action clause, never from the body of the message.

Do not manufacture next steps. “If you want, I can help with that,” generic possibilities, and references to recorded work do not qualify as concrete recommendations. Stronger unfinished evidence also wins: a response that still needs a user choice is `needs input`, even if it suggests what might happen afterward.

Malformed, quoted, embedded, stale, contradicted, or incomplete footers are rejected and fall through to stronger structured evidence or semantic classification.

## Seven task states

| State | Title prefix | Meaning | Auto-archive eligible |
|---|---|---|---:|
| running | `⏳` | a runtime is active | no |
| blocked | `🚨` | structured or semantic evidence shows a blocker | no |
| needs input | `🙋` | the current request needs the user | no |
| automation | `🤖` | healthy automated work is waiting/running | no |
| next steps | `➡️` | current work is complete with one concrete warranted follow-up | no |
| complete | `✅` | current work is finished with no warranted follow-up | yes, after configured inactivity |
| unknown | `❔` | evidence is insufficient or work was interrupted | no |

Example transformation:

```text
Footer: 🧵🐻 needs input (you): choose the release region
Title:  🙋 Release deployment → choose the release region
```

ThreadBear preserves the durable subject, adds the concise action, and never places the footer line itself in the title.

When token display is enabled, ThreadBear formats cumulative output tokens from the rollout's latest `token_count` event with stable magnitude buckets such as `1.2k`, `340k`, and `1.6m`. Start mode renders `🚨 1.6m Subject`; end mode renders `🚨 Subject · out 1.6m`. The figure is part of the managed title zone, so user edits remain the durable subject, disabling the setting removes only the figure, and a title write occurs only when the rendered bucket changes.
