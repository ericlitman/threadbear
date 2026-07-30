package assets

import (
	"os/exec"
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

func TestManagedGuidanceBindsSourceNativeBatchOrdering(t *testing.T) {
	for name, content := range map[string]string{"agents": AgentsManagedContent, "skill": SkillManagedContent} {
		for _, required := range []string{"title-batch --json --stage", "title-batch --json --list", "title-batch --json --operation", "codex_app__set_thread_title", "title-batch --json --report", "no further tool call or commentary"} {
			if !strings.Contains(content, required) {
				t.Fatalf("%s missing native batch surface %q", name, required)
			}
		}
		ordered := []string{"--list", "--operation", "codex_app__set_thread_title", "--report", "text(JSON.stringify"}
		position := 0
		for _, token := range ordered {
			next := strings.Index(content[position:], token)
			if next < 0 {
				t.Fatalf("%s missing ordered native batch token %q", name, token)
			}
			position += next + len(token)
		}
		for _, forbidden := range []string{"title-plan --json --batch", "THREADBEAR_TITLE_ACTUATOR", "codex_app__create_thread", "child actuator", "set_thread_archived", "process.env", "source_task_id"} {
			if strings.Contains(content, forbidden) {
				t.Fatalf("%s contains retired actuator surface %q", name, forbidden)
			}
		}
	}
}

func TestManagedTitleBatchRunsInFreshV8WithoutNodeGlobals(t *testing.T) {
	command := exec.Command("node", "../scripts/replay-title-batch.mjs")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("fresh V8 replay failed: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "fresh V8 title batch replay passed") {
		t.Fatalf("unexpected replay output: %s", output)
	}
}
