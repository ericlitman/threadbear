# ThreadBear

For every ordinary interactive turn in a main Codex Desktop task:

1. Write the substantive response first. Keep any owner or next action in that prose.
2. Choose exactly one status: `complete`, `next_steps`, `needs_input`, `blocked`, or `automation`.
3. Immediately before the final response, run this one terminal cell. Replace only `STATUS` with the exact enum:

```js
// @exec: {"yield_time_ms": 30000, "max_output_tokens": 1000}
const local = await tools.exec_command({
  cmd:"\"$HOME/.local/bin/threadbear\" title --status STATUS --json",
  yield_time_ms:30000,
  max_output_tokens:1000
});
if (local.exit_code !== 0) { text(local); exit(); }
let plan;
try { plan = JSON.parse(local.output); } catch {
  text(JSON.stringify({ready:false, reason:"ThreadBear title planner returned malformed JSON"}));
  exit();
}
if (!plan || plan.ready !== true || typeof plan.write_required !== "boolean" ||
    typeof plan.task_id !== "string" ||
    (plan.write_required && typeof plan.desired_title !== "string")) {
  text(JSON.stringify({ready:false, reason:"ThreadBear title planner returned an invalid plan"}));
  exit();
}
const decodeNative = value => {
  if (typeof value !== "string") return value;
  try { return JSON.parse(value); } catch { return null; }
};
if (!plan.write_required) { text(local); exit(); }
let renamed;
try {
  renamed = decodeNative(await tools.codex_app__set_thread_title({title:plan.desired_title}));
} catch (error) {
  text(JSON.stringify({ready:false, reason:"Codex title write failed", error:String(error)}));
  exit();
}
if (!renamed || typeof renamed !== "object" || renamed.threadId !== plan.task_id ||
    renamed.title !== plan.desired_title) {
  text(JSON.stringify({ready:false, reason:"Codex title write was not confirmed exactly"}));
  exit();
}
text(JSON.stringify({ready:true, task_id:plan.task_id, title:renamed.title, updated:true}));
```

The local command only prepares a safe title. When a write is needed, the mounted Codex app is the sole writer and receives no explicit task ID, so it can target only the calling task. Make at most one native write attempt. Never run the cell as a progress update. If the outer cell yields, wait only for that same cell; the yield does not cancel a slow native call. Never start another cell, poll the title, retry, or reconcile. A returned failure is local to this turn.

The status controls only the visible icon. ThreadBear preserves the task's exact safe subject and user-authored emoji. It never puts an owner or action in the title. Use:

- `complete` when the work is finished with no warranted follow-up.
- `next_steps` when the response establishes one concrete next action for the user, agent, or an external party.
- `needs_input` when required user input is blocking progress.
- `blocked` when an external condition prevents progress.
- `automation` for healthy scheduled or automated work with nothing pending.

Use `complete` unless the response itself establishes another disposition. Generic offers and speculative possibilities are not next steps.

After a confirmed uninstall removes ThreadBear and this guidance, do not run the title command. Ask the user to restart Codex so open tasks stop using their snapshotted guidance.
