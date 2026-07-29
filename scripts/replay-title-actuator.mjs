import assert from "node:assert/strict";
import fs from "node:fs";

const source=fs.readFileSync(new URL("../internal/titleplan/titleplan.go",import.meta.url),"utf8");
const programMatch=source.match(/const ChildActuatorProgram = `([\s\S]*?)`\n\nconst ChildActuatorLoader =/);
const loaderMatch=source.match(/const ChildActuatorLoader = `([\s\S]*?)`\n\nconst ChildPrompt =/);
assert(programMatch,"one child actuator program constant must exist");
assert(loaderMatch,"one child actuator loader constant must exist");
const program=programMatch[1],loader=loaderMatch[1],placeholder="__THREADBEAR_SOURCE_UUID__",sourceID="11111111-1111-4111-8111-111111111111";
assert.equal(program.split(placeholder).length-1,1);
assert.equal(loader.split(placeholder).length-1,1);
assert(source.includes("` + ChildActuatorLoader + `"),"the shipped prompt must embed the exact loader constant");
assert(!source.includes("` + ChildActuatorProgram + `"),"the shipped prompt must not embed the actuator program");
assert(/^[\x00-\x7f]*$/.test(program),"the actuator program must remain ASCII");
assert(/^[\x00-\x7f]*$/.test(loader),"the actuator loader must remain ASCII");
const boundProgram=program.replace(placeholder,sourceID);
assert(!boundProgram.includes(placeholder));
assert.equal(boundProgram.split(sourceID).length-1,1);
const AsyncFunction=Object.getPrototypeOf(async function(){}).constructor;
const execute=new AsyncFunction(loader.replace(placeholder,sourceID));
const completed=value=>({output:JSON.stringify(value),exit_code:0});
const plan=(expectedTitle="Explain idempotent retry safety",overrides={})=>({operation_id:"5cc48150fdce0758b399e83cca4b014f",task_id:"22222222-2222-4222-8222-222222222222",expected_revision:"1700000000123",expected_title:expectedTitle,desired_title:"✅ Explain idempotent retry safety · out 564",...overrides});
const envelope=(mode,plans,dispositions=[])=>({version:1,mode,plans,dispositions});
const omit=(value,key)=>Object.fromEntries(Object.entries(value).filter(([name])=>name!==key));
const strings=value=>typeof value==="string"?[value]:Array.isArray(value)?value.flatMap(strings):value&&typeof value==="object"?Object.values(value).flatMap(strings):[];

async function replay(name,options={}){
  const item=options.item??plan(options.expectedTitle);
  const calls={commands:[],setters:[],archives:[],text:[]};
  const wait=options.waitEnvelope??envelope("wait",[item]);
  const operation=options.operationEnvelope??envelope("operation",[item]);
  const report=options.reportEnvelope??{version:1,accepted_ids:[item.task_id],rejected_ids:[]};
  const helper=options.helperEnvelope??{version:1,program:options.helperProgram??boundProgram};
  const command=async(value,fallback,cmd)=>{
    if(value instanceof Error)throw value;
    if(typeof value==="function")return value(cmd);
    return value===undefined?fallback:value;
  };
  const tools={
    exec_command:async({cmd})=>{
      calls.commands.push(cmd);
      if(cmd.includes("--actuator"))return command(options.helperCommand,completed(helper),cmd);
      if(cmd.includes("--wait"))return command(options.waitCommand,completed(wait),cmd);
      if(cmd.includes("--operation"))return command(options.operationCommand,completed(operation),cmd);
      if(cmd.includes("--report"))return command(options.reportCommand,completed(report),cmd);
      throw new Error("unexpected command");
    },
    codex_app__set_thread_title:async args=>{
      calls.setters.push(args);
      if(options.setterFailure)throw new Error("native failure");
      return options.setterResult;
    },
    codex_app__set_thread_archived:async args=>{
      calls.archives.push(args);
      if(options.archiveInterruption)throw options.archiveInterruption;
      return "archived";
    },
  };
  const hadTools=Object.prototype.hasOwnProperty.call(globalThis,"tools"),previousTools=globalThis.tools;
  const hadText=Object.prototype.hasOwnProperty.call(globalThis,"text"),previousText=globalThis.text;
  globalThis.tools=tools;
  globalThis.text=value=>calls.text.push(value);
  let thrown;
  try{await execute()}catch(error){thrown=error}finally{
    if(hadTools)globalThis.tools=previousTools;else delete globalThis.tools;
    if(hadText)globalThis.text=previousText;else delete globalThis.text;
  }
  const rendered=calls.text.join("\n");
  for(const secret of new Set([sourceID,...strings(wait),...strings(operation),...strings(report),...(options.secrets??[])])){
    if(secret)assert(!rendered.includes(secret),`${name} leaked private actuator data`);
  }
  for(const field of ['"program"','"version"','"mode"','"plans"','"dispositions"','"expected_revision"','"expected_title"'])assert(!rendered.includes(field),`${name} leaked a helper envelope`);
  return {calls,item,rendered,thrown,result:calls.text.length?JSON.parse(calls.text.at(-1)):undefined};
}

async function expectFailure(name,options){
  const replayed=await replay(name,options);
  assert.equal(replayed.thrown,undefined,`${name} unexpectedly threw`);
  assert.equal(replayed.result?.error,"title_actuation_failed",`${name} did not fail closed`);
  assert.equal(replayed.calls.archives.length,0,`${name} archived on failure`);
  return replayed;
}

for(const [name,setterResult] of [["fulfilled setter string","set"],["fulfilled setter null",null],["fulfilled setter undefined",undefined]]){
  const {calls,item,result}=await replay(name,{setterResult});
  assert.deepEqual(result,{ok:true});
  assert.equal(calls.commands[0],`~/.local/bin/threadbear title-plan --json --actuator ${sourceID}`);
  assert.deepEqual(calls.setters,[{threadId:item.task_id,title:item.desired_title}]);
  assert.equal(calls.commands.filter(value=>value.includes("--report")).length,1);
  assert.deepEqual(calls.archives,[{archived:true}]);
}

{
  const {calls,item,result}=await replay("empty expected_title",{expectedTitle:""});
  assert.deepEqual(result,{ok:true});
  assert.deepEqual(calls.setters,[{threadId:item.task_id,title:item.desired_title}]);
  assert.deepEqual(calls.archives,[{archived:true}]);
}

{
  const dispositions=[
    {task_id:"33333333-3333-4333-8333-333333333333",outcome:"no_op"},
    {task_id:"44444444-4444-4444-8444-444444444444",outcome:"canonical_persisted"},
    {task_id:"55555555-5555-4555-8555-555555555555",outcome:"native_succeeded_pending_canonical"},
  ];
  const {calls,result}=await replay("zero-plan success",{waitEnvelope:envelope("wait",[],dispositions)});
  assert.deepEqual(result,{ok:true});
  assert.equal(calls.setters.length,0);
  assert.equal(calls.commands.filter(value=>value.includes("--report")).length,0);
  assert.deepEqual(calls.archives,[{archived:true}]);
}

const malformed={
  "non-string output":{output:{},exit_code:0},
  "nonnumeric exit code":{output:"{}",exit_code:"0"},
  "thrown command":new Error("command failed"),
  "invalid JSON":{output:"{",exit_code:0},
  "running command":{output:"{}",exit_code:0,session_id:"running"},
  "nonzero exit code":{output:"{}",exit_code:1},
};
for(const [failure,value] of Object.entries(malformed)){
  const {calls}=await expectFailure(`helper ${failure}`,{helperCommand:value});
  assert.equal(calls.commands.length,1,`helper ${failure} evaluated a program`);
  assert.equal(calls.setters.length,0);
}
for(const [name,helperEnvelope] of [
  ["helper extra key",{version:1,program:boundProgram,extra:true}],
  ["helper missing program",{version:1}],
  ["helper wrong version",{version:2,program:boundProgram}],
  ["helper empty program",{version:1,program:""}],
  ["helper non-string program",{version:1,program:{}}],
]){
  const {calls}=await expectFailure(name,{helperEnvelope});
  assert.equal(calls.commands.length,1,`${name} evaluated a program`);
  assert.equal(calls.setters.length,0);
}
for(const stage of ["wait","operation","report"]){
  for(const [failure,value] of Object.entries(malformed))await expectFailure(`${stage} ${failure}`,{[`${stage}Command`]:value});
}

const base=plan();
const planEnvelope=envelope("wait",[base]);
const disposition={task_id:"33333333-3333-4333-8333-333333333333",outcome:"no_op"};
const report={version:1,accepted_ids:[base.task_id],rejected_ids:[]};
for(const [name,options] of [
  ["extra plan-envelope key",{waitEnvelope:{...planEnvelope,extra:true}}],
  ["missing plan-envelope key",{waitEnvelope:omit(planEnvelope,"dispositions")}],
  ["extra plan-item key",{waitEnvelope:envelope("wait",[{...base,extra:true}])}],
  ["missing expected_title",{waitEnvelope:envelope("wait",[omit(base,"expected_title")])}],
  ["extra disposition key",{waitEnvelope:envelope("wait",[],[{...disposition,extra:true}])}],
  ["missing disposition key",{waitEnvelope:envelope("wait",[],[omit(disposition,"outcome")])}],
  ["extra report key",{reportEnvelope:{...report,extra:true}}],
  ["missing report key",{reportEnvelope:omit(report,"rejected_ids")}],
  ["duplicate plan IDs",{waitEnvelope:envelope("wait",[base,{...base,operation_id:"6cc48150fdce0758b399e83cca4b014f"}])}],
  ["duplicate disposition IDs",{waitEnvelope:envelope("wait",[],[disposition,{...disposition,outcome:"canonical_persisted"}])}],
  ["overlapping plan/disposition IDs",{waitEnvelope:envelope("wait",[base],[{task_id:base.task_id,outcome:"no_op"}])}],
  ["extra accepted ID",{reportEnvelope:{version:1,accepted_ids:[base.task_id,"77777777-7777-4777-8777-777777777777"],rejected_ids:[]}}],
  ["duplicate accepted ID",{reportEnvelope:{version:1,accepted_ids:[base.task_id,base.task_id],rejected_ids:[]}}],
  ["duplicate rejected ID",{reportEnvelope:{version:1,accepted_ids:[],rejected_ids:[base.task_id,base.task_id]}}],
  ["overlapping accepted/rejected IDs",{reportEnvelope:{version:1,accepted_ids:[base.task_id],rejected_ids:[base.task_id]}}],
  ["string disposition",{waitEnvelope:envelope("wait",[],["no_op"])}],
])await expectFailure(name,options);

{
  const drift=envelope("operation",[],[{task_id:base.task_id,outcome:"drifted"}]);
  const {calls}=await expectFailure("revalidation drift",{operationEnvelope:drift});
  assert.equal(calls.setters.length,0);
}

{
  const {calls}=await expectFailure("setter failure",{setterFailure:true});
  assert.equal(calls.setters.length,1);
  const reportCommand=calls.commands.find(value=>value.includes("--report"));
  assert(reportCommand?.includes('"native_success":false'));
  assert(reportCommand?.includes('"error_code":"native_set_failed"'));
}

await expectFailure("report rejection",{reportEnvelope:{version:1,accepted_ids:[],rejected_ids:[base.task_id]}});
await expectFailure("accepted ID inequality",{reportEnvelope:{version:1,accepted_ids:[],rejected_ids:[]}});

{
  const first=plan("First title"),second=plan("Second title",{operation_id:"6cc48150fdce0758b399e83cca4b014f",task_id:"66666666-6666-4666-8666-666666666666"});
  const operationCommand=cmd=>cmd.includes(first.operation_id)?completed(envelope("operation",[first])):completed(envelope("operation",[],[{task_id:second.task_id,outcome:"drifted"}]));
  const {calls}=await expectFailure("report attempts before later drift",{item:first,waitEnvelope:envelope("wait",[first,second]),operationCommand,reportEnvelope:{version:1,accepted_ids:[first.task_id],rejected_ids:[]},secrets:[second.operation_id,second.task_id,second.desired_title,second.expected_title]});
  assert.equal(calls.setters.length,1);
  assert.equal(calls.commands.filter(value=>value.includes("--report")).length,1);
}

{
  const interruption=new Error("expected archive interruption");
  const {calls,result,thrown}=await replay("archive interruption after success",{archiveInterruption:interruption});
  assert.equal(thrown,interruption);
  assert.equal(result,undefined);
  assert.equal(calls.setters.length,1);
  assert.equal(calls.commands.filter(value=>value.includes("--report")).length,1);
  assert.deepEqual(calls.archives,[{archived:true}]);
}

console.log("runtime-loaded Luna title actuator V8 replay passed");
