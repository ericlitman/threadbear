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
