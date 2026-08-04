# ThreadBear

For every ordinary interactive turn in a main Codex Desktop task:

1. Your first action must be one bounded `functions.exec` cell containing the native current-task title call. Its `title` begins exactly `⏳ ThreadBear is working: ` followed by a concise 2–6 word subject for this task. Keep the complete title to one line and at most 58 UTF-16 units after the colon. Omit `threadId`. Do not send commentary or call another tool first.
2. End the response with exactly one compact status footer chosen from the forms below.
3. Immediately before the final response, use the same bounded cell with `title` exactly equal to that footer line and no `threadId`, then deliver the response.

For both title moments, replace the title literal and execute this exact shape:

```js
const result = await Promise.race([
  tools.codex_app__set_thread_title({title:"REPLACE WITH THE REQUIRED TITLE"})
    .then(value => ({status:"returned", value}))
    .catch(error => ({status:"failed", error:String(error)})),
  new Promise(resolve => setTimeout(() => resolve({status:"timeout"}), 4000))
]);
text(result);
```

Make exactly one native attempt. The four-second timer is the total wait budget. If it wins, the write result is unknown: end the cell, never retry or await that promise, and continue the turn. Also continue after an explicit returned failure. Do not call the native title tool directly outside this bounded cell.

ThreadBear uses that first-call subject only when a fresh task still exposes its raw first message; explicit names, generated short titles, committed subjects, and later user renames remain authoritative. Its hooks expand the compact inputs into the stable visible title. They do not repair stopped turns. If the user stops a turn, the running title remains until the next real turn replaces it.

## Status footer

Use the matching literal example as the footer's shape:

- Finished with no warranted follow-up: `🧵🐻 complete`
- Finished with one concrete action for the user: `🧵🐻 next steps (you): approve the release plan`
- Finished with one concrete action for the agent: `🧵🐻 next steps (agent): implement the approved plan`
- Finished with one concrete action for someone or something external: `🧵🐻 next steps (external): review the security exception`
- Waiting for required user input: `🧵🐻 needs input (you): choose the release region`
- Unable to continue because of an external condition: `🧵🐻 blocked (external): restore the signing service`
- Healthy scheduled or automated work with nothing pending: `🧵🐻 automation`

The footer must be the final non-empty line. Never write the literal placeholder words `STATUS`, `OWNER`, or `ACTION`. Report the turn's actual disposition. Use `complete` unless the substantive response already establishes one clear, concrete, warranted next step. Generic offers, speculative possibilities, and mentions of recorded work do not qualify.

A prepared uninstall suspends this turn protocol from the moment `uninstall --prepare` succeeds until that operation is aborted or committed: make no running-title or footer call while it is pending. After commit has removed the managed hooks and guidance, respond without another title call or ThreadBear footer so the clean control-task title is not decorated again. After abort, ordinary title calls resume on the next turn.
