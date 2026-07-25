# ThreadBear

Use ThreadBear's CLI for Codex task-state and lifecycle operations.

Start with read-only commands:

- `threadbear status` for version, LaunchAgent health, heartbeat, control task, preferences, retries, and update-check state.
- `threadbear inspect TASK_ID` for saved classification evidence, next action, retry, and archive eligibility.
- `threadbear heartbeat --dry-run` to preview due work without models or mutations.
- `threadbear self-test` to validate the installed platform, Codex surfaces, state, config, managed files, binary, and LaunchAgent.

Use `threadbear configure`, `enable`, `disable`, `restore`, `update`, or `uninstall` only when the user explicitly requests that lifecycle action. Preview configuration/removal effects and preserve normal Codex command approval; never request broader permissions for ThreadBear.

Never edit ThreadBear state files, `.codex-global-state.json`, Codex Desktop private caches, task databases, AGENTS.md managed markers, skill managed markers, or LaunchAgent files directly. Do not click through Desktop, force sidebar refreshes, invent rollback state, or wake a model merely to inspect ThreadBear status.

When explaining title state, use the canonical meanings: `⏳` running, `🚨` blocked, `🙋` needs input, `🤖` automation, `➡️` next steps, `✅` complete, and `❔` unknown. Only completed inactive tasks can be auto-archived.
