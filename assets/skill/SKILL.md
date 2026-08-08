---
name: threadbear
description: Operate the local ThreadBear title decorator for Codex Desktop on macOS.
---

# ThreadBear

Be brief, warm, and lightly bear-themed. Explain effects first. Get explicit consent before install/reset, historical onboarding, manual update, or uninstall.

One terminal cell runs the local planner, then at most one mounted Codex title write. Status changes only the icon; owners/actions stay in prose. There is no persistent task, controller, classifier, queue, or repair job.

## Help and status

Run `status --json` before calling it healthy. `threadbear help` is authoritative; core and updater health are separate.

## Install or reset

Follow `https://threadbear.sh/install` and candidate help. Preview first. Explain the binary, subject records, guidance, skill, and updater.

For a 2.2.1 reset, require the exact former task and automation. Delete only it, unpin only that task without renaming it, and remove exact old title hooks. Verify both results before `install --reset`; mismatch stops. Import no old state or title guess.

After consent, install; verify `version`, `self-test`, and `status` JSON. Ask for one restart, then say:

> Open any task after restart and say: **ThreadBear onboard**

## Onboard existing tasks

1. Run `status --json`, then `onboard --dry-run --json`. Require `ready:true`, `plan_complete:true`, and `read_only:true`. It must enumerate and deduplicate the complete unarchived App Server catalog; failure means zero writes.
2. Explain `total`, `safe`, and `needs_update`. Active, blank, unsafe, ambiguous, or overlong titles stay unchanged; never adopt `preview`. Ask for explicit consent.
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
for (const item of prepared) {
  let current;
  try {
    current = await tools.codex_app__read_thread({threadId:item.task_id,
      includeOutputs:false,turnLimit:1,maxOutputCharsPerItem:1});
  } catch { current = null; }
  if (current?.thread?.title !== item.title) skipped++;
  else {
    let renamed;
    try {
      renamed = await tools.codex_app__set_thread_title({threadId:item.task_id,
        title:item.desired_title});
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

If the cell yields, wait only for it; never start another process or title call. Progress appears during preparation and every 25 outcomes. Report: `Updated X of N existing tasks; Y were left unchanged; Z could not be confirmed.` Ready requires zero `unconfirmed`. Drift is skipped. A later **ThreadBear onboard** makes a fresh plan. Never create a cap, wave, controller, worker task, queue, or persistent ThreadBear task.

## Update

The daily LaunchAgent runs only `threadbear update` and never reads tasks. For manual update, get consent, run `update --json`, and report `restart_required`. Failure may return a rerunnable partial; the binary is last.

## Uninstall

Run status and uninstall dry run. Explain removal and remaining icons; ask consent. Preserve unrelated content. Do not run the title cell again. Ask for restart.
