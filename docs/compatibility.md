# Compatibility

ThreadBear supports macOS 12 or newer on Apple silicon and Intel, Codex Desktop
tasks indexed with `source='vscode'`, Codex App Server `thread/read`, and the
Codex Desktop native title setter exposed to the retained task.

The SQLite dependency is read-only. ThreadBear selects the highest local
`state_N.sqlite` database and requires the current `threads` columns used by the
task index. An unsupported schema fails closed without title mutation.

Visible titles are at most 60 UTF-16 units. The native handoff must return the
exact requested task ID and title, and the task index must then expose that
title. ThreadBear does not write Desktop caches, private UI databases, or
invented compatibility state.

The supported public commands are `install`, `heartbeat`, `status`,
`self-test`, `uninstall`, and `version`. The former configuration, lifecycle,
archive, inspect, update, and restore commands were removed with their product
scope rather than retained as adapters.
