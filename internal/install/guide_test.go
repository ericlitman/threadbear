package install

import (
	"os"
	"os/exec"
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
		"final-review and confirmed argument-construction stanzas\nmust be byte-for-byte identical",
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
		"let them keep,\nchange, or ask about every choice",
		"promise a friendly review showing exactly\nwhat will happen before anything changes",
		"verify that everything\nis healthy before calling the work complete",
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
		"If the person already supplied a valid token position, reflect that choice directly",
	} {
		if !strings.Contains(compactGuide, want) {
			t.Fatalf("INSTALL.md missing title/token dependency rule %q", want)
		}
	}

	currentStart := strings.Index(guide, "For a reinstall, present a dedicated current-settings card once")
	currentEnd := strings.Index(guide, "Title maintenance controls the token display.")
	current := guide[currentStart:currentEnd]
	if !strings.Contains(current, "quiet-day timing is inactive") ||
		strings.Contains(current, "14 days") {
		t.Fatal("current-settings card does not render inactive archive timing honestly")
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
		"Give that reinstall discovery sentence once.",
		"When discovery identifies a first\ninstall, keep the detection backstage",
	} {
		if !strings.Contains(guide, want) {
			t.Fatalf("INSTALL.md missing mode-specific card branch %q", want)
		}
	}
	if got := strings.Count(guide, "so I’ll keep it right where it is and show you"); got != 1 {
		t.Fatalf("INSTALL.md has %d reinstall discovery disclosures, want exactly one", got)
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
	recommendationStart := strings.Index(guide, "For a first install, present the recommended setup:")
	recommendationEnd := strings.Index(guide, "For a reinstall, present a dedicated current-settings card once")
	recommendation := guide[recommendationStart:recommendationEnd]
	if !strings.Contains(recommendation, "one-line footer") ||
		!strings.Contains(recommendation, "🧵🐻 complete") {
		t.Fatal("default card does not show the concrete status-footer artifact and sample")
	}
}

func TestCodexInstallGuideFreezesResolvedPreferencesForConsent(t *testing.T) {
	guide := readInstallGuide(t)
	for _, want := range []string{
		"baseline dry-run with the task ID plus only preference changes the\nperson explicitly requested",
		"Assign each resolved text value separately as a shell-safe quoted literal",
		"never interpolate or flatten text values into one aggregate string",
		"Read every resolved preference from the baseline result.",
		"Materialize all of\nthem",
		"classifier model placeholder intentionally contains internal whitespace to\ndemonstrate safe quoting",
		"second dry-run is the final review source",
		"The final-review and confirmed argument-construction stanzas\nmust be byte-for-byte identical, and therefore semantically identical",
		"identical complete argument list prevents the person’s preference choices from\ndrifting between review and confirmation",
		"revalidates the task\nand managed resources before mutation and stops if a safety check fails",
	} {
		if !strings.Contains(guide, want) {
			t.Fatalf("INSTALL.md missing frozen preference guidance %q", want)
		}
	}

	if strings.Contains(guide, "If the world changed") {
		t.Fatal("INSTALL.md claims an unsupported cross-invocation preview check")
	}
	for _, stale := range []string{
		"DISCOVERY_FLAGS=",
		"BASELINE_FLAGS=",
		"FROZEN_FLAGS=",
		"$DISCOVERY_FLAGS",
		"$BASELINE_FLAGS",
		"$FROZEN_FLAGS",
	} {
		if strings.Contains(guide, stale) {
			t.Fatalf("INSTALL.md still uses unsafe aggregate flag expansion %q", stale)
		}
	}

	blocks := map[string]string{
		"discovery":    shellBlockAfter(t, guide, "run this initial task-ID-only dry-run"),
		"baseline":     shellBlockAfter(t, guide, "First, run a baseline dry-run"),
		"final review": shellBlockAfter(t, guide, "Read every resolved preference from the baseline result."),
		"confirmation": shellBlockAfter(t, guide, "In the same shell call as the confirmed curl"),
	}
	for name, block := range blocks {
		if !strings.Contains(block, "set --") {
			t.Fatalf("%s block does not construct POSIX positional arguments", name)
		}
		curlAt := strings.LastIndex(block, "curl -fsSL")
		if curlAt == -1 || !strings.Contains(block[curlAt:], `"$@"`) {
			t.Fatalf("%s block does not pass positional arguments with quoted \"$@\"", name)
		}
	}

	finalStanza := shellArgumentStanza(t, blocks["final review"])
	confirmedStanza := shellArgumentStanza(t, blocks["confirmation"])
	if finalStanza != confirmedStanza {
		t.Fatalf("final-review and confirmed argument stanzas differ:\n%s\n---\n%s", finalStanza, confirmedStanza)
	}

	for _, flag := range []string{
		"--control-task-id",
		"--heartbeat-seconds",
		"--auto-update=",
		"--archive=",
		"--archive-after-days",
		"--rename=",
		"--token-display",
		"--agents=",
		"--classifier-model",
		"--classifier-effort",
		"--classifier-context-budget-bytes",
	} {
		if !strings.Contains(finalStanza, flag) {
			t.Fatalf("frozen argument stanza omits %q", flag)
		}
	}

	for _, name := range []string{"final review", "confirmation"} {
		block := blocks[name]
		idAt := strings.Index(block, "CONTROL_TASK_ID=")
		modelAt := strings.Index(block, "CLASSIFIER_MODEL=")
		setAt := strings.Index(block, "set --")
		curlAt := strings.Index(block, "curl -fsSL")
		if idAt == -1 || modelAt <= idAt || setAt <= modelAt || curlAt <= setAt {
			t.Fatalf("%s does not assign text values and positional arguments before curl", name)
		}
	}
}

func TestCodexInstallGuidePreservesWhitespaceInClassifierModelArgument(t *testing.T) {
	guide := readInstallGuide(t)
	for _, tc := range []struct {
		name   string
		marker string
	}{
		{name: "final review", marker: "Read every resolved preference from the baseline result."},
		{name: "confirmation", marker: "In the same shell call as the confirmed curl"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			block := shellBlockAfter(t, guide, tc.marker)
			script := shellArgumentStanza(t, block) + `
for arg do
  printf '%s\n' "$arg"
done
`
			output, err := exec.Command("/bin/sh", "-c", script).Output()
			if err != nil {
				t.Fatalf("execute guide argument stanza: %v", err)
			}
			args := strings.Split(strings.TrimSuffix(string(output), "\n"), "\n")
			modelFlag := -1
			for i, arg := range args {
				if arg == "--classifier-model" {
					modelFlag = i
					break
				}
			}
			if modelFlag == -1 || modelFlag+1 >= len(args) {
				t.Fatalf("classifier model flag missing from argv: %#v", args)
			}
			if got, want := args[modelFlag+1], "example model with internal whitespace"; got != want {
				t.Fatalf("classifier model split or changed: got %q, want %q; argv=%#v", got, want, args)
			}
		})
	}
}

func TestCodexInstallGuideKeepsAdvancedClassifierSettingsBackstage(t *testing.T) {
	guide := readInstallGuide(t)
	for _, want := range []string{
		"Always resolve and freeze the classifier model, effort, and context budget\nbackstage",
		"never show them in a settings card, review, or warm close unless\nthe person asked about advanced settings",
		"A complete frozen argument list is a\nsafety mechanism, not a reason to expose a complete technical settings list",
		"Do not add classifier details or other backstage\nvalues merely because they are frozen",
	} {
		if !strings.Contains(guide, want) {
			t.Fatalf("INSTALL.md missing advanced-settings disclosure boundary %q", want)
		}
	}
}

func TestCodexInstallGuideWaitsForVerificationBeforeAnotherProgressMessage(t *testing.T) {
	guide := readInstallGuide(t)
	start := strings.Index(guide, "## 6. Install with one calm progress update")
	end := strings.Index(guide, "## 7. Verify and close with warmth")
	if start == -1 || end <= start {
		t.Fatal("INSTALL.md missing install-progress section")
	}
	section := guide[start:end]
	for _, want := range []string{
		"That opening progress message is the only\nconversation message until every verification step in section 7 finishes",
		"Do\nnot send an interim message saying ThreadBear is installed, complete, or\nsuccessful",
	} {
		if !strings.Contains(section, want) {
			t.Fatalf("INSTALL.md missing verification-gated progress rule %q", want)
		}
	}
}

func TestCodexInstallGuideRendersCompletionFromEnabledFeatures(t *testing.T) {
	guide := readInstallGuide(t)
	firstStart := strings.Index(guide, "For a first adoption, unreadable replacement, or exact repair")
	refreshStart := strings.Index(guide, "For a retained home, whether this task or another task")
	rulesStart := strings.Index(guide, "Render the tidy-up from enabled features")
	if firstStart == -1 || refreshStart <= firstStart || rulesStart <= refreshStart {
		t.Fatal("INSTALL.md missing completion rendering sections")
	}
	first := guide[firstStart:refreshStart]
	for _, want := range []string{
		"refreshed\n> X task titles and archived Y completed tasks",
		"Nothing needs another try.",
	} {
		if !strings.Contains(first, want) {
			t.Fatalf("first-install completion missing %q", want)
		}
	}
	refresh := guide[refreshStart:rulesStart]
	for _, want := range []string{
		"Title maintenance stayed off",
		"completed tasks stayed visible",
		"nothing needs another try",
	} {
		if !strings.Contains(refresh, want) {
			t.Fatalf("refresh completion missing disabled-feature outcome %q", want)
		}
	}
	for _, forbidden := range []string{"refreshed X task titles", "archived Y completed tasks", "left Z items to retry"} {
		if strings.Contains(refresh, forbidden) {
			t.Fatalf("refresh completion reports inactive count %q", forbidden)
		}
	}
	rulesEnd := strings.Index(guide[rulesStart:], "The final response follows")
	if rulesEnd == -1 {
		t.Fatal("INSTALL.md missing completion feature rules")
	}
	rules := guide[rulesStart : rulesStart+rulesEnd]
	for _, want := range []string{
		"When title maintenance is enabled, report the refreshed-title count",
		"When it is disabled, do not report\n  a title count",
		"When automatic archiving is enabled, report the archived-task count",
		"When it is disabled,\n  do not report an archive count",
		"When retries are zero, say “Nothing needs another try.”",
	} {
		if !strings.Contains(rules, want) {
			t.Fatalf("INSTALL.md missing conditional completion rule %q", want)
		}
	}
}

func TestCodexInstallGuideFinalFooterFollowsStatusGuidance(t *testing.T) {
	guide := readInstallGuide(t)
	for _, want := range []string{
		"When enabled,\nappend one compact ThreadBear footer such as `🧵🐻 complete`",
		"when disabled,\nomit the footer entirely",
		"If this task already loaded a higher-priority earlier\nfooter rule that conflicts with the new choice",
		"This task started with earlier reply guidance, so its footer may not change",
		"Your choice will apply in new task sessions.",
	} {
		if !strings.Contains(guide, want) {
			t.Fatalf("INSTALL.md missing selected-footer behavior %q", want)
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

func shellBlockAfter(t *testing.T, guide, marker string) string {
	t.Helper()
	markerAt := strings.Index(guide, marker)
	if markerAt == -1 {
		t.Fatalf("INSTALL.md missing shell-block marker %q", marker)
	}
	fenceAt := strings.Index(guide[markerAt:], "```sh\n")
	if fenceAt == -1 {
		t.Fatalf("INSTALL.md missing shell block after %q", marker)
	}
	blockStart := markerAt + fenceAt + len("```sh\n")
	blockEndOffset := strings.Index(guide[blockStart:], "\n```")
	if blockEndOffset == -1 {
		t.Fatalf("INSTALL.md has unterminated shell block after %q", marker)
	}
	return guide[blockStart : blockStart+blockEndOffset]
}

func shellArgumentStanza(t *testing.T, block string) string {
	t.Helper()
	curlAt := strings.LastIndex(block, "\ncurl -fsSL")
	if curlAt == -1 {
		t.Fatalf("shell block missing curl after argument stanza:\n%s", block)
	}
	return strings.TrimSpace(block[:curlAt])
}

func readInstallGuide(t *testing.T) string {
	t.Helper()
	content, err := os.ReadFile("../../INSTALL.md")
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}
