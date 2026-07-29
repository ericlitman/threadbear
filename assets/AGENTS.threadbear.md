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

## Native title handoff

If task instructions start with `THREADBEAR_TITLE_ACTUATOR_V1`, this is the child: never dispatch recursively or read the skill; follow only the helper-owned prompt.

Otherwise, after substantive work, run one actual `functions.exec` containing only this program. It creates at most one child, never performs title/archive work in the source, and never retries. Return the substantive final immediately and leave the source unarchived.

```js
const result=await(async()=>{const o=x=>x!==null&&typeof x==="object"&&!Array.isArray(x),k=x=>Object.keys(x).sort().join(","),n=new Set(["source_missing","source_invalid","config_unavailable","config_invalid","state_unavailable","state_invalid","control_task","rename_disabled","agents_disabled"]),f=()=>({allow:false,dispatched:false,error:"dispatch_unavailable"});let r,e;try{r=await tools.exec_command({cmd:"~/.local/bin/threadbear title-plan --json --dispatch"})}catch{return f()}if(!o(r)||typeof r.output!=="string"||typeof r.exit_code!=="number"||r.exit_code!==0||"session_id"in r)return f();try{e=JSON.parse(r.output)}catch{return f()}if(!o(e)||e.version!==1||typeof e.allow!=="boolean"||typeof e.disposition!=="string")return f();if(!e.allow)return k(e)==="allow,disposition,version"&&n.has(e.disposition)?{allow:false,dispatched:false}:f();const c=e.child;if(k(e)!=="allow,child,disposition,version"||e.disposition!=="dispatch"||!o(c)||k(c)!=="model,prompt,target,thinking"||c.model!=="gpt-5.6-luna"||c.thinking!=="medium"||typeof c.prompt!=="string"||c.prompt.length>6000||!/^[\x00-\x7f]*$/.test(c.prompt)||!c.prompt.startsWith("THREADBEAR_TITLE_ACTUATOR_V1\n")||!o(c.target)||k(c.target)!=="directoryName,type"||c.target.type!=="projectless"||c.target.directoryName!=="threadbear-title-actuator")return f();try{await tools.codex_app__create_thread(e.child);return{allow:true,dispatched:true}}catch{return{allow:true,dispatched:false,error:"dispatch_failed"}}})();text(JSON.stringify(result))
```
