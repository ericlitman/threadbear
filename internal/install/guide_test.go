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
		"every newly started Codex task session",
		"one-line ThreadBear footer",
		"Show exactly one settings card",
		"final-review and confirmed argument-construction stanzas\nmust be byte-for-byte identical",
		"Ready for me to install ThreadBear with these choices?",
		"Ready for me to refresh ThreadBear with these choices?",
		"ThreadBear is installed",
		"ThreadBear is refreshed",
		"Your choices are saved in the welcome note above.",
		"Your current settings remain in effect.",
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
		"Finish that same opening assistant turn with these two promises",
		"the readiness result and one appropriate settings\ncard MUST begin a new assistant message",
		"It is forbidden to put a readiness\nresult or settings card in the same assistant message as the opening\ncompatibility promise",
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
		"installer\ncannot move a healthy readable home during a refresh",
		"requires a separate uninstall and new adoption, not a refresh\nchoice",
		"Do not offer or begin that destructive rehome path",
	} {
		if !strings.Contains(guide, want) {
			t.Fatalf("INSTALL.md missing mode-specific card branch %q", want)
		}
	}
	if got := strings.Count(guide, "this refresh will keep it there"); got != 1 {
		t.Fatalf("INSTALL.md has %d reinstall discovery disclosures, want exactly one", got)
	}
	if strings.Contains(normalizeGuideText(guide), "home works if you’d like. Here’s the setup it’s using now.") {
		t.Fatal("reinstall disclosure repeats the current-settings heading")
	}
}

func TestCodexInstallGuideKeepsAutoUpdateCopyConsistent(t *testing.T) {
	guide := readInstallGuide(t)
	enabled := "ThreadBear updates itself automatically, safely verifying every download before installation."
	disabled := "ThreadBear waits for you to start verified updates; updates happen only when you choose."

	firstCardStart := strings.Index(guide, "For a first install, present the recommended setup:")
	currentCardStart := strings.Index(guide, "For a reinstall, present a dedicated current-settings card once")
	currentCardEnd := strings.Index(guide, "Give that reinstall discovery sentence once.")
	firstReviewStart := strings.Index(guide, "Read the full final-review result yourself.")
	refreshReviewStart := strings.Index(guide, "For `retained` or `stayed_home`")
	refreshReviewEnd := strings.Index(guide, "The settings sentence is an example")
	if firstCardStart == -1 || currentCardStart <= firstCardStart || currentCardEnd <= currentCardStart ||
		firstReviewStart == -1 || refreshReviewStart <= firstReviewStart || refreshReviewEnd <= refreshReviewStart {
		t.Fatal("INSTALL.md missing auto-update card or review sections")
	}

	for name, section := range map[string]string{
		"recommended card": guide[firstCardStart:currentCardStart],
		"first review":     guide[firstReviewStart:refreshReviewStart],
	} {
		if !strings.Contains(normalizeGuideText(section), enabled) {
			t.Fatalf("INSTALL.md %s omits enabled automatic-update semantics", name)
		}
	}
	for name, section := range map[string]string{
		"current-settings card": guide[currentCardStart:currentCardEnd],
		"refresh review":        guide[refreshReviewStart:refreshReviewEnd],
	} {
		normalized := normalizeGuideText(section)
		if !strings.Contains(normalized, disabled) {
			t.Fatalf("INSTALL.md %s omits disabled automatic-update semantics", name)
		}
		for _, contradiction := range []string{enabled, "verified automatic updates"} {
			if strings.Contains(strings.ToLower(normalized), strings.ToLower(contradiction)) {
				t.Fatalf("INSTALL.md %s describes disabled updates as automatic: %q", name, contradiction)
			}
		}
	}

	for _, want := range []string{
		"Automatic-update copy must follow the frozen `auto_update` value in every settings card and review.",
		"When `auto_update=true`, say: “" + enabled + "”",
		"When `auto_update=false`, say: “" + disabled + "”",
		"Never describe disabled updates as automatic.",
	} {
		if !strings.Contains(normalizeGuideText(guide), want) {
			t.Fatalf("INSTALL.md missing auto-update rendering rule %q", want)
		}
	}
}

func TestCodexInstallGuideDisclosesStatusGuidanceBeforeConsent(t *testing.T) {
	guide := readInstallGuide(t)
	normalized := normalizeGuideText(guide)
	enabled := "**Status guidance on.** In every newly started Codex task session, agent replies get a one-line ThreadBear footer such as `🧵🐻 complete`. Tasks already open keep their current reply guidance. This lets ThreadBear use lightweight checks, with a careful second look when a task is unclear."
	disabled := "**Status guidance off.** In every newly started Codex task session, agent replies stay unchanged. Tasks already open keep their current reply guidance. When ThreadBear needs to understand a task, it takes a careful full look instead."
	if got := strings.Count(normalized, enabled); got < 5 {
		t.Fatalf("INSTALL.md contains enabled immutable status copy %d times, want definition, both cards, and both reviews", got)
	}
	if got := strings.Count(normalized, disabled); got < 2 {
		t.Fatalf("INSTALL.md contains disabled immutable status copy %d times, want definition and review guard", got)
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
		section := normalizeGuideText(guide[start:end])
		if !strings.Contains(section, enabled) {
			t.Fatalf("INSTALL.md %s section does not preserve immutable enabled status copy", name)
		}
	}
	for _, want := range []string{
		"its language is\nimmutable",
		"Use the selected variant below verbatim in the settings card, the\nshort choice echo when one is required below, and the final review",
		"Do not\nparaphrase, shorten, split, or omit any sentence",
		"Only when the person changes status guidance, send a short choice echo",
		"Start with one warm consumer sentence acknowledging all\nrequested changes",
		"The changed-status echo must never be a bare compliance paragraph",
		"A question or explanation request alone does not trigger this echo",
		"then accepts the recommendation, do\nnot add an extra echo",
	} {
		if !strings.Contains(normalized, normalizeGuideText(want)) {
			t.Fatalf("INSTALL.md missing immutable status-guidance contract %q", want)
		}
	}
	for _, want := range []string{
		"Never claim or imply that status guidance, its footer, or lightweight\nclassification uses zero or no model tokens",
		"That property belongs only to an\nunchanged heartbeat",
		"In every newly started Codex task session, the footer is one compact line at the end of agent replies—for example, `🧵🐻 complete`. Tasks already open keep their current reply guidance. The footer lets ThreadBear use lightweight checks most of the time; when a task is unclear, ThreadBear takes a careful second look.",
	} {
		if !strings.Contains(normalized, normalizeGuideText(want)) {
			t.Fatalf("INSTALL.md missing truthful status explanation %q", want)
		}
	}
	explanationStart := strings.Index(guide, "> In every newly started Codex task session")
	explanationEnd := strings.Index(guide, "For a first install, present the recommended setup:")
	if explanationStart == -1 || explanationEnd <= explanationStart {
		t.Fatal("INSTALL.md missing friendly status explanation block")
	}
	if strings.Contains(normalizeGuideText(guide[explanationStart:explanationEnd]), "model token") {
		t.Fatal("friendly status explanation falsely associates the footer with model-token usage")
	}
	lines := strings.Split(guide, "\n")
	for index, line := range lines {
		if !strings.Contains(line, "uses no model tokens") {
			continue
		}
		contextStart := index
		if contextStart > 0 {
			contextStart--
		}
		context := normalizeGuideText(strings.Join(lines[contextStart:index+1], "\n"))
		if !strings.Contains(context, "Unchanged heartbeats") &&
			!strings.Contains(context, "when nothing changed") {
			t.Fatalf("zero-token claim is associated with something other than an unchanged heartbeat: %q", context)
		}
	}
}

func TestCodexInstallGuideRendersArchiveStateConditionally(t *testing.T) {
	guide := readInstallGuide(t)
	normalized := normalizeGuideText(guide)
	if strings.Contains(normalized, "It won’t ask for administrator access or Full Disk Access, archive unfinished tasks") {
		t.Fatal("general review safety list implies archiving is active")
	}
	for _, want := range []string{
		"Only completed tasks that have been quiet for 14 days will be archived; unfinished tasks stay visible.",
		"Completed tasks stay visible, and quiet-day timing is inactive.",
		"Never put an archive claim in the general “It won’t…” list.",
	} {
		if !strings.Contains(normalized, want) {
			t.Fatalf("INSTALL.md missing conditional archive review copy %q", want)
		}
	}
	firstStart := strings.Index(guide, "Read the full final-review result yourself.")
	refreshStart := strings.Index(guide, "For `retained` or `stayed_home`")
	refreshEnd := strings.Index(guide, "The settings sentence is an example")
	if firstStart == -1 || refreshStart <= firstStart || refreshEnd <= refreshStart {
		t.Fatal("INSTALL.md missing archive review examples")
	}
	if !strings.Contains(
		normalizeGuideText(guide[firstStart:refreshStart]),
		"Only completed tasks that have been quiet for 14 days will be archived; unfinished tasks stay visible.",
	) {
		t.Fatal("first-install review omits enabled archive boundary")
	}
	if !strings.Contains(
		normalizeGuideText(guide[refreshStart:refreshEnd]),
		"Completed tasks stay visible, and quiet-day timing is inactive.",
	) {
		t.Fatal("refresh review omits disabled archive tradeoff")
	}
}

func TestCodexInstallGuideAcknowledgesChangedChoicesWarmly(t *testing.T) {
	guide := readInstallGuide(t)
	normalized := normalizeGuideText(guide)
	echo := "Updated — completed tasks will stay visible, output-token figures will be hidden, and agent replies will stay unchanged. **Status guidance off.** In every newly started Codex task session, agent replies stay unchanged. Tasks already open keep their current reply guidance. When ThreadBear needs to understand a task, it takes a careful full look instead."
	if !strings.Contains(normalized, echo) {
		t.Fatal("changed-status echo does not pair an all-change warm lead-in with the immutable off tradeoff")
	}
	for _, want := range []string{
		"Only after the person has seen a full review and then changes a choice, open the re-review with one short, consumer-facing delta sentence naming those changed outcomes",
		"Review updated — completed tasks will stay visible, output-token figures will be hidden, and agent replies will stay unchanged.",
		"Then immediately show the full authoritative review below.",
		"Do not expose raw flags",
		"This re-review delta is required even when an earlier changed-status echo already acknowledged the new choices.",
		"When the person changes choices from the recommendation before seeing their first review, do not prepend “Review updated” to that first review.",
		"If the change included status guidance, the warm all-changes echo above is sufficient",
		"Then begin the first full review with “Everything is ready for your review.”",
	} {
		if !strings.Contains(normalized, want) {
			t.Fatalf("INSTALL.md missing first-review/re-review distinction %q", want)
		}
	}
}

func TestCodexInstallGuideKeepsCloseSpare(t *testing.T) {
	guide := readInstallGuide(t)
	for _, want := range []string{
		"A flourish is a decorative emoji, mascot aside, or\n  bear/thread pun or metaphor",
		"required functional status footer is not decorative",
		"Literal product-output\n  examples such as a title sample or footer sample are functional artifacts",
		"Keep completion messages spare",
	} {
		if !strings.Contains(guide, want) {
			t.Fatalf("INSTALL.md missing flourish contract %q", want)
		}
	}
	if strings.Contains(guide, "I’ll mind the threads") {
		t.Fatal("completion examples still stack a thread flourish with the status footer")
	}

	firstStart := strings.Index(guide, "For a first adoption, unreadable replacement, or exact repair")
	refreshStart := strings.Index(guide, "For a retained home, whether this task or another task")
	refreshEnd := strings.Index(guide, "The example above shows `retained`.")
	if firstStart == -1 || refreshStart <= firstStart || refreshEnd <= refreshStart {
		t.Fatal("INSTALL.md missing completion examples")
	}
	for name, close := range map[string]string{
		"first install": guide[firstStart:refreshStart],
		"refresh":       guide[refreshStart:refreshEnd],
	} {
		if got := strings.Count(close, "🧵🐻"); got != 0 {
			t.Fatalf("%s reusable close has %d baked-in status marks, want none", name, got)
		}
	}
	if !strings.Contains(guide, "The quoted completion templates intentionally omit the footer.") {
		t.Fatal("INSTALL.md does not keep the conditional footer outside reusable completion copy")
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
		"choose exactly one of the following complete variants from the frozen archive setting",
		"ThreadBear updated X task titles, archived Y completed tasks, and nothing needs another try.",
		"Completed tasks stayed visible while ThreadBear updated X task titles in the first tidy-up, and nothing needs another try.",
		"Do not adapt the archive-enabled variant when `archive=false`; use the full archive-disabled variant instead.",
	} {
		if !strings.Contains(normalizeGuideText(first), want) {
			t.Fatalf("first-install completion missing %q", want)
		}
	}
	disabledStart := strings.Index(first, "When `archive=false`, use:")
	if disabledStart == -1 {
		t.Fatal("first-install completion is missing the archive-disabled complete variant")
	}
	disabledOutcome := "Completed tasks stayed visible while ThreadBear updated X task titles in the first tidy-up, and nothing needs another try."
	if !strings.Contains(normalizeGuideText(first[disabledStart:]), disabledOutcome) {
		t.Fatal("archive-disabled first-install close does not preserve the cohesive disabled outcome")
	}
	resultStart := strings.Index(first[disabledStart:], "> Everything passed:")
	actionsStart := strings.Index(first[disabledStart:], "> Your choices are saved")
	if resultStart == -1 || actionsStart <= resultStart {
		t.Fatal("archive-disabled first-install close is missing its result paragraph")
	}
	disabledResult := strings.ToLower(normalizeGuideText(first[disabledStart+resultStart : disabledStart+actionsStart]))
	for _, forbidden := range []string{"ready for the archive", "archived y", "archive count"} {
		if strings.Contains(disabledResult, forbidden) {
			t.Fatalf("archive-disabled first-install result mentions an enabled archive outcome %q", forbidden)
		}
	}
	if strings.Contains(first, "Title maintenance is on") {
		t.Fatal("first-install completion redundantly restates enabled title maintenance")
	}
	refresh := normalizeGuideText(guide[refreshStart:rulesStart])
	for _, want := range []string{
		"Title maintenance stayed off",
		"completed tasks stayed visible",
		"nothing needs another try",
	} {
		if !strings.Contains(refresh, want) {
			t.Fatalf("refresh completion missing disabled-feature outcome %q", want)
		}
	}
	for _, forbidden := range []string{
		"refreshed X task titles",
		"archived Y completed tasks",
		"left Z items to retry",
		"ready for the archive",
	} {
		if strings.Contains(refresh, forbidden) {
			t.Fatalf("refresh completion reports inactive count %q", forbidden)
		}
	}
	rulesEnd := strings.Index(guide[rulesStart:], "The final response follows")
	if rulesEnd == -1 {
		t.Fatal("INSTALL.md missing completion feature rules")
	}
	rules := normalizeGuideText(guide[rulesStart : rulesStart+rulesEnd])
	for _, want := range []string{
		"When title maintenance is enabled, say how many task titles ThreadBear updated",
		"When it is disabled, do not report a title count",
		"When automatic archiving is enabled, report the archived-task count",
		"When it is disabled, do not report an archive count",
		"When retries are zero, say “Nothing needs another try.”",
		"no completed tasks were ready for the archive",
	} {
		if !strings.Contains(rules, want) {
			t.Fatalf("INSTALL.md missing conditional completion rule %q", want)
		}
	}
	for _, want := range []string{
		"The sentence “No completed tasks were ready for the archive.” is legal only when the frozen review has `archive=true`.",
		"When the frozen review has `archive=false`, the close MUST say “Completed tasks stayed visible” and MUST NOT mention archive readiness, an archive count, or any task being archived.",
		"Never combine the enabled zero-count sentence with the disabled close.",
	} {
		if !strings.Contains(normalizeGuideText(guide), want) {
			t.Fatalf("INSTALL.md missing archive forbidden-pair rule %q", want)
		}
	}
}

func TestCodexInstallGuideAdaptsCloseActionsToInstalledPreferences(t *testing.T) {
	guide := readInstallGuide(t)
	firstStart := strings.Index(guide, "For a first adoption, unreadable replacement, or exact repair")
	refreshStart := strings.Index(guide, "For a retained home, whether this task or another task")
	refreshEnd := strings.Index(guide, "The example above shows `retained`.")
	if firstStart == -1 || refreshStart <= firstStart || refreshEnd <= refreshStart {
		t.Fatal("INSTALL.md missing adaptive close examples")
	}
	first := normalizeGuideText(guide[firstStart:refreshStart])
	refresh := normalizeGuideText(guide[refreshStart:refreshEnd])
	for _, want := range []string{"“stop archiving,”", "“put token counts at the end,”"} {
		if !strings.Contains(first, want) {
			t.Fatalf("default-enabled close missing state-changing action %q", want)
		}
	}
	for _, want := range []string{
		"“archive completed tasks after two weeks,”",
		"“turn title updates on and show token counts at the start,”",
	} {
		if !strings.Contains(refresh, want) {
			t.Fatalf("disabled-preference close missing state-changing action %q", want)
		}
	}
	for _, forbidden := range []string{"“stop archiving,”", "“hide token counts,”"} {
		if strings.Contains(refresh, forbidden) {
			t.Fatalf("disabled-preference close suggests no-op action %q", forbidden)
		}
	}

	rulesStart := strings.Index(guide, "Choose no more than two closeout action examples from the actual installed")
	rulesEnd := strings.Index(guide, "Keep closeout results flowing:")
	if rulesStart == -1 || rulesEnd <= rulesStart {
		t.Fatal("INSTALL.md missing adaptive close-action rules")
	}
	rules := normalizeGuideText(guide[rulesStart:rulesEnd])
	for _, want := range []string{
		"Choose no more than two closeout action examples from the actual installed preferences, then include the three always-safe examples.",
		"when archiving is enabled, offer “stop archiving”; when disabled, offer “archive completed tasks after two weeks”",
		"when title maintenance is disabled and token figures are inactive, offer “turn title updates on and show token counts at the start”",
		"when title maintenance is enabled and token figures are at the start, offer “put token counts at the end”; when they are at the end, offer “hide token counts”; when they are hidden, offer “put token counts at the start”",
		"a status-guidance example is optional and should normally be omitted from the close",
		"the choice applies to newly started task sessions",
		"never promise an already-running session will change immediately",
		"“pause,” “how are you?”, and “uninstall ThreadBear” are always-safe examples.",
		"Never suggest an action that is already true, inactive because of another setting, or otherwise a no-op or contradiction.",
		"Every quoted successful-close template uses exactly two preference-specific examples followed by “pause,” “how are you?”, and “uninstall ThreadBear.”",
	} {
		if !strings.Contains(rules, want) {
			t.Fatalf("INSTALL.md missing adaptive close rule %q", want)
		}
	}
	enabledStart := strings.Index(guide, "When `archive=true`, use:")
	disabledStart := strings.Index(guide, "When `archive=false`, use:")
	if enabledStart == -1 || disabledStart <= enabledStart || refreshStart <= disabledStart {
		t.Fatal("INSTALL.md missing complete first-install close variants")
	}
	for name, close := range map[string]string{
		"archive-enabled first install":  guide[enabledStart:disabledStart],
		"archive-disabled first install": guide[disabledStart:refreshStart],
		"refresh":                        guide[refreshStart:refreshEnd],
	} {
		if got := strings.Count(close, "“"); got != 5 {
			t.Fatalf("%s close has %d quoted actions, want two preference actions and three safe actions", name, got)
		}
		for _, safe := range []string{"“pause,”", "“how are you?”", "“uninstall ThreadBear.”"} {
			if !strings.Contains(normalizeGuideText(close), safe) {
				t.Fatalf("%s close is missing always-safe action %q", name, safe)
			}
		}
	}
	if !strings.Contains(refresh, "Everything passed. ThreadBear VERSION is refreshed, and its quiet background check is healthy.") {
		t.Fatal("refresh close does not use the required version-and-health sentence")
	}
	if strings.Contains(refresh, "ThreadBear VERSION is refreshed, its quiet background check") {
		t.Fatal("refresh close retains the comma splice")
	}
	for _, want := range []string{
		"combine version and health in one crafted sentence",
		"use exactly one home sentence",
		"combine tidy-up outcomes in one cohesive sentence",
		"Do not emit a sequence of clipped status fragments.",
	} {
		if !strings.Contains(normalizeGuideText(guide), want) {
			t.Fatalf("INSTALL.md missing flowing close rule %q", want)
		}
	}
}

func TestCodexInstallGuideFinalFooterFollowsStatusGuidance(t *testing.T) {
	guide := readInstallGuide(t)
	normalized := normalizeGuideText(guide)
	for _, want := range []string{
		"add a blank line after the completion\nprose and finish with exactly this standalone final line:\n\n> 🧵🐻 complete",
		"The footer must be its own final line.",
		"Never append it to a sentence, place it\ninline after an example, or put it in the same paragraph as completion prose.",
		"When status guidance is disabled, omit the footer entirely.",
		"If this task already loaded a higher-priority earlier\nfooter rule that conflicts with the new choice",
		"This task started with earlier reply guidance, so its footer may not change",
		"Your choice will apply in new task sessions.",
	} {
		if !strings.Contains(normalized, normalizeGuideText(want)) {
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
		"your\n> Mac and Codex are exactly as they were",
		"ThreadBear will be here whenever",
		"Do not run the confirmed command",
		"or ask them to\nreconsider",
	} {
		if !strings.Contains(guide, want) {
			t.Fatalf("INSTALL.md missing warm decline guidance %q", want)
		}
	}
	if strings.Contains(guide, "your\n> current setup is still exactly as it was") {
		t.Fatal("first-install decline incorrectly assumes a current ThreadBear setup")
	}
}

func TestCodexInstallGuideUsesDistinctRefreshCompletionCopy(t *testing.T) {
	guide := readInstallGuide(t)
	refreshStart := strings.Index(guide, "For a retained home, whether this task or another task")
	refreshEnd := strings.Index(guide, "The example above shows `retained`.")
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
	if strings.Contains(refresh, "ThreadBear is home") ||
		!strings.Contains(refresh, "ThreadBear is refreshed") {
		t.Fatal("retained-home completion uses a truth-unsafe home headline")
	}
	for _, want := range []string{
		"For `retained`, keep exactly the one sentence\n“ThreadBear remains based in this task.”",
		"For `stayed_home`, replace that entire\nsentence—do not append to it or to a generic home clause—with exactly\n“ThreadBear remains based in its existing home in another task; this task was\nnot renamed or pinned.”",
		"Never headline either refresh branch “ThreadBear is\nhome.”",
	} {
		if !strings.Contains(guide[refreshEnd:], want) {
			t.Fatalf("INSTALL.md missing exact retained-home branch copy %q", want)
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

func normalizeGuideText(text string) string {
	lines := strings.Split(text, "\n")
	for index, line := range lines {
		line = strings.TrimSpace(line)
		line = strings.TrimPrefix(line, ">")
		line = strings.TrimSpace(line)
		line = strings.TrimPrefix(line, "- ")
		lines[index] = strings.TrimSpace(line)
	}
	return strings.Join(strings.Fields(strings.Join(lines, " ")), " ")
}

func readInstallGuide(t *testing.T) string {
	t.Helper()
	content, err := os.ReadFile("../../INSTALL.md")
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}
