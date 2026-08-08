---
name: threadbear
description: Install, onboard, update, check, or uninstall ThreadBear for Codex Desktop on macOS.
---

# ThreadBear

Be upbeat/plain. For each lifecycle change:

- Before consent, end with **Here's what will happen**: changes, untouched items, restart, one question.
- After tools, end with **ThreadBear recap 🐻**: result/counts, uncertainty, next action.

Never leave it in commentary/tool output. Recap visible facts—no JSON/self-test/state/files/planners/records/booleans. Safe skips are “left unchanged”; title failure is “this title stayed as-is.” Give partial work one safe next action.

## Install or reset

Follow `https://threadbear.sh/install`. Preview helper, title memory, instructions, daily check; leave tasks/settings/titles. Restart, then onboard.

For 2.2.1, verify old task/automation; delete/unpin only those, without renaming. Stop on mismatch; import nothing.

Install with consent; verify `version`, `self-test`, `status`. Recap:

> Open any task after restart and say: **ThreadBear onboard**

## Onboard existing tasks

1. Run `status --json`, then `onboard --dry-run --json`. Require `ready:true`, `plan_complete:true`, `read_only:true`, full catalog.
2. Say: “I found N tasks. X are safe; Y need an icon. The rest stay untouched. I'll recheck each before one change.” Ask consent; ignore `preview`.
3. After consent, run this exact cell once:

```js
// @exec: {"yield_time_ms": 30000, "max_output_tokens": 4000}
notify("ThreadBear onboarding: preparing complete catalog");
let local = await tools.exec_command({
  cmd:"\"$HOME/.local/bin/threadbear\" onboard --noninteractive --confirm --json",
  yield_time_ms:30000,
  max_output_tokens:200000
});
let output = local.output || "";
while (local.session_id !== undefined) {
  notify("ThreadBear onboarding: preparing");
  local = await tools.write_stdin({
    session_id:local.session_id,
    yield_time_ms:30000,
    max_output_tokens:200000
  });
  output += local.output || "";
}
if (local.exit_code !== 0) { text(local); exit(); }
let plan;
try { plan = JSON.parse(output); }
catch { text(JSON.stringify({ready:false,reason:"Malformed preparation"})); exit(); }
if (!plan || plan.ready !== true || plan.plan_complete !== true ||
    plan.read_only !== false || !Number.isInteger(plan.total) || !Array.isArray(plan.items)) {
  text(JSON.stringify({ready:false,reason:"Incomplete preparation"})); exit();
}
const prepared = plan.items.filter(item => item.outcome === "prepared");
if (prepared.some(item => !item || typeof item.task_id !== "string" ||
    typeof item.title !== "string" || typeof item.desired_title !== "string")) {
  text(JSON.stringify({ready:false,reason:"Invalid prepared item"})); exit();
}
let updated = 0, skipped = 0, unconfirmed = 0;
const parseNative = value => {
  if (typeof value !== "string") return value;
  try { return JSON.parse(value); } catch { return null; }
};
for (const item of prepared) {
  let current;
  try {
    current = parseNative(await tools.codex_app__read_thread({threadId:item.task_id,
      includeOutputs:false,turnLimit:1,maxOutputCharsPerItem:1}));
  } catch { current = null; }
  if (current?.thread?.id !== item.task_id || current.thread.title !== item.title) skipped++;
  else {
    let renamed;
    try {
      renamed = parseNative(await tools.codex_app__set_thread_title({threadId:item.task_id,
        title:item.desired_title}));
    } catch { renamed = null; }
    if (renamed && typeof renamed === "object" && renamed.threadId === item.task_id &&
        renamed.title === item.desired_title) updated++;
    else unconfirmed++;
  }
  const done = updated + skipped + unconfirmed;
  if (done % 25 === 0 || done === prepared.length) notify(`ThreadBear onboarding: ${done}/${prepared.length}`);
}
const accounted = updated + skipped + unconfirmed === prepared.length;
text(JSON.stringify({
  ready:accounted && unconfirmed === 0,
  plan_complete:true,
  onboarding_complete:accounted && unconfirmed === 0,
  total:plan.total,
  updated,
  skipped,
  unchanged:plan.total - updated - unconfirmed,
  unconfirmed
}));
```

Wait if yielded; progress every 25. Recap: `Updated X of N existing tasks; Y were left unchanged; Z could not be confirmed.` Ready needs no `unconfirmed`. Skip drift; no retry. Rows may refresh after reopen/restart. Reruns replan. No cap, controller, worker, queue, or persistent task.

## Update

Daily checks never read tasks. Preview download, verification, replacement, restart. After consent run `update --json`; recap version, restart, safe rerun.

## Uninstall

Preview removing helper, title memory, instructions, skill, daily check; keep tasks/settings/files; icons may remain. Then no title cell. Recap exactly: “ThreadBear was removed. Its helper, title memory, instructions, skill, and daily check are gone. Tasks/settings/files stayed; icons may remain. Restart Codex.”
