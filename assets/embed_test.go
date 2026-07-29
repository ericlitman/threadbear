package assets

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestManagedGuidanceUsesConcreteFooterExamples(t *testing.T) {
	examples := []string{
		"🧵🐻 complete",
		"🧵🐻 next steps (you): approve the release plan",
		"🧵🐻 next steps (agent): implement the approved plan",
		"🧵🐻 next steps (external): review the security exception",
		"🧵🐻 needs input (you): choose the release region",
		"🧵🐻 blocked (external): restore the signing service",
		"🧵🐻 automation",
	}
	for _, example := range examples {
		if !strings.Contains(AgentsManagedContent, example) {
			t.Errorf("managed AGENTS content is missing %q", example)
		}
		if !strings.Contains(SkillManagedContent, example) {
			t.Errorf("managed skill content is missing %q", example)
		}
	}
	if strings.Contains(AgentsManagedContent, "🧵🐻 STATUS") {
		t.Fatal("managed AGENTS content contains a fill-in footer")
	}
}

func TestManagedSkillConversationalContract(t *testing.T) {
	required := []string{
		"lead with a short friendly capability card, never a raw command dump",
		"`~/.local/bin/threadbear help`",
		"`~/.local/bin/threadbear help <command>`",
		"`~/.local/bin/threadbear configure --archive=false`",
		"`~/.local/bin/threadbear configure --token-display=off`",
		"`~/.local/bin/threadbear configure --auto-update=false`",
		"`--noninteractive` and `--confirm`",
		"closes this very chat the user is typing in",
		"add `--archive-control-task` only after an explicit yes",
		"Thanks for using ThreadBear. I'd love any feedback on why this wasn't for you. Drop me an email at eric@litman.org if you're open to sharing. Now, on to the uninstall!",
	}
	for _, text := range required {
		if !strings.Contains(SkillManagedContent, text) {
			t.Errorf("managed skill content is missing %q", text)
		}
	}
}

func javascriptBlocks(content string) []string {
	blocks := []string{}
	for {
		start := strings.Index(content, "```js\n")
		if start < 0 {
			return blocks
		}
		content = content[start+len("```js\n"):]
		end := strings.Index(content, "\n```")
		if end < 0 {
			return blocks
		}
		blocks = append(blocks, content[:end])
		content = content[end+len("\n```"):]
	}
}

func sourceProgram(t *testing.T, content string) string {
	t.Helper()
	blocks := javascriptBlocks(content)
	if len(blocks) != 1 {
		t.Fatalf("managed source has %d JavaScript blocks", len(blocks))
	}
	return blocks[0]
}

func TestManagedSourceIsRawV8StrictAndPhaseIsolated(t *testing.T) {
	if len([]byte(AgentsManagedContent)) > 3500 {
		t.Fatalf("managed AGENTS bytes=%d", len([]byte(AgentsManagedContent)))
	}
	for _, forbidden := range []string{"title-plan --json --wait", "title-plan --json --operation", "title-plan --json --report", "codex_app__set_thread_title", "codex_app__set_thread_archived", "accepted_ids", "native_success"} {
		if strings.Contains(AgentsManagedContent, forbidden) {
			t.Fatalf("managed AGENTS duplicates child contract %q", forbidden)
		}
	}
	if !strings.Contains(AgentsManagedContent, "THREADBEAR_TITLE_ACTUATOR_V1") || !strings.Contains(AgentsManagedContent, "never dispatch recursively") {
		t.Fatal("managed AGENTS is missing child-sentinel suppression")
	}
	install, err := os.ReadFile(filepath.Join("..", "INSTALL.md"))
	if err != nil {
		t.Fatal(err)
	}
	published, err := os.ReadFile(filepath.Join("..", "site", "install"))
	if err != nil {
		t.Fatal(err)
	}
	wantProgram := sourceProgram(t, AgentsManagedContent)
	for name, managed := range map[string]string{"agents": AgentsManagedContent, "skill": SkillManagedContent, "INSTALL.md": string(install), "site/install": string(published)} {
		for _, program := range javascriptBlocks(managed) {
			if program != wantProgram {
				t.Fatalf("%s source program differs from the acceptance-tested managed source", name)
			}
			for _, forbidden := range []string{"import(", "process", "require(", "node:", "ALL_TOOLS", "fetch(", "XMLHttpRequest", "Deno.", "Bun."} {
				if strings.Contains(program, forbidden) {
					t.Fatalf("%s executable JavaScript contains %q", name, forbidden)
				}
			}
			for _, required := range []string{"tools.exec_command", "title-plan --json --dispatch", "tools.codex_app__create_thread(e.child)", "THREADBEAR_TITLE_ACTUATOR_V1\\n", "c.prompt.length>6000", "Object.keys", "JSON.parse", `typeof r.output!=="string"`, `typeof r.exit_code!=="number"`, `r.exit_code!==0`, `"session_id"in r`, "text(JSON.stringify(result))"} {
				if !strings.Contains(program, required) {
					t.Fatalf("%s executable JavaScript is missing %q", name, required)
				}
			}
			if strings.Count(program, "tools.codex_app__create_thread") != 1 || strings.Contains(program, "create_thread(e)") {
				t.Fatalf("%s does not pass the child directly exactly once", name)
			}
			if strings.Count(program, "text(") != 1 || strings.Count(program, "JSON.stringify(result)") != 1 {
				t.Fatalf("%s does not explicitly emit exactly one aggregate", name)
			}
		}
	}
	for _, required := range []string{
		"**Child actuator phase (child only).**", "codex_delegation.source_thread_id", "one model pass", "exactly one `functions.exec`",
		"title-plan --json --wait", "title-plan --json --operation", "tools.codex_app__set_thread_title", "title-plan --json --report",
		"exact set equality", "no_op", "canonical_persisted", "native_succeeded_pending_canonical", "title_actuation_failed",
		"tools.codex_app__set_thread_archived", "implementation inspection", "no recovery",
	} {
		if !strings.Contains(SkillManagedContent, required) {
			t.Fatalf("managed skill is missing child contract %q", required)
		}
	}
}

func TestManagedSkillKeepsGuidedDirectBatchingContract(t *testing.T) {
	guidedStart := strings.Index(SkillManagedContent, "For a guided install")
	postTurnStart := strings.Index(SkillManagedContent, "For post-turn application")
	if guidedStart < 0 || postTurnStart <= guidedStart {
		t.Fatal("managed skill is missing guided or post-turn sections")
	}
	guided := SkillManagedContent[guidedStart:postTurnStart]
	if !strings.Contains(guided, "Use the named callable expressions directly; do not enumerate, inspect, or look up available tools or schemas inside that execution.") {
		t.Fatal("guided actuator is missing the no-discovery contract")
	}
	previous := -1
	for _, required := range []string{
		"title-plan --json --batch",
		"title-plan --json --operation",
		"`await tools.codex_app__set_thread_title({threadId: TASK_ID, title: DESIRED_TITLE})`",
		"title-plan --json --report",
	} {
		index := strings.Index(guided, required)
		if index < 0 {
			t.Fatalf("guided actuator missing %q", required)
		}
		if index <= previous {
			t.Fatalf("guided actuator puts %q out of order", required)
		}
		previous = index
	}
	for _, forbidden := range []string{
		"codex_app__create_thread", "codex_app__set_thread_archived", "title-plan --json --dispatch", "title-plan --json --wait",
		"THREADBEAR_TITLE_ACTUATOR_V1", "codex_delegation.source_thread_id", "projectless", "gpt-5.6-luna", "child actuator",
		"ALL_TOOLS", ".filter(", "list_tools", "get_tool_schema", "discover the callable", "discover the tool schema",
		"Object.keys(tools)", "Reflect.ownKeys(tools)", "tools.list", "implementation inspection",
	} {
		if strings.Contains(guided, forbidden) {
			t.Fatalf("guided actuator contains forbidden discovery, child, or archive behavior %q", forbidden)
		}
	}
}

type sourceAggregate struct {
	Allow      bool   `json:"allow"`
	Dispatched bool   `json:"dispatched"`
	Error      string `json:"error,omitempty"`
}

func sourceDecision(helper any, create func(map[string]any) (any, error)) sourceAggregate {
	unavailable := sourceAggregate{Error: "dispatch_unavailable"}
	command, ok := helper.(map[string]any)
	if !ok {
		return unavailable
	}
	encoded, outputOK := command["output"].(string)
	exitCode, exitOK := command["exit_code"].(float64)
	if !outputOK || !exitOK || exitCode != 0 {
		return unavailable
	}
	if _, running := command["session_id"]; running {
		return unavailable
	}
	var envelope map[string]any
	if encoded == "" || json.Unmarshal([]byte(encoded), &envelope) != nil {
		return unavailable
	}
	allow, allowOK := envelope["allow"].(bool)
	disposition, dispositionOK := envelope["disposition"].(string)
	version, versionOK := envelope["version"].(float64)
	if !allowOK || !dispositionOK || !versionOK || version != 1 {
		return unavailable
	}
	if !allow {
		allowed := map[string]bool{"source_missing": true, "source_invalid": true, "config_unavailable": true, "config_invalid": true, "state_unavailable": true, "state_invalid": true, "control_task": true, "rename_disabled": true, "agents_disabled": true}
		if exactMapKeys(envelope, "allow", "disposition", "version") && allowed[disposition] {
			return sourceAggregate{}
		}
		return unavailable
	}
	child, childOK := envelope["child"].(map[string]any)
	if !exactMapKeys(envelope, "allow", "child", "disposition", "version") || disposition != "dispatch" || !childOK {
		return unavailable
	}
	target, targetOK := child["target"].(map[string]any)
	prompt, promptOK := child["prompt"].(string)
	if !exactMapKeys(child, "model", "prompt", "target", "thinking") || child["model"] != "gpt-5.6-luna" || child["thinking"] != "medium" ||
		!promptOK || len(prompt) > 6000 || !isASCII(prompt) || !strings.HasPrefix(prompt, "THREADBEAR_TITLE_ACTUATOR_V1\n") || !targetOK ||
		!exactMapKeys(target, "directoryName", "type") || target["type"] != "projectless" || target["directoryName"] != "threadbear-title-actuator" {
		return unavailable
	}
	if _, err := create(child); err != nil {
		return sourceAggregate{Allow: true, Error: "dispatch_failed"}
	}
	return sourceAggregate{Allow: true, Dispatched: true}
}

func exactMapKeys(value map[string]any, keys ...string) bool {
	actual := make([]string, 0, len(value))
	for key := range value {
		actual = append(actual, key)
	}
	sort.Strings(actual)
	sort.Strings(keys)
	return strings.Join(actual, ",") == strings.Join(keys, ",")
}

func isASCII(value string) bool {
	for _, char := range value {
		if char > 127 {
			return false
		}
	}
	return true
}

func TestManagedSourceAggregatesAndFulfilledCreateShapes(t *testing.T) {
	program := sourceProgram(t, AgentsManagedContent)
	for _, marker := range []string{
		`if(!o(r)||typeof r.output!=="string"||typeof r.exit_code!=="number"||r.exit_code!==0||"session_id"in r)return f()`,
		`if(!e.allow)return k(e)==="allow,disposition,version"&&n.has(e.disposition)?{allow:false,dispatched:false}:f()`,
		`await tools.codex_app__create_thread(e.child);return{allow:true,dispatched:true}`,
		`catch{return{allow:true,dispatched:false,error:"dispatch_failed"}}`,
		`f=()=>({allow:false,dispatched:false,error:"dispatch_unavailable"})`,
		`text(JSON.stringify(result))`,
	} {
		if !strings.Contains(program, marker) {
			t.Fatalf("source program is missing aggregate decision marker %q", marker)
		}
	}
	envelope := map[string]any{
		"version":     1,
		"allow":       true,
		"disposition": "dispatch",
		"child": map[string]any{
			"model":    "gpt-5.6-luna",
			"thinking": "medium",
			"target":   map[string]any{"type": "projectless", "directoryName": "threadbear-title-actuator"},
			"prompt":   "THREADBEAR_TITLE_ACTUATOR_V1\nprivate child contract",
		},
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name    string
		helper  any
		created any
	}{
		{name: "completed helper and fulfilled string creation", helper: map[string]any{"output": string(encoded), "exit_code": float64(0)}, created: "created"},
		{name: "completed helper and fulfilled object creation", helper: map[string]any{"output": string(encoded), "exit_code": float64(0)}, created: map[string]any{"id": "child"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			got := sourceDecision(test.helper, func(child map[string]any) (any, error) {
				calls++
				if child["prompt"] != envelope["child"].(map[string]any)["prompt"] {
					t.Fatal("model did not pass the validated child envelope directly")
				}
				return test.created, nil
			})
			assertSourceAggregate(t, got, `{"allow":true,"dispatched":true}`)
			if calls != 1 {
				t.Fatalf("create calls=%d, want 1", calls)
			}
		})
	}
	t.Run("no-op does not create", func(t *testing.T) {
		calls := 0
		got := sourceDecision(map[string]any{"output": `{"version":1,"allow":false,"disposition":"control_task"}`, "exit_code": float64(0)}, func(map[string]any) (any, error) {
			calls++
			return nil, errors.New("must not be called")
		})
		assertSourceAggregate(t, got, `{"allow":false,"dispatched":false}`)
		if calls != 0 {
			t.Fatalf("create calls=%d, want 0", calls)
		}
	})
	t.Run("thrown create is stable failure", func(t *testing.T) {
		got := sourceDecision(map[string]any{"output": string(encoded), "exit_code": float64(0)}, func(map[string]any) (any, error) { return nil, errors.New("failed") })
		assertSourceAggregate(t, got, `{"allow":true,"dispatched":false,"error":"dispatch_failed"}`)
	})
	for _, test := range []struct {
		name   string
		helper any
	}{
		{name: "raw string", helper: string(encoded)},
		{name: "failed command", helper: map[string]any{"output": string(encoded), "exit_code": float64(1)}},
		{name: "running command", helper: map[string]any{"output": string(encoded), "exit_code": float64(0), "session_id": "session-1"}},
		{name: "missing output", helper: map[string]any{"exit_code": float64(0)}},
		{name: "nonnumeric exit code", helper: map[string]any{"output": string(encoded), "exit_code": "0"}},
		{name: "invalid envelope", helper: map[string]any{"output": `{"allow":true}`, "exit_code": float64(0)}},
	} {
		t.Run(test.name+" is stable unavailable", func(t *testing.T) {
			got := sourceDecision(test.helper, func(map[string]any) (any, error) { return nil, nil })
			assertSourceAggregate(t, got, `{"allow":false,"dispatched":false,"error":"dispatch_unavailable"}`)
		})
	}
}

func assertSourceAggregate(t *testing.T, got sourceAggregate, want string) {
	t.Helper()
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != want {
		t.Fatalf("aggregate=%s want=%s", encoded, want)
	}
	for _, forbidden := range []string{"prompt", "THREADBEAR_TITLE_ACTUATOR_V1", "private child contract", "child", "disposition", "version"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("aggregate exposed %q: %s", forbidden, encoded)
		}
	}
}
