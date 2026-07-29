package assets

import (
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
	card := strings.Index(SkillManagedContent, "lead with a short friendly capability card")
	help := strings.Index(SkillManagedContent, "`~/.local/bin/threadbear help`")
	if card < 0 || help < 0 || card > help {
		t.Fatal("managed skill does not put the capability card before command help")
	}
}

func assertOrderedContract(t *testing.T, name, content string, markers ...string) {
	t.Helper()
	previous := -1
	for _, marker := range markers {
		index := strings.Index(content, marker)
		if index < 0 {
			t.Fatalf("%s content missing %q", name, marker)
		}
		if index <= previous {
			t.Fatalf("%s content puts %q out of order", name, marker)
		}
		previous = index
	}
}

func managedPostTurnPhases(t *testing.T, name, content string) (string, string) {
	t.Helper()
	sourceStart := strings.Index(content, "**Source phase (source only, never actuator).**")
	childStart := strings.Index(content, "**Child actuator phase (child only).**")
	if sourceStart < 0 || childStart <= sourceStart {
		t.Fatalf("%s is missing ordered source and child phases", name)
	}
	return content[sourceStart:childStart], content[childStart:]
}

func sourceDispatchProgram(t *testing.T, name, source string) string {
	t.Helper()
	start := strings.Index(source, "```js\n")
	if start < 0 {
		t.Fatalf("%s source phase is missing executable dispatch", name)
	}
	start += len("```js\n")
	end := strings.Index(source[start:], "\n```")
	if end < 0 {
		t.Fatalf("%s source phase has an unterminated dispatch", name)
	}
	return source[start : start+end]
}

func TestManagedAssetsEnforceSourceThenChildBoundary(t *testing.T) {
	guidedStart := strings.Index(SkillManagedContent, "For a guided install")
	postTurnStart := strings.Index(SkillManagedContent, "For post-turn application")
	if guidedStart < 0 || postTurnStart <= guidedStart {
		t.Fatal("managed skill is missing guided or post-turn sections")
	}
	assertOrderedContract(t, "skill guided", SkillManagedContent[guidedStart:postTurnStart],
		"title-plan --json --batch", "title-plan --json --operation", "`await tools.codex_app__set_thread_title({threadId: TASK_ID, title: DESIRED_TITLE})`", "title-plan --json --report")
	for name, content := range map[string]string{"agents": AgentsManagedContent, "skill": SkillManagedContent[postTurnStart:]} {
		source, child := managedPostTurnPhases(t, name, content)
		assertOrderedContract(t, name+" source", source,
			"one actual `functions.exec` program", "own `CODEX_THREAD_ID`", "status --json", "control_task_id",
			"CHILD_PROMPT", "await tools.codex_app__create_thread", "substantive final response immediately", "remain unarchived")
		assertOrderedContract(t, name+" child", child,
			"codex_delegation.source_thread_id", "one model pass", "exactly one `functions.exec`",
			"title-plan --json --wait", "title-plan --json --operation", "`await tools.codex_app__set_thread_title({threadId: TASK_ID, title: DESIRED_TITLE})`",
			"title-plan --json --report", `{"reports":[{"operation_id":"OPERATION_ID","task_id":"TASK_ID","native_success":true}]}`,
			"rejected_ids", "accepted_ids", "`await tools.codex_app__set_thread_archived({archived: true})`")
	}
}

func TestManagedSkillKeepsGuidedDirectBatchingContract(t *testing.T) {
	guidedStart := strings.Index(SkillManagedContent, "For a guided install")
	postTurnStart := strings.Index(SkillManagedContent, "For post-turn application")
	guided := SkillManagedContent[guidedStart:postTurnStart]
	for _, required := range []string{
		"title-plan --json --batch",
		"title-plan --json --operation",
		"`await tools.codex_app__set_thread_title({threadId: TASK_ID, title: DESIRED_TITLE})`",
		"title-plan --json --report",
		"Use the named callable expressions directly; do not enumerate, inspect, or look up available tools or schemas inside that execution.",
	} {
		if !strings.Contains(guided, required) {
			t.Fatalf("managed skill guided actuator missing %q", required)
		}
	}
	for _, forbidden := range []string{"codex_app__create_thread", "codex_app__set_thread_archived", "list_tools", "get_tool_schema", "discover the callable", "discover the tool schema"} {
		if strings.Contains(guided, forbidden) {
			t.Fatalf("managed skill guided actuator changed to forbidden behavior %q", forbidden)
		}
	}
}

func TestManagedSourcePhaseUsesExactChildDispatch(t *testing.T) {
	const childPrompt = "ThreadBear child actuator phase. Follow only the installed child-actuator contract. Obtain the source solely from your own codex_delegation.source_thread_id."
	for name, content := range map[string]string{"agents": AgentsManagedContent, "skill": SkillManagedContent} {
		source, _ := managedPostTurnPhases(t, name, content)
		for _, required := range []string{
			"const CHILD_PROMPT = \"" + childPrompt + "\"",
			"await tools.codex_app__create_thread({",
			`model: "gpt-5.6-luna"`,
			`thinking: "medium"`,
			`target: {type: "projectless", directoryName: "threadbear-title-actuator"}`,
			"prompt: CHILD_PROMPT",
			"whether it returns a JSON string or object",
			"only a thrown call is dispatch failure",
			"never reads or reuses its own incoming `codex_delegation.source_thread_id`",
			"do not wait for, read, message, retry, recover, or archive the child",
			"no transcript, task metadata, title, manifest, or source ID",
		} {
			if !strings.Contains(source, required) {
				t.Fatalf("%s source phase missing exact dispatch rule %q", name, required)
			}
		}
		program := sourceDispatchProgram(t, name, source)
		exactDispatch := `await tools.codex_app__create_thread({ model: "gpt-5.6-luna", thinking: "medium", target: {type: "projectless", directoryName: "threadbear-title-actuator"}, prompt: CHILD_PROMPT, })`
		compactProgram := strings.Join(strings.Fields(program), " ")
		if !strings.Contains(compactProgram, exactDispatch) {
			t.Fatalf("%s source dispatch does not contain the exact supported call", name)
		}
		assertOrderedContract(t, name+" source program", compactProgram, "process.env.CODEX_THREAD_ID", "run(`${home}/.local/bin/threadbear`, [\"status\", \"--json\"], {encoding: \"utf8\"})", "JSON.parse(stdout)", "status.control_task_id", exactDispatch)
		for _, forbidden := range []string{"title-plan", "codex_app__set_thread_title", "codex_app__set_thread_archived", "--report"} {
			if strings.Contains(program, forbidden) {
				t.Fatalf("%s source dispatch contains child actuator work %q", name, forbidden)
			}
		}
		for _, forbidden := range []string{"one model pass", "exactly one `functions.exec`"} {
			if strings.Contains(source, forbidden) {
				t.Fatalf("%s source phase inherits child-only ceiling %q", name, forbidden)
			}
		}
	}
}

func TestManagedChildPhaseKeepsActuatorGates(t *testing.T) {
	for name, content := range map[string]string{"agents": AgentsManagedContent, "skill": SkillManagedContent} {
		_, child := managedPostTurnPhases(t, name, content)
		for _, required := range []string{
			"Only the child", "boolean `native_success`", `error_code:"native_set_failed"`, "exact set equality",
			"skip the empty report", "no_op", "canonical_persisted", "native_succeeded_pending_canonical",
			"drifted", "missing", "title_actuation_failed", "unarchived", "no second command",
			"implementation inspection", "deterministic helper", "child archives itself, never the source",
			"Use the named callable expressions directly; do not enumerate, inspect, or look up available tools or schemas inside that execution.",
		} {
			if !strings.Contains(child, required) {
				t.Fatalf("%s child phase missing actuator contract %q", name, required)
			}
		}
		for _, forbidden := range []string{"`set_thread_title`", "`set_thread_archived`", "ALL_TOOLS", ".filter(", "list_tools", "get_tool_schema", "discover the callable", "discover the tool schema"} {
			if strings.Contains(child, forbidden) {
				t.Fatalf("%s child phase permits native-tool discovery or conceptual-only calls %q", name, forbidden)
			}
		}
	}
}

func TestManagedGuidanceRequiresToolBackedControlSuppression(t *testing.T) {
	for name, content := range map[string]string{"agents": AgentsManagedContent, "skill": SkillManagedContent} {
		for _, required := range []string{"actual `functions.exec`", "returned tool result", "never a prose claim", "CODEX_THREAD_ID", "control_task_id", "hard no-op", "no child"} {
			if !strings.Contains(content, required) {
				t.Fatalf("%s content missing control isolation rule %q", name, required)
			}
		}
	}
}
