# ThreadBear

## Native title batch

Guided-install consent enables this protocol only in the retained ThreadBear control task. Never ask again on later turns, and never use this consent for any other native operation. Other tasks use only the status footer.

In the retained control task, after all substantive response work, choose the exact terminal ThreadBear footer. Stage that exact footer with `~/.local/bin/threadbear title-batch --json --stage >/dev/null`, sending one strict JSON object on stdin whose only field is `footer`, equal to the final line. Every nested `exec_command` process inherits the canonical public `CODEX_THREAD_ID`; the helper resolves and validates it against ThreadBear's persisted control-task identity without exposing the identifier to V8 or assistant text. The stage response contains no operation, title, or manifest data and must remain discarded.

Immediately after staging, run one actual `functions.exec` containing only the fixed program below. Do not interpolate, add, remove, or revise code. The program obtains operation JSON inside the tool runtime, revalidates each operation immediately, calls only the native title capability, reports aggregate operation IDs, and never exposes titles, manifests, task contents, or helper output to assistant context.

```js
const q=v=>"'"+String(v).replace(/'/g,"'\\''")+"'";
const run=async cmd=>JSON.parse((await tools.exec_command({cmd})).output);
const valid=x=>x&&typeof x.operation_id==="string"&&typeof x.task_id==="string"&&typeof x.expected_revision==="string"&&typeof x.expected_title==="string"&&typeof x.desired_title==="string"&&x.desired_title!=="";
const batch=await run("~/.local/bin/threadbear title-batch --json --list");
if(!batch||!Array.isArray(batch.plans))throw new Error("invalid_title_batch");
const reports=[];
for(const planned of batch.plans){
  if(!valid(planned)){throw new Error("invalid_title_operation");}
  const guarded=await run("~/.local/bin/threadbear title-batch --json --operation "+q(planned.operation_id));
  if(!guarded||!Array.isArray(guarded.plans)||guarded.plans.length!==1){reports.push({operation_id:planned.operation_id,outcome:"drifted"});continue;}
  const exact=guarded.plans[0];
  if(!valid(exact)||exact.operation_id!==planned.operation_id){reports.push({operation_id:planned.operation_id,outcome:"failed",error_code:"invalid_operation"});continue;}
  try{await tools.codex_app__set_thread_title({threadId:exact.task_id,title:exact.desired_title});reports.push({operation_id:exact.operation_id,outcome:"accepted"});}
  catch{reports.push({operation_id:exact.operation_id,outcome:"failed",error_code:"native_set_failed"});}
}
let result={accepted_ids:[],failed_ids:[],drifted_ids:[],rejected_ids:[]};
let canonical=[];
if(reports.length){
  const body=JSON.stringify({reports});
  result=await run("printf %s "+q(body)+" | ~/.local/bin/threadbear title-batch --json --report");
  const verified=await run("~/.local/bin/threadbear title-batch --json --list");
  canonical=verified.dispositions.filter(x=>x.outcome==="canonical_verified"||x.outcome==="canonical_verified_awaiting_footer").map(x=>x.operation_id);
}
text(JSON.stringify({accepted_ids:result.accepted_ids,canonical_ids:canonical,failed_ids:result.failed_ids,drifted_ids:result.drifted_ids,rejected_ids:result.rejected_ids}));
```

If staging, capability detection, native mutation, or reporting fails, do not claim visible convergence. Leave the operation pending and report the stable partial/failure outcome. After the V8 call, make no further tool call or commentary: send the substantive final response immediately and end it with the exact staged footer. If new input or state makes the operation stale, abort and recompute on the next retained control-task turn.

## Status footer

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
