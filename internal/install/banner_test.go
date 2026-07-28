package install

import (
	"strings"
	"testing"
)

func TestWelcomeBannerFitsEightyColumns(t *testing.T) {
	banner := WelcomeBanner()
	lines := strings.Split(strings.TrimRight(banner, "\n"), "\n")
	if len(lines) < len(welcomeText) {
		t.Fatalf("banner has %d lines", len(lines))
	}
	joined := strings.Join(lines, "\n")
	for _, text := range welcomeText {
		if !strings.Contains(joined, text) {
			t.Fatalf("banner missing text line %q", text)
		}
		if strings.Contains(text, "—") {
			t.Fatalf("em dash in welcome text %q", text)
		}
	}
	for index, line := range lines {
		if width := visibleWidth(line); width > 80 {
			t.Fatalf("line %d visible width %d exceeds 80", index, width)
		}
	}
}

func TestWelcomeNoticeCarriesSettingsAndChatInstructions(t *testing.T) {
	preferences := DefaultPreferences()
	notice := welcomeNotice("1.2.3", preferences)
	for _, want := range []string{
		"ThreadBear 1.2.3 is home",
		"To keep your Codex tasks tidy, I will:",
		"check quietly every 5 minutes",
		"install verified updates automatically",
		"tuck completed tasks away after 14 quiet days",
		"keep status and next actions easy to spot in task titles",
		"show output tokens at the start, like 🚨 1.6m Fix checkout",
		"add a one-line status hint to agent replies so most checks stay lightweight",
		"use local task evidence first, then ask Codex for a careful second look only when a task is unclear",
		`Say "check every ten minutes"`,
		"I will mind the threads",
	} {
		if !strings.Contains(notice, want) {
			t.Fatalf("welcome notice missing %q\n%s", want, notice)
		}
	}
	for _, leak := range []string{
		"300 seconds",
		"AGENTS.md",
		"byte budget",
		"threadbear configure",
		"managed",
		"gpt-5.6-luna",
		"250 KB",
		"recommended settings",
		"helpful task titles: on",
		"conversation size in titles:",
	} {
		if strings.Contains(notice, leak) {
			t.Fatalf("welcome notice leaks %q\n%s", leak, notice)
		}
	}
	if strings.Contains(notice, "—") {
		t.Fatal("em dash in welcome notice")
	}
}

func TestWelcomeNoticeHumanizesCustomSettings(t *testing.T) {
	preferences := DefaultPreferences()
	preferences.HeartbeatSeconds = 3600
	preferences.AutoUpdateEnabled = false
	preferences.ArchiveEnabled = false
	preferences.RenameEnabled = false
	preferences.TokenDisplay = "end"
	preferences.AgentsEnabled = false
	preferences.ClassifierModel = "gpt-custom"
	preferences.ClassifierEffort = "high"
	preferences.ClassifierContextBudgetBytes = 9000

	notice := welcomeNotice("1.2.3", preferences)
	for _, want := range []string{
		"every hour",
		"choose when to install available updates",
		"keep completed tasks visible until you archive them",
		"leave every task title entirely to you",
		"keep output-token figures out of task titles while title updates are off",
		"leave agent replies unchanged",
		"custom: gpt-custom with high reasoning, up to 9 KB of context",
	} {
		if !strings.Contains(notice, want) {
			t.Fatalf("welcome notice missing %q\n%s", want, notice)
		}
	}
	if strings.Contains(notice, "show output tokens at the end") {
		t.Fatalf("welcome notice promises token placement while title updates are off\n%s", notice)
	}
}

func TestWelcomeNoticeShowsConfiguredTokenPositionWhenTitleUpdatesAreEnabled(t *testing.T) {
	preferences := DefaultPreferences()
	preferences.RenameEnabled = true
	preferences.TokenDisplay = "end"

	notice := welcomeNotice("1.2.3", preferences)
	if want := "show output tokens at the end, like 🚨 Fix checkout · out 1.6m"; !strings.Contains(notice, want) {
		t.Fatalf("welcome notice missing %q\n%s", want, notice)
	}
	if strings.Contains(notice, "while title updates are off") {
		t.Fatalf("welcome notice says title updates are off when they are enabled\n%s", notice)
	}
}
