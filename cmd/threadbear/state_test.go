package main

import (
	"strings"
	"testing"
)

func TestSubjectFromTitleUsesFiniteVisiblePrefixes(t *testing.T) {
	for _, test := range []struct {
		title, subject string
		decorated      bool
	}{
		{"Quarterly  close ", "Quarterly  close ", false},
		{"🎉 Quarterly  close ", "🎉 Quarterly  close ", false},
		{"✅ Quarterly  close ", "Quarterly  close ", true},
		{"✅✦ Quarterly  close ", "Quarterly  close ", true},
		{"🐻 Existing task", "Existing task", true},
	} {
		subject, decorated, err := subjectFromTitle(test.title)
		if err != nil || subject != test.subject || decorated != test.decorated {
			t.Errorf("subjectFromTitle(%q) = %q, %t, %v", test.title, subject, decorated, err)
		}
	}
	// A current ThreadBear icon is one intentionally reserved ambiguity. The
	// finite old prefixes below are also left untouched because they cannot be
	// distinguished safely; every other user emoji remains byte-exact.
	if subject, decorated, err := subjectFromTitle("✅ User-authored prefix"); err != nil ||
		subject != "User-authored prefix" || !decorated {
		t.Fatalf("reserved prefix = %q, %t, %v", subject, decorated, err)
	}
}

func TestSubjectFromTitleRejectsAmbiguousAndInternalText(t *testing.T) {
	for _, title := range []string{
		"🧵🐻 needs input (you): approve cleanup",
		"⏳ ThreadBear is working",
		"❔ old prompt",
		"<codex_delegation>private</codex_delegation>",
		"<app-context>Codex desktop context</app-context>",
		"<collaboration_mode>Default</collaboration_mode>",
		"<permissions instructions>unrestricted</permissions instructions>",
	} {
		if _, _, err := subjectFromTitle(title); err == nil {
			t.Errorf("unsafe title %q was accepted", title)
		}
	}
}

func TestRenderTitlePreservesSubjectAndNeverTruncates(t *testing.T) {
	subject := "  🎉 Exact  whitespace "
	got, err := renderTitle("complete", subject)
	if err != nil || got != "✅ "+subject {
		t.Fatalf("render = %q, %v", got, err)
	}
	fit := strings.Repeat("x", 57)
	if got, err := renderTitle("blocked", fit); err != nil || len([]rune(got)) == 0 || len(got) == 0 {
		t.Fatalf("fitting title = %q, %v", got, err)
	}
	if got, err := renderTitle("complete", strings.Repeat("x", 58)); err == nil || got != "" {
		t.Fatalf("too-long title = %q, %v", got, err)
	}
	emojiFit := strings.Repeat("🧵", 28)
	if got, err := renderTitle("complete", emojiFit); err != nil || got != "✅ "+emojiFit {
		t.Fatalf("emoji title = %q, %v", got, err)
	}
}

func TestTitlePolicyCoversEveryStatusAndLegacyBearCleanup(t *testing.T) {
	for status, icon := range statusIcons {
		if got, err := renderTitle(status, "subject"); err != nil || got != icon+" subject" {
			t.Errorf("render %s = %q, %v", status, got, err)
		}
		if !containsString(ownedTitlePrefixes, icon+" ") {
			t.Errorf("owned prefixes omit %q", icon)
		}
		if got, err := renderOnboardTitle(status, "inferred", "subject"); err != nil || got != icon+"✦ subject" {
			t.Errorf("inferred render %s = %q, %v", status, got, err)
		}
		if !containsString(ownedTitlePrefixes, icon+"✦ ") {
			t.Errorf("owned prefixes omit inferred %q", icon)
		}
	}
	if !containsString(ownedTitlePrefixes, "🐻 ") {
		t.Fatal("owned prefixes omit legacy bear cleanup")
	}
	for _, icon := range statusIcons {
		if icon == "🐻" {
			t.Fatal("neutral bear remains a writable status")
		}
	}
}

func TestInferredTitleStillHonorsUTF16Limit(t *testing.T) {
	if got, err := renderOnboardTitle("complete", "exact", strings.Repeat("x", 57)); err != nil || got == "" {
		t.Fatalf("exact fitting title = %q, %v", got, err)
	}
	if got, err := renderOnboardTitle("complete", "inferred", strings.Repeat("x", 58)); err == nil || got != "" {
		t.Fatalf("inferred oversized title = %q, %v", got, err)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
