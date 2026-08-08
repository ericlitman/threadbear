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
  ready:true, plan_complete:true, read_only:false, total:4,
  items:[
    {outcome:"prepared",task_id:"drift",title:"old drift",desired_title:"🐻 old drift"},
    {outcome:"prepared",task_id:"exact",title:"old exact",desired_title:"🐻 old exact"},
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
    if (args.threadId === "drift") return {thread:{title:"changed"}};
    if (args.threadId === "exact") return {thread:{title:"old exact"}};
    if (args.threadId === "bad") return {thread:{title:"old bad"}};
    throw new Error("unexpected read target");
  },
  codex_app__set_thread_title: async args => {
    trace.push("set:" + args.threadId);
    if (Object.keys(args).sort().join(",") !== "threadId,title") throw new Error("bad setter args");
    if (args.threadId === "exact" && args.title === "🐻 old exact")
      return {threadId:"exact",title:"🐻 old exact"};
    if (args.threadId === "bad" && args.title === "🐻 old bad") return "malformed";
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
		"read:exact", "set:exact",
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
		receipt.Total != 4 || receipt.Updated != 1 || receipt.Skipped != 1 ||
		receipt.Unchanged != 2 || receipt.Unconfirmed != 1 {
		t.Fatalf("unexpected managed-loop receipt: %+v", receipt)
	}
	if len(run.Notices) < 3 || run.Notices[0] != "ThreadBear onboarding: preparing complete catalog" ||
		run.Notices[len(run.Notices)-1] != "ThreadBear onboarding: 3/3" {
		t.Fatalf("unexpected progress notifications: %v", run.Notices)
	}
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
