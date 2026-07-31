# Benchmark

Run the read-only local inventory with:

```sh
threadbear inventory --json
```

Report task count, deterministic count, ambiguous count, and elapsed time. Inventory reads the Codex index and settled rollout tails; it does not write titles, call a model, or create migration work.
