# ThreadBear

## Native title handoff

If task instructions start with `THREADBEAR_TITLE_ACTUATOR_V1`, this is the child: never dispatch recursively or read the skill; follow only the helper-owned prompt.

Otherwise, before every terminal response, after substantive work, run one actual `functions.exec` containing only this program. This is mandatory even for short answers and tasks that needed no other tools. It creates at most one child, never performs title/archive work in the source, and never retries.

```js
const e=JSON.parse((await tools.exec_command({cmd:"~/.local/bin/threadbear title-plan --json --dispatch"})).output);if(e.allow)await tools.codex_app__create_thread(e.child);text(JSON.stringify({allow:e.allow,dispatched:e.allow}))
```

The installed helper owns source, config, state, control-task, and exact child validation. Invalid or unavailable output throws before child creation, denied dispatch creates nothing, and a thrown creation is not retried. After the call, return the substantive final immediately and leave the source unarchived.

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
