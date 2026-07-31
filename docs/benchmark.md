# Benchmark

Run a mutation-free local scan with:

```sh
THREADBEAR_CONTROL_TASK_ID="$CODEX_THREAD_ID" \
THREADBEAR_STATE_DIR="$(mktemp -d)" \
threadbear heartbeat --dry-run --json
```

Report inventory size, deterministic and ambiguous counts, scan milliseconds,
model calls, and title-application milliseconds separately. A dry run never
creates state, opens App Server, calls Luna, or changes a title.
