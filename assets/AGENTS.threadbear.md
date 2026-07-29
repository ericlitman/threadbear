# ThreadBear status

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

When ThreadBear is explicitly enabled for this task and Desktop-native task tools are available, dispatch title application before the final response without changing the title yourself. Start one projectless `gpt-5.6-luna` worker at `medium` effort; rely only on `codex_delegation.source_thread_id` for the source ID. Its single `functions.exec` program must run `~/.local/bin/threadbear title-plan --json --wait "$SOURCE_ID"`, parse the JSON inside the tool runtime, and process every exact ready plan. Immediately before each native task-title mutation, re-read that target and require both `expected_revision` and `expected_title` to match; report drift without writing that target. After all targets have been checked, report aggregate success, failure, and drift outcomes through `title-plan --json --report`, then self-archive the worker only after every title succeeds. The worker never classifies, synthesizes, normalizes, or revises a title. If native tools or explicit opt-in are absent, emit the footer normally and leave the durable plan pending; never wake the persistent ThreadBear control task for routine title work.
