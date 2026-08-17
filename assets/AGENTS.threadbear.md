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
	text(JSON.stringify({ready:false, reason:"ThreadBear title helper returned malformed JSON"}));
	exit();
}
if (!plan || plan.ready !== true || typeof plan.task_id !== "string" ||
	typeof plan.icon !== "string" || !Array.isArray(plan.owned_prefixes) ||
	!Array.isArray(plan.blocked_prefixes) || !Array.isArray(plan.internal_markers) ||
	!Number.isInteger(plan.max_title_units)) {
	text(JSON.stringify({ready:false, reason:"ThreadBear title helper returned an invalid policy"}));
	exit();
}
const decodeNative = value => {
	if (typeof value !== "string") return value;
	try { return JSON.parse(value); } catch { return null; }
};
let current;
try {
	current = decodeNative(await tools.codex_app__read_thread({threadId:plan.task_id,
		includeOutputs:false,turnLimit:1,maxOutputCharsPerItem:1}));
} catch (error) {
	text(JSON.stringify({ready:false, reason:"Codex title read failed", error:String(error)}));
	exit();
}
if (!current || current?.thread?.id !== plan.task_id ||
	typeof current.thread.title !== "string") {
	text(JSON.stringify({ready:false, reason:"Codex title read was not confirmed exactly"}));
	exit();
}
const previous = current.thread.title;
if (plan.blocked_prefixes.some(prefix => previous.startsWith(prefix))) {
	text(JSON.stringify({ready:false, reason:"The current title has an ambiguous old ThreadBear prefix"}));
	exit();
}
let subject = previous;
for (const prefix of plan.owned_prefixes) {
	if (subject.startsWith(prefix)) { subject = subject.slice(prefix.length); break; }
}
const lower = subject.toLowerCase();
if (subject.trim() === "" || /[\u0000-\u001f\u007f-\u009f\u2028\u2029]/u.test(subject) ||
	plan.internal_markers.some(marker => lower.includes(marker)) ||
	(plan.icon + " " + subject).length > plan.max_title_units) {
	text(JSON.stringify({ready:false, reason:"The current title is not safe to decorate"}));
	exit();
}
const desired = plan.icon + " " + subject;
if (desired === previous) {
	text(JSON.stringify({ready:true, task_id:plan.task_id, title:previous, updated:false}));
	exit();
}
let renamed;
try {
	renamed = decodeNative(await tools.codex_app__set_thread_title({title:desired}));
} catch (error) {
	text(JSON.stringify({ready:false, reason:"Codex title write failed", error:String(error)}));
	exit();
}
if (!renamed || typeof renamed !== "object" || renamed.threadId !== plan.task_id ||
	renamed.title !== desired) {
	text(JSON.stringify({ready:false, reason:"Codex title write was not confirmed exactly"}));
	exit();
}
text(JSON.stringify({ready:true, task_id:plan.task_id, title:renamed.title, updated:true}));
```

The local command only returns the calling task ID and fixed title policy. The mounted Codex app reads the exact current title and is the sole writer. It receives no explicit task ID when writing, so it can target only the calling task. Make at most one native write attempt. Never run the cell as a progress update. If the outer cell yields, wait only for that same cell; the yield does not cancel a slow native call. Never start another cell, poll the title, retry, or reconcile. A returned failure is local to this turn.

The status controls only the visible icon. ThreadBear emits five exact status prefixes. It also recognizes the five `✦` first-read prefixes and the obsolete neutral bear prefix only so the next ordinary turn can replace them with one exact current status. It preserves every other safe subject and user-authored emoji, and leaves an ambiguous old ThreadBear prefix unchanged rather than guessing. It never puts an owner or action in the title. Use:

- `complete` when the work is finished with no warranted follow-up.
- `next_steps` when the response establishes one concrete next action for the user, agent, or an external party.
- `needs_input` when required user input is blocking progress.
- `blocked` when an external condition prevents progress.
- `automation` for healthy scheduled or automated work with nothing pending.

Use `complete` unless the response itself establishes another disposition. Generic offers and speculative possibilities are not next steps.

After a confirmed uninstall removes ThreadBear and this guidance, do not run the title command. Ask the user to restart Codex so open tasks stop using their snapshotted guidance.
