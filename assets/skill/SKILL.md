---
name: threadbear
description: Operate ThreadBear for Codex Desktop on macOS.
---

# ThreadBear

Be brief and warm. Explain effects first. Get explicit consent before install/reset, historical onboarding, manual update, or uninstall.

One terminal cell plans, then makes at most one mounted title write. Status changes only the icon; actions stay in prose. No persistent task or background title machinery.

## Help and status

Run `status --json` before calling it healthy. `threadbear help` is authoritative; core and updater health are separate.

## Install or reset

Follow `https://threadbear.sh/install` and candidate help. Preview the binary, subject records, guidance, skill, and updater.

For a 2.2.1 reset, require the exact former task and automation. Delete only that automation, unpin only that task without renaming, and remove exact old title hooks. Verify deletion and unpin before `install --reset`; stop on mismatch. Import no old state or title guess.

After consent, install; verify `version`, `self-test`, and `status` JSON. Ask for one restart, then say:

> Open any task after restart and say: **ThreadBear onboard**

## Onboard existing tasks

1. Run `status --json`, then `onboard --dry-run --json`. Require `ready:true`, `plan_complete:true`, and `read_only:true`; it must enumerate and deduplicate the complete unarchived App Server catalog.
2. Report `total`, `safe`, and `needs_update`. Leave active, blank, unsafe, ambiguous, or overlong titles unchanged; never adopt `preview`. Ask for explicit consent.
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

If it yields, wait for that cell; start no second process or title call. Show progress during preparation and every 25 items. Report: `Updated X of N existing tasks; Y were left unchanged; Z could not be confirmed.` Ready requires zero `unconfirmed`; drift is skipped. A later **ThreadBear onboard** replans. Never create a cap, wave, controller, worker task, queue, or persistent ThreadBear task.

## Update

The daily LaunchAgent runs only `threadbear update` and never reads tasks. For manual update, get consent, run `update --json`, and report `restart_required`. A partial is rerunnable; the binary is last.

## Uninstall

Run status and uninstall dry run. Explain removal and remaining icons; ask consent. Preserve unrelated content. Do not run the title cell again. Ask for restart.
