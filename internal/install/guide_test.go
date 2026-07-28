package install

import (
	"os"
	"strings"
	"testing"
)

func TestCodexInstallGuideCarriesConversationContract(t *testing.T) {
	guide := readInstallGuide(t)
	for _, want := range []string{
		"### Conversation contract",
		"## 1. Welcome the person",
		"Welcome to ThreadBear 🧵🐻",
		"show you exactly what will happen",
		"Would you like the recommended setup, change a choice, or have me explain any",
		"At the start (recommended)",
		"At the end",
		"Hidden",
		"reset a reinstall to fresh-install",
		"Ready for me to install ThreadBear with these choices?",
		"Ready for me to refresh ThreadBear with these choices?",
		"ThreadBear is home 🧵🐻",
		"I’ll mind the threads. You go make the next thing.",
	} {
		if !strings.Contains(guide, want) {
			t.Fatalf("INSTALL.md missing conversation contract text %q", want)
		}
	}
}

func TestCodexInstallGuideKeepsInternalsOutOfExampleDialogue(t *testing.T) {
	guide := readInstallGuide(t)
	var dialogue strings.Builder
	for _, line := range strings.Split(guide, "\n") {
		if strings.HasPrefix(line, ">") {
			dialogue.WriteString(line)
			dialogue.WriteByte('\n')
		}
	}
	for _, leak := range []string{
		"--control-task-id",
		"CONTROL_TASK_ID",
		"App Server",
		"LaunchAgent",
		"AGENTS.md",
		"PreviewResult",
		"stayed_home",
		"zero-mutation",
		"explicit approval",
		"Apply exactly this preview",
		"byte budget",
		"classifier",
		"published release",
		"Adapt that list",
	} {
		if strings.Contains(dialogue.String(), leak) {
			t.Fatalf("example dialogue leaks %q:\n%s", leak, dialogue.String())
		}
	}
}

func readInstallGuide(t *testing.T) string {
	t.Helper()
	content, err := os.ReadFile("../../INSTALL.md")
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}
