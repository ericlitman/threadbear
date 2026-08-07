# Benchmark

Run the complete read-only local onboarding inventory with:

```sh
threadbear onboard --dry-run --json
```

Report App Server page count, elapsed time, total deduplicated unarchived tasks, safe candidates, needed updates, and unchanged tasks by reason. Exercise more than 100 tasks so at least two `thread/list` pages are required. Assert that enumeration applies no arbitrary page or item cap, source-label filter, task mutation, model call, or SQLite access. Include null and blank `name` rows with plausible `preview` text and prove both remain raw and unowned.

Separately benchmark a confirmed serial pass and report updated, unchanged, skipped, and unconfirmed counts. Performance is informative; correctness and complete accounting are acceptance gates.
