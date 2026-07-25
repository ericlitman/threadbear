# ThreadBear status convention

When managed global guidance is enabled, ThreadBear asks terminal agents to end each response with exactly one compact line:

```text
🐻 STATUS · next (OWNER): ACTION
```

Allowed `STATUS` values are `complete`, `next steps`, `needs input`, `blocked`, and `automation`. Allowed `OWNER` values are `you`, `agent`, `external`, and `none`.

The governing instruction is:

> Report the turn's actual disposition; do not invent or recommend work to populate this line. After finished work, use `complete · next (none): none` unless the substantive response already ends with one clear, concrete, warranted next step; generic offers, speculative possibilities, and mentions of recorded work do not qualify.

Completed work with no warranted follow-up must end:

```text
🐻 complete · next (none): none
```

## Footer examples

```text
🐻 needs input · next (you): choose the release region
🐻 next steps · next (agent): create the implementation plan
🐻 blocked · next (external): wait for vendor access
🐻 automation · next (agent): monitor the deployment
🐻 complete · next (none): none
```

A valid footer is a deterministic signal, so ThreadBear can update the title without another classifier call. The footer itself is removed from title input.

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
Footer: 🐻 needs input · next (you): choose the release region
Title:  🙋 Release deployment → choose the release region
```

ThreadBear preserves the durable subject, adds the concise action, and never places the footer line itself in the title.
