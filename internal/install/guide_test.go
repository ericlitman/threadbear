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
		"one compact ThreadBear status line to agent replies",
		"status is unavailable, do not guess fresh-install\ndefaults",
		"The installer dry-run remains the authoritative\nbaseline",
		"Ready for me to install ThreadBear with these choices?",
		"Ready for me to refresh ThreadBear with these choices?",
		"ThreadBear is home 🧵🐻",
		"Your choices are saved in the welcome note above.",
		"Your current settings remain in effect.",
		"I’ll mind the threads. You go make the next thing.",
	} {
		if !strings.Contains(guide, want) {
			t.Fatalf("INSTALL.md missing conversation contract text %q", want)
		}
	}
}

func TestCodexInstallGuideUsesOneHiddenTokenLabel(t *testing.T) {
	guide := readInstallGuide(t)
	if !strings.Contains(guide, "exact labels “At the\nstart,” “At the end,” and “Hidden.”") {
		t.Fatal("INSTALL.md does not use Hidden in the structured token-display choices")
	}
	for _, stale := range []string{"Don’t show it", "Don't show it"} {
		if strings.Contains(guide, stale) {
			t.Fatalf("INSTALL.md still uses stale token-display label %q", stale)
		}
	}
}

func TestCodexInstallGuideDisclosesStatusGuidanceBeforeConsent(t *testing.T) {
	guide := readInstallGuide(t)
	if got := strings.Count(guide, "one compact ThreadBear status line"); got < 3 {
		t.Fatalf("INSTALL.md discloses the visible status line %d times, want recommendation plus both reviews", got)
	}
	for name, bounds := range map[string][2]string{
		"recommendation": {"Present the recommended setup as a short card:", "If the person wants to change the token display"},
		"first install":  {"Read the full preview yourself.", "For `retained` or `stayed_home`"},
		"refresh":        {"For `retained` or `stayed_home`", "The settings sentence is an example"},
	} {
		start := strings.Index(guide, bounds[0])
		end := strings.Index(guide, bounds[1])
		if start == -1 || end == -1 || end <= start {
			t.Fatalf("INSTALL.md missing %s status-guidance section", name)
		}
		section := strings.ReplaceAll(guide[start:end], "\n> ", " ")
		if !strings.Contains(section, "one compact ThreadBear status line") ||
			!strings.Contains(section, "agent replies") ||
			!strings.Contains(section, "Codex tasks") {
			t.Fatalf("INSTALL.md %s section does not disclose the visible cross-task reply change", name)
		}
	}
}

func TestCodexInstallGuideUsesPartialFlagsAsPreferenceBaseline(t *testing.T) {
	guide := readInstallGuide(t)
	for _, want := range []string{
		"For both a\nfirst install and a reinstall, begin with only the task ID",
		"Append only flags for preference changes the person explicitly requested",
		"On a first install,\nunspecified preferences take ThreadBear’s defaults. On a reinstall, unspecified\npreferences preserve the installed values.",
		"The dry-run result, rather than an earlier status call or an assumption,\nis the authoritative source for every setting shown to the person",
	} {
		if !strings.Contains(guide, want) {
			t.Fatalf("INSTALL.md missing partial preference baseline guidance %q", want)
		}
	}
	if strings.Contains(guide, `INSTALL_FLAGS="--control-task-id $CONTROL_TASK_ID --heartbeat-seconds`) {
		t.Fatal("INSTALL.md still contains the all-default flag fast path")
	}
	if strings.Contains(guide, "Full noninteractive defaults:") {
		t.Fatal("INSTALL.md still offers a full-default noninteractive command")
	}
}

func TestCodexInstallGuideHasWarmFailureResponse(t *testing.T) {
	guide := readInstallGuide(t)
	for _, want := range []string{
		"ThreadBear hit a snag while starting its quiet background check.",
		"The install itself finished and your settings are in place",
		"I’m checking why the background check did not start now.",
		"You don’t need to\n> restart the installation or repeat anything",
		"Nothing was\ninstalled and your settings did not change.",
	} {
		if !strings.Contains(guide, want) {
			t.Fatalf("INSTALL.md missing failure-response guidance %q", want)
		}
	}
}

func TestCodexInstallGuideUsesDistinctRefreshCompletionCopy(t *testing.T) {
	guide := readInstallGuide(t)
	refreshStart := strings.Index(guide, "For a retained home, whether this task or another task")
	refreshEnd := strings.Index(guide, "Adapt the home sentence")
	if refreshStart == -1 || refreshEnd == -1 || refreshEnd <= refreshStart {
		t.Fatal("INSTALL.md missing dedicated retained-home completion section")
	}
	refresh := guide[refreshStart:refreshEnd]
	if !strings.Contains(refresh, "because no new welcome note was posted") {
		t.Fatal("retained-home completion guidance does not explain why its copy is distinct")
	}
	if !strings.Contains(refresh, "Your current settings remain in effect.") {
		t.Fatal("retained-home completion copy does not preserve current settings")
	}
	if strings.Contains(refresh, "Your choices are saved in the welcome note above.") {
		t.Fatal("retained-home completion copy incorrectly claims a new welcome note")
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
