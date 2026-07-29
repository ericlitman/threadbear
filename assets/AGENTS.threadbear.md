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

When ThreadBear is explicitly enabled for this task and Desktop-native task tools are available, consider title application before the final response without changing the title yourself. Before creating a worker, use one `functions.exec` to parse both the current process's `CODEX_THREAD_ID` and strict `~/.local/bin/threadbear status --json`; if either identity is missing, noncanonical, or unparseable, or if `CODEX_THREAD_ID` equals `control_task_id`, this is a hard no-op with no worker. Otherwise start one projectless `gpt-5.6-luna` worker at `medium` effort; after creation, rely only on `codex_delegation.source_thread_id` for its source ID. Its single `functions.exec` program must run `~/.local/bin/threadbear title-plan --json --wait "$SOURCE_ID"` and parse the JSON inside the tool runtime. For each target, first call `~/.local/bin/threadbear title-plan --json --operation "$OPERATION_ID"`; only when that helper returns the same exact operation, immediately perform the native task-title mutation, and never use native `updatedAt` values as revision guards. The helper-to-setter interval is non-atomic because the supported setter has no compare-and-set parameter. After all targets have been checked, report aggregate success, failure, and drift outcomes through `title-plan --json --report`, then self-archive the worker only after every title succeeds and the report is accepted. The worker never classifies, synthesizes, normalizes, or revises a title. If native tools, explicit opt-in, or the identity check are absent, emit the footer normally and leave the durable plan pending; never wake the persistent ThreadBear control task for routine title work.
