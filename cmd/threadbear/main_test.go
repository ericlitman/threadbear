package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ericlitman/threadbear/internal/app"
	"github.com/ericlitman/threadbear/internal/codex"
	"github.com/ericlitman/threadbear/internal/codex/appserver"
	"github.com/ericlitman/threadbear/internal/config"
)

func TestParseLifecycleFlags(t *testing.T) {
	request, err := parseRequest([]string{"install", "--noninteractive", "--confirm", "--version", "1.2.3", "--heartbeat-seconds", "45", "--agents=false"})
	if err != nil {
		t.Fatal(err)
	}
	if request.Command != app.CommandInstall || !request.NonInteractive || !request.Confirm || request.Version != "1.2.3" || request.Configure.HeartbeatSeconds == nil || *request.Configure.HeartbeatSeconds != 45 || request.Configure.AgentsEnabled == nil || *request.Configure.AgentsEnabled {
		t.Fatalf("request=%+v", request)
	}
	request, err = parseRequest([]string{"uninstall", "--non-interactive", "--confirm", "--archive-control-task", "--delete-state"})
	if err != nil {
		t.Fatal(err)
	}
	if !request.NonInteractive || !request.Confirm || !request.ArchiveControlTask || !request.DeleteState {
		t.Fatalf("request=%+v", request)
	}
	request, err = parseRequest([]string{"self-test", "--candidate", "--json"})
	if err != nil || !request.Candidate || !request.JSON {
		t.Fatalf("request=%+v err=%v", request, err)
	}
}

func TestParseConfigurePreviewAndConfirmation(t *testing.T) {
	request, err := parseRequest([]string{"configure", "--dry-run", "--agents=false"})
	if err != nil || !request.DryRun || request.Confirm {
		t.Fatalf("request=%+v err=%v", request, err)
	}
	request, err = parseRequest([]string{"configure", "--noninteractive", "--confirm", "--classifier-model", "model", "--classifier-effort", "high", "--classifier-context-budget-bytes", "1234"})
	if err != nil || !request.NonInteractive || !request.Confirm || request.Configure.ClassifierModel == nil || request.Configure.ClassifierEffort == nil || request.Configure.ClassifierContextBudgetBytes == nil {
		t.Fatalf("request=%+v err=%v", request, err)
	}
	if _, err := parseRequest([]string{"install", "--version", "v1.2.3"}); err == nil {
		t.Fatal("accepted a version with leading v")
	}
}

func TestInstallScriptRejectsNonSemverBeforeURLUse(t *testing.T) {
	script := filepath.Join("..", "..", "install.sh")
	for _, version := range []string{"1.2", "1.2.3.4", "1.2.x", "v1.2.3", "1.2.3-beta"} {
		command := exec.Command("/bin/sh", script, "--version", version)
		output, err := command.CombinedOutput()
		if err == nil || !strings.Contains(string(output), "exact N.N.N") {
			t.Fatalf("version %q output=%q err=%v", version, output, err)
		}
	}
}

func TestInstallScriptRejectsInvalidManifestVersionBeforeAssetURL(t *testing.T) {
	directory := t.TempDir()
	curl := filepath.Join(directory, "curl")
	if err := os.WriteFile(curl, []byte("#!/bin/sh\nprintf '%s\\n' '{\"version\":\"1.2.x\"}'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("/bin/sh", filepath.Join("..", "..", "install.sh"))
	command.Env = append(os.Environ(), "PATH="+directory+":/usr/bin:/bin", "THREADBEAR_RELEASE_BASE_URL=https://invalid.example")
	output, err := command.CombinedOutput()
	if err == nil || !strings.Contains(string(output), "manifest version must be exact N.N.N") {
		t.Fatalf("output=%q err=%v", output, err)
	}
}

type controlTaskClientFake struct {
	thread      appserver.Thread
	starts      int
	reads       int
	unarchives  int
	titles      int
	archives    int
	setTitleErr error
}

func (c *controlTaskClientFake) ReadThread(context.Context, string) (appserver.Thread, error) {
	c.reads++
	return c.thread, nil
}
func (c *controlTaskClientFake) StartPersistentThread(context.Context) (appserver.Thread, error) {
	c.starts++
	c.thread = appserver.Thread{ID: "control-new", Status: appserver.ThreadStatus{Type: "idle"}}
	return c.thread, nil
}
func (c *controlTaskClientFake) Unarchive(context.Context, string) (appserver.Thread, error) {
	c.unarchives++
	c.thread.Status.Type = "idle"
	return c.thread, nil
}
func (c *controlTaskClientFake) SetTitle(_ context.Context, _ string, title string) error {
	c.titles++
	if c.setTitleErr != nil {
		return c.setTitleErr
	}
	c.thread.Name = title
	return nil
}
func (c *controlTaskClientFake) Archive(context.Context, string) error {
	c.archives++
	c.thread.Status.Type = "archived"
	return nil
}

func TestEnsureControlTaskMutatesOnlyWhenNeeded(t *testing.T) {
	client := &controlTaskClientFake{thread: appserver.Thread{ID: "control-1", Name: controlTaskTitle, Status: appserver.ThreadStatus{Type: "idle"}}}
	id, changed, err := ensureControlTask(context.Background(), client, "control-1")
	if err != nil || changed || id != "control-1" || client.unarchives != 0 || client.titles != 0 || client.starts != 0 {
		t.Fatalf("id=%q changed=%t err=%v client=%+v", id, changed, err, client)
	}
	client.thread.Status.Type = "archived"
	client.thread.Name = "old"
	_, changed, err = ensureControlTask(context.Background(), client, "control-1")
	if err != nil || !changed || client.unarchives != 1 || client.titles != 1 {
		t.Fatalf("changed=%t err=%v client=%+v", changed, err, client)
	}
	created := &controlTaskClientFake{setTitleErr: errors.New("title failed")}
	id, changed, err = ensureControlTask(context.Background(), created, "")
	if err == nil || !changed || id != "control-new" || created.starts != 1 {
		t.Fatalf("id=%q changed=%t err=%v client=%+v", id, changed, err, created)
	}
}

func TestArchiveControlTaskReportsTruthfulChangedState(t *testing.T) {
	client := &controlTaskClientFake{thread: appserver.Thread{ID: "control-1", Status: appserver.ThreadStatus{Type: "idle"}}}
	changed, err := archiveControlTask(context.Background(), client, "control-1")
	if err != nil || !changed || client.archives != 1 {
		t.Fatalf("first changed=%t err=%v client=%+v", changed, err, client)
	}
	changed, err = archiveControlTask(context.Background(), client, "control-1")
	if err != nil || changed || client.archives != 1 {
		t.Fatalf("second changed=%t err=%v client=%+v", changed, err, client)
	}
}

func TestVersionDoesNotRequireCodexExecutable(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, "custom-codex"))
	t.Setenv("PATH", "/usr/bin:/bin")
	var stdout, stderr strings.Builder
	if code := run(context.Background(), []string{"version", "--json"}, &stdout, &stderr); code != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestInstallMissingCodexReportsStepAndDoesNotCreateState(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, "custom-codex"))
	t.Setenv("PATH", filepath.Join(home, "missing-bin"))
	originalResolver := resolveCodexExecutableSpec
	resolveCodexExecutableSpec = func(string, string) (codex.ExecutableSpec, error) {
		return codex.ExecutableSpec{}, errors.New("Codex executable not found; install the Codex CLI")
	}
	t.Cleanup(func() { resolveCodexExecutableSpec = originalResolver })
	var stdout, stderr strings.Builder
	code := run(context.Background(), []string{"install", "--noninteractive", "--confirm", "--json"}, &stdout, &stderr)
	if code != 1 || !strings.Contains(stdout.String(), `"step":"resolve_codex_executable"`) || !strings.Contains(stdout.String(), "install the Codex CLI") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	stateDirectory := filepath.Join(home, ".local", "share", "threadbear")
	if _, err := os.Stat(stateDirectory); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("state directory created: %v", err)
	}
}

type configStoreFake struct{ config config.Config }

func (s configStoreFake) LoadConfig() (config.Config, error) { return s.config, nil }

func TestAppServerRuntimeUsesConfiguredExecutableAndResolvedCodexHome(t *testing.T) {
	home := t.TempDir()
	codexHome := filepath.Join(home, "custom-codex")
	executable := filepath.Join(home, "bin", "codex")
	if err := os.MkdirAll(filepath.Dir(executable), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(executable, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default("control")
	cfg.CodexExecutable = executable
	process, err := (appServerRuntime{store: configStoreFake{config: cfg}, home: home, codexHome: codexHome}).process()
	if err != nil {
		t.Fatal(err)
	}
	if process.Path != executable {
		t.Fatalf("process.Path=%q", process.Path)
	}
	found := false
	for _, entry := range process.Env {
		found = found || entry == "CODEX_HOME="+codexHome
	}
	if !found {
		t.Fatalf("process.Env=%v", process.Env)
	}
}

type staticConfigLoader struct{ value config.Config }

func (s staticConfigLoader) LoadConfig() (config.Config, error) { return s.value, nil }

func TestHeartbeatRuntimeUsesInstalledPinnedCodexExecutable(t *testing.T) {
	home := t.TempDir()
	codexHome := filepath.Join(home, "codex-home")
	codexDirectory := filepath.Join(home, "stable")
	nodeDirectory := filepath.Join(home, "node")
	for _, directory := range []string{codexDirectory, nodeDirectory} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	executable := filepath.Join(codexDirectory, "codex")
	if err := os.WriteFile(executable, []byte("#!/usr/bin/env node\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nodeDirectory, "node"), []byte("#!/bin/sh\n[ \"$2\" = \"--version\" ]\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	installPath := strings.Join([]string{codexDirectory, nodeDirectory}, string(os.PathListSeparator))
	spec, err := codex.ResolveExecutableSpec(home, installPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default("control")
	cfg.CodexExecutable = spec.Path
	cfg.CodexSpawnPath = spec.SpawnPath
	t.Setenv("PATH", filepath.Join(home, "different-path"))
	process, err := (appServerRuntime{store: staticConfigLoader{value: cfg}, home: home, codexHome: codexHome}).process()
	if err != nil {
		t.Fatal(err)
	}
	if process.Path != executable {
		t.Fatalf("process path=%q want installed path %q", process.Path, executable)
	}
	storedPath := spec.SpawnPath
	found := false
	for _, entry := range process.Env {
		found = found || entry == "PATH="+storedPath
	}
	if !found {
		t.Fatalf("process env=%v want stored PATH=%q", process.Env, storedPath)
	}
}
