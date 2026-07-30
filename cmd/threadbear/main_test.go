package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ericlitman/threadbear/assets"
	"github.com/ericlitman/threadbear/internal/app"
	"github.com/ericlitman/threadbear/internal/codex"
	"github.com/ericlitman/threadbear/internal/codex/appserver"
	"github.com/ericlitman/threadbear/internal/config"
	"github.com/ericlitman/threadbear/internal/install"
	"github.com/ericlitman/threadbear/internal/launchagent"
	"github.com/ericlitman/threadbear/internal/state"
	"github.com/ericlitman/threadbear/internal/tokens"
)

func TestParseLifecycleFlags(t *testing.T) {
	request, err := parseRequest([]string{"install", "--noninteractive", "--confirm", "--version", "1.2.3", "--heartbeat-seconds", "45", "--auto-update=false", "--agents=false"})
	if err != nil {
		t.Fatal(err)
	}
	if request.Command != app.CommandInstall || !request.NonInteractive || !request.Confirm || request.Version != "1.2.3" || request.Configure.HeartbeatSeconds == nil || *request.Configure.HeartbeatSeconds != 45 || request.Configure.AutoUpdateEnabled == nil || *request.Configure.AutoUpdateEnabled || request.Configure.AgentsEnabled == nil || *request.Configure.AgentsEnabled {
		t.Fatalf("request=%+v", request)
	}
	request, err = parseRequest([]string{"uninstall", "--non-interactive", "--confirm", "--archive-control-task", "--delete-state"})
	if err != nil {
		t.Fatal(err)
	}
	if !request.NonInteractive || !request.Confirm || !request.ArchiveControlTask {
		t.Fatalf("request=%+v", request)
	}
	withoutDeprecatedFlag, err := parseRequest([]string{"uninstall", "--non-interactive", "--confirm", "--archive-control-task"})
	if err != nil || request != withoutDeprecatedFlag {
		t.Fatalf("deprecated --delete-state changed request: with=%+v without=%+v err=%v", request, withoutDeprecatedFlag, err)
	}
	request, err = parseRequest([]string{"self-test", "--candidate", "--json"})
	if err != nil || !request.Candidate || !request.JSON {
		t.Fatalf("request=%+v err=%v", request, err)
	}
}

func TestParseConfigurePreviewAndConfirmation(t *testing.T) {
	request, err := parseRequest([]string{"configure", "--dry-run", "--auto-update=false", "--agents=false", "--token-display=end"})
	if err != nil || !request.DryRun || request.Confirm || request.Configure.AutoUpdateEnabled == nil || *request.Configure.AutoUpdateEnabled || request.Configure.TokenDisplay == nil || *request.Configure.TokenDisplay != tokens.PositionEnd {
		t.Fatalf("request=%+v err=%v", request, err)
	}
	request, err = parseRequest([]string{"configure", "--noninteractive", "--confirm", "--classifier-model", "model", "--classifier-effort", "high", "--classifier-context-budget-bytes", "1234"})
	if err != nil || !request.NonInteractive || !request.Confirm || request.Configure.ClassifierModel == nil || request.Configure.ClassifierEffort == nil || request.Configure.ClassifierContextBudgetBytes == nil {
		t.Fatalf("request=%+v err=%v", request, err)
	}
	if _, err := parseRequest([]string{"install", "--version", "v1.2.3"}); err == nil {
		t.Fatal("accepted a version with leading v")
	}
	if _, err := parseRequest([]string{"configure", "--token-display=middle"}); err == nil {
		t.Fatal("accepted an invalid token display position")
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
	thread   appserver.Thread
	reads    int
	archives int
}

func (c *controlTaskClientFake) ReadThread(context.Context, string) (appserver.Thread, error) {
	c.reads++
	return c.thread, nil
}
func (c *controlTaskClientFake) Archive(context.Context, string) error {
	c.archives++
	c.thread.Status.Type = "archived"
	return nil
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
	code := run(context.Background(), []string{"install", "--control-task-id", "task-home", "--noninteractive", "--confirm", "--json"}, &stdout, &stderr)
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
	storedPath, err := codex.ComposeSpawnPath(spec.SpawnPath)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, entry := range process.Env {
		found = found || entry == "PATH="+storedPath
	}
	if !found {
		t.Fatalf("process env=%v want stored PATH=%q", process.Env, storedPath)
	}
}

func TestCandidateInstalledStateMatrix(t *testing.T) {
	originalResolver := resolveCodexExecutableSpec
	resolveCodexExecutableSpec = func(string, string) (codex.ExecutableSpec, error) {
		return codex.ExecutableSpec{}, errors.New("synthetic Codex failure")
	}
	t.Cleanup(func() { resolveCodexExecutableSpec = originalResolver })

	tests := []struct {
		name  string
		setup func(*testing.T, install.Paths)
		ok    bool
	}{
		{name: "absent", ok: true},
		{name: "lock only", setup: func(t *testing.T, paths install.Paths) {
			lock, err := state.NewStore(paths.StateDirectory).AcquireLock()
			if err != nil {
				t.Fatal(err)
			}
			if err := lock.Close(); err != nil {
				t.Fatal(err)
			}
		}, ok: true},
		{name: "partial config without state", setup: func(t *testing.T, paths install.Paths) {
			if err := install.NewDiskStore(paths).SaveConfig(config.Default("control")); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "partial state without config", setup: func(t *testing.T, paths install.Paths) {
			if err := install.NewDiskStore(paths).SaveState(state.New()); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "malformed config", setup: func(t *testing.T, paths install.Paths) {
			store := install.NewDiskStore(paths)
			if err := store.SaveState(state.New()); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(paths.Config, []byte("{\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "valid install", setup: func(t *testing.T, paths install.Paths) {
			store := install.NewDiskStore(paths)
			if err := store.SaveConfig(config.Default("control")); err != nil {
				t.Fatal(err)
			}
			if err := store.SaveState(state.New()); err != nil {
				t.Fatal(err)
			}
		}, ok: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			home := filepath.Join(t.TempDir(), "missing-home")
			paths := install.PathsForHomes(home, filepath.Join(home, "codex"))
			if test.setup != nil {
				test.setup(t, paths)
			}
			err := validateInstalledState(paths)
			if (err == nil) != test.ok {
				t.Fatalf("validateInstalledState() error=%v want ok=%t", err, test.ok)
			}
			result := (runtimeSelfTest{paths: paths}).Run(context.Background(), true)
			found := false
			for _, check := range result.Checks {
				if check.Name != "installed_state" {
					continue
				}
				found = true
				if check.OK != test.ok {
					t.Fatalf("installed_state=%+v want ok=%t", check, test.ok)
				}
			}
			if !found {
				t.Fatal("candidate self-test omitted installed_state")
			}
		})
	}
}

func TestCandidateSelfTestRejectsUnsupportedInstalledState(t *testing.T) {
	home := t.TempDir()
	paths := install.PathsForHomes(home, filepath.Join(home, "codex"))
	store := install.NewDiskStore(paths)
	if err := store.SaveConfig(config.Default("control")); err != nil {
		t.Fatal(err)
	}
	unsupported := []byte(`{"schema_version":2}` + "\n")
	if err := os.WriteFile(paths.State, unsupported, 0o600); err != nil {
		t.Fatal(err)
	}
	originalResolver := resolveCodexExecutableSpec
	resolveCodexExecutableSpec = func(string, string) (codex.ExecutableSpec, error) {
		return codex.ExecutableSpec{}, errors.New("synthetic Codex failure")
	}
	t.Cleanup(func() { resolveCodexExecutableSpec = originalResolver })
	result := (runtimeSelfTest{paths: paths}).Run(context.Background(), true)
	found := false
	for _, check := range result.Checks {
		if check.Name == "agents" || check.Name == "skill" {
			t.Fatalf("candidate self-test unexpectedly checked managed surface %q", check.Name)
		}
		if check.Name == "installed_state" {
			found = true
			if check.OK {
				t.Fatal("unsupported installed state passed candidate self-test")
			}
		}
	}
	if !found {
		t.Fatal("candidate self-test omitted installed state compatibility")
	}
	after, err := os.ReadFile(paths.State)
	if err != nil || string(after) != string(unsupported) {
		t.Fatalf("installed state mutated: %q err=%v", after, err)
	}
}

func TestParseUpdateExactVersion(t *testing.T) {
	request, err := parseRequest([]string{"update", "--version", "1.2.3", "--json"})
	if err != nil || request.Command != app.CommandUpdate || request.Version != "1.2.3" || !request.JSON {
		t.Fatalf("request=%+v err=%v", request, err)
	}
	if _, err := parseRequest([]string{"update", "--version", "v1.2.3"}); err == nil {
		t.Fatal("leading v update version was accepted")
	}
}

func TestRunExportsCandidateManagedAssetsOnlyWithCandidatePattern(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := run(context.Background(), []string{"managed-assets", "--candidate", "--json"}, &stdout, &stderr); code != 0 {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
	var exported install.ManagedAssets
	if err := json.Unmarshal(stdout.Bytes(), &exported); err != nil {
		t.Fatal(err)
	}
	if exported.Agents != assets.AgentsManagedContent || exported.Skill != assets.SkillManagedContent {
		t.Fatalf("exported=%+v", exported)
	}
	stdout.Reset()
	if code := run(context.Background(), []string{"managed-assets", "--json"}, &stdout, &stderr); code == 0 {
		t.Fatal("managed assets exported without candidate pattern")
	}
}

type healthyLaunchAgent struct{}

func (healthyLaunchAgent) Healthy(context.Context) (bool, error)      { return true, nil }
func (healthyLaunchAgent) Apply(context.Context, config.Config) error { return nil }
func (healthyLaunchAgent) Enable(context.Context) (bool, error)       { return false, nil }
func (healthyLaunchAgent) Disable(context.Context) (bool, error)      { return false, nil }

func TestInstalledSelfTestManagedDiagnosticsAreActionable(t *testing.T) {
	home := t.TempDir()
	paths := install.PathsForHomes(home, filepath.Join(home, "codex"))
	if err := os.MkdirAll(paths.CodexHome, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(paths.Binary), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.Binary, []byte("binary"), 0o700); err != nil {
		t.Fatal(err)
	}
	store := install.NewDiskStore(paths)
	cfg := config.Default("control")
	cfg.CodexExecutable = "/bin/sh"
	cfg.CodexSpawnPath = "/usr/bin:/bin"
	if err := store.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveState(state.New()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.Agents, []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	result := (runtimeSelfTest{paths: paths, launchAgent: healthyLaunchAgent{}}).Run(context.Background(), false)
	managedChecks := 0
	for _, check := range result.Checks {
		if check.Name != "agents" && check.Name != "skill" {
			continue
		}
		managedChecks++
		if check.OK || check.ErrorCode != "managed_surface_stale" || check.Remedy != "run threadbear update or threadbear configure" {
			t.Fatalf("check=%+v", check)
		}
	}
	if managedChecks != 2 {
		t.Fatalf("checks=%+v", result.Checks)
	}
}

func TestInstalledSelfTestManagedSymlinkReportsStaleDiagnostic(t *testing.T) {
	home := t.TempDir()
	paths := install.PathsForHomes(home, filepath.Join(home, "codex"))
	if err := os.MkdirAll(paths.CodexHome, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(paths.Binary), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.Binary, []byte("binary"), 0o700); err != nil {
		t.Fatal(err)
	}
	store := install.NewDiskStore(paths)
	cfg := config.Default("control")
	cfg.CodexExecutable = "/bin/sh"
	cfg.CodexSpawnPath = "/usr/bin:/bin"
	if err := store.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveState(state.New()); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(home, "unsafe-agents-target")
	if err := os.WriteFile(target, []byte("sensitive target"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, paths.Agents); err != nil {
		t.Fatal(err)
	}
	if err := install.WriteManagedBlock(paths.Skill, []byte(assets.SkillManagedContent)); err != nil {
		t.Fatal(err)
	}
	result := (runtimeSelfTest{paths: paths, launchAgent: healthyLaunchAgent{}}).Run(context.Background(), false)
	for _, check := range result.Checks {
		if check.Name == "agents" {
			if check.OK || check.ErrorCode != "managed_surface_stale" || check.Remedy != "run threadbear update or threadbear configure" {
				t.Fatalf("check=%+v", check)
			}
			data, err := json.Marshal(check)
			if err != nil || bytes.Contains(data, []byte(target)) {
				t.Fatalf("diagnostic exposed path: %s err=%v", data, err)
			}
			return
		}
	}
	t.Fatalf("checks=%+v", result.Checks)
}

func TestManagedSurfaceCheckRemediesAreConditionSpecific(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		code   string
		remedy string
	}{
		{name: "malformed", err: fmt.Errorf("private path: %w", install.ErrMalformedManagedBlock), code: "managed_surface_malformed", remedy: "replace or move aside the malformed managed file so it has no invalid ThreadBear markers, then rerun update or configure"},
		{name: "stale", err: install.ErrManagedSurfaceStale, code: "managed_surface_stale", remedy: "run threadbear update or threadbear configure"},
		{name: "unsafe", err: install.ErrUnsafeManagedPath, code: "managed_surface_unsafe_path", remedy: "replace the unsafe or symlinked managed path with a regular file, then rerun update or configure"},
		{name: "unavailable", err: errors.New("secret/path: permission denied"), code: "managed_surface_unavailable", remedy: "fix managed file access or permissions, then rerun update or configure"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			check := managedSurfaceCheck("agents", test.err)
			if check.ErrorCode != test.code || check.Remedy != test.remedy || strings.Contains(check.Remedy, "secret/path") {
				t.Fatalf("check=%+v", check)
			}
		})
	}
}

func TestInstalledSelfTestMalformedManagedSurfaceHasStablePathSafeDiagnostic(t *testing.T) {
	home := t.TempDir()
	paths := install.PathsForHomes(home, filepath.Join(home, "codex"))
	if err := os.MkdirAll(paths.CodexHome, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(paths.Binary), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.Binary, []byte("binary"), 0o700); err != nil {
		t.Fatal(err)
	}
	store := install.NewDiskStore(paths)
	cfg := config.Default("control")
	cfg.CodexExecutable = "/bin/sh"
	cfg.CodexSpawnPath = "/usr/bin:/bin"
	if err := store.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveState(state.New()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.Agents, []byte(install.ManagedBlockStart+"\nbroken\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := install.WriteManagedBlock(paths.Skill, []byte(assets.SkillManagedContent)); err != nil {
		t.Fatal(err)
	}
	result := (runtimeSelfTest{paths: paths, launchAgent: healthyLaunchAgent{}}).Run(context.Background(), false)
	for _, check := range result.Checks {
		if check.Name != "agents" {
			continue
		}
		wantRemedy := "replace or move aside the malformed managed file so it has no invalid ThreadBear markers, then rerun update or configure"
		if check.OK || check.ErrorCode != "managed_surface_malformed" || check.Remedy != wantRemedy || strings.Contains(check.Remedy, paths.Agents) || strings.Contains(check.Remedy, "permission") {
			t.Fatalf("check=%+v", check)
		}
		return
	}
	t.Fatalf("checks=%+v", result.Checks)
}

func TestInstallPrompterFailurePointsToSupportedPath(t *testing.T) {
	err := installPrompterFailure(errors.New("open /dev/tty: device not configured"))
	var failure *install.InstallFailure
	if !errors.As(err, &failure) {
		t.Fatalf("failure=%T %v", err, err)
	}
	if failure.Step != "open_prompter" {
		t.Fatalf("step=%q", failure.Step)
	}
	for _, text := range []string{"open /dev/tty: device not configured", "https://threadbear.sh/install", "--noninteractive --confirm"} {
		if !strings.Contains(failure.Cause, text) {
			t.Fatalf("cause=%q missing %q", failure.Cause, text)
		}
	}
	if strings.Index(failure.Cause, "https://threadbear.sh/install") > strings.Index(failure.Cause, "open /dev/tty: device not configured") {
		t.Fatalf("cause does not lead with supported guide: %q", failure.Cause)
	}
}

func TestParseInstallControlTaskAndDryRun(t *testing.T) {
	request, err := parseRequest([]string{"install", "--control-task-id", "task-home", "--dry-run"})
	if err != nil || request.ControlTaskID != "task-home" || !request.DryRun || request.Confirm {
		t.Fatalf("request=%+v err=%v", request, err)
	}
}

func TestRunFirstInstallWithoutControlTaskIDExitsTwoWithoutStateMutation(t *testing.T) {
	home := t.TempDir()
	codexHome := filepath.Join(home, "codex")
	if err := os.MkdirAll(codexHome, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", codexHome)
	originalResolver := resolveCodexExecutableSpec
	resolveCodexExecutableSpec = func(string, string) (codex.ExecutableSpec, error) {
		return codex.ExecutableSpec{Path: "/bin/sh", SpawnPath: "/usr/bin:/bin"}, nil
	}
	t.Cleanup(func() { resolveCodexExecutableSpec = originalResolver })
	var stdout, stderr strings.Builder
	code := run(context.Background(), []string{"install", "--noninteractive", "--confirm"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	combined := stdout.String() + stderr.String()
	if !strings.Contains(combined, "Codex") || !strings.Contains(combined, "INSTALL.md") || !strings.Contains(combined, "--control-task-id") {
		t.Fatalf("missing friendly install guidance: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	stateDirectory := filepath.Join(home, ".local", "share", "threadbear")
	if _, err := os.Stat(stateDirectory); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("state directory mutated: %v", err)
	}
}

type productionSchedulerRunner struct {
	calls  []string
	loaded bool
}

func (r *productionSchedulerRunner) Run(_ context.Context, _ string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, strings.Join(args, " "))
	switch args[0] {
	case "print-disabled":
		return []byte(`"org.litman.threadbear" => true`), nil
	case "print":
		if r.loaded {
			return []byte("loaded"), nil
		}
		return []byte("Could not find service"), errors.New("exit status 113")
	case "enable":
		return nil, nil
	case "bootstrap":
		r.loaded = true
		return nil, nil
	case "kickstart":
		return nil, nil
	default:
		return nil, fmt.Errorf("unexpected launchctl command %q", args[0])
	}
}

func TestProductionSchedulerEnableDoesNotKickstart(t *testing.T) {
	home := t.TempDir()
	runner := &productionSchedulerRunner{}
	adapter, err := launchagent.New(launchagent.Options{Home: home, BinaryPath: filepath.Join(home, "threadbear"), UID: 501, Runner: runner})
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default("control")
	cfg.CodexExecutable = "/bin/sh"
	cfg.CodexSpawnPath = "/usr/bin:/bin"
	if _, err := adapter.Stage(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	start := len(runner.calls)
	changed, err := (productionScheduler{adapter: adapter}).Enable(context.Background())
	if err != nil || !changed {
		t.Fatalf("Enable=%t, %v", changed, err)
	}
	calls := strings.Join(runner.calls[start:], "\n")
	if !strings.Contains(calls, "bootstrap ") || strings.Contains(calls, "kickstart ") {
		t.Fatalf("production scheduler calls=%s", calls)
	}
}

type welcomeControlTaskClientFake struct {
	found   bool
	reads   int
	inserts int
}

func (c *welcomeControlTaskClientFake) ReadPersistedAssistantMessage(context.Context, string, string) (appserver.PersistedMessageResult, error) {
	c.reads++
	return appserver.PersistedMessageResult{Found: c.found}, nil
}

func (c *welcomeControlTaskClientFake) InsertNotice(context.Context, string, string) error {
	c.inserts++
	c.found = true
	return nil
}

func TestPostWelcomeOnceUsesPersistedReadback(t *testing.T) {
	client := &welcomeControlTaskClientFake{}
	if err := postWelcomeOnce(context.Background(), client, "home", "welcome"); err != nil {
		t.Fatal(err)
	}
	if err := postWelcomeOnce(context.Background(), client, "home", "welcome"); err != nil {
		t.Fatal(err)
	}
	if client.reads != 2 || client.inserts != 1 {
		t.Fatalf("client=%+v", client)
	}
}

type hostedWaitInventoryFake struct {
	tasks       []codex.Task
	calls       int
	err         error
	block       bool
	hadDeadline bool
}

func (f *hostedWaitInventoryFake) Inventory(ctx context.Context, _ string) (codex.Inventory, error) {
	f.calls++
	_, f.hadDeadline = ctx.Deadline()
	if f.block {
		<-ctx.Done()
		return codex.Inventory{}, ctx.Err()
	}
	return codex.Inventory{Tasks: append([]codex.Task(nil), f.tasks...)}, f.err
}

type hostedWaitClientFake struct {
	reads       int
	activeReads int
	err         error
	block       bool
	hadDeadline bool
	closed      bool
}

func (f *hostedWaitClientFake) ReadLatestTurn(ctx context.Context, _, _ string) (appserver.RecentEvidence, error) {
	f.reads++
	_, f.hadDeadline = ctx.Deadline()
	if f.block {
		<-ctx.Done()
		return appserver.RecentEvidence{}, ctx.Err()
	}
	if f.err != nil {
		return appserver.RecentEvidence{}, f.err
	}
	if f.reads <= f.activeReads {
		return appserver.RecentEvidence{ThreadStatus: appserver.ThreadStatus{Type: "active"}}, nil
	}
	return appserver.RecentEvidence{ThreadStatus: appserver.ThreadStatus{Type: "idle"}}, nil
}

func (f *hostedWaitClientFake) Close() error { f.closed = true; return nil }

func TestRetiredTitlePlanCompatibilityIsHiddenAndFailClosed(t *testing.T) {
	request, err := parseRequest([]string{"title-plan", "--json", "--dispatch"})
	if err != nil || !request.TitlePlanDispatch {
		t.Fatalf("request=%+v err=%v", request, err)
	}
	for _, args := range [][]string{{"title-plan", "--dispatch"}, {"title-plan", "--json"}, {"title-plan", "--json", "--batch"}} {
		if _, err := parseRequest(args); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	if strings.Contains(renderTopLevelHelp(), "title-plan") {
		t.Fatal("retired compatibility command is visible in help")
	}
	home := t.TempDir()
	codexHome := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexHome, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", codexHome)
	var stdout, stderr bytes.Buffer
	if code := run(context.Background(), []string{"title-plan", "--json", "--dispatch"}, &stdout, &stderr); code != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if got, want := stdout.String(), "{\"version\":1,\"allow\":false,\"disposition\":\"retired\"}\n"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestManagedTitleBatchCommandIsHiddenAndStrict(t *testing.T) {
	request, err := parseRequest([]string{"title-batch", "--json", "--list"})
	if err != nil || !request.TitleBatchList {
		t.Fatalf("request=%+v err=%v", request, err)
	}
	operation, err := parseRequest([]string{"title-batch", "--json", "--operation", "op"})
	if err != nil || operation.TitleBatchOperation != "op" {
		t.Fatalf("operation=%+v err=%v", operation, err)
	}
	for _, args := range [][]string{{"title-batch", "--list"}, {"title-batch", "--json"}, {"title-batch", "--json", "--list", "--report"}} {
		if _, err := parseRequest(args); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	if strings.Contains(renderTopLevelHelp(), "title-batch") {
		t.Fatal("managed title batch command is visible in help")
	}
}
