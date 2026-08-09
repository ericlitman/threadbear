---
name: threadbear
description: Install, update, check, or uninstall ThreadBear for Codex Desktop on macOS.
---

# ThreadBear

Be upbeat and plain.

- Before consent, end with **Here's what will happen**: changes, untouched items, restart, one question.
- After tools, end with **ThreadBear recap 🐻**: result, uncertainty, next action.

Put the recap in the final answer. Call safe skips “left unchanged.” Give partials one action.

## Install or reset

Follow `https://threadbear.sh/install`. Preview the helper, instructions, skill, and daily updates; leave tasks, settings, and titles alone. Restart after installation.

For 2.2.1, touch only the verified old task and automation; stop on mismatch.

After consent, install; verify `version`, `self-test`, `status`. Recap:

> Restart Codex so open tasks load the new ThreadBear instructions.

## Uninstall

1. Say: “Codex will ask once so ThreadBear can read the complete task list. This preview changes nothing.” Run `uninstall --dry-run --json` with `sandbox_permissions:"require_escalated"`. Require `ready:true`, `plan_complete:true`, `read_only:true`. Preview removing one ThreadBear prefix from each safe unarchived task, then the helper, instructions, skill, and updates. Tasks, settings, files, user-authored titles, ambiguous prefixes, unsafe titles, and archived tasks stay unchanged. Ask consent.
2. If Codex says approval requests are disabled, stop. Never change settings or bypass permission.
3. After consent, run this exact cell once:

```js
// @exec: {"yield_time_ms": 30000, "max_output_tokens": 4000}
const runCLI = async (cmd, justification) => {
  let call = await tools.exec_command({cmd,yield_time_ms:30000,max_output_tokens:200000,
    sandbox_permissions:"require_escalated",justification});
  let output = call.output || "";
  while (call.session_id !== undefined) {
    call = await tools.write_stdin({session_id:call.session_id,yield_time_ms:30000,
      max_output_tokens:200000});
    output += call.output || "";
  }
  return {call,output};
};
notify("ThreadBear uninstall: preparing title cleanup");
const preparedCall = await runCLI(
  "\"$HOME/.local/bin/threadbear\" uninstall --prepare --noninteractive --confirm --json",
  "Allow ThreadBear to read the complete Codex task list for the uninstall you approved?"
);
if (preparedCall.call.exit_code !== 0) { text(preparedCall.call); exit(); }
let plan;
try { plan = JSON.parse(preparedCall.output); }
catch { text(JSON.stringify({ready:false,reason:"Malformed plan"})); exit(); }
if (!plan || plan.ready !== true || plan.plan_complete !== true ||
    plan.read_only !== false || !Number.isInteger(plan.total) ||
    !Number.isInteger(plan.needs_cleanup) || !Number.isInteger(plan.prepared) ||
    !Number.isInteger(plan.unchanged) || !Number.isInteger(plan.skipped) ||
    plan.needs_cleanup !== plan.prepared ||
    !Array.isArray(plan.items)) {
  text(JSON.stringify({ready:false,reason:"Incomplete plan"})); exit();
}
const prepared = plan.items.filter(item => item.outcome === "prepared");
if (prepared.length !== plan.prepared || prepared.some(item => !item || typeof item.task_id !== "string" ||
    typeof item.title !== "string" || typeof item.desired_title !== "string")) {
  text(JSON.stringify({ready:false,reason:"Invalid item"})); exit();
}
let updated = 0, drifted = 0, unconfirmed = 0;
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
  if (current?.thread?.id !== item.task_id || current.thread.title !== item.title) drifted++;
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
  const done = updated + drifted + unconfirmed;
  if (done % 25 === 0 || done === prepared.length) notify(`ThreadBear uninstall: titles ${done}/${prepared.length}`);
}
const accounted = updated + drifted + unconfirmed === prepared.length;
if (!accounted || drifted !== 0 || unconfirmed !== 0) {
  text(JSON.stringify({ready:false,uninstalled:false,plan_complete:true,
    cleanup_complete:false,total:plan.total,prepared:plan.prepared,updated,drifted,
    unchanged:plan.unchanged,skipped:plan.skipped,unconfirmed,
    safe_rerun:"threadbear uninstall --dry-run --json"}));
  exit();
}
notify("ThreadBear uninstall: removing managed artifacts");
const removedCall = await runCLI(
  "\"$HOME/.local/bin/threadbear\" uninstall --commit --noninteractive --confirm --json",
  "Allow ThreadBear to remove the managed artifacts from the uninstall you approved?"
);
let removed;
try { removed = JSON.parse(removedCall.output); }
catch { text(JSON.stringify({ready:false,uninstalled:false,reason:"Malformed uninstall result",
  title_cleanup:{total:plan.total,updated,unchanged:plan.unchanged,skipped:plan.skipped}})); exit(); }
removed.title_cleanup = {total:plan.total,updated,unchanged:plan.unchanged,skipped:plan.skipped};
text(JSON.stringify(removed));
```

Only `uninstalled:true` means removed. Otherwise recap the partial and its one next action. Never retry a drifted or unconfirmed title in the same pass. After artifact commit, make no title call. Successful recap: “ThreadBear was removed. X task titles were cleaned; Y were left unchanged. Its helper, instructions, skill, and automatic updates are gone. Tasks, settings, and files stayed. Restart Codex.”

## Update

Preview download, checks, replacement, and restart. With consent run `update --json`; recap version and next action.
