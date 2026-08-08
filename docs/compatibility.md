# Compatibility

ThreadBear supports macOS 12 or newer on Apple silicon and Intel, Codex Desktop's stdio App Server, the mounted app-native `set_thread_title` tool, and the current task ID supplied to terminal commands. Release canaries record the exact Codex version used for proof.

The terminal planner starts one bounded `codex app-server --stdio` process. It requires an exact current-task match and nonblank native `name`, resolves the safe subject, and returns a prepared title without writing it. A protocol, ID, process, timeout, or unsafe-title failure stays local and is never retried.

When `write_required` is true, the same terminal cell calls the mounted Codex app's native setter once with no explicit task ID. The mounted boundary normally returns raw JSON text, which the cell decodes once; it also accepts an already-decoded object. Success still requires the exact planned task ID and title. A throw, undecodable or non-object response, or mismatch stays local; there is no alternate writer or reconciliation path. If the outer cell yields after 30 seconds, the task waits only for that same running cell. The yield does not cancel a slow native call, which can delay the response; it never starts another cell or retries.

`onboard --dry-run --json` follows the complete unarchived `thread/list` catalog, tolerates interleaved notifications, and deduplicates IDs. Rows with null or blank `name` remain raw and unowned regardless of `preview`. Any page failure aborts before mutation. After consent, `onboard --noninteractive --confirm --json` takes a fresh complete snapshot, stores safe subjects, and returns every prepared action with its snapshot title and desired title, no item cap, no per-target app read, and zero title writes. The installed skill resumes only that same preparation process if it yields, then serially reads each prepared target through the mounted app immediately before any explicit-target write. It decodes raw JSON-text reads and setter results once while retaining object compatibility. A read failure, wrong returned ID, or title drift is skipped without a write; an exact ID/title match receives at most one setter call.

ThreadBear never opens Codex SQLite or edits Desktop storage. It runs no App Server daemon or proxy, keeps no App Server cache, uses no model, and has no retry or alternate read/write path.

Visible titles are at most 60 UTF-16 units and never split a surrogate pair. A subject that would not fit intact is left unchanged. ThreadBear does not truncate it. Native acknowledgement is not rendered-product proof, so every release verifies the active header and sidebar before and after restart.

The supported public commands are `install`, `title`, `onboard`, `status`, `self-test`, `update`, `uninstall`, and `version`. There is no `inventory`, `migration`, `maintenance`, archive, classifier, controller, or persistent ThreadBear-task API.

The daily update-only LaunchAgent requires ordinary per-user `launchd` support. Its health is reported separately from title-core `ready`. It does not need `sudo`, Full Disk Access, a model call, or a persistent Codex task. Release binaries are checksum-verified but are not Developer ID signed or notarized.
