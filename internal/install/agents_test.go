package install

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ericlitman/threadbear/assets"
)

func TestManagedBlockPreservesBytesAndIsIdempotent(t *testing.T) {
	original := []byte("first\r\nsecond-without-final-newline")
	content := []byte("managed\ncontent\n")
	updated, err := UpdateManagedBlock(original, content)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(updated, original) {
		t.Fatalf("prefix changed: %q", updated)
	}
	again, err := UpdateManagedBlock(updated, content)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(updated, again) {
		t.Fatalf("not idempotent\n%q\n%q", updated, again)
	}
	replaced, err := UpdateManagedBlock(updated, []byte("replacement"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(replaced, original) {
		t.Fatalf("replacement changed prefix: %q", replaced)
	}
	removed, err := RemoveManagedBlock(updated)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(removed, original) {
		t.Fatalf("removed=%q want=%q", removed, original)
	}
}

func TestManagedBlockUpgradePreservesBytesOutsideLegacyBlock(t *testing.T) {
	prefix := []byte("user guidance before\r\n")
	suffix := []byte("user guidance after without final newline")
	legacy := ManagedBlock([]byte("legacy ThreadBear guidance"))
	original := append(append(append([]byte(nil), prefix...), legacy...), suffix...)

	updated, err := UpdateManagedBlock(original, []byte("new ThreadBear guidance"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(updated, prefix) {
		t.Fatalf("prefix changed: %q", updated)
	}
	if !bytes.HasSuffix(updated, suffix) {
		t.Fatalf("suffix changed: %q", updated)
	}
	if bytes.Contains(updated, []byte("legacy ThreadBear guidance")) {
		t.Fatalf("legacy managed content survived: %q", updated)
	}

	again, err := UpdateManagedBlock(updated, []byte("new ThreadBear guidance"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(updated, again) {
		t.Fatalf("upgrade is not idempotent\n%q\n%q", updated, again)
	}
}

func TestApplyManagedUpgradesBareBearBlockToCurrentMark(t *testing.T) {
	// BEAR-23: installed users carry a managed block whose footer examples use
	// the bare 🐻. Reconfiguring must replace it in place with the current
	// 🧵🐻 content, byte-preserving everything outside the markers.
	path := filepath.Join(t.TempDir(), "AGENTS.md")
	prefix := []byte("user guidance before\n")
	suffix := []byte("user guidance after\n")
	stale := ManagedBlock([]byte("# ThreadBear status\n\n`🐻 complete · next (none): none`\n"))
	original := append(append(append([]byte(nil), prefix...), stale...), suffix...)
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteManagedBlock(path, []byte(assets.AgentsManagedContent)); err != nil {
		t.Fatal(err)
	}
	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(updated, prefix) || !bytes.HasSuffix(updated, suffix) {
		t.Fatalf("user content outside the block changed: %q", updated)
	}
	if bytes.Contains(updated, []byte("`🐻 complete")) {
		t.Fatal("stale bare-bear example survived the upgrade")
	}
	if !bytes.Contains(updated, []byte("🧵🐻 complete")) {
		t.Fatalf("current mark missing after upgrade: %q", updated)
	}
	if got := bytes.Count(updated, []byte(ManagedBlockStart)); got != 1 {
		t.Fatalf("managed block count = %d, want 1", got)
	}
}

func TestApplyManagedUpgradesLegacyAgentsContentOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "AGENTS.md")
	prefix := []byte("user guidance before\n")
	suffix := []byte("user guidance after\n")
	legacy := ManagedBlock([]byte("legacy ThreadBear guidance"))
	original := append(append(append([]byte(nil), prefix...), legacy...), suffix...)
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}

	detail, changed, err := ManagedMutationPreview(path, true, []byte(assets.AgentsManagedContent))
	if err != nil || !changed {
		t.Fatalf("preview=%q changed=%t err=%v", detail, changed, err)
	}
	changed, err = applyManaged(path, true, []byte(assets.AgentsManagedContent))
	if err != nil || !changed {
		t.Fatalf("changed=%t err=%v", changed, err)
	}
	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(updated, prefix) || !bytes.HasSuffix(updated, suffix) {
		t.Fatalf("outside bytes changed: %q", updated)
	}

	detail, changed, err = ManagedMutationPreview(path, true, []byte(assets.AgentsManagedContent))
	if err != nil || changed {
		t.Fatalf("second preview=%q changed=%t err=%v", detail, changed, err)
	}
	changed, err = applyManaged(path, true, []byte(assets.AgentsManagedContent))
	if err != nil || changed {
		t.Fatalf("second apply changed=%t err=%v", changed, err)
	}
}

func TestManagedBlockRejectsMalformedMarkers(t *testing.T) {
	cases := [][]byte{
		[]byte(ManagedBlockStart + "\nmissing end"),
		[]byte(ManagedBlockEnd),
		[]byte(ManagedBlockStart + "\nx\n" + ManagedBlockEnd + "\n" + ManagedBlockEnd),
		[]byte(ManagedBlockEnd + "\n" + ManagedBlockStart),
	}
	for _, data := range cases {
		if _, err := UpdateManagedBlock(data, []byte("x")); !errors.Is(err, ErrMalformedManagedBlock) {
			t.Fatalf("data=%q error=%v", data, err)
		}
	}
}

func TestManagedFilesRejectSymlinksAndWriteAtomically(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".codex", "AGENTS.md")
	if err := WriteManagedBlock(path, []byte("one")); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteManagedBlock(path, []byte("one")); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("idempotent write changed bytes")
	}
	// A managed file reached through a symlink is followed: people keep
	// AGENTS.md in a dotfiles repo and link it into ~/.codex. The block lands
	// in the real file, the user's content survives, and the link itself is
	// still a link afterwards.
	target := filepath.Join(home, "target")
	if err := os.WriteFile(target, []byte("user content\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(home, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := WriteManagedBlock(link, []byte("managed")); err != nil {
		t.Fatalf("write through symlink: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(got, []byte("user content")) || !bytes.Contains(got, []byte("managed")) {
		t.Fatalf("target=%q", got)
	}
	linkInfo, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if linkInfo.Mode()&os.ModeSymlink == 0 {
		t.Fatal("symlink was replaced by a regular file")
	}
	if err := ValidateManagedFile(link); err != nil {
		t.Fatalf("validate through symlink: %v", err)
	}
	// A symlink pointing at something that is not a regular file is still refused.
	directoryLink := filepath.Join(home, "dirlink")
	if err := os.Symlink(home, directoryLink); err != nil {
		t.Fatal(err)
	}
	if err := WriteManagedBlock(directoryLink, []byte("bad")); !errors.Is(err, ErrUnsafeManagedPath) {
		t.Fatalf("directory symlink error=%v", err)
	}
	// A dangling symlink is refused rather than silently creating a file.
	danglingLink := filepath.Join(home, "danglink")
	if err := os.Symlink(filepath.Join(home, "missing-target"), danglingLink); err != nil {
		t.Fatal(err)
	}
	if err := WriteManagedBlock(danglingLink, []byte("bad")); !errors.Is(err, ErrUnsafeManagedPath) {
		t.Fatalf("dangling symlink error=%v", err)
	}
}

func TestConfirmDefaultsToYesForApplyAndNoForRemoval(t *testing.T) {
	for _, test := range []struct {
		name       string
		defaultYes bool
		want       string
	}{
		{name: "apply", defaultYes: true, want: "[yes]"},
		{name: "removal", defaultYes: false, want: "[no]"},
	} {
		t.Run(test.name, func(t *testing.T) {
			output := &strings.Builder{}
			prompter := NewTTYPrompter(strings.NewReader("\n"), output)
			confirmed, err := prompter.Confirm(test.defaultYes)
			if err != nil {
				t.Fatal(err)
			}
			if confirmed != test.defaultYes {
				t.Fatalf("bare Return gave confirmed=%t, want %t", confirmed, test.defaultYes)
			}
			if !strings.Contains(output.String(), test.want) {
				t.Fatalf("prompt %q missing %s", output.String(), test.want)
			}
		})
	}
}

func TestWelcomeNoticeMentionsVersion(t *testing.T) {
	notice := welcomeNotice("1.0.1", DefaultPreferences())
	if !strings.Contains(notice, "ThreadBear 1.0.1") {
		t.Fatalf("notice=%q", notice)
	}
}
