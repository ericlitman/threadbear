import assert from "node:assert/strict";
import fs from "node:fs";

const source=fs.readFileSync(new URL("../internal/titleplan/titleplan.go",import.meta.url),"utf8");
const match=source.match(/const ChildActuatorProgram = `([\s\S]*?)`\n\nconst ChildPrompt =/);
assert(match,"one child actuator program constant must exist");
const program=match[1],placeholder="__THREADBEAR_SOURCE_UUID__";
assert.equal(program.split(placeholder).length-1,1);
assert(source.includes("` + ChildActuatorProgram + `"),"the shipped prompt must embed the exact program constant");
assert(/^[\x00-\x7f]*$/.test(program),"the actuator program must remain ASCII");
const AsyncFunction=Object.getPrototypeOf(async function(){}).constructor;
const execute=new AsyncFunction("tools","text",program.replace(placeholder,"11111111-1111-4111-8111-111111111111"));
const completed=value=>({output:JSON.stringify(value),exit_code:0});
const plan=expectedTitle=>({operation_id:"5cc48150fdce0758b399e83cca4b014f",task_id:"22222222-2222-4222-8222-222222222222",expected_revision:"1700000000123",expected_title:expectedTitle,desired_title:"✅ Explain idempotent retry safety · out 564"});
const envelope=(mode,plans,dispositions=[])=>({version:1,mode,plans,dispositions});

async function replay(name,options={}){
  const expectedTitle=options.expectedTitle??"Explain idempotent retry safety";
  const item=plan(expectedTitle);
  const calls={commands:[],setters:[],archives:[],text:[]};
  const wait=options.waitEnvelope??envelope("wait",[item]);
  const operation=options.operationEnvelope??envelope("operation",[item]);
  const report=options.reportEnvelope??{version:1,accepted_ids:[item.task_id],rejected_ids:[]};
  const tools={
    exec_command:async({cmd})=>{
      calls.commands.push(cmd);
      if(cmd.includes("--wait"))return options.waitCommand??completed(wait);
      if(cmd.includes("--operation"))return typeof options.operationCommand==="function"?options.operationCommand(cmd):options.operationCommand??completed(operation);
      if(cmd.includes("--report"))return options.reportCommand??completed(report);
      throw new Error("unexpected command");
    },
    codex_app__set_thread_title:async args=>{
      calls.setters.push(args);
      if(options.setterFailure)throw new Error("native failure");
      return options.setterResult;
    },
    codex_app__set_thread_archived:async args=>{calls.archives.push(args);return "archived"},
  };
  await execute(tools,value=>calls.text.push(value));
  const rendered=calls.text.join("\n");
  for(const secret of ["11111111-1111-4111-8111-111111111111",item.operation_id,item.task_id,item.desired_title,expectedTitle]){
    if(secret)assert(!rendered.includes(secret),`${name} leaked private actuator data`);
  }
  return {calls,item,result:JSON.parse(calls.text.at(-1))};
}

for(const expectedTitle of ["Explain idempotent retry safety",""]){
  const {calls,item,result}=await replay(`success expected_title=${JSON.stringify(expectedTitle)}`,{expectedTitle,setterResult:{ignored:true}});
  assert.deepEqual(result,{ok:true});
  assert.deepEqual(calls.setters,[{threadId:item.task_id,title:item.desired_title}]);
  assert.equal(calls.commands.filter(x=>x.includes("--report")).length,1);
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
  assert.equal(calls.commands.filter(x=>x.includes("--report")).length,0);
  assert.deepEqual(calls.archives,[{archived:true}]);
}

{
  const drift=envelope("operation",[],[{task_id:"22222222-2222-4222-8222-222222222222",outcome:"drifted"}]);
  const {calls,result}=await replay("revalidation drift",{operationEnvelope:drift});
  assert.equal(result.error,"title_actuation_failed");
  assert.equal(calls.setters.length,0);
  assert.equal(calls.archives.length,0);
}

for(const [name,options] of [
  ["malformed wait completion",{waitCommand:"raw"}],
  ["running operation completion",{operationCommand:{output:"{}",exit_code:0,session_id:"running"}}],
  ["failed report completion",{reportCommand:{output:"{}",exit_code:1}}],
  ["missing expected revision",{waitEnvelope:envelope("wait",[{operation_id:"5cc48150fdce0758b399e83cca4b014f",task_id:"22222222-2222-4222-8222-222222222222",expected_title:"",desired_title:"Title"}])}],
  ["string disposition",{waitEnvelope:envelope("wait",[],["no_op"])}],
]){
  const {calls,result}=await replay(name,options);
  assert.equal(result.error,"title_actuation_failed");
  assert.equal(calls.archives.length,0);
}

{
  const {calls,result}=await replay("setter failure",{setterFailure:true});
  assert.equal(result.error,"title_actuation_failed");
  assert.equal(calls.setters.length,1);
  const report=calls.commands.find(x=>x.includes("--report"));
  assert(report?.includes('"native_success":false'));
  assert(report?.includes('"error_code":"native_set_failed"'));
  assert.equal(calls.archives.length,0);
}

{
  const rejected={version:1,accepted_ids:[],rejected_ids:["22222222-2222-4222-8222-222222222222"]};
  const {calls,result}=await replay("report rejection",{reportEnvelope:rejected});
  assert.equal(result.error,"title_actuation_failed");
  assert.equal(calls.setters.length,1);
  assert.equal(calls.archives.length,0);
}

{
  const unequal={version:1,accepted_ids:[],rejected_ids:[]};
  const {calls,result}=await replay("accepted ID inequality",{reportEnvelope:unequal});
  assert.equal(result.error,"title_actuation_failed");
  assert.equal(calls.setters.length,1);
  assert.equal(calls.archives.length,0);
}

{
  const first=plan("First title"),second={...plan("Second title"),operation_id:"6cc48150fdce0758b399e83cca4b014f",task_id:"66666666-6666-4666-8666-666666666666"};
  const wait=envelope("wait",[first,second]);
  const operationCommand=cmd=>cmd.includes(first.operation_id)?completed(envelope("operation",[first])):completed(envelope("operation",[],[{task_id:second.task_id,outcome:"drifted"}]));
  const report={version:1,accepted_ids:[first.task_id],rejected_ids:[]};
  const {calls,result}=await replay("report attempts before later drift",{waitEnvelope:wait,operationCommand,reportEnvelope:report});
  assert.equal(result.error,"title_actuation_failed");
  assert.equal(calls.setters.length,1);
  assert.equal(calls.commands.filter(x=>x.includes("--report")).length,1);
  assert.equal(calls.archives.length,0);
}

console.log("exact Luna title actuator V8 replay passed");
