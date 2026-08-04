# Compatibility

ThreadBear supports macOS 12 or newer on Apple silicon and Intel, Codex Desktop tasks indexed in the current local `state_N.sqlite`, Codex `PreToolUse` and `PostToolUse` hooks, and the native current-task and explicit-target title setter. The native task-catalog contract is verified against Codex 0.146.0.

The hook matcher is the plain literal `codex_appset_thread_title`. The anchored-regex form is not supported because Codex 0.146.0 treated it as match-all. Hook installation preserves unrelated definitions and their array order. Each hook process has a one-second limit, and the managed native-call cell has one attempt with a four-second total wait budget.

ThreadBear reads the highest local Codex state database and fails closed when the required thread schema, calling session ID, current title, hook payload, or exact native result is unavailable. Inventory mirrors the verified local native catalog: unarchived records with a nonempty preview and source `vscode` or `cli`. Older signed-in ChatGPT chat-history rows may also appear in the Desktop sidebar, but the current native task APIs do not provide pageable enumeration and explicit-target title mutation for that population; ThreadBear neither inventories nor renames them. A release or migration must stop if a read-only inventory-count canary differs from the live local native task catalog. ThreadBear does not run an app-server subprocess or write the Codex database, Desktop caches, or private UI storage.

Visible titles are at most 60 UTF-16 units and never split a surrogate pair. Native setter success is the runtime acknowledgement. Each release must separately prove the rendered active header and sidebar in a fresh Codex Desktop task.

The supported public commands are `install`, `inventory`, `migration`, `maintenance`, `update`, `status`, `self-test`, `uninstall`, and `version`. Guided uninstall uses a prepared active-task owner and one explicit-target native writer; an archived main task is temporarily unarchived and restored through native archive control without opening or navigating to it.
