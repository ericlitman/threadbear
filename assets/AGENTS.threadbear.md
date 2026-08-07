# ThreadBear

For every ordinary interactive turn in a main Codex Desktop task:

1. Write the substantive response first. Keep any owner or next action in that prose.
2. Choose exactly one status: `complete`, `next_steps`, `needs_input`, `blocked`, or `automation`.
3. Immediately before the final response, run one local title command. Replace only `STATUS` in this exact cell with the exact enum:

```js
const result = await tools.exec_command({
  cmd:"\"$HOME/.local/bin/threadbear\" title --status STATUS --json",
  yield_time_ms:4000,
  max_output_tokens:1000
});
text(result);
```

Make exactly one attempt at that terminal moment. Never run it as a progress update. If it fails or has not returned after the bounded yield, do not poll, retry, reconcile, or delay the response. The failure is local to this turn.

The status controls only the visible icon. ThreadBear preserves the task's exact safe subject and user-authored emoji. It never puts an owner or action in the title. Use:

- `complete` when the work is finished with no warranted follow-up.
- `next_steps` when the response establishes one concrete next action for the user, agent, or an external party.
- `needs_input` when required user input is blocking progress.
- `blocked` when an external condition prevents progress.
- `automation` for healthy scheduled or automated work with nothing pending.

Use `complete` unless the response itself establishes another disposition. Generic offers and speculative possibilities are not next steps.

After a confirmed uninstall removes ThreadBear and this guidance, do not run the title command. Ask the user to restart Codex so open tasks stop using their snapshotted guidance.
