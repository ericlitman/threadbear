# Live classifier release gate

The live evaluation runs ThreadBear's shipped status cascade against an
operator-supplied corpus. It applies deterministic runtime and footer resolution
first, then sends only unresolved cases through the configured ephemeral
classifier using the shipped prompt and output schema. Classifier-owned cases
run independently so one response cannot influence another case or corrupt its
task ID.

The corpus stays outside this public repository. Run the gate with absolute
paths:

```sh
THREADBEAR_LIVE_EVAL=1 \
THREADBEAR_LIVE_AUTH_FILE=/absolute/path/to/auth.json \
THREADBEAR_LIVE_EVAL_CORPUS=/absolute/path/to/live-eval-corpus.json \
go test -tags=integration ./internal/status -run TestLiveLunaMediumCorpus -v -count=1
```

The default classifier is `gpt-5.6-luna` at `medium` effort. Operators may set
`THREADBEAR_LIVE_MODEL`, `THREADBEAR_LIVE_EFFORT`,
`THREADBEAR_LIVE_EVAL_CONTEXT_BYTES`, `THREADBEAR_LIVE_EVAL_TIMEOUT`, or
`THREADBEAR_LIVE_CODEX_BIN` to exercise another supported configuration.

The JSON corpus contract is:

```json
{
  "schema_revision": "threadbear.live-eval.v1",
  "cases": [
    {
      "id": "stable-case-id",
      "expected": "complete",
      "provenance": {
        "model": "gpt-5.6-sol",
        "effort": "xhigh",
        "source": "vscode",
        "agents_block_version": "v3"
      },
      "facts": {
        "waiting_for_user": false,
        "runtime_active": false,
        "structured_failure": false,
        "healthy_idle_automation": false,
        "interrupted": false,
        "footer": {
          "message": "Finished.\n🧵🐻 complete",
          "latest_turn_completed": true,
          "newer_user_message": false,
          "stale": false,
          "structured_status": ""
        }
      },
      "latest": {
        "user": "Finish the task.",
        "final_agent": "Finished.\n🧵🐻 complete"
      },
      "previous": {
        "user": "Earlier request.",
        "final_agent": "Earlier response."
      }
    }
  ]
}
```

Allowed expected states are `blocked`, `needs_input`, `running`, `automation`,
`next_steps`, `complete`, and `unknown`. Every case must include authoring model,
effort, source, and managed-block provenance. The report includes per-state
accuracy, provenance and footer-path counts, individual errors, and the
release-blocking false-complete and false-next-steps totals. A classifier
transport/schema diagnostic invalidates the run. Any false-complete or
false-next-steps fails the gate.
