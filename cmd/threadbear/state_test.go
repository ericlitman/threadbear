package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"
	"unicode/utf8"
)

func utf16Len(value string) int { return len(utf16.Encode([]rune(value))) }

func TestNativeStateStoreInitializesPrivatelyAndRoundTrips(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	disk := newStore(dir)
	if filepath.Base(disk.path()) != "native.json" {
		t.Fatalf("state path = %q", disk.path())
	}
	if _, err := disk.read(); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("absent read error = %v", err)
	}
	want := taskState{Subject: "Customer outage", Last: "🚨 Customer outage → restore service"}
	if err := disk.update(func(value *state) (bool, error) {
		if value.Format != stateFormat || value.Tasks == nil {
			t.Fatalf("initial state = %#v", value)
		}
		value.Tasks["task-1"] = want
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	for path, mode := range map[string]os.FileMode{dir: 0o700, disk.path(): 0o600, filepath.Join(dir, "native.lock"): 0o600} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != mode {
			t.Fatalf("%s mode = %o, want %o", path, info.Mode().Perm(), mode)
		}
	}
	got, err := disk.read()
	if err != nil {
		t.Fatal(err)
	}
	if got.Format != stateFormat || got.Tasks["task-1"] != want {
		t.Fatalf("read = %#v", got)
	}
	data, err := os.ReadFile(disk.path())
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"format"`, `"tasks"`, `"subject"`} {
		if !strings.Contains(string(data), key) {
			t.Fatalf("state JSON %q lacks %s", data, key)
		}
	}
}

func TestNativeStateCorruptionFailsClosed(t *testing.T) {
	for name, body := range map[string]string{
		"malformed":     `{`,
		"wrong format":  `{"format":2,"tasks":{}}`,
		"missing tasks": `{"format":3}`,
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			disk := newStore(dir)
			if err := os.WriteFile(disk.path(), []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			called := false
			if err := disk.update(func(*state) (bool, error) { called = true; return false, nil }); err == nil {
				t.Fatal("corrupt state was accepted")
			}
			if called {
				t.Fatal("mutation ran against corrupt state")
			}
			got, err := os.ReadFile(disk.path())
			if err != nil || string(got) != body {
				t.Fatalf("corrupt state was replaced: %q, %v", got, err)
			}
		})
	}
}

func TestNativeStateRejectsUnsafePaths(t *testing.T) {
	realDir := t.TempDir()
	link := filepath.Join(filepath.Dir(realDir), "state-link")
	if err := os.Symlink(realDir, link); err != nil {
		t.Fatal(err)
	}
	if err := newStore(link).update(func(*state) (bool, error) { return false, nil }); err == nil {
		t.Fatal("symlink state directory was accepted")
	}
	dir := filepath.Join(t.TempDir(), "state")
	disk := newStore(dir)
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(disk.path(), []byte(`{"format":3,"tasks":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(disk.path(), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := disk.read(); err == nil {
		t.Fatal("public state file was accepted")
	}
}

func TestNativeStateMutationErrorDoesNotSave(t *testing.T) {
	disk := newStore(filepath.Join(t.TempDir(), "state"))
	want := errors.New("stop")
	if err := disk.update(func(value *state) (bool, error) {
		value.Tasks["task-1"] = taskState{Subject: "not saved"}
		return false, want
	}); !errors.Is(err, want) {
		t.Fatalf("update error = %v", err)
	}
	if _, err := disk.read(); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed mutation wrote state: %v", err)
	}
}

func TestCanonicalSubjectUsesOnlyExactOwnership(t *testing.T) {
	previous := taskState{
		Subject: "Customer outage",
		Last:    "🚨 Customer outage → restore service",
		Pending: &pendingProposal{BaseSubject: "Customer outage", Prior: "⏳ Customer outage", Proposed: "✅ Customer outage"},
	}
	for name, pair := range map[string][2]string{
		"last committed":      {previous.Last, previous.Subject},
		"pending prior":       {previous.Pending.Prior, previous.Subject},
		"pending proposed":    {previous.Pending.Proposed, previous.Subject},
		"lost post near miss": {"✅ Customer outage!", "✅ Customer outage!"},
		"user rename":         {"🚨 Billing → literal user arrow", "🚨 Billing → literal user arrow"},
		"outer whitespace":    {" ✅ Customer outage ", "✅ Customer outage"},
	} {
		t.Run(name, func(t *testing.T) {
			if got := canonicalSubject(pair[0], previous); got != pair[1] {
				t.Fatalf("canonicalSubject(%q) = %q, want %q", pair[0], got, pair[1])
			}
		})
	}
	if got := canonicalSubject("🚨 User title → do not parse", taskState{}); got != "🚨 User title → do not parse" {
		t.Fatalf("fresh title was parsed as owned: %q", got)
	}
	if got := canonicalSubject("Fresh task\n  subject", taskState{}); got != "Fresh task subject" {
		t.Fatalf("fresh subject was not normalized: %q", got)
	}
}

func TestParseFooterExactGrammar(t *testing.T) {
	valid := map[string]footer{
		"Done.\n\n🧵🐻 complete":                               {Status: "complete"},
		"🧵🐻 automation\r\n":                                  {Status: "automation"},
		"🧵🐻 next steps (you): approve the release":           {Status: "next_steps", Action: "approve the release"},
		"🧵🐻 next steps (agent): retry the title handoff":     {Status: "next_steps", Action: "retry the title handoff"},
		"🧵🐻 next steps (external): review the pull request":  {Status: "next_steps", Action: "review the pull request"},
		"🧵🐻 needs input (you): choose the release region":    {Status: "needs_input", Action: "choose the release region"},
		"🧵🐻 needs input (you): approve":                      {Status: "needs_input", Action: "approve"},
		"🧵🐻 blocked (external): restore the signing service": {Status: "blocked", Action: "restore the signing service"},
	}
	for message, want := range valid {
		got, ok := parseFooter(message)
		if !ok || got != want {
			t.Errorf("parseFooter(%q) = %#v, %v; want %#v", message, got, ok, want)
		}
	}
	invalid := []string{
		"", "🧵🐻 Complete", "> 🧵🐻 complete", "🧵🐻 complete\nextra",
		"🧵🐻 complete\n🧵🐻 automation", "🧵🐻 needs input (you): ", "🧵🐻 needs input (you):   ",
		"🧵🐻 needs input (agent): choose the region", "🧵🐻 blocked (you): restore the service",
		"🧵🐻 next steps (bear): approve the release", " 🧵🐻 complete", "🧵🐻 complete ",
	}
	for _, message := range invalid {
		if got, ok := parseFooter(message); ok {
			t.Errorf("parseFooter(%q) accepted %#v", message, got)
		}
	}
}

func TestStripStatusIcons(t *testing.T) {
	for title, want := range map[string]string{
		"✅ ✅ ❔ hello":        "hello",
		"✅✅❔hello":           "hello",
		"➡ task":             "task",
		"❔ ❔ ❔":              "",
		"➡️ 🙋 task → action": "task → action",
		"🎉 ✅ user title":     "🎉 ✅ user title",
		"text ✅ suffix":      "text ✅ suffix",
	} {
		if got := stripStatusIcons(title); got != want {
			t.Errorf("stripStatusIcons(%q) = %q, want %q", title, got, want)
		}
	}
}

func TestRenderTitleContractAndSubjectPriority(t *testing.T) {
	for name, values := range map[string][4]string{
		"running":    {"running", "Ship BEAR-102", "", "⏳ Ship BEAR-102"},
		"next steps": {"next_steps", "Ship BEAR-102", "approve the release", "➡️ Ship BEAR-102 → approve the release"},
		"complete":   {"complete", "Ship BEAR-102", "ignored action", "✅ Ship BEAR-102"},
		"automation": {"automation", "Nightly cleanup", "ignored action", "🤖 Nightly cleanup"},
		"unknown":    {"not-a-status", "Legacy task", "", "❔ Legacy task"},
	} {
		t.Run(name, func(t *testing.T) {
			if got := renderTitle(values[0], values[1], values[2]); got != values[3] {
				t.Fatalf("renderTitle() = %q, want %q", got, values[3])
			}
		})
	}
	got := renderTitle("next_steps", strings.Repeat("s", 100), "keep this action")
	want := renderTitle("next_steps", strings.Repeat("s", 100), "")
	if got != want || utf16Len(got) != 60 {
		t.Fatalf("action displaced durable subject: %q, want %q", got, want)
	}
	got = renderTitle("blocked", "keep subject", strings.Repeat("a", 100))
	if utf16Len(got) > 60 || !strings.HasPrefix(got, "🚨 keep subject → ") || !strings.HasSuffix(got, "…") {
		t.Fatalf("long action truncation = %q (%d units)", got, utf16Len(got))
	}
	subject := strings.Repeat("d", 45)
	got = renderTitle("next_steps", subject, "this action must be truncated before the subject")
	if !strings.HasPrefix(got, "➡️ "+subject+" → ") || utf16Len(got) != 60 || !strings.HasSuffix(got, "…") {
		t.Fatalf("bounded action displaced subject: %q (%d units)", got, utf16Len(got))
	}
}

func TestRenderTitleUTF16Boundaries(t *testing.T) {
	for subjectLen, want := range map[int][2]int{
		57: {59, 0},
		58: {60, 0},
		59: {60, 1},
	} {
		got := renderTitle("complete", strings.Repeat("x", subjectLen), "")
		if utf16Len(got) != want[0] || strings.HasSuffix(got, "…") != (want[1] == 1) {
			t.Errorf("subject %d: %q has %d units", subjectLen, got, utf16Len(got))
		}
	}
	got := renderTitle("next_steps", strings.Repeat("🧵", 29), "")
	if utf16Len(got) > 60 || !utf8.ValidString(got) || !strings.HasSuffix(got, "…") {
		t.Fatalf("emoji title = %q (%d units, valid=%v)", got, utf16Len(got), utf8.ValidString(got))
	}
	got = renderTitle("complete", strings.Repeat("🧵", 28), "")
	if got != "✅ "+strings.Repeat("🧵", 28) || !utf8.ValidString(got) {
		t.Fatalf("fitting emoji pair was split: %q", got)
	}
}
