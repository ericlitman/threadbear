# Benchmark

Run the complete read-only local uninstall-cleanup inventory with:

```sh
threadbear uninstall --dry-run --json
```

Report App Server page count, elapsed time, total deduplicated unarchived tasks, titles needing cleanup, unchanged titles, and skipped titles by reason. Exercise more than 100 tasks so at least two `thread/list` pages are required. Assert that enumeration applies no arbitrary page or item cap, source-label filter, task mutation, model call, SQLite access, or artifact removal. Include null and blank `name` rows with plausible `preview` text and prove both remain raw and unowned.

Separately benchmark confirmed preparation and the serial mounted app-native pass. Report prepared, updated, unchanged, skipped, and unconfirmed counts. Performance is informative; correctness, exact native responses, and complete accounting are acceptance gates.
