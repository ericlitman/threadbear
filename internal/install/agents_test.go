package install

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
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
	target := filepath.Join(home, "target")
	if err := os.WriteFile(target, []byte("safe"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(home, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := WriteManagedBlock(link, []byte("bad")); !errors.Is(err, ErrUnsafeManagedPath) {
		t.Fatalf("error=%v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "safe" {
		t.Fatalf("target=%q", got)
	}
}
