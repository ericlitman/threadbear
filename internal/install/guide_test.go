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
		"Show exactly one settings card",
		"final-review and confirmed preference lists\nmust be byte-for-byte identical",
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

func TestCodexInstallGuideHasEnforceableWelcomeOrientation(t *testing.T) {
	guide := readInstallGuide(t)
	if strings.Contains(guide, "five-step orientation") {
		t.Fatal("INSTALL.md still describes orientation with an unenforceable step count")
	}
	for _, want := range []string{
		"say what\nThreadBear will do for the person",
		"keep the setup in this task",
		"let them keep,\nchange, or ask about every choice before anything changes",
		"verify that everything is healthy before calling the work complete",
		"I’ll take care of the setup right here.",
		"ThreadBear won’t be installed and no\n> settings will change until you say go",
	} {
		if !strings.Contains(guide, want) {
			t.Fatalf("INSTALL.md welcome is missing orientation promise %q", want)
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

func TestCodexInstallGuideHonorsTitleTokenDependency(t *testing.T) {
	guide := readInstallGuide(t)
	for name, bounds := range map[string][2]string{
		"current settings": {"For a reinstall, present a dedicated current-settings card once", "Title maintenance controls the token display."},
		"refresh review":   {"For `retained` or `stayed_home`", "The settings sentence is an example"},
	} {
		start := strings.Index(guide, bounds[0])
		end := strings.Index(guide, bounds[1])
		if start == -1 || end == -1 || end <= start {
			t.Fatalf("INSTALL.md missing %s title/token section", name)
		}
		section := strings.ToLower(strings.ReplaceAll(guide[start:end], "\n> ", " "))
		for _, want := range []string{"titles", "untouched", "token figures", "inactive"} {
			if !strings.Contains(section, want) {
				t.Fatalf("INSTALL.md %s does not carry inactive title/token outcome %q", name, want)
			}
		}
	}
	compactGuide := strings.ReplaceAll(guide, "\n", " ")
	for _, want := range []string{
		"never present a stored token position as active",
		"Skip the token-position choices unless the person is considering re-enabling title maintenance",
	} {
		if !strings.Contains(compactGuide, want) {
			t.Fatalf("INSTALL.md missing title/token dependency rule %q", want)
		}
	}
}

func TestCodexInstallGuideShowsOneModeSpecificSettingsCard(t *testing.T) {
	guide := readInstallGuide(t)
	if got := strings.Count(guide, "Here’s the setup ThreadBear is using now:"); got != 1 {
		t.Fatalf("INSTALL.md has %d current-settings cards, want exactly one", got)
	}
	checkStart := strings.Index(guide, "## 2. Check compatibility quietly")
	checkEnd := strings.Index(guide, "## 3. Identify this task backstage")
	if checkStart == -1 || checkEnd <= checkStart {
		t.Fatal("INSTALL.md missing compatibility section")
	}
	compatibility := guide[checkStart:checkEnd]
	if strings.Contains(compatibility, "Here’s the setup") ||
		!strings.Contains(compatibility, "Do not present settings during\nthe compatibility check") {
		t.Fatal("compatibility section presents settings instead of recording them backstage")
	}
	for _, want := range []string{
		"When installed status was unavailable, do not guess whether this is a first\n  install or reinstall",
		"run this initial task-ID-only dry-run",
		"show exactly one appropriate card in the next section",
	} {
		if !strings.Contains(guide, want) {
			t.Fatalf("INSTALL.md missing mode-specific card branch %q", want)
		}
	}
}

func TestCodexInstallGuideDisclosesStatusGuidanceBeforeConsent(t *testing.T) {
	guide := readInstallGuide(t)
	if got := strings.Count(guide, "one compact ThreadBear status line"); got < 3 {
		t.Fatalf("INSTALL.md discloses the visible status line %d times, want recommendation plus both reviews", got)
	}
	for name, bounds := range map[string][2]string{
		"recommendation": {"For a first install, present the recommended setup:", "For a reinstall, present a dedicated current-settings card once"},
		"current setup":  {"For a reinstall, present a dedicated current-settings card once", "Title maintenance controls the token display."},
		"first install":  {"Read the full final-review result yourself.", "For `retained` or `stayed_home`"},
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

func TestCodexInstallGuideFreezesResolvedPreferencesForConsent(t *testing.T) {
	guide := readInstallGuide(t)
	for _, want := range []string{
		"baseline dry-run with the task ID plus only preference changes the\nperson explicitly requested",
		"Read every resolved preference from the baseline result.",
		"Materialize all of\nthem",
		"second dry-run is the final review source",
		"The final-review and confirmed preference lists\nmust be byte-for-byte identical, and therefore semantically identical",
		"identical complete list prevents the person’s preference choices\nfrom drifting between review and confirmation",
		"revalidates the\ntask and managed resources before mutation and stops if a safety check fails",
	} {
		if !strings.Contains(guide, want) {
			t.Fatalf("INSTALL.md missing frozen preference guidance %q", want)
		}
	}

	if !strings.Contains(guide, `BASELINE_FLAGS="--control-task-id $CONTROL_TASK_ID"`) {
		t.Fatal("INSTALL.md baseline dry-run is not task-ID-only before explicit changes")
	}
	if strings.Contains(guide, "If the world changed") {
		t.Fatal("INSTALL.md claims an unsupported cross-invocation preview check")
	}

	var frozenLines []string
	for _, line := range strings.Split(guide, "\n") {
		if strings.HasPrefix(line, "FROZEN_FLAGS=") {
			frozenLines = append(frozenLines, line)
		}
	}
	if len(frozenLines) != 2 {
		t.Fatalf("INSTALL.md has %d frozen flag examples, want final review and confirmed run", len(frozenLines))
	}
	if frozenLines[0] != frozenLines[1] {
		t.Fatalf("final-review and confirmed frozen flags differ:\n%s\n%s", frozenLines[0], frozenLines[1])
	}
	for _, flag := range []string{
		"--control-task-id",
		"--heartbeat-seconds",
		"--auto-update=",
		"--archive=",
		"--archive-after-days",
		"--rename=",
		"--token-display=",
		"--agents=",
		"--classifier-model",
		"--classifier-effort",
		"--classifier-context-budget-bytes",
	} {
		if !strings.Contains(frozenLines[0], flag) {
			t.Fatalf("frozen flag list omits %q", flag)
		}
	}

	for name, bounds := range map[string][2]string{
		"final review":  {"Read every resolved preference from the baseline result.", "This second dry-run is the final review source."},
		"confirmed run": {"## 6. Install with one calm progress update", "The `CONTROL_TASK_ID` value and `FROZEN_FLAGS` text"},
	} {
		start := strings.Index(guide, bounds[0])
		end := strings.Index(guide, bounds[1])
		if start == -1 || end == -1 || end <= start {
			t.Fatalf("INSTALL.md missing %s command block", name)
		}
		block := guide[start:end]
		idAt := strings.Index(block, "CONTROL_TASK_ID=")
		flagsAt := strings.Index(block, "FROZEN_FLAGS=")
		curlAt := strings.Index(block, "curl -fsSL")
		if idAt == -1 || flagsAt <= idAt || curlAt <= flagsAt {
			t.Fatalf("INSTALL.md %s does not assign task ID and frozen flags before curl", name)
		}
	}
}

func TestCodexInstallGuideHasDistinctWarmFailureResponses(t *testing.T) {
	guide := readInstallGuide(t)
	preStart := strings.Index(guide, "For a failure before mutation")
	postStart := strings.Index(guide, "For a failure after installation has started")
	postEnd := strings.Index(guide, "Never combine the pre-mutation headline")
	if preStart == -1 || postStart <= preStart || postEnd <= postStart {
		t.Fatal("INSTALL.md missing split failure-response sections")
	}
	for name, section := range map[string]string{
		"pre-mutation":  guide[preStart:postStart],
		"post-mutation": guide[postStart:postEnd],
	} {
		for _, want := range map[string][]string{
			"pre-mutation": {
				"ThreadBear paused before installing",
				"Nothing was installed and your settings did not change.",
				"I’m checking the connection",
				"You don’t need to",
				"restart or repeat anything",
			},
			"post-mutation": {
				"ThreadBear hit a snag while starting its quiet background check.",
				"The install itself finished and your settings are in place",
				"I’m checking why the background check did not start now.",
				"You don’t need to",
				"restart the installation or repeat anything",
			},
		}[name] {
			if !strings.Contains(strings.ReplaceAll(section, "\n> ", " "), want) {
				t.Fatalf("INSTALL.md %s response missing %q", name, want)
			}
		}
	}
}

func TestCodexInstallGuideHandlesNotNowWarmly(t *testing.T) {
	guide := readInstallGuide(t)
	for _, want := range []string{
		"If the person says “not now” or otherwise declines",
		"Nothing from this review has been installed or changed",
		"ThreadBear will be here whenever",
		"Do not run the confirmed command",
		"or ask them to\nreconsider",
	} {
		if !strings.Contains(guide, want) {
			t.Fatalf("INSTALL.md missing warm decline guidance %q", want)
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
