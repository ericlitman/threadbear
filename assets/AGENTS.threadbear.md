# ThreadBear status

End each terminal response with exactly one compact status line. Use the matching literal example as its shape:

- Finished with no warranted follow-up: `🧵🐻 complete`
- Finished with one concrete action for the user: `🧵🐻 next steps (you): approve the release plan`
- Finished with one concrete action for the agent: `🧵🐻 next steps (agent): implement the approved plan`
- Finished with one concrete action for someone or something external: `🧵🐻 next steps (external): review the security exception`
- Waiting for required user input: `🧵🐻 needs input (you): choose the release region`
- Unable to continue because of an external condition: `🧵🐻 blocked (external): restore the signing service`
- Healthy scheduled or automated work with nothing pending: `🧵🐻 automation`

Never write the literal placeholder words `STATUS`, `OWNER`, or `ACTION` in the line. Start with `🧵🐻` — the full mark, both characters — and one of the lowercase states shown above.

Report the turn's actual disposition; do not invent or recommend work to populate this line. Use `complete` unless the substantive response already ends with one clear, concrete, warranted next step. Generic offers, speculative possibilities, and mentions of recorded work do not qualify.

When ThreadBear is explicitly enabled for this task and Desktop-native task tools are available, use these two phases in order before the final response. The one-model-pass/one-`functions.exec` actuator ceiling applies only to the child actuator phase below, never to the source task or its substantive work.

**Source phase (source only, never actuator).** After completing the substantive work, make one actual `functions.exec` program dedicated to the post-turn handoff. In that program, first parse the current process's own `CODEX_THREAD_ID` and strict `~/.local/bin/threadbear status --json`; only the returned tool result proves the guard, never a prose claim. Missing, noncanonical, or unparseable identity/status, or `CODEX_THREAD_ID` equal to `control_task_id`, is a hard no-op with no child. The source program contains only this self-identity/control guard and, when allowed, the exact child creation below. It performs no title planning, native mutation, reporting, archive operation, or actuator branch.

Run this exact source program; its returned `{allow:false}` or `{allow:true}` is the only post-turn handoff result:

```js
const {execFile} = await import("node:child_process")
const {promisify} = await import("node:util")
const run = promisify(execFile)
const canonicalId = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/
const CHILD_PROMPT = "ThreadBear child actuator phase. Follow only the installed child-actuator contract. Obtain the source solely from your own codex_delegation.source_thread_id."
const guard = await (async () => {
  const sourceId = process.env.CODEX_THREAD_ID ?? ""
  const home = process.env.HOME ?? ""
  if (!home || !canonicalId.test(sourceId)) return {allow: false}
  try {
    const {stdout} = await run(`${home}/.local/bin/threadbear`, ["status", "--json"], {encoding: "utf8"})
    const status = JSON.parse(stdout)
    if (!status || Array.isArray(status) || !canonicalId.test(status.control_task_id) || sourceId === status.control_task_id) return {allow: false}
  } catch {
    return {allow: false}
  }
  return {allow: true}
})()
if (guard.allow) {
  await tools.codex_app__create_thread({
    model: "gpt-5.6-luna",
    thinking: "medium",
    target: {type: "projectless", directoryName: "threadbear-title-actuator"},
    prompt: CHILD_PROMPT,
  })
}
guard
```

A fulfilled creation promise means dispatch succeeded whether it returns a JSON string or object; only a thrown call is dispatch failure. Do not discover tools or inspect another creation response shape. The source never reads or reuses its own incoming `codex_delegation.source_thread_id`. After successful dispatch, return the substantive final response immediately and remain unarchived; do not wait for, read, message, retry, recover, or archive the child. The source passes no transcript, task metadata, title, manifest, or source ID in the child prompt.

**Child actuator phase (child only).** The child obtains the actual source task solely from its own `codex_delegation.source_thread_id`. Only the child gets one model pass and exactly one `functions.exec`, with no preliminary execution, no second command, implementation inspection, retry, or recovery conversation. Use the named callable expressions directly; do not enumerate, inspect, or look up available tools or schemas inside that execution. Inside that execution, run `~/.local/bin/threadbear title-plan --json --wait "$SOURCE_ID"` and parse the JSON without returning plans or titles to model context. For each plan, set `OPERATION_ID` from `operation_id`, run `~/.local/bin/threadbear title-plan --json --operation "$OPERATION_ID"`, and only when the returned `plans` array contains that same exact operation set `TASK_ID` and `DESIRED_TITLE` from its exact `task_id` and `desired_title`, then immediately call `await tools.codex_app__set_thread_title({threadId: TASK_ID, title: DESIRED_TITLE})`. A fulfilled setter is `native_success:true`; a thrown setter error is `native_success:false` with stable `error_code:"native_set_failed"`. Never validate another setter response shape or use native `updatedAt` as a revision guard. The helper-to-setter interval is non-atomic because the setter has no compare-and-set parameter.

After checking every plan, submit only operations for which the native setter was attempted. Pipe exactly one JSON value to `~/.local/bin/threadbear title-plan --json --report` with this strict shape: `{"reports":[{"operation_id":"OPERATION_ID","task_id":"TASK_ID","native_success":true}]}`. Every report object requires exact `operation_id`, `task_id`, and boolean `native_success`; a failed native call also requires nonempty `error_code`. Reporting is accepted only when `rejected_ids` is empty and `accepted_ids` has exact set equality with the submitted task IDs. Self-archive inside the same child `functions.exec` with `await tools.codex_app__set_thread_archived({archived: true})` only after accepted reporting and every `native_success` is true; omit `threadId` deliberately so the child archives itself, never the source. If initial `plans` is empty, skip the empty report and self-archive only when every disposition is `no_op`, `canonical_persisted`, or `native_succeeded_pending_canonical`. Any `drifted`, `missing`, malformed, unexpected, rejected, or native-failure result leaves the child visible and unarchived with one stable `title_actuation_failed` error and no retry or inspection. A successful self-archive is expected to interrupt the child, so no final message is required. The deterministic helper owns all title and semantic decisions; never classify, synthesize, normalize, or revise a title. If native tools, explicit opt-in, or the source guard are absent, emit the footer normally and leave the durable plan pending; never wake the persistent ThreadBear control task for routine title work.
