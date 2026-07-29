import assert from "node:assert/strict";
import fs from "node:fs";

const source=fs.readFileSync(new URL("../internal/titleplan/titleplan.go",import.meta.url),"utf8");
const programMatch=source.match(/const ChildActuatorProgram = `([\s\S]*?)`\n\nconst ChildActuatorLoader =/);
const loaderMatch=source.match(/const ChildActuatorLoader = `([\s\S]*?)`\n\nconst ChildPrompt =/);
assert(programMatch,"one child actuator program constant must exist");
assert(loaderMatch,"one child actuator loader constant must exist");
const program=programMatch[1],loader=loaderMatch[1],placeholder="__THREADBEAR_SOURCE_UUID__",sourceID="11111111-1111-4111-8111-111111111111";
const expectedLoader=String.raw`const r=await tools.exec_command({cmd:"~/.local/bin/threadbear title-plan --json --actuator __THREADBEAR_SOURCE_UUID__"});await(0,eval)("(async function(){"+JSON.parse(r.output).program+"\n})()")`;
const prompt="THREADBEAR_TITLE_ACTUATOR_V1\none model pass;replace sole placeholder with lowercase canonical codex_delegation.source_thread_id;submit otherwise byte-for-byte to one functions.exec;no other tool or prose;"+loader;
assert.equal(loader,expectedLoader);
assert.equal(Buffer.byteLength(loader),195);
assert.equal(Buffer.byteLength(prompt),399);
assert.equal(prompt.split(placeholder).length-1,1);
assert.equal(prompt.split("functions.exec").length-1,1);
assert(!/[<>&]/.test(prompt));
assert.equal(program.split(placeholder).length-1,1);
assert(source.includes("` + ChildActuatorLoader + `"));
assert(!source.includes("` + ChildActuatorProgram + `"));
assert(/^[\x00-\x7f]*$/.test(program));
const boundProgram=program.replace(placeholder,sourceID);
const AsyncFunction=Object.getPrototypeOf(async function(){}).constructor;
const completed=value=>({output:JSON.stringify(value),exit_code:0});
const plan=(overrides={})=>({operation_id:"5cc48150fdce0758b399e83cca4b014f",task_id:"22222222-2222-4222-8222-222222222222",expected_revision:"1700000000123",expected_title:"Explain idempotent retry safety",desired_title:"✅ Explain idempotent retry safety · out 564",...overrides});
const envelope=(mode,plans,dispositions=[])=>({version:1,mode,plans,dispositions,helper_metadata:true});

async function replay(options={}){
  const execute=new AsyncFunction(loader.replace(placeholder,sourceID));
  const item=options.item??plan();
  const wait=options.waitEnvelope??envelope("wait",[item]);
  const operation=options.operationEnvelope??envelope("operation",[item]);
  const report=options.reportEnvelope??{version:1,accepted_ids:[item.task_id],rejected_ids:[],helper_metadata:true};
  const calls={commands:[],setters:[],archives:[],text:[]};
  const command=async(value,fallback,cmd)=>{
    if(value instanceof Error)throw value;
    if(typeof value==="function")return value(cmd);
    return value===undefined?fallback:value;
  };
  globalThis.tools={
    exec_command:async({cmd})=>{
      calls.commands.push(cmd);
      if(cmd.includes("--actuator"))return completed({version:1,program:boundProgram});
      if(cmd.includes("--wait"))return command(options.waitCommand,completed(wait),cmd);
      if(cmd.includes("--operation"))return command(options.operationCommand,completed(operation),cmd);
      if(cmd.includes("--report"))return command(options.reportCommand,completed(report),cmd);
      throw new Error("unexpected command");
    },
    codex_app__set_thread_title:async args=>{calls.setters.push(args);if(options.setterFailure)throw new Error("native failure")},
    codex_app__set_thread_archived:async args=>{calls.archives.push(args);if(options.archiveInterruption)throw options.archiveInterruption},
  };
  globalThis.text=value=>calls.text.push(value);
  let thrown;
  try{await execute()}catch(error){thrown=error}finally{delete globalThis.tools;delete globalThis.text}
  return {calls,item,thrown,result:calls.text.length?JSON.parse(calls.text.at(-1)):undefined};
}

async function expectFailure(options){
  const replayed=await replay(options);
  assert.equal(replayed.thrown,undefined);
  assert.deepEqual(replayed.result,{ok:false,error:"title_actuation_failed"});
  assert.equal(replayed.calls.archives.length,0);
  return replayed;
}

{
  const {calls,item,result}=await replay();
  assert.deepEqual(result,{ok:true});
  assert.equal(calls.commands[0],`~/.local/bin/threadbear title-plan --json --actuator ${sourceID}`);
  assert.deepEqual(calls.setters,[{threadId:item.task_id,title:item.desired_title}]);
  assert.equal(calls.commands.filter(value=>value.includes("--report")).length,1);
  assert.deepEqual(calls.archives,[{archived:true}]);
}

{
  const dispositions=[
    {task_id:"33333333-3333-4333-8333-333333333333",outcome:"no_op"},
    {task_id:"44444444-4444-4444-8444-444444444444",outcome:"canonical_persisted"},
    {task_id:"55555555-5555-4555-8555-555555555555",outcome:"native_succeeded_pending_canonical"},
  ];
  const {calls,result}=await replay({waitEnvelope:envelope("wait",[],dispositions)});
  assert.deepEqual(result,{ok:true});
  assert.equal(calls.setters.length,0);
  assert.equal(calls.commands.filter(value=>value.includes("--report")).length,0);
  assert.deepEqual(calls.archives,[{archived:true}]);
}

for(const waitCommand of [new Error("command failed"),{output:"{}",exit_code:1},{output:"{}",exit_code:0,session_id:"running"},{output:"{",exit_code:0}]){
  const {calls}=await expectFailure({waitCommand});
  assert.equal(calls.setters.length,0);
}

await expectFailure({waitEnvelope:envelope("wait",[plan({operation_id:""})])});
await expectFailure({waitEnvelope:envelope("wait",[plan({task_id:""})])});
await expectFailure({operationEnvelope:envelope("operation",[],[{task_id:plan().task_id,outcome:"drifted"}])});
await expectFailure({operationEnvelope:envelope("operation",[plan({task_id:""})])});
await expectFailure({operationEnvelope:envelope("operation",[plan({task_id:"33333333-3333-4333-8333-333333333333"})])});

{
  const {calls}=await expectFailure({setterFailure:true});
  assert.equal(calls.setters.length,1);
  const reportCommand=calls.commands.find(value=>value.includes("--report"));
  assert(reportCommand?.includes('"native_success":false'));
  assert(reportCommand?.includes('"error_code":"native_set_failed"'));
}

await expectFailure({reportEnvelope:{version:1,accepted_ids:[],rejected_ids:[plan().task_id]}});
await expectFailure({reportEnvelope:{version:1,accepted_ids:[],rejected_ids:[]}});
await expectFailure({reportEnvelope:{version:1,accepted_ids:[plan().task_id,plan().task_id],rejected_ids:[]}});

{
  const first=plan(),second=plan({operation_id:"6cc48150fdce0758b399e83cca4b014f",task_id:"66666666-6666-4666-8666-666666666666"});
  const operationCommand=cmd=>cmd.includes(first.operation_id)?completed(envelope("operation",[first])):completed(envelope("operation",[],[{task_id:second.task_id,outcome:"drifted"}]));
  const {calls}=await expectFailure({item:first,waitEnvelope:envelope("wait",[first,second]),operationCommand,reportEnvelope:{version:1,accepted_ids:[first.task_id],rejected_ids:[]}});
  assert.equal(calls.setters.length,1);
  assert.equal(calls.commands.filter(value=>value.includes("--report")).length,1);
}

{
  const interruption=new Error("expected archive interruption");
  const {calls,result,thrown}=await replay({archiveInterruption:interruption});
  assert.equal(thrown,interruption);
  assert.equal(result,undefined);
  assert.equal(calls.setters.length,1);
  assert.equal(calls.commands.filter(value=>value.includes("--report")).length,1);
  assert.deepEqual(calls.archives,[{archived:true}]);
}

console.log("supported Luna title actuator V8 replay passed");
