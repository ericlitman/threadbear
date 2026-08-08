# Status convention

Immediately before an ordinary final response, ThreadBear's managed guidance runs one terminal cell whose stateless local helper receives one of:

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

The enum controls only the icon. The helper returns the task ID and fixed policy without reading Codex or writing state. The mounted app reads the exact current title; when a change is needed, the same cell makes one native title call and accepts only the exact returned task ID/title. Any owner or next action stays in the substantive response. There is no ThreadBear footer or running icon. Ordinary turns never emit the neutral onboarding mark `🐻`.

ThreadBear strips at most one of its exact reserved prefixes: the five status icons above or neutral `🐻 `. Every other safe current byte is the subject, including user-authored emoji and arrows. A title beginning with a reserved prefix is the deliberate visible ambiguity; other old ThreadBear prefixes, blank, multiline, control-bearing, raw internal, or overlong titles stay unchanged. ThreadBear never normalizes or truncates a subject.

Use `complete` when work is finished with no warranted follow-up; `next_steps` only when the response establishes one concrete next action; `needs_input` for required user input; `blocked` for an external blocker; and `automation` for healthy automated work with nothing pending. Generic offers and speculative possibilities do not qualify as next steps.
