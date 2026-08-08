---
name: threadbear
description: Install, onboard, update, check, or uninstall ThreadBear for Codex Desktop on macOS.
---

# ThreadBear

Be upbeat and plain.

- Before consent, end with **Here's what will happen**: changes, untouched items, restart, one question.
- After tools, end with **ThreadBear recap 🐻**: result, uncertainty, next action.

Put the recap in the final answer. Call safe skips “left unchanged.” Give partials one action.

## Install or reset

Follow `https://threadbear.sh/install`. Preview the helper, instructions, skill, and daily updates; leave tasks, settings, and titles alone. Restart, then onboard.

For 2.2.1, touch only the verified old task and automation; stop on mismatch.

After consent, install; verify `version`, `self-test`, `status`. Recap:

> Open any task after restart and say: **ThreadBear onboard**

## Onboard existing tasks

1. Run `status --json`. Say: “Codex will ask once so ThreadBear can read your complete task list. This preview changes nothing.” Run `onboard --dry-run --json` with `sandbox_permissions:"require_escalated"`. Require `ready:true`, `plan_complete:true`, `read_only:true`.
   If Codex says approval requests are disabled, stop. Never change settings or bypass permission. Recap: “No tasks changed. This task cannot ask. Next: use a task that can, then say **ThreadBear onboard**.”
2. Say: “N tasks found. X are safe; Y need an icon. The rest stay untouched. I'll recheck each before one change.” Ask consent.
3. After consent, run this exact cell once:

```js
// @exec: {"yield_time_ms": 30000, "max_output_tokens": 4000}
notify("ThreadBear onboarding: preparing");
let local = await tools.exec_command({
  cmd:"\"$HOME/.local/bin/threadbear\" onboard --noninteractive --confirm --json",
  yield_time_ms:30000,
  max_output_tokens:200000,
  sandbox_permissions:"require_escalated",
  justification:"Allow ThreadBear to read the full Codex task list for the onboarding you approved?"
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
catch { text(JSON.stringify({ready:false,reason:"Malformed plan"})); exit(); }
if (!plan || plan.ready !== true || plan.plan_complete !== true ||
    plan.read_only !== false || !Number.isInteger(plan.total) || !Array.isArray(plan.items)) {
  text(JSON.stringify({ready:false,reason:"Incomplete plan"})); exit();
}
const prepared = plan.items.filter(item => item.outcome === "prepared");
if (prepared.some(item => !item || typeof item.task_id !== "string" ||
    typeof item.title !== "string" || typeof item.desired_title !== "string")) {
  text(JSON.stringify({ready:false,reason:"Invalid item"})); exit();
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

Recap: `Updated X of N existing tasks; Y were left unchanged; Z could not be confirmed.` No retry, cap, or persistent task.

## Update

Preview download, checks, replacement, and restart. With consent run `update --json`; recap version and next action.

## Uninstall

Run `uninstall --dry-run --json`. Preview removing the helper, instructions, skill, and updates; keep tasks, settings, and files; icons may remain. Ask consent. Run `uninstall --noninteractive --confirm --json`. Only `uninstalled:true` means removed; otherwise recap the partial and next action. After commit, no title cell. Recap exactly: “ThreadBear was removed. Its helper, instructions, skill, and automatic updates are gone. Tasks, settings, and files stayed; icons may remain. Restart Codex.”
