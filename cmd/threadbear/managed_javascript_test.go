package main

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

func TestEmbeddedUninstallJavaScriptBlocksTeardownOnDriftAndCommitsAfterExactCleanup(t *testing.T) {
	protocol := readRepoFile(t, "assets", "skill", "SKILL.md")
	source := extractJavaScriptCell(t, protocol)
	sourceJSON, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}

	harness := fmt.Sprintf(`
const source = %s;
class Exit extends Error {}
const AsyncFunction = Object.getPrototypeOf(async function(){}).constructor;
async function run(plan,yieldPreparation) {
  const trace = [], outputs = [], notices = [];
  const encoded = JSON.stringify(plan);
  let commandCalls = 0, writeCalls = 0;
  const tools = {
    exec_command: async args => {
      commandCalls++;
      if (args.yield_time_ms !== 30000 || args.max_output_tokens !== 200000 ||
          args.sandbox_permissions !== "require_escalated") throw new Error("bad exec args");
      if (commandCalls === 1) {
        trace.push("exec:prepare");
        if (args.cmd !== "\"$HOME/.local/bin/threadbear\" uninstall --prepare --noninteractive --confirm --json")
          throw new Error("bad preparation command");
        if (yieldPreparation) return {session_id:77,output:encoded.slice(0,Math.floor(encoded.length/2))};
        return {exit_code:0,output:encoded};
      }
      trace.push("exec:commit");
      if (args.cmd !== "\"$HOME/.local/bin/threadbear\" uninstall --noninteractive --confirm --json")
        throw new Error("bad commit command");
      return {exit_code:0,output:JSON.stringify({ready:true,uninstalled:true,restart_required:true})};
    },
    write_stdin: async args => {
      trace.push("write:" + args.session_id);
      writeCalls++;
      if (!yieldPreparation || writeCalls !== 1 || args.session_id !== 77 ||
          args.yield_time_ms !== 30000 || args.max_output_tokens !== 200000)
        throw new Error("bad resume args");
      return {exit_code:0,output:encoded.slice(Math.floor(encoded.length/2))};
    },
    codex_app__read_thread: async args => {
      trace.push("read:" + args.threadId);
      if (args.includeOutputs !== false || args.turnLimit !== 1 || args.maxOutputCharsPerItem !== 1)
        throw new Error("bad read args");
      const item = plan.items.find(value => value.task_id === args.threadId);
      if (args.threadId === "drift") return JSON.stringify({thread:{id:"drift",title:"changed"}});
      return args.threadId === "active" ? {thread:{id:args.threadId,title:item.title}} :
        JSON.stringify({thread:{id:args.threadId,title:item.title}});
    },
    codex_app__set_thread_title: async args => {
      trace.push("set:" + args.threadId + ":" + args.title);
      if (Object.keys(args).sort().join(",") !== "threadId,title" || args.title.startsWith("🐻 "))
        throw new Error("bad setter args");
      if (args.threadId === "failset") return "{malformed";
      return args.threadId === "active" ? {threadId:args.threadId,title:args.title} :
        JSON.stringify({threadId:args.threadId,title:args.title});
    }
  };
  const text = value => outputs.push(typeof value === "string" ? value : JSON.stringify(value));
  const notify = value => notices.push(value);
  const exit = () => { throw new Exit(); };
  try { await new AsyncFunction("tools","text","exit","notify",source)(tools,text,exit,notify); }
  catch (error) { if (!(error instanceof Exit)) throw error; }
  return {trace,outputs,notices,writeCalls};
}
const failure = await run({ready:true,plan_complete:true,read_only:false,total:4,
  needs_cleanup:3,prepared:3,unchanged:1,skipped:0,items:[
    {outcome:"prepared",task_id:"exact",title:"✅ exact",desired_title:"exact"},
    {outcome:"prepared",task_id:"drift",title:"🐻 drift",desired_title:"drift"},
    {outcome:"prepared",task_id:"failset",title:"🚨 fail",desired_title:"fail"},
    {outcome:"unchanged",task_id:"plain",title:"plain"}
  ]},true);
const success = await run({ready:true,plan_complete:true,read_only:false,total:4,
  needs_cleanup:2,prepared:2,unchanged:1,skipped:1,items:[
    {outcome:"prepared",task_id:"history",title:"🐻 history",desired_title:"history"},
    {outcome:"unchanged",task_id:"plain",title:"plain"},
    {outcome:"skipped",task_id:"unsafe"},
    {outcome:"prepared",task_id:"active",title:"✅ active",desired_title:"active"}
  ]},false);
process.stdout.write(JSON.stringify({failure,success}));
`, sourceJSON)

	output, err := exec.Command("node", "--input-type=module", "--eval", harness).CombinedOutput()
	if err != nil {
		t.Fatalf("execute embedded uninstall JavaScript: %v\n%s", err, output)
	}

	var run struct {
		Failure struct {
			Trace      []string `json:"trace"`
			Outputs    []string `json:"outputs"`
			Notices    []string `json:"notices"`
			WriteCalls int      `json:"writeCalls"`
		} `json:"failure"`
		Success struct {
			Trace      []string `json:"trace"`
			Outputs    []string `json:"outputs"`
			Notices    []string `json:"notices"`
			WriteCalls int      `json:"writeCalls"`
		} `json:"success"`
	}
	if err := json.Unmarshal(output, &run); err != nil {
		t.Fatalf("decode JavaScript harness output: %v\n%s", err, output)
	}
	wantFailureTrace := []string{
		"exec:prepare", "write:77", "read:exact", "set:exact:exact",
		"read:drift", "read:failset", "set:failset:fail",
	}
	if !reflect.DeepEqual(run.Failure.Trace, wantFailureTrace) {
		t.Fatalf("unexpected failed cleanup order\n got: %v\nwant: %v", run.Failure.Trace, wantFailureTrace)
	}
	if run.Failure.WriteCalls != 1 || len(run.Failure.Outputs) != 1 {
		t.Fatalf("failed cleanup resumptions/outputs = %d/%d", run.Failure.WriteCalls, len(run.Failure.Outputs))
	}
	var failedReceipt struct {
		Ready           bool `json:"ready"`
		Uninstalled     bool `json:"uninstalled"`
		CleanupComplete bool `json:"cleanup_complete"`
		Updated         int  `json:"updated"`
		Drifted         int  `json:"drifted"`
		Unconfirmed     int  `json:"unconfirmed"`
	}
	if err := json.Unmarshal([]byte(run.Failure.Outputs[0]), &failedReceipt); err != nil {
		t.Fatalf("decode failed cleanup receipt: %v\n%s", err, run.Failure.Outputs[0])
	}
	if failedReceipt.Ready || failedReceipt.Uninstalled || failedReceipt.CleanupComplete ||
		failedReceipt.Updated != 1 || failedReceipt.Drifted != 1 || failedReceipt.Unconfirmed != 1 {
		t.Fatalf("unexpected failed cleanup receipt: %+v", failedReceipt)
	}
	if containsString(run.Failure.Trace, "exec:commit") ||
		run.Failure.Notices[len(run.Failure.Notices)-1] != "ThreadBear uninstall: titles 3/3" {
		t.Fatalf("failed cleanup crossed teardown boundary: trace=%v notices=%v", run.Failure.Trace, run.Failure.Notices)
	}

	wantSuccessTrace := []string{"exec:prepare", "read:history", "set:history:history",
		"read:active", "set:active:active", "exec:commit"}
	if !reflect.DeepEqual(run.Success.Trace, wantSuccessTrace) {
		t.Fatalf("unexpected successful cleanup order\n got: %v\nwant: %v", run.Success.Trace, wantSuccessTrace)
	}
	var successReceipt struct {
		Uninstalled  bool `json:"uninstalled"`
		TitleCleanup struct {
			Updated, Unchanged, Skipped int
		} `json:"title_cleanup"`
	}
	if len(run.Success.Outputs) != 1 || json.Unmarshal([]byte(run.Success.Outputs[0]), &successReceipt) != nil ||
		!successReceipt.Uninstalled || successReceipt.TitleCleanup.Updated != 2 ||
		successReceipt.TitleCleanup.Unchanged != 1 || successReceipt.TitleCleanup.Skipped != 1 {
		t.Fatalf("unexpected successful uninstall receipt: outputs=%v receipt=%+v", run.Success.Outputs, successReceipt)
	}
	if run.Success.Notices[len(run.Success.Notices)-1] != "ThreadBear uninstall: removing managed artifacts" ||
		run.Success.Trace[len(run.Success.Trace)-1] != "exec:commit" {
		t.Fatalf("successful uninstall made work after commit: trace=%v notices=%v", run.Success.Trace, run.Success.Notices)
	}
}

func TestEmbeddedOrdinaryJavaScriptAcceptsStringAndObjectNativeResults(t *testing.T) {
	guidance := readRepoFile(t, "assets", "AGENTS.threadbear.md")
	source := extractJavaScriptCell(t, guidance)
	sourceJSON, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}

	harness := fmt.Sprintf(`
const source = %s;
const policy = {ready:true,task_id:"current",status:"complete",icon:"✅",
  owned_prefixes:["✅ ","➡️ ","🙋 ","🚨 ","🤖 ","🐻 "],
  blocked_prefixes:["➡ ","⏳ ","❔ ","🧵🐻"],
  internal_markers:["<codex_delegation>"],max_title_units:60};
const AsyncFunction = Object.getPrototypeOf(async function(){}).constructor;
async function run(currentResult,setResult) {
  const trace = [], outputs = [];
  const tools = {
    exec_command: async args => {
      trace.push("exec");
      if (args.cmd !== "\"$HOME/.local/bin/threadbear\" title --status STATUS --json" ||
          args.yield_time_ms !== 30000 || args.max_output_tokens !== 1000) throw new Error("bad helper args");
      return {exit_code:0,output:JSON.stringify(policy)};
    },
    codex_app__read_thread: async args => {
      trace.push("read");
      if (args.threadId !== policy.task_id || args.includeOutputs !== false ||
          args.turnLimit !== 1 || args.maxOutputCharsPerItem !== 1) throw new Error("bad read args");
      return currentResult;
    },
    codex_app__set_thread_title: async args => {
      trace.push("set:" + args.title);
      if (Object.keys(args).join(",") !== "title") throw new Error("setter received explicit task ID");
      return setResult;
    }
  };
  const text = value => outputs.push(typeof value === "string" ? value : JSON.stringify(value));
  class Exit extends Error {}
  const exit = () => { throw new Exit(); };
  try { await new AsyncFunction("tools","text","exit",source)(tools,text,exit); }
  catch (error) { if (!(error instanceof Exit)) throw error; }
  return {trace,outputs};
}
const current = {thread:{id:"current",title:"🎉 exact subject"}};
const expected = {threadId:"current",title:"✅ 🎉 exact subject"};
const stringRun = await run(JSON.stringify(current),JSON.stringify(expected));
const objectRun = await run(current,expected);
const malformedRun = await run(JSON.stringify(current),"{malformed");
const wrongIDRun = await run(JSON.stringify(current),JSON.stringify({...expected,threadId:"wrong"}));
const wrongTitleRun = await run(JSON.stringify(current),JSON.stringify({...expected,title:"wrong"}));
const noWriteRun = await run(JSON.stringify({thread:{id:"current",title:"✅ exact subject"}}),null);
const badReadRun = await run("{malformed",null);
const wrongReadIDRun = await run(JSON.stringify({thread:{id:"other",title:"exact subject"}}),null);
const blockedRun = await run(JSON.stringify({thread:{id:"current",title:"🧵🐻 needs input (you): approve"}}),null);
process.stdout.write(JSON.stringify({stringRun,objectRun,malformedRun,wrongIDRun,
  wrongTitleRun,noWriteRun,badReadRun,wrongReadIDRun,blockedRun}));
`, sourceJSON)

	output, err := exec.Command("node", "--input-type=module", "--eval", harness).CombinedOutput()
	if err != nil {
		t.Fatalf("execute embedded ordinary JavaScript: %v\n%s", err, output)
	}

	var got struct {
		StringRun      javascriptRun `json:"stringRun"`
		ObjectRun      javascriptRun `json:"objectRun"`
		MalformedRun   javascriptRun `json:"malformedRun"`
		WrongIDRun     javascriptRun `json:"wrongIDRun"`
		WrongTitleRun  javascriptRun `json:"wrongTitleRun"`
		NoWriteRun     javascriptRun `json:"noWriteRun"`
		BadReadRun     javascriptRun `json:"badReadRun"`
		WrongReadIDRun javascriptRun `json:"wrongReadIDRun"`
		BlockedRun     javascriptRun `json:"blockedRun"`
	}
	if err := json.Unmarshal(output, &got); err != nil {
		t.Fatalf("decode ordinary JavaScript harness output: %v\n%s", err, output)
	}
	for name, run := range map[string]javascriptRun{"string": got.StringRun, "object": got.ObjectRun} {
		if !reflect.DeepEqual(run.Trace, []string{"exec", "read", "set:✅ 🎉 exact subject"}) {
			t.Fatalf("%s native result trace = %v; want one helper, read, and setter", name, run.Trace)
		}
		if len(run.Outputs) != 1 {
			t.Fatalf("%s native result outputs = %d; want one receipt", name, len(run.Outputs))
		}
		var receipt struct {
			Ready   bool   `json:"ready"`
			TaskID  string `json:"task_id"`
			Title   string `json:"title"`
			Updated bool   `json:"updated"`
		}
		if err := json.Unmarshal([]byte(run.Outputs[0]), &receipt); err != nil {
			t.Fatalf("decode %s native result receipt: %v", name, err)
		}
		if !receipt.Ready || receipt.TaskID != "current" ||
			receipt.Title != "✅ 🎉 exact subject" || !receipt.Updated {
			t.Fatalf("unexpected %s native result receipt: %+v", name, receipt)
		}
	}
	for name, run := range map[string]javascriptRun{
		"malformed":   got.MalformedRun,
		"wrong ID":    got.WrongIDRun,
		"wrong title": got.WrongTitleRun,
	} {
		if !reflect.DeepEqual(run.Trace, []string{"exec", "read", "set:✅ 🎉 exact subject"}) {
			t.Fatalf("%s native result trace = %v; want one helper, read, and setter", name, run.Trace)
		}
		if len(run.Outputs) != 1 {
			t.Fatalf("%s native result outputs = %d; want one failure", name, len(run.Outputs))
		}
		var receipt struct {
			Ready  bool   `json:"ready"`
			Reason string `json:"reason"`
		}
		if err := json.Unmarshal([]byte(run.Outputs[0]), &receipt); err != nil {
			t.Fatalf("decode %s native result failure: %v", name, err)
		}
		if receipt.Ready || receipt.Reason != "Codex title write was not confirmed exactly" {
			t.Fatalf("unexpected %s native result failure: %+v", name, receipt)
		}
	}
	if !reflect.DeepEqual(got.NoWriteRun.Trace, []string{"exec", "read"}) || len(got.NoWriteRun.Outputs) != 1 {
		t.Fatalf("exact title must run one helper/read and zero setters: %+v", got.NoWriteRun)
	}
	for name, run := range map[string]javascriptRun{
		"malformed read": got.BadReadRun, "wrong read ID": got.WrongReadIDRun, "blocked title": got.BlockedRun,
	} {
		if !reflect.DeepEqual(run.Trace, []string{"exec", "read"}) || len(run.Outputs) != 1 {
			t.Fatalf("%s must stop before setter: %+v", name, run)
		}
	}
}

type javascriptRun struct {
	Trace   []string `json:"trace"`
	Outputs []string `json:"outputs"`
}

func extractJavaScriptCell(t *testing.T, markdown string) string {
	t.Helper()
	const opener = "```js\n"
	start := strings.Index(markdown, opener)
	if start < 0 {
		t.Fatal("installed skill has no JavaScript cell")
	}
	rest := markdown[start+len(opener):]
	end := strings.Index(rest, "\n```")
	if end < 0 {
		t.Fatal("installed skill JavaScript cell is not closed")
	}
	if strings.Contains(rest[end+len("\n```"):], opener) {
		t.Fatal("installed skill contains more than one JavaScript cell")
	}
	return rest[:end]
}
