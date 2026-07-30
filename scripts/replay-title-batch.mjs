import assert from "node:assert/strict";
import fs from "node:fs";
import vm from "node:vm";

const managed=fs.readFileSync(new URL("../assets/AGENTS.threadbear.md",import.meta.url),"utf8");
const match=managed.match(/## Native title batch[\s\S]*?```js\n([\s\S]*?)\n```/);
assert(match,"embedded native title batch program is missing");
const program=match[1];
assert(!program.includes("process"));
assert(!program.includes("CODEX_THREAD_ID"));
assert(!program.includes("source_task_id"));

const plans=[
  {operation_id:"op-accepted",task_id:"task-accepted",expected_revision:"1",expected_title:"old-a",desired_title:"new-a"},
  {operation_id:"op-drifted",task_id:"task-drifted",expected_revision:"1",expected_title:"old-b",desired_title:"new-b"},
  {operation_id:"op-failed",task_id:"task-failed",expected_revision:"1",expected_title:"old-c",desired_title:"new-c"},
];
const calls=[];
let listCalls=0;
let rendered="";
const completed=value=>({output:JSON.stringify(value),exit_code:0});
const tools={
  exec_command:async({cmd})=>{
    calls.push(cmd);
    if(cmd.endsWith("title-batch --json --list")){listCalls++;return listCalls===1?completed({version:1,mode:"list",plans,dispositions:[]}):completed({version:1,mode:"list",plans:[],dispositions:[{operation_id:"op-accepted",task_id:"task-accepted",outcome:"canonical_verified"}]});}
    if(cmd.includes("--operation 'op-accepted'")) return completed({version:1,mode:"operation",plans:[plans[0]],dispositions:[]});
    if(cmd.includes("--operation 'op-drifted'")) return completed({version:1,mode:"operation",plans:[],dispositions:[{operation_id:"op-drifted",task_id:"task-drifted",outcome:"drifted"}]});
    if(cmd.includes("--operation 'op-failed'")) return completed({version:1,mode:"operation",plans:[plans[2]],dispositions:[]});
    if(cmd.includes("title-batch --json --report")) return completed({version:1,accepted_ids:["op-accepted"],failed_ids:["op-failed"],drifted_ids:["op-drifted"],rejected_ids:[]});
    throw new Error(`unexpected command: ${cmd}`);
  },
  codex_app__set_thread_title:async({threadId,title})=>{
    calls.push(`set:${threadId}:${title}`);
    if(threadId==="task-failed") throw new Error("native failure");
  },
};
const context=vm.createContext({tools,text:value=>{rendered=value;}});
assert.equal("process" in context,false);
await vm.runInContext(`(async()=>{${program}\n})()`,context,{timeout:5000});
assert.deepEqual(JSON.parse(rendered),{accepted_ids:["op-accepted"],canonical_ids:["op-accepted"],failed_ids:["op-failed"],drifted_ids:["op-drifted"],rejected_ids:[]});
assert.equal(calls[0],"~/.local/bin/threadbear title-batch --json --list");
assert(calls.indexOf("set:task-accepted:new-a")>calls.findIndex(value=>value.includes("--operation 'op-accepted'")));
assert(calls.findIndex(value=>value.includes("title-batch --json --report"))>calls.indexOf("set:task-accepted:new-a"));
for(const privateValue of ["old-a","new-a","old-b","new-b","old-c","new-c","task-accepted","task-drifted","task-failed"]){assert(!rendered.includes(privateValue));}
console.log("fresh V8 title batch replay passed");

let malformedSetterCalls=0;
const malformedTools={
  exec_command:async({cmd})=>{
    if(cmd.endsWith("title-batch --json --list")) return completed({version:1,mode:"list",plans:[{operation_id:"bad"}],dispositions:[]});
    throw new Error(`unexpected malformed command: ${cmd}`);
  },
  codex_app__set_thread_title:async()=>{malformedSetterCalls++;},
};
const malformedContext=vm.createContext({tools:malformedTools,text:()=>{}});
await assert.rejects(vm.runInContext(`(async()=>{${program}\n})()`,malformedContext,{timeout:5000}),/invalid_title_operation/);
assert.equal(malformedSetterCalls,0);
