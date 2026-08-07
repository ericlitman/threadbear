# Compatibility

ThreadBear supports macOS 12 or newer on Apple silicon and Intel, Codex Desktop's stdio App Server, and the current task ID supplied to terminal commands. Release canaries record the exact Codex version used for proof.

The terminal writer starts one bounded `codex app-server --stdio` process. It requires an exact current-task match and nonblank native `name`, makes at most one `thread/name/set` request, and requires exact readback to confirm a change. A protocol, ID, process, timeout, unsafe-title, or readback failure stays local and is never retried.

`onboard --dry-run --json` follows the complete unarchived `thread/list` catalog, tolerates interleaved notifications, and deduplicates IDs. Rows with null or blank `name` remain raw and unowned regardless of `preview`. Any page failure aborts before mutation. `onboard --noninteractive --confirm --json` rereads each candidate and processes the complete safe set serially with no item cap.

ThreadBear never opens Codex SQLite or edits Desktop storage. It runs no App Server daemon or proxy, keeps no App Server cache, uses no model, and has no retry or alternate read/write path.

Visible titles are at most 60 UTF-16 units and never split a surrogate pair. A subject that would not fit intact is left unchanged. ThreadBear does not truncate it. App Server acknowledgement is not rendered-product proof, so every release verifies the active header and sidebar before and after restart.

The supported public commands are `install`, `title`, `onboard`, `status`, `self-test`, `update`, `uninstall`, and `version`. There is no `inventory`, `migration`, `maintenance`, archive, classifier, controller, or persistent ThreadBear-task API.

The daily update-only LaunchAgent requires ordinary per-user `launchd` support. Its health is reported separately from title-core `ready`. It does not need `sudo`, Full Disk Access, a model call, or a persistent Codex task. Release binaries are checksum-verified but are not Developer ID signed or notarized.
