# ThreadBear

Use ThreadBear's CLI for task lifecycle operations. Prefer read-only `threadbear status`, `threadbear inspect TASK_ID`, and `threadbear heartbeat --dry-run` before changing state. Use `threadbear configure`, `enable`, `disable`, `restore`, or `uninstall` only when the user explicitly requests that lifecycle action.

Never edit ThreadBear state files, Codex Desktop private global state, sidebar caches, task databases, or LaunchAgent files directly. Do not wake a model merely to inspect ThreadBear status.
