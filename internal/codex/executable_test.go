package codex

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeExecutable(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), mode); err != nil {
		t.Fatal(err)
	}
}

func TestResolveExecutableUsesAbsolutePATHFirstAndPreservesSymlink(t *testing.T) {
	home := t.TempDir()
	first := filepath.Join(t.TempDir(), "bin")
	target := filepath.Join(t.TempDir(), "codex-real")
	writeExecutable(t, target, 0o700)
	if err := os.MkdirAll(first, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(first, "codex")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	fallback := filepath.Join(home, ".local", "bin", "codex")
	writeExecutable(t, fallback, 0o700)
	got, err := ResolveExecutable(home, "relative::"+first)
	if err != nil {
		t.Fatal(err)
	}
	if got != link {
		t.Fatalf("ResolveExecutable()=%q want stable symlink %q", got, link)
	}
}

func TestResolveExecutableRequiresRegularExecutable(t *testing.T) {
	home := t.TempDir()
	directory := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(filepath.Join(directory, "codex"), 0o700); err != nil {
		t.Fatal(err)
	}
	nonExecutable := filepath.Join(home, ".local", "bin", "codex")
	writeExecutable(t, nonExecutable, 0o600)
	_, err := resolveExecutable(home, directory, nil)
	if !errors.Is(err, ErrExecutableNotFound) {
		t.Fatalf("error=%v", err)
	}
	if !strings.Contains(err.Error(), "install the Codex CLI") || !strings.Contains(err.Error(), filepath.Join(home, ".local", "bin", "codex")) {
		t.Fatalf("failure is not actionable: %v", err)
	}
}

func TestResolveExecutableUsesStandardHomeLocationAfterPATH(t *testing.T) {
	home := t.TempDir()
	fallback := filepath.Join(home, ".local", "bin", "codex")
	writeExecutable(t, fallback, 0o700)
	got, err := resolveExecutable(home, "relative::", []string{fallback})
	if err != nil || got != fallback {
		t.Fatalf("got=%q err=%v", got, err)
	}
}
