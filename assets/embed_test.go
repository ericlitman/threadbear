package assets

import (
	"os"
	"path/filepath"
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

func TestManagedSourceIsExactCompactAndPhaseIsolated(t *testing.T) {
	const wantProgram = `const e=JSON.parse((await tools.exec_command({cmd:"~/.local/bin/threadbear title-plan --json --dispatch"})).output);if(e.allow)await tools.codex_app__create_thread(e.child);text(JSON.stringify({allow:e.allow,dispatched:e.allow}))`
	if len([]byte(wantProgram)) != 229 {
		t.Fatalf("source loader bytes=%d", len([]byte(wantProgram)))
	}
	if len([]byte(AgentsManagedContent)) > 3500 {
		t.Fatalf("managed AGENTS bytes=%d", len([]byte(AgentsManagedContent)))
	}
	const mandatory = "mandatory even for short answers and tasks that needed no other tools"
	if !strings.Contains(AgentsManagedContent, mandatory) || !strings.Contains(SkillManagedContent, mandatory) {
		t.Fatal("managed guidance is missing the mandatory ordinary-turn rule")
	}
	if strings.Index(AgentsManagedContent, "## Native title handoff") >= strings.Index(AgentsManagedContent, "## Status footer") {
		t.Fatal("managed AGENTS puts the title handoff after the footer guidance")
	}
	if strings.Index(SkillManagedContent, "**Source phase (source only, never actuator).**") >= strings.Index(SkillManagedContent, "When managed global guidance is enabled") {
		t.Fatal("managed skill puts the source handoff after the footer guidance")
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
	for name, managed := range map[string]string{"agents": AgentsManagedContent, "skill": SkillManagedContent, "INSTALL.md": string(install), "site/install": string(published)} {
		program := sourceProgram(t, managed)
		if program != wantProgram {
			t.Fatalf("%s source program differs from the exact 229-byte loader", name)
		}
		if strings.Count(program, "title-plan --json --dispatch") != 1 || strings.Count(program, "tools.codex_app__create_thread(e.child)") != 1 {
			t.Fatalf("%s source loader does not have exactly one dispatch and creation call", name)
		}
		if strings.ContainsAny(program, "<>&") {
			t.Fatalf("%s source loader contains a transport-sensitive character", name)
		}
		for _, forbidden := range []string{"try{", "catch{", "Object.keys", "THREADBEAR_TITLE_ACTUATOR_V1", "codex_app__set_thread_title", "codex_app__set_thread_archived", "title-plan --json --wait", "title-plan --json --actuator", "title-plan --json --report"} {
			if strings.Contains(program, forbidden) {
				t.Fatalf("%s source loader contains child or validation behavior %q", name, forbidden)
			}
		}
	}
	for _, required := range []string{
		"**Child actuator phase (child only).**", "codex_delegation.source_thread_id", "one model pass", "exactly one `functions.exec`", "compact immutable raw-V8 loader", "indirectly evaluates only",
		"title-plan --json --actuator", "title-plan --json --wait", "title-plan --json --operation", "tools.codex_app__set_thread_title", "title-plan --json --report",
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
