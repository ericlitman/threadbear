# Status convention

Immediately before an ordinary final response, ThreadBear's managed guidance runs one tiny terminal loader whose stateless local command receives one of:

```text
threadbear title-script --status complete
threadbear title-script --status next_steps
threadbear title-script --status needs_input
threadbear title-script --status blocked
threadbear title-script --status automation
```

The status maps to one owned icon:

| Status | Visible title |
| --- | --- |
| `complete` | `✅ <exact subject>` |
| `next_steps` | `➡️ <exact subject>` |
| `needs_input` | `🙋 <exact subject>` |
| `blocked` | `🚨 <exact subject>` |
| `automation` | `🤖 <exact subject>` |

The enum controls only the icon. The verified binary binds the task ID and fixed policy into its embedded reviewed program without reading Codex or writing state. The loader evaluates that complete program uncached inside the current Codex tool context. The mounted app reads the exact current title; when a change is needed, the program makes one native title call with `threadId` omitted and accepts only the exact returned task ID/title. The public `title --status ENUM --json` command remains a data-only diagnostic for the same plan. Any owner or next action stays in the substantive response. There is no current-turn footer, running icon, or neutral bear status.

During the one post-install existing-task first read, conservative historical inference uses the corresponding `✅✦ `, `➡️✦ `, `🙋✦ `, `🚨✦ `, or `🤖✦ ` prefix. `✦` means first read, not warning; the next ordinary turn strips it and writes one plain current prefix. Unknown history gets no decoration.

ThreadBear strips at most one of its exact removable prefixes: the five status icons above, the five inferred prefixes, or legacy neutral `🐻 `. The bear is cleanup-only and is never emitted. Every other safe current byte is the subject, including user-authored emoji and arrows. A title beginning with a removable prefix is the deliberate visible ambiguity; other old ThreadBear prefixes, blank, multiline, control-bearing, raw internal, or overlong titles stay unchanged. ThreadBear never normalizes or truncates a subject.

Use `complete` when work is finished with no warranted follow-up; `next_steps` only when the response establishes one concrete next action; `needs_input` for required user input; `blocked` for an external blocker; and `automation` for healthy automated work with nothing pending. Generic offers and speculative possibilities do not qualify as next steps.
