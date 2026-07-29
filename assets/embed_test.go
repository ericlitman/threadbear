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

func TestManagedAssetsEnforceHelperRevalidationOrdering(t *testing.T) {
	assertOrderedContract(t, "agents", AgentsManagedContent,
		"CODEX_THREAD_ID", "status --json", "gpt-5.6-luna", "codex_delegation.source_thread_id",
		"title-plan --json --wait", "title-plan --json --operation", "native task-title mutation",
		"title-plan --json --report", "self-archive")
	guidedStart := strings.Index(SkillManagedContent, "For a guided install")
	fallbackStart := strings.Index(SkillManagedContent, "For post-turn application")
	if guidedStart < 0 || fallbackStart <= guidedStart {
		t.Fatal("managed skill is missing guided or fallback actuator sections")
	}
	assertOrderedContract(t, "skill guided", SkillManagedContent[guidedStart:fallbackStart],
		"title-plan --json --batch", "title-plan --json --operation", "native mutation", "title-plan --json --report")
	assertOrderedContract(t, "skill fallback", SkillManagedContent[fallbackStart:],
		"CODEX_THREAD_ID", "control_task_id", "gpt-5.6-luna", "codex_delegation.source_thread_id",
		"title-plan --json --wait", "title-plan --json --operation", "exact title", "aggregate outcomes", "self-archives")
}

func TestManagedGuidanceSuppressesControlTaskDispatch(t *testing.T) {
	for name, content := range map[string]string{"agents": AgentsManagedContent, "skill": SkillManagedContent} {
		for _, required := range []string{"CODEX_THREAD_ID", "control_task_id", "hard no-op", "no worker"} {
			if !strings.Contains(content, required) {
				t.Fatalf("%s content missing control isolation rule %q", name, required)
			}
		}
	}
}
