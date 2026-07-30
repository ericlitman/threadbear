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
		"persistent ThreadBear control task remains the user-facing master",
	}
	for _, text := range required {
		if !strings.Contains(SkillManagedContent, text) {
			t.Errorf("managed skill content is missing %q", text)
		}
	}
}

func TestManagedGuidanceCarriesRetainedNativeTitleHandoff(t *testing.T) {
	for name, content := range map[string]string{"agents": AgentsManagedContent, "skill": SkillManagedContent} {
		for _, required := range []string{"title-plan --json --stage", "exact actual footer", "ready=true", "heartbeat_active", "heartbeat_cycle_active", "functions.exec", "functions.wait", "no more tools or commentary"} {
			if !strings.Contains(content, required) {
				t.Fatalf("managed %s content is missing %q", name, required)
			}
		}
	}
	for _, required := range []string{"tools.exec_command", "--operation OP_ID", "tools.codex_app__set_thread_title", "exit_code === 0", "accepted, canonically verified, failed, drifted, and rejected counts"} {
		if !strings.Contains(SkillManagedContent, required) {
			t.Fatalf("managed skill content is missing %q", required)
		}
	}
	for _, forbidden := range []string{"THREADBEAR_TITLE_ACTUATOR", "codex_app__create_thread", "child actuator"} {
		if strings.Contains(SkillManagedContent, forbidden) || strings.Contains(AgentsManagedContent, forbidden) {
			t.Fatalf("managed guidance contains retired actuator surface %q", forbidden)
		}
	}
}
