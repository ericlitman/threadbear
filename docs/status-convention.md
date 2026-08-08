# Status convention

Immediately before an ordinary final response, ThreadBear's managed guidance runs one terminal cell whose local planner receives one of:

```text
threadbear title --status complete --json
threadbear title --status next_steps --json
threadbear title --status needs_input --json
threadbear title --status blocked --json
threadbear title --status automation --json
```

The status maps to one owned icon:

| Status | Visible title |
| --- | --- |
| `complete` | `✅ <exact subject>` |
| `next_steps` | `➡️ <exact subject>` |
| `needs_input` | `🙋 <exact subject>` |
| `blocked` | `🚨 <exact subject>` |
| `automation` | `🤖 <exact subject>` |

The enum controls only the icon. The planner writes no Codex title; when a change is needed, the same cell makes one native title call through the mounted Codex app and accepts only the exact returned task ID and title. Any owner or next action stays in the substantive response. There is no special ThreadBear line appended to the response and no running icon. Ordinary turns never emit the neutral onboarding mark `🐻`.

ThreadBear reuses its stored subject when the current title byte-matches a valid owned rendering. Any other safe current title is a user rename and becomes the exact subject, including user-authored emoji and arrows. A null or blank native name is raw and stays unchanged; `preview` is never adopted. Multiline, control-bearing, raw internal, ambiguous unowned legacy-prefixed, or overlong subjects also stay unchanged. ThreadBear never normalizes, strips, or truncates a subject.

Use `complete` when work is finished with no warranted follow-up; `next_steps` only when the response establishes one concrete next action; `needs_input` for required user input; `blocked` for an external blocker; and `automation` for healthy automated work with nothing pending. Generic offers and speculative possibilities do not qualify as next steps.
