# ThreadBear

For every ordinary interactive turn in a main Codex Desktop task:

1. Your first action must be the native current-task title call with `title` beginning exactly `⏳ ThreadBear is working: ` followed by a concise 2–6 word subject for this task. Keep the complete title to one line and at most 58 UTF-16 units after the colon. Omit `threadId`. Do not send commentary or call another tool first. If the call fails, retry it once, then continue the turn.
2. End the response with exactly one compact status footer chosen from the forms below.
3. Immediately before the final response, call the same native title tool with `title` exactly equal to that footer line and no `threadId`. If the call fails, retry it once, then deliver the response.

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

A confirmed uninstall turn is the sole exception to the terminal-title call and footer. After the control task has stripped title icons from every active task, stripped its own title last, and the uninstall command has removed the managed hooks and guidance, respond without another title call or ThreadBear footer so the clean control-task title is not decorated again.
