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

func TestResolveExecutableSkipsCandidateThatDoesNotRun(t *testing.T) {
	home := t.TempDir()
	pathDirectory := filepath.Join(t.TempDir(), "bin")
	broken := filepath.Join(pathDirectory, "codex")
	if err := os.MkdirAll(pathDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(broken, []byte("#!/bin/sh\nexit 9\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	fallback := filepath.Join(home, ".local", "bin", "codex")
	writeExecutable(t, fallback, 0o700)
	got, err := resolveExecutable(home, pathDirectory, []string{fallback})
	if err != nil || got != fallback {
		t.Fatalf("got=%q err=%v", got, err)
	}
}

func TestResolveExecutableSpecPinsEnvInterpreterWithoutAmbientPath(t *testing.T) {
	home := t.TempDir()
	codexDirectory := filepath.Join(t.TempDir(), "codex-bin")
	nodeDirectory := filepath.Join(t.TempDir(), "node-bin")
	ambientDirectory := filepath.Join(t.TempDir(), "ambient-bin")
	for _, directory := range []string{codexDirectory, nodeDirectory, ambientDirectory} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	target := filepath.Join(t.TempDir(), "codex-real")
	if err := os.WriteFile(target, []byte("#!/usr/bin/env node\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(codexDirectory, "codex")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nodeDirectory, "node"), []byte("#!/bin/sh\n[ \"$2\" = \"--version\" ]\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ambientDirectory, "node"), []byte("#!/bin/sh\nexit 91\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	spec, err := ResolveExecutableSpec(home, strings.Join([]string{codexDirectory, nodeDirectory, ambientDirectory}, string(os.PathListSeparator)))
	if err != nil {
		t.Fatal(err)
	}
	if spec.Path != link {
		t.Fatalf("path=%q want symlink %q", spec.Path, link)
	}
	want := append([]string{nodeDirectory, codexDirectory}, fixedSystemPath...)
	if strings.Join(spec.SpawnPath, "|") != strings.Join(want, "|") {
		t.Fatalf("spawn path=%v want %v", spec.SpawnPath, want)
	}
	t.Setenv("PATH", ambientDirectory)
	if err := VerifyExecutableSpec(spec); err != nil {
		t.Fatalf("stored spec depends on ambient PATH: %v", err)
	}
}
