# Install ThreadBear with Codex

Open a new Codex Desktop task and paste:

```text
Install ThreadBear — follow https://threadbear.sh/install
```

## Agent brief

You are the ThreadBear guide in this task. Keep the experience short, warm, and concrete. Explain visible outcomes before commands and keep raw JSON, paths, and task IDs backstage unless they explain a failure. Do not use screenshots, computer control, or ask the user to inspect private files during an ordinary install.

Open with this orientation:

> ## Hi. Let's install ThreadBear.
>
> ThreadBear adds one useful status icon while keeping the rest of each safe task title intact. Codex reads and applies the title itself.
>
> I'll check this Mac, show you exactly what will change, and ask before installing anything. After installation, existing tasks will progressively receive best-guess icons when their history is clear enough. Codex needs one restart for future turns.

Codex collapses commentary after a turn finishes, so the final answer that asks for consent must repeat the orientation, readiness result, complete recommendation, and question. If a check fails, report it plainly and do not ask for install consent.

For every lifecycle action, write the lasting summary after all tool calls. End the final response with **ThreadBear recap 🐻** and include the result, counts or uncertainty, what stayed untouched, and the next action. Never leave that recap only in commentary, progress notices, notifications, or raw tool output; those can disappear when Codex summarizes the turn.

Keep that recap user-facing: do not copy raw fields or list internal files. Translate them into helper, instructions, skill, and automatic updates. Group safe skips as “left unchanged” unless the user needs to act. An unconfirmed title write means “I couldn't confirm whether this title changed,” never “it stayed unchanged.”

## 1. Check without changing anything

Say: “First I'll check that this Mac is ready and preview the exact ThreadBear setup. Nothing changes in this step.”

Run:

```sh
sw_vers -productVersion
uname -m
codex_found=
for codex_path in \
  "$HOME/Applications/ChatGPT.app/Contents/Resources/codex" \
  "$HOME/Applications/Codex.app/Contents/Resources/codex" \
  /Applications/ChatGPT.app/Contents/Resources/codex \
  /Applications/Codex.app/Contents/Resources/codex \
  "$HOME/.local/bin/codex"; do
  if [ -x "$codex_path" ]; then
    printf '%s: ' "$codex_path"
    if "$codex_path" --version; then codex_found=1; fi
  fi
done
test -n "$codex_found"
curl --version
curl -fsSLI https://threadbear.sh/install.sh >/dev/null
curl -fsSLI https://github.com/ericlitman/threadbear/releases/latest >/dev/null
if [ -x "$HOME/.local/bin/threadbear" ]; then
  "$HOME/.local/bin/threadbear" status --json
fi
```

ThreadBear requires macOS 12 or newer, Apple silicon or Intel, Codex Desktop 0.146.0 or newer, and HTTPS access to the official guide and GitHub Releases. The check prints every fixed Codex Desktop command it finds; ThreadBear uses the first one that actually reports a compatible version. It needs no `sudo` or Full Disk Access. Ordinary title updates work with Codex's default workspace permissions. Uninstall cleanup asks once for permission to read the complete local task catalog. ThreadBear never opens Codex SQLite.

For an official release, run the verified bootstrap preview:

```sh
curl -fsSL https://threadbear.sh/install.sh | sh -s -- --dry-run --json
```

For an already-built local candidate, run:

```sh
/path/to/threadbear install --dry-run --json
```

The preview must pass candidate self-test and be limited to the binary, private lifecycle/update state, managed AGENTS block, installed skill, and one daily update-only LaunchAgent. It must preserve unrelated AGENTS content, skills, settings, files, and LaunchAgents.

If the preview returns `legacy_reset_required:true`, require `legacy_main_task_id` plus `legacy_automation_id`, `legacy_automation_name`, `legacy_automation_kind`, and `legacy_automation_target_thread_id`. The target must equal the main-task ID. This is a clean 2.2.1 reset, not an in-place migration. Through supported native controls, verify the exact automation and former persistent task before proposing mutation. A collision, missing target, or uncertain owner stops the reset. The reset also removes only exact obsolete ThreadBear Pre/Post title-interception entries and preserves every foreign entry and its order. Import no old state and reinterpret no legacy title.

## 2. Show the recommendation

Only after the checks and dry run succeed, present this complete card in the same final answer as the consent question:

> ## Here's what will happen
>
> - ThreadBear adds one helpful status icon without rewriting your task's subject or emoji.
> - After installation succeeds, ThreadBear reads bounded recent history and existing task icons appear progressively. A small sparkle marks a conservative first read; unclear tasks stay unchanged.
> - A small local helper, Codex instructions, and a ThreadBear skill are added.
> - Once a day, ThreadBear checks for and installs only verified official releases. Updates never read tasks or change titles.
> - Unclear or unsafe titles are left alone, and there is no persistent ThreadBear task.
> - Other Codex settings and files stay untouched.
> - Codex restarts once so open tasks load the new instructions.
>
> Install ThreadBear?

For a 2.2.1 reset, add: “I'll remove only the verified old ThreadBear automation, unpin its former task without renaming it, and install the simpler version fresh. Old title history will not be guessed or imported, so some existing icons may remain.”

A clear yes to the unchanged recommendation is consent. Ask again only if the effect changes or the answer is ambiguous.

## 3. Install after consent

Say: “Thanks—I'll install ThreadBear now, then check that it is healthy. Existing task titles will not change until those checks finish.”

Before a 2.2.1 reset, delete the exact fingerprinted `threadbear-maintenance` automation through supported native control and verify it is absent. Then unpin the preview's exact legacy main-task ID and verify the returned and reread task ID match with `pinned:false`. Do not rename that task. Any mismatch aborts before filesystem reset. The confirmed candidate command must include `--reset`.

For the official release, run:

```sh
curl -fsSL https://threadbear.sh/install.sh | sh -s -- \
  --noninteractive --confirm --json
```

For a local candidate, run:

```sh
/path/to/threadbear install --noninteractive --confirm --json
```

Add `--reset` only after the exact legacy cleanup is verified. Then run:

```sh
~/.local/bin/threadbear version --json
~/.local/bin/threadbear self-test --json
~/.local/bin/threadbear status --json
```

Core `ready` is healthy when the installed binary, private lifecycle state, compatible Codex Desktop, managed guidance, and skill match the candidate. Report the daily updater separately; missing automatic updates do not make title handling globally unready. Core readiness does not depend on historical title counts.

No controller, worker, migration phase, persistent task, durable onboarding state, queue, retry sweep, or additional automation should exist after installation. If installation fails after mutation starts, report `partial:true`, the failed stage, whether restart is required, and the one safe rerun action. `planned_changes` is a plan, not a claim that every item ran.

After all three checks pass, start this exact cell once. Its first output is the handoff boundary: when that handoff arrives, immediately send the friendly install recap below without waiting for the cell to finish. The same yielded cell then performs one ephemeral read-only classification pass and applies each eligible title serially through mounted Codex tools. Do not start another cell, poll it from the conversation, or turn it into a task, automation, queue, or persisted job.

```js
// @exec: {"yield_time_ms": 30000, "max_output_tokens": 4000}
const handoff = {kind:"handoff",ready:true,activity:"existing-task-icons"};
if (Object.keys(handoff).sort().join(",") !== "activity,kind,ready" || handoff.ready !== true) exit();
text(JSON.stringify(handoff));
yield_control();

const parseNative = value => {
  if (typeof value !== "string") return value;
  try { return JSON.parse(value); } catch { return null; }
};
const exactKeys = (value, keys) => value && typeof value === "object" && !Array.isArray(value) &&
  Object.keys(value).sort().join(",") === [...keys].sort().join(",");
const statuses = new Set(["complete","next_steps","needs_input","blocked","automation"]);
const icons = {complete:"✅",next_steps:"➡️",needs_input:"🙋",blocked:"🚨",automation:"🤖"};
const seen = new Set();
let previousID = "", terminal = false, invalid = false, carry = "";
let received = 0, updated = 0, unknown = 0, drifted = 0, unconfirmed = 0;

const acceptLine = async line => {
  let record;
  try { record = JSON.parse(line); } catch { invalid = true; return; }
  if (terminal || !record || typeof record.kind !== "string") { invalid = true; return; }
  if (record.kind === "summary") {
    if (!exactKeys(record,["kind","total","eligible","exact","inferred","unknown","skipped"]) ||
        ![record.total,record.eligible,record.exact,record.inferred,record.unknown,record.skipped]
          .every(Number.isInteger) || record.total !== record.eligible + record.skipped ||
        record.eligible !== record.exact + record.inferred + record.unknown ||
        record.eligible !== received) { invalid = true; return; }
    terminal = true;
    return;
  }
  if (record.kind !== "candidate" ||
      !exactKeys(record,["kind","task_id","snapshot_title","status","provenance"]) ||
      typeof record.task_id !== "string" || typeof record.snapshot_title !== "string" ||
      typeof record.status !== "string" || typeof record.provenance !== "string" ||
      seen.has(record.task_id) || (previousID && record.task_id <= previousID)) {
    invalid = true; return;
  }
  const semantic = statuses.has(record.status);
  if ((!semantic && (record.status !== "unknown" || record.provenance !== "unknown")) ||
      (semantic && record.provenance !== "exact" && record.provenance !== "inferred")) {
    invalid = true; return;
  }
  seen.add(record.task_id); previousID = record.task_id; received++;
  let current;
  try {
    current = parseNative(await tools.codex_app__read_thread({threadId:record.task_id,
      includeOutputs:false,turnLimit:1,maxOutputCharsPerItem:1}));
  } catch { current = null; }
  if (current?.thread?.id !== record.task_id || current.thread.title !== record.snapshot_title) {
    drifted++;
  } else if (!semantic) {
    unknown++;
  } else {
    const mark = record.provenance === "inferred" ? "✦" : "";
    const desired = icons[record.status] + mark + " " + record.snapshot_title;
    const lower = record.snapshot_title.toLowerCase();
    if (record.snapshot_title.trim() === "" ||
        /[\u0000-\u001f\u007f-\u009f\u2028\u2029]/u.test(record.snapshot_title) ||
        ["<codex_delegation>","<codex_internal_context","<environment_context>","<app-context",
         "<collaboration_mode","<multi_agent_mode","<permissions_instructions","<permissions instructions",
         "<apps_instructions","<plugins_instructions","<skills_instructions","<recommended_plugins"]
          .some(marker => lower.includes(marker)) || desired.length > 60) {
      drifted++;
    } else {
      let renamed;
      try {
        renamed = parseNative(await tools.codex_app__set_thread_title({threadId:record.task_id,title:desired}));
      } catch { renamed = null; }
      if (renamed?.threadId === record.task_id && renamed.title === desired) updated++;
      else unconfirmed++;
    }
  }
  if (received % 25 === 0) notify(`ThreadBear: first read ${received} tasks`);
};

const consume = async chunk => {
  carry += chunk || "";
  for (;;) {
    const newline = carry.indexOf("\n");
    if (newline < 0) return;
    const line = carry.slice(0,newline); carry = carry.slice(newline + 1);
    if (line.trim() !== "" && !invalid) await acceptLine(line);
  }
};

let call = await tools.exec_command({
  cmd:"\"$HOME/.local/bin/threadbear\" onboard-stream",
  yield_time_ms:1000,
  max_output_tokens:200000
});
await consume(call.output);
while (call.session_id !== undefined) {
  call = await tools.write_stdin({session_id:call.session_id,yield_time_ms:30000,max_output_tokens:200000});
  await consume(call.output);
}
if (carry.trim() !== "" || call.exit_code !== 0 || !terminal) invalid = true;
text(JSON.stringify({ready:!invalid,finished:true,received,updated,unknown,drifted,unconfirmed}));
```

The helper emits strict ordered JSON Lines and never writes a title. Exact historical ThreadBear footers produce plain icons. Ambiguous completed turns are classified in fixed sequential batches by `gpt-5.6-luna` at medium reasoning; malformed or failed batches become unknown. Unknown tasks are reread but not renamed. Every semantic candidate gets one immediate mounted reread and at most one explicit-target mounted setter call, with exact acknowledgement required. Drift and unconfirmed writes affect only that row and are never retried. The pass ends without durable state; restarting early leaves unfinished tasks untouched, and a later confirmed reinstall naturally skips already decorated titles.

After the checks finish, end the final response with this plain-language receipt, filled with the real result:

> ## ThreadBear recap 🐻
>
> - ThreadBear is installed and automatic updates are [ready / need attention].
> - Over the next several minutes, existing tasks with enough evidence will gain best-guess status icons. A small sparkle marks a first read and disappears after that task's next turn; unclear tasks stay untouched.
> - You can keep working while this finishes. Restart Codex when ready so open tasks load the new instructions; restarting early simply leaves unfinished tasks untouched.

## 4. Restart

Say: “Installation is finished. One restart loads the new instructions. The background first read may still be adding icons to existing tasks.”

After a successful install say:

> ThreadBear is installed. Over the next several minutes, existing tasks with clear enough history will gain best-guess status icons. A small sparkle marks a first read and disappears after that task's next turn; unclear tasks stay untouched. You can keep working while it runs. Restart Codex when ready so open tasks load the new managed guidance; restarting early leaves unfinished tasks untouched.

## Commands and updater

```sh
~/.local/bin/threadbear help
~/.local/bin/threadbear status --json
~/.local/bin/threadbear title --status complete --json
~/.local/bin/threadbear update --json
```

The managed guidance runs one injection-safe terminal JavaScript cell immediately before an ordinary final response. Replace only the status enum. The cell runs `title --status <complete|next_steps|needs_input|blocked|automation> --json` exactly once; the stateless helper returns the calling task ID and fixed title policy without starting App Server or writing state. The mounted app then reads that exact task, derives one safe desired title, and—only when it differs—calls `tools.codex_app__set_thread_title({title:desired})` once with `threadId` omitted. Exact returned task ID/title is required. If the outer cell yields after 30 seconds, wait only for that same cell; the yield does not cancel a slow native call. Never retry, start another cell, poll the title, or reconcile.

`update` verifies the official manifest, release URLs, architecture, checksum, embedded version, and candidate self-test before replacement. Network or verification failure leaves the old installation untouched. A later managed-surface write can truthfully leave a rerunnable partial; the binary is written last. Every successful update reports `restart_required`. The daily LaunchAgent runs only this command and never reads tasks or changes titles.

For a manual update, preview first and end the consent turn with:

> ## Here's what will happen
>
> - ThreadBear will download the official update, verify it, and replace its local helper only after the checks pass.
> - The update check does not read tasks or change titles.
> - I'll tell you whether Codex needs a restart.
>
> Update ThreadBear now?

Afterward, end with:

> ## ThreadBear recap 🐻
>
> - ThreadBear is now version [version], and automatic updates are [ready / need attention].
> - Codex [does / does not] need a restart.
> - Next: [nothing—you're up to date / the one safe rerun for a partial update].

## Uninstall

Preview first:

```sh
~/.local/bin/threadbear uninstall --dry-run --json
```

Run the preview with `sandbox_permissions:"require_escalated"` and explain that Codex is asking once to read the complete unarchived task catalog. Require `ready:true`, `plan_complete:true`, and `read_only:true`. If permission is unavailable or catalog enumeration fails, stop without changing titles or files.

End the consent turn with:

> ## Here's what will happen
>
> - I'll remove one ThreadBear status prefix from each safe unarchived task title, then remove ThreadBear's local helper, Codex instructions, skill, and automatic updates.
> - Plain, user-authored, ambiguous, unsafe, and archived titles stay unchanged, as do other Codex settings and unrelated files.
> - I'll reread every prepared task immediately before its one possible title change. Any drift or unconfirmed result stops before ThreadBear files are removed.
> - After removal, you'll restart Codex once.
>
> Uninstall ThreadBear now?

After consent, follow the installed skill's single uninstall JavaScript cell. It first runs exact `uninstall --prepare --noninteractive --confirm --json`, taking one fresh complete plan. It then serially rereads and removes one owned prefix from every prepared target, with the initiating task last. Any missing, drifted, malformed, wrong-target, wrong-title, or thrown result blocks teardown and gets one fresh-rerun action; never retry in the same pass. Only after every prepared write returns the exact target and title does the cell run exact `uninstall --commit --noninteractive --confirm --json`. A bare confirmed uninstall is refused. There is no final catalog scan, marker, queue, controller, or resume state.

Require committed removal and verify unrelated AGENTS content, skills, settings, files, titles, and LaunchAgents remain byte-for-byte intact. After commit, do not run the title command. Ask the user to restart Codex so open tasks stop using snapshotted guidance.

The final response after committed removal is:

> ## ThreadBear recap 🐻
>
> - ThreadBear and its automatic updates were removed after cleaning X task titles.
> - Y task titles were left unchanged. Your task content and unrelated Codex content stayed untouched.
> - Next: restart Codex so open tasks drop the old instructions.

## Release proof

Before release, run unit and integration tests, race tests, both Darwin builds, shell checks, experiment validation, installer/guide parity, and the focused fixture smoke.

Release acceptance additionally requires one reviewed candidate live-tested end to end in Codex Desktop:

- the stateless terminal helper works under Codex's default workspace permissions and starts no App Server or title-state write;
- the mounted app-native reader supplies the exact current title, and the setter receives no explicit current-task ID and returns the exact task ID/title;
- the rendered sidebar shows the expected title before and after a clean restart;
- a full uninstall preview enumerates every unarchived task, confirmed preparation writes no title, and the consented serial app-native pass processes the initiating task last;
- drift and unconfirmed results block teardown without retries or global failure state, while all-exact cleanup proceeds directly to artifact removal with no final inventory scan or post-commit title call;
- automatic update and uninstall preserve neighboring user content.

If the mounted app-native writer causes practical title corruption or response blocking, disable rewriting instead of adding reconciliation machinery.
