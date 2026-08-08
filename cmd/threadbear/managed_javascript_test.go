package main

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

func TestEmbeddedOnboardingJavaScriptResumesAndSerializesNativeWrites(t *testing.T) {
	protocol := readRepoFile(t, "assets", "skill", "SKILL.md")
	source := extractJavaScriptCell(t, protocol)
	sourceJSON, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}

	harness := fmt.Sprintf(`
const source = %s;
const plan = {
  ready:true, plan_complete:true, read_only:false, total:7,
  items:[
    {outcome:"prepared",task_id:"drift",title:"old drift",desired_title:"🐻 old drift"},
    {outcome:"prepared",task_id:"unreadable",title:"old unreadable",desired_title:"🐻 old unreadable"},
    {outcome:"prepared",task_id:"wrongid",title:"old wrongid",desired_title:"🐻 old wrongid"},
    {outcome:"prepared",task_id:"exact",title:"old exact",desired_title:"🐻 old exact"},
    {outcome:"prepared",task_id:"object",title:"old object",desired_title:"🐻 old object"},
    {outcome:"prepared",task_id:"bad",title:"old bad",desired_title:"🐻 old bad"},
    {outcome:"unchanged",task_id:"same",title:"same",desired_title:"same"}
  ]
};
const encoded = JSON.stringify(plan);
const cut1 = Math.floor(encoded.length / 3);
const cut2 = Math.floor(encoded.length * 2 / 3);
const trace = [], outputs = [], notices = [];
let writeCalls = 0;
const tools = {
  exec_command: async args => {
    trace.push("exec");
    if (args.cmd !== "\"$HOME/.local/bin/threadbear\" onboard --noninteractive --confirm --json" ||
        args.yield_time_ms !== 30000 || args.max_output_tokens !== 200000) throw new Error("bad exec args");
    return {session_id:77,output:encoded.slice(0,cut1)};
  },
  write_stdin: async args => {
    trace.push("write:" + args.session_id);
    if (args.session_id !== 77 || args.yield_time_ms !== 30000 ||
        args.max_output_tokens !== 200000) throw new Error("bad resume args");
    writeCalls++;
    if (writeCalls === 1) return {session_id:77,output:encoded.slice(cut1,cut2)};
    if (writeCalls === 2) return {exit_code:0,output:encoded.slice(cut2)};
    throw new Error("preparation process resumed more than needed");
  },
  codex_app__read_thread: async args => {
    trace.push("read:" + args.threadId);
    if (Object.keys(args).sort().join(",") !==
        "includeOutputs,maxOutputCharsPerItem,threadId,turnLimit" ||
        args.includeOutputs !== false || args.turnLimit !== 1 ||
        args.maxOutputCharsPerItem !== 1) throw new Error("bad read args");
    if (args.threadId === "drift") return JSON.stringify({thread:{id:"drift",title:"changed"}});
    if (args.threadId === "unreadable") return "{malformed";
    if (args.threadId === "wrongid") return JSON.stringify({thread:{id:"other",title:"old wrongid"}});
    if (args.threadId === "exact") return JSON.stringify({thread:{id:"exact",title:"old exact"}});
    if (args.threadId === "object") return {thread:{id:"object",title:"old object"}};
    if (args.threadId === "bad") return JSON.stringify({thread:{id:"bad",title:"old bad"}});
    throw new Error("unexpected read target");
  },
  codex_app__set_thread_title: async args => {
    trace.push("set:" + args.threadId);
    if (Object.keys(args).sort().join(",") !== "threadId,title") throw new Error("bad setter args");
    if (args.threadId === "exact" && args.title === "🐻 old exact")
      return JSON.stringify({threadId:"exact",title:"🐻 old exact"});
    if (args.threadId === "object" && args.title === "🐻 old object")
      return {threadId:"object",title:"🐻 old object"};
    if (args.threadId === "bad" && args.title === "🐻 old bad") return "{malformed";
    throw new Error("unexpected setter target");
  }
};
const text = value => outputs.push(value);
const notify = value => notices.push(value);
class Exit extends Error {}
const exit = () => { throw new Exit(); };
const AsyncFunction = Object.getPrototypeOf(async function(){}).constructor;
try {
  await new AsyncFunction("tools","text","exit","notify",source)(tools,text,exit,notify);
} catch (error) {
  if (!(error instanceof Exit)) throw error;
}
process.stdout.write(JSON.stringify({trace,outputs,notices,writeCalls}));
`, sourceJSON)

	output, err := exec.Command("node", "--input-type=module", "--eval", harness).CombinedOutput()
	if err != nil {
		t.Fatalf("execute embedded onboarding JavaScript: %v\n%s", err, output)
	}

	var run struct {
		Trace      []string `json:"trace"`
		Outputs    []string `json:"outputs"`
		Notices    []string `json:"notices"`
		WriteCalls int      `json:"writeCalls"`
	}
	if err := json.Unmarshal(output, &run); err != nil {
		t.Fatalf("decode JavaScript harness output: %v\n%s", err, output)
	}
	wantTrace := []string{
		"exec", "write:77", "write:77",
		"read:drift",
		"read:unreadable",
		"read:wrongid",
		"read:exact", "set:exact",
		"read:object", "set:object",
		"read:bad", "set:bad",
	}
	if !reflect.DeepEqual(run.Trace, wantTrace) {
		t.Fatalf("unexpected managed-loop order\n got: %v\nwant: %v", run.Trace, wantTrace)
	}
	if run.WriteCalls != 2 {
		t.Fatalf("write_stdin calls = %d; want two resumptions of the same process", run.WriteCalls)
	}
	if len(run.Outputs) != 1 {
		t.Fatalf("terminal outputs = %d; want one receipt", len(run.Outputs))
	}

	var receipt struct {
		Ready              bool `json:"ready"`
		PlanComplete       bool `json:"plan_complete"`
		OnboardingComplete bool `json:"onboarding_complete"`
		Total              int  `json:"total"`
		Updated            int  `json:"updated"`
		Skipped            int  `json:"skipped"`
		Unchanged          int  `json:"unchanged"`
		Unconfirmed        int  `json:"unconfirmed"`
	}
	if err := json.Unmarshal([]byte(run.Outputs[0]), &receipt); err != nil {
		t.Fatalf("decode managed-loop receipt: %v\n%s", err, run.Outputs[0])
	}
	if receipt.Ready || !receipt.PlanComplete || receipt.OnboardingComplete ||
		receipt.Total != 7 || receipt.Updated != 2 || receipt.Skipped != 3 ||
		receipt.Unchanged != 4 || receipt.Unconfirmed != 1 {
		t.Fatalf("unexpected managed-loop receipt: %+v", receipt)
	}
	if len(run.Notices) < 3 || run.Notices[0] != "ThreadBear onboarding: preparing" ||
		run.Notices[len(run.Notices)-1] != "ThreadBear onboarding: 6/6" {
		t.Fatalf("unexpected progress notifications: %v", run.Notices)
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
const writePlan = {ready:true,write_required:true,task_id:"current",desired_title:"✅ exact subject"};
const noWritePlan = {ready:true,write_required:false,task_id:"current"};
const AsyncFunction = Object.getPrototypeOf(async function(){}).constructor;
async function run(plan,nativeResult) {
  const trace = [], outputs = [];
  const tools = {
    exec_command: async args => {
      trace.push("exec");
      if (args.cmd !== "\"$HOME/.local/bin/threadbear\" title --status STATUS --json" ||
          args.yield_time_ms !== 30000 || args.max_output_tokens !== 1000) throw new Error("bad planner args");
      return {exit_code:0,output:JSON.stringify(plan)};
    },
    codex_app__set_thread_title: async args => {
      trace.push("set");
      if (Object.keys(args).join(",") !== "title" || args.title !== plan.desired_title)
        throw new Error("bad current-task setter args");
      return nativeResult;
    }
  };
  const text = value => outputs.push(typeof value === "string" ? value : JSON.stringify(value));
  class Exit extends Error {}
  const exit = () => { throw new Exit(); };
  try {
    await new AsyncFunction("tools","text","exit",source)(tools,text,exit);
  } catch (error) {
    if (!(error instanceof Exit)) throw error;
  }
  return {trace,outputs};
}
const expected = {threadId:writePlan.task_id,title:writePlan.desired_title};
const stringRun = await run(writePlan,JSON.stringify(expected));
const objectRun = await run(writePlan,expected);
const malformedRun = await run(writePlan,"{malformed");
const wrongIDRun = await run(writePlan,JSON.stringify({...expected,threadId:"wrong"}));
const wrongTitleRun = await run(writePlan,JSON.stringify({...expected,title:"wrong"}));
const noWriteRun = await run(noWritePlan,null);
process.stdout.write(JSON.stringify({
  stringRun,objectRun,malformedRun,wrongIDRun,wrongTitleRun,noWriteRun
}));
`, sourceJSON)

	output, err := exec.Command("node", "--input-type=module", "--eval", harness).CombinedOutput()
	if err != nil {
		t.Fatalf("execute embedded ordinary JavaScript: %v\n%s", err, output)
	}

	var got struct {
		StringRun     javascriptRun `json:"stringRun"`
		ObjectRun     javascriptRun `json:"objectRun"`
		MalformedRun  javascriptRun `json:"malformedRun"`
		WrongIDRun    javascriptRun `json:"wrongIDRun"`
		WrongTitleRun javascriptRun `json:"wrongTitleRun"`
		NoWriteRun    javascriptRun `json:"noWriteRun"`
	}
	if err := json.Unmarshal(output, &got); err != nil {
		t.Fatalf("decode ordinary JavaScript harness output: %v\n%s", err, output)
	}
	for name, run := range map[string]javascriptRun{"string": got.StringRun, "object": got.ObjectRun} {
		if !reflect.DeepEqual(run.Trace, []string{"exec", "set"}) {
			t.Fatalf("%s native result trace = %v; want one planner then one setter", name, run.Trace)
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
			receipt.Title != "✅ exact subject" || !receipt.Updated {
			t.Fatalf("unexpected %s native result receipt: %+v", name, receipt)
		}
	}
	for name, run := range map[string]javascriptRun{
		"malformed":   got.MalformedRun,
		"wrong ID":    got.WrongIDRun,
		"wrong title": got.WrongTitleRun,
	} {
		if !reflect.DeepEqual(run.Trace, []string{"exec", "set"}) {
			t.Fatalf("%s native result trace = %v; want one planner then one setter", name, run.Trace)
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
	if !reflect.DeepEqual(got.NoWriteRun.Trace, []string{"exec"}) || len(got.NoWriteRun.Outputs) != 1 {
		t.Fatalf("no-write plan must run one planner and zero setters: %+v", got.NoWriteRun)
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
