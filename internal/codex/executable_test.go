package codex

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeExecutable(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
		t.Fatal(err)
	}
}

func TestSanitizePathKeepsCanonicalAbsoluteEntriesInOrder(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "first")
	second := filepath.Join(root, "second")
	input := strings.Join([]string{"relative", first + string(os.PathSeparator) + ".", "", second, first}, string(filepath.ListSeparator))
	want := strings.Join([]string{first, second}, string(filepath.ListSeparator))
	if got := SanitizePath(input); got != want {
		t.Fatalf("SanitizePath()=%q want %q", got, want)
	}
}

func TestResolveExecutableUsesStandardHomeLocationAfterInvokingPath(t *testing.T) {
	home := t.TempDir()
	executable := filepath.Join(home, ".local", "bin", "codex")
	writeExecutable(t, executable, "#!/bin/sh\nexit 0\n")
	spec, err := ResolveExecutableSpec(home, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if spec.Path != executable || spec.SpawnPath != filepath.Dir(executable) {
		t.Fatalf("spec=%+v", spec)
	}
}

func TestResolveExecutableEnvShebangUsesInterpreterFromCapturedPath(t *testing.T) {
	home := t.TempDir()
	codexDirectory := filepath.Join(t.TempDir(), "codex-bin")
	interpreterDirectory := filepath.Join(t.TempDir(), "interpreter-bin")
	writeExecutable(t, filepath.Join(codexDirectory, "codex"), "#!/usr/bin/env node\n")
	writeExecutable(t, filepath.Join(interpreterDirectory, "node"), "#!/bin/sh\n[ \"$2\" = \"--version\" ]\n")
	captured := strings.Join([]string{codexDirectory, interpreterDirectory}, string(filepath.ListSeparator))
	spec, err := ResolveExecutableSpec(home, captured)
	if err != nil {
		t.Fatal(err)
	}
	if spec.Path != filepath.Join(codexDirectory, "codex") || spec.SpawnPath != captured {
		t.Fatalf("spec=%+v", spec)
	}
	t.Setenv("PATH", t.TempDir())
	if err := VerifyExecutableSpec(home, spec); err != nil {
		t.Fatalf("persisted pair depends on ambient PATH: %v", err)
	}
}

func TestValidateExecutableSpecRequiresCanonicalSpawnPath(t *testing.T) {
	executable := filepath.Join(t.TempDir(), "codex")
	writeExecutable(t, executable, "#!/bin/sh\nexit 0\n")
	for _, value := range []string{"", "relative", "/usr/bin" + string(filepath.ListSeparator) + "/usr/bin", "/usr/bin/../bin"} {
		if err := ValidateExecutableSpec(ExecutableSpec{Path: executable, SpawnPath: value}); err == nil {
			t.Fatalf("accepted %q", value)
		}
	}
}

func TestResolveExecutableEnvShebangMissingInterpreterFailsActionably(t *testing.T) {
	home := t.TempDir()
	codexDirectory := filepath.Join(t.TempDir(), "codex-bin")
	writeExecutable(t, filepath.Join(codexDirectory, "codex"), "#!/usr/bin/env threadbear-missing-node\n")
	_, err := ResolveExecutableSpec(home, codexDirectory)
	if !errors.Is(err, ErrExecutableNotFound) || !strings.Contains(err.Error(), "threadbear-missing-node") || !strings.Contains(err.Error(), "then rerun install") {
		t.Fatalf("error=%v", err)
	}
}
