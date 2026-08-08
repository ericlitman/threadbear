# Compatibility

ThreadBear supports macOS 12 or newer on Apple silicon and Intel, Codex Desktop 0.146.0 or newer, mounted app-native `read_thread` and `set_thread_title`, and the current task ID supplied to terminal commands. Install, self-test, and status reject an older or missing Desktop command before title work begins.

ThreadBear resolves Codex only from fixed Desktop locations: the system or user Applications bundle and the Desktop-managed `~/.local/bin/codex`. It never executes `codex` from ambient repository `PATH`.

For an ordinary turn, the local `title` command returns only the validated task ID and fixed title policy. The mounted app reads the exact current task and, if needed, writes once with no explicit target ID. Raw JSON-text results are decoded once; already-decoded objects are also accepted. A wrong ID, unsafe title, throw, undecodable response, or non-exact setter result stays local with no alternate reader, writer, or retry.

Ordinary title handling starts no App Server and writes no ThreadBear state, so it works under Codex's default workspace permissions. The six exact ThreadBear icon prefixes are reserved; every other safe leading emoji and subject byte is preserved. Visible titles are at most 60 UTF-16 units and are never truncated.

Historical onboarding explicitly asks for command permission, then follows the complete unarchived App Server `thread/list` catalog, tolerates notifications, and deduplicates IDs. Null or blank `name` stays raw regardless of `preview`. A later-page failure returns no partial plan. Confirmed preparation stores no subjects and writes no titles. The installed skill serially rereads each prepared target through the mounted app immediately before its one possible explicit-target setter call; drift or wrong IDs are skipped without retry.

ThreadBear never opens Codex SQLite or edits Desktop storage. It runs no App Server daemon or proxy, keeps no task-title database or App Server cache, uses no model, and has no controller, queue, reconciliation, or alternate path.

The supported public commands are `install`, `title`, `onboard`, `status`, `self-test`, `update`, `uninstall`, and `version`. The daily update-only LaunchAgent needs no `sudo`, Full Disk Access, model call, or persistent Codex task. Release binaries are checksum-verified but are not Developer ID signed or notarized.
