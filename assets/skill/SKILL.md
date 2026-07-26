# ThreadBear

Use ThreadBear's CLI for Codex task-state and lifecycle operations.

Start with read-only commands:

- `~/.local/bin/threadbear status` for version, LaunchAgent health, heartbeat, control task, preferences, retries, and update-check state.
- `~/.local/bin/threadbear inspect TASK_ID` for saved classification evidence, next action, retry, and archive eligibility.
- `~/.local/bin/threadbear heartbeat --dry-run` to preview due work without models or mutations.
- `~/.local/bin/threadbear self-test` to validate the installed platform, Codex surfaces, state, config, managed files, binary, and LaunchAgent.

Use `~/.local/bin/threadbear configure`, `~/.local/bin/threadbear enable`, `~/.local/bin/threadbear disable`, `~/.local/bin/threadbear restore`, `~/.local/bin/threadbear update`, or `~/.local/bin/threadbear uninstall` only when the user explicitly requests that lifecycle action. Preview configuration/removal effects and preserve normal Codex command approval; never request broader permissions for ThreadBear.

Never edit ThreadBear state files, `.codex-global-state.json`, Codex Desktop private caches, task databases, AGENTS.md managed markers, skill managed markers, or LaunchAgent files directly. Do not click through Desktop, force sidebar refreshes, invent rollback state, or wake a model merely to inspect ThreadBear status.

When explaining title state, use the canonical meanings: `⏳` running, `🚨` blocked, `🙋` needs input, `🤖` automation, `➡️` next steps, `✅` complete, and `❔` unknown. Only completed inactive tasks can be auto-archived.

When managed global guidance is enabled, terminal responses use one concrete footer line:

- `🧵🐻 complete`
- `🧵🐻 next steps (you): approve the release plan`
- `🧵🐻 next steps (agent): implement the approved plan`
- `🧵🐻 next steps (external): review the security exception`
- `🧵🐻 needs input (you): choose the release region`
- `🧵🐻 blocked (external): restore the signing service`
- `🧵🐻 automation`

Never emit the literal placeholder words `STATUS`, `OWNER`, or `ACTION`. Report the actual disposition, and do not invent work merely to populate a next action; generic offers, speculative possibilities, and recorded-work mentions do not qualify.
