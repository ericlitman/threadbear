package install

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ericlitman/threadbear/assets"
	"github.com/ericlitman/threadbear/internal/codex"

	"github.com/ericlitman/threadbear/internal/config"
	"github.com/ericlitman/threadbear/internal/state"
	"github.com/ericlitman/threadbear/internal/tokens"
)

type fakeLock struct {
	closed  int
	onClose func()
}

func (l *fakeLock) Close() error {
	l.closed++
	if l.onClose != nil {
		l.onClose()
	}
	return nil
}

type fakeStore struct {
	config       config.Config
	state        state.State
	exists       bool
	configExists bool
	stateExists  bool
	saveStateErr error
	locks        int
	saveConfig   int
	saveState    int
	onLockClose  func()
	onAcquire    func()
	lockHeld     bool
}

func (s *fakeStore) AcquireLock() (Lock, error) {
	s.locks++
	s.lockHeld = true
	if s.onAcquire != nil {
		s.onAcquire()
	}
	return &fakeLock{onClose: func() {
		s.lockHeld = false
		if s.onLockClose != nil {
			s.onLockClose()
		}
	}}, nil
}
func (s *fakeStore) LoadConfig() (config.Config, error) {
	if !s.configExists && s.config.SchemaVersion == 0 {
		return config.Config{}, fs.ErrNotExist
	}
	return s.config, nil
}
func (s *fakeStore) SaveConfig(v config.Config) error {
	s.config = v
	s.exists = true
	s.configExists = true
	s.saveConfig++
	return nil
}
func (s *fakeStore) LoadState() (state.State, error) {
	if !s.stateExists && s.state.SchemaVersion == 0 {
		return state.State{}, fs.ErrNotExist
	}
	return s.state, nil
}
func (s *fakeStore) SaveState(v state.State) error {
	s.saveState++
	if s.saveStateErr != nil {
		err := s.saveStateErr
		s.saveStateErr = nil
		return err
	}
	s.state = v
	s.stateExists = true
	s.exists = true
	return nil
}

type fakeScheduler struct {
	calls      []string
	disabled   bool
	loaded     bool
	loadedErr  error
	stageCalls int
	stageErrAt int
	stageErr   error
}

func (s *fakeScheduler) Stage(context.Context, config.Config) (bool, error) {
	s.calls = append(s.calls, "stage")
	s.stageCalls++
	if s.stageCalls == s.stageErrAt && s.stageErr != nil {
		return false, s.stageErr
	}
	s.disabled = true
	s.loaded = false
	return false, nil
}
func (s *fakeScheduler) VerifyHealthy(context.Context) error {
	s.calls = append(s.calls, "healthy")
	return nil
}
func (s *fakeScheduler) Enable(context.Context) (bool, error) {
	s.calls = append(s.calls, "enable")
	s.disabled = false
	s.loaded = true
	return false, nil
}
func (s *fakeScheduler) Loaded(context.Context) (bool, error) {
	s.calls = append(s.calls, "loaded")
	return s.loaded, s.loadedErr
}
func (s *fakeScheduler) Remove(context.Context) error {
	s.calls = append(s.calls, "remove")
	s.loaded = false
	return nil
}

type fakeTasks struct {
	tasks       map[string]ControlTask
	readErr     map[string]error
	reads       []string
	unarchived  []string
	archived    []string
	archivedIDs map[string]bool
	welcomes    []string
	welcomeErr  error
}

func (t *fakeTasks) ReadControlTask(_ context.Context, id string) (ControlTask, error) {
	t.reads = append(t.reads, id)
	if err := t.readErr[id]; err != nil {
		return ControlTask{}, err
	}
	if task, ok := t.tasks[id]; ok {
		return task, nil
	}
	if t.tasks == nil {
		return ControlTask{ID: id}, nil
	}
	return ControlTask{}, errors.New("task not found")
}
func (t *fakeTasks) UnarchiveControlTask(_ context.Context, id string) (bool, error) {
	t.unarchived = append(t.unarchived, id)
	task := t.tasks[id]
	if !task.Archived {
		return false, nil
	}
	task.Archived = false
	t.tasks[id] = task
	return true, nil
}
func (t *fakeTasks) PostWelcome(_ context.Context, taskID, text string) error {
	if t.welcomeErr != nil {
		return t.welcomeErr
	}
	t.welcomes = append(t.welcomes, taskID+"\n"+text)
	return nil
}
func (t *fakeTasks) ArchiveControlTask(_ context.Context, id string) (bool, error) {
	t.archived = append(t.archived, id)
	if t.archivedIDs == nil {
		t.archivedIDs = map[string]bool{}
	}
	if t.archivedIDs[id] {
		return false, nil
	}
	t.archivedIDs[id] = true
	return true, nil
}

type fakeBinary struct{ calls int }

func (b *fakeBinary) Install(path string) error {
	b.calls++
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte("binary"), 0o700)
}

type fakeSelfTest struct {
	calls  int
	inputs []SelfTestInput
}

func (s *fakeSelfTest) Test(_ context.Context, input SelfTestInput) error {
	s.calls++
	s.inputs = append(s.inputs, input)
	return nil
}

type fakePrompter struct {
	collected      Preferences
	collect        bool
	previews       int
	confirms       int
	confirmed      bool
	choices        []bool
	choiceDefaults []bool
	messages       []string
	events         []string
	confirmDefault bool
}

func (p *fakePrompter) Collect(v Preferences) (Preferences, error) {
	p.collect = true
	if p.collected.HeartbeatSeconds != 0 {
		return p.collected, nil
	}
	return v, nil
}
func (p *fakePrompter) ShowMessage(message string) error {
	p.messages = append(p.messages, message)
	p.events = append(p.events, "message")
	return nil
}
func (p *fakePrompter) ShowPreview(Preview) error {
	p.previews++
	p.events = append(p.events, "preview")
	return nil
}
func (p *fakePrompter) Confirm(defaultYes bool) (bool, error) {
	p.confirms++
	p.confirmDefault = defaultYes
	p.events = append(p.events, "confirm")
	return p.confirmed, nil
}
func (p *fakePrompter) Choose(label string, defaultYes bool) (bool, error) {
	p.choiceDefaults = append(p.choiceDefaults, defaultYes)
	p.events = append(p.events, "choose:"+label)
	v := p.choices[0]
	p.choices = p.choices[1:]
	return v, nil
}

type lifecycleSelfTest struct {
	scheduler *fakeScheduler
	inputs    []SelfTestInput
}

func (s *lifecycleSelfTest) Test(_ context.Context, input SelfTestInput) error {
	s.inputs = append(s.inputs, input)
	if input.Candidate && (!s.scheduler.disabled || s.scheduler.loaded) {
		return errors.New("candidate ThreadBear scheduler was not disabled and unloaded")
	}
	if !input.Candidate && (s.scheduler.disabled || !s.scheduler.loaded) {
		return errors.New("installed ThreadBear scheduler was not enabled and loaded")
	}
	return nil
}

func testCodexExecutable(t *testing.T, home string) string {
	t.Helper()
	path := filepath.Join(home, "bin", "codex")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func newInstaller(t *testing.T, store *fakeStore, scheduler *fakeScheduler, tasks *fakeTasks, prompt Prompter) Installer {
	t.Helper()
	paths := PathsForHome(t.TempDir())
	return Installer{Paths: paths, Store: store, Scheduler: scheduler, ControlTasks: tasks, Binary: &fakeBinary{}, SelfTester: &fakeSelfTest{}, Prompter: prompt, CodexExecutable: testCodexExecutable(t, paths.Home)}
}

func TestPathsForHome(t *testing.T) {
	home := "/tmp/threadbear-home"
	p := PathsForHome(home)
	want := map[string]string{
		"binary": home + "/.local/bin/threadbear", "state": home + "/.local/share/threadbear",
		"agents": home + "/.codex/AGENTS.md", "skill": home + "/.codex/skills/threadbear/SKILL.md",
		"launch": home + "/Library/LaunchAgents/org.litman.threadbear.plist",
	}
	got := map[string]string{"binary": p.Binary, "state": p.StateDirectory, "agents": p.Agents, "skill": p.Skill, "launch": p.LaunchAgent}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("paths=%v", got)
	}
}

func TestPathsForHomesUsesResolvedCodexHome(t *testing.T) {
	home := "/tmp/threadbear-home"
	codexHome := "/tmp/threadbear-codex"
	paths := PathsForHomes(home, codexHome)
	if paths.CodexHome != codexHome || paths.Agents != codexHome+"/AGENTS.md" || paths.Skill != codexHome+"/skills/threadbear/SKILL.md" {
		t.Fatalf("paths=%+v", paths)
	}
}

func TestInstallDefaultsAndOneConfirmation(t *testing.T) {
	store := &fakeStore{}
	scheduler := &fakeScheduler{}
	tasks := &fakeTasks{}
	prompt := &fakePrompter{confirmed: true}
	installer := newInstaller(t, store, scheduler, tasks, prompt)
	result, err := installer.Install(context.Background(), InstallRequest{ControlTaskID: "control-new"})
	if err != nil {
		t.Fatal(err)
	}
	defaults := DefaultPreferences()
	if result.Config.HeartbeatSeconds != defaults.HeartbeatSeconds || result.Config.ClassifierModel != defaults.ClassifierModel {
		t.Fatalf("config=%+v", result.Config)
	}
	if prompt.previews != 1 || prompt.confirms != 1 || !prompt.collect {
		t.Fatalf("prompt=%+v", prompt)
	}
	if !reflect.DeepEqual(scheduler.calls, []string{"stage", "stage", "enable", "healthy"}) {
		t.Fatalf("calls=%v", scheduler.calls)
	}
	if len(tasks.reads) != 2 || store.saveConfig != 1 || store.saveState != 2 {
		t.Fatalf("reads=%v saves=%d/%d", tasks.reads, store.saveConfig, store.saveState)
	}
}

func TestInstallCustomNoninteractiveAndCancellation(t *testing.T) {
	custom := DefaultPreferences()
	custom.HeartbeatSeconds = 42
	custom.ArchiveEnabled = false
	custom.ClassifierEffort = config.EffortHigh
	custom.ClassifierContextBudgetBytes = 1234
	store := &fakeStore{}
	scheduler := &fakeScheduler{}
	tasks := &fakeTasks{}
	installer := newInstaller(t, store, scheduler, tasks, nil)
	if _, err := installer.Install(context.Background(), InstallRequest{ControlTaskID: "control-new", NonInteractive: true, Preferences: &custom}); err == nil {
		t.Fatal("missing confirmation accepted")
	}
	result, err := installer.Install(context.Background(), InstallRequest{ControlTaskID: "control-new", NonInteractive: true, Confirm: true, Preferences: &custom})
	if err != nil {
		t.Fatal(err)
	}
	if result.Config.HeartbeatSeconds != 42 || result.Config.ArchiveEnabled || result.Config.ClassifierEffort != config.EffortHigh {
		t.Fatalf("config=%+v", result.Config)
	}
	cancelStore := &fakeStore{}
	prompt := &fakePrompter{confirmed: false}
	cancel := newInstaller(t, cancelStore, &fakeScheduler{}, &fakeTasks{}, prompt)
	if _, err := cancel.Install(context.Background(), InstallRequest{ControlTaskID: "control-new"}); !errors.Is(err, ErrCancelled) {
		t.Fatalf("error=%v", err)
	}
	if cancelStore.locks != 0 || prompt.confirms != 1 || prompt.previews != 1 {
		t.Fatalf("mutated on cancel store=%+v prompt=%+v", cancelStore, prompt)
	}
}

func TestReinstallAdoptsExistingControlTaskAndState(t *testing.T) {
	cfg := config.Default("control-existing")
	original := state.New()
	original.Generation = 9
	store := &fakeStore{config: cfg, state: original, exists: true}
	scheduler := &fakeScheduler{}
	tasks := &fakeTasks{}
	installer := newInstaller(t, store, scheduler, tasks, nil)
	first, err := installer.Install(context.Background(), InstallRequest{NonInteractive: true, Confirm: true})
	if err != nil {
		t.Fatal(err)
	}
	second, err := installer.Install(context.Background(), InstallRequest{NonInteractive: true, Confirm: true})
	if err != nil {
		t.Fatal(err)
	}
	if !first.Reinstalled || !second.Reinstalled || len(tasks.reads) != 4 {
		t.Fatalf("results=%+v %+v reads=%v", first, second, tasks.reads)
	}
	if tasks.reads[0] != "control-existing" || store.state.Generation != 9 {
		t.Fatalf("reads=%v state=%+v", tasks.reads, store.state)
	}
}

func TestReinstallBackfillsCodexExecutable(t *testing.T) {
	cfg := config.Default("control-existing")
	store := &fakeStore{config: cfg, state: state.New(), configExists: true, stateExists: true}
	installer := newInstaller(t, store, &fakeScheduler{}, &fakeTasks{}, nil)
	result, err := installer.Install(context.Background(), InstallRequest{NonInteractive: true, Confirm: true})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Reinstalled || result.Config.CodexExecutable != installer.CodexExecutable || store.config.CodexExecutable != installer.CodexExecutable || store.saveConfig != 1 {
		t.Fatalf("result=%+v store=%+v", result, store)
	}
}

func TestInstallSelfTestPrecedesEnableAndHealthVerification(t *testing.T) {
	store := &fakeStore{}
	scheduler := &fakeScheduler{}
	selfTest := &orderedSelfTest{scheduler: scheduler}
	installer := newInstaller(t, store, scheduler, &fakeTasks{}, nil)
	installer.SelfTester = selfTest
	if _, err := installer.Install(context.Background(), InstallRequest{ControlTaskID: "control-new", NonInteractive: true, Confirm: true}); err != nil {
		t.Fatal(err)
	}
	want := []string{"stage", "selftest", "stage", "enable", "healthy", "selftest"}
	if !reflect.DeepEqual(scheduler.calls, want) {
		t.Fatalf("calls=%v want=%v", scheduler.calls, want)
	}
}

type orderedSelfTest struct{ scheduler *fakeScheduler }

func (s *orderedSelfTest) Test(context.Context, SelfTestInput) error {
	s.scheduler.calls = append(s.scheduler.calls, "selftest")
	return nil
}

func TestFileBinaryInstallerAllowsSymlinkedSourceAncestorAndRejectsDestinationSymlink(t *testing.T) {
	root := t.TempDir()
	realDirectory := filepath.Join(root, "real")
	if err := os.MkdirAll(realDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(realDirectory, "candidate")
	if err := os.WriteFile(source, []byte("candidate"), 0o700); err != nil {
		t.Fatal(err)
	}
	linkedDirectory := filepath.Join(root, "linked")
	if err := os.Symlink(realDirectory, linkedDirectory); err != nil {
		t.Fatal(err)
	}
	installer := FileBinaryInstaller{Source: filepath.Join(linkedDirectory, "candidate")}
	destination := filepath.Join(root, "bin", "threadbear")
	if err := installer.Install(destination); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(destination)
	if err != nil || string(data) != "candidate" {
		t.Fatalf("destination=%q err=%v", data, err)
	}
	victim := filepath.Join(root, "victim")
	if err := os.WriteFile(victim, []byte("safe"), 0o600); err != nil {
		t.Fatal(err)
	}
	linkedDestination := filepath.Join(root, "linked-destination")
	if err := os.Symlink(victim, linkedDestination); err != nil {
		t.Fatal(err)
	}
	if err := installer.Install(linkedDestination); !errors.Is(err, ErrUnsafeManagedPath) {
		t.Fatalf("destination symlink error=%v", err)
	}
	data, err = os.ReadFile(victim)
	if err != nil || string(data) != "safe" {
		t.Fatalf("victim=%q err=%v", data, err)
	}
}

func TestNoOpReinstallReportsUnchanged(t *testing.T) {
	home := t.TempDir()
	paths := PathsForHome(home)
	source := filepath.Join(home, "candidate")
	if err := os.WriteFile(source, []byte("same"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(paths.Binary), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.Binary, []byte("same"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := WriteManagedBlock(paths.Agents, []byte(assets.AgentsManagedContent)); err != nil {
		t.Fatal(err)
	}
	if err := WriteManagedBlock(paths.Skill, []byte(assets.SkillManagedContent)); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default("control-existing")
	committed := state.New()
	store := &fakeStore{config: cfg, state: committed, exists: true, configExists: true, stateExists: true}
	codexExecutable := testCodexExecutable(t, home)
	spec, err := codex.DeriveExecutableSpec(home, codexExecutable, os.Getenv("PATH"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.CodexExecutable = codexExecutable
	cfg.CodexSpawnPath = spec.SpawnPath
	store.config = cfg
	installer := Installer{Paths: paths, Store: store, Scheduler: &fakeScheduler{}, ControlTasks: &fakeTasks{}, Binary: FileBinaryInstaller{Source: source}, SelfTester: &fakeSelfTest{}, CodexExecutable: codexExecutable, CodexSpawnPath: spec.SpawnPath}
	result, err := installer.Install(context.Background(), InstallRequest{NonInteractive: true, Confirm: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Changed || len(result.Resources) != 0 {
		t.Fatalf("no-op reinstall result=%+v", result)
	}
}

func TestNoninteractiveInstallPublishesPreviewBeforeMutation(t *testing.T) {
	store := &fakeStore{}
	tasks := &fakeTasks{}
	installer := newInstaller(t, store, &fakeScheduler{}, tasks, nil)
	previews := 0
	installer.Previewer = func(preview Preview) error {
		previews++
		if store.locks != 0 || len(tasks.reads) != 1 || len(preview.Lines) < 8 {
			t.Fatalf("preview after mutation or incomplete: store=%+v tasks=%+v preview=%+v", store, tasks, preview)
		}
		foundMutation := false
		for _, line := range preview.Lines {
			if strings.Contains(line, installer.Paths.Agents+": write managed block") {
				foundMutation = true
			}
		}
		if !foundMutation {
			t.Fatalf("managed-block mutation missing: %+v", preview)
		}
		return nil
	}
	if _, err := installer.Install(context.Background(), InstallRequest{ControlTaskID: "control-new", NonInteractive: true, Confirm: true}); err != nil {
		t.Fatal(err)
	}
	if previews != 1 {
		t.Fatalf("previews=%d", previews)
	}
}

type selfTestProbeFake struct {
	major      int
	err        error
	codexHome  *string
	executable *string
}

func (p selfTestProbeFake) Platform() (string, string, int) { return "darwin", "arm64", p.major }
func (p selfTestProbeFake) ValidateCodex(_ context.Context, _ string, codexHome string, spec codex.ExecutableSpec) error {
	if p.codexHome != nil {
		*p.codexHome = codexHome
	}
	if p.executable != nil {
		*p.executable = spec.Path
	}
	return p.err
}

func TestCoreSelfTestRequiresSupportedHealthyPrivateSurfacesWithoutStateMutation(t *testing.T) {
	home := t.TempDir()
	paths := PathsForHomes(home, filepath.Join(home, "custom-codex"))
	cfg := config.Default("control")
	cfg.CodexExecutable = testCodexExecutable(t, paths.Home)
	cfg.CodexSpawnPath = filepath.Dir(cfg.CodexExecutable)
	committed := state.New()
	store := &fakeStore{config: cfg, state: committed, exists: true, configExists: true, stateExists: true}
	for _, directory := range []string{paths.StateDirectory, filepath.Dir(paths.Binary)} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(paths.CodexHome, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.Binary, []byte("binary"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := WriteManagedBlock(paths.Agents, []byte(assets.AgentsManagedContent)); err != nil {
		t.Fatal(err)
	}
	if err := WriteManagedBlock(paths.Skill, []byte(assets.SkillManagedContent)); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(paths.LaunchAgent), 0o700); err != nil {
		t.Fatal(err)
	}
	plist := fmt.Sprintf("<plist><dict><string>%s</string><key>StartInterval</key><integer>%d</integer></dict></plist>", config.LaunchAgentLabel, cfg.HeartbeatSeconds)
	if err := os.WriteFile(paths.LaunchAgent, []byte(plist), 0o600); err != nil {
		t.Fatal(err)
	}
	var probedHome, probedExecutable string
	tester := CoreSelfTester{Probe: selfTestProbeFake{major: 12, codexHome: &probedHome, executable: &probedExecutable}, Store: store}
	if err := tester.Test(context.Background(), SelfTestInput{Paths: paths, Config: cfg, State: committed}); err != nil {
		t.Fatal(err)
	}
	if probedHome != paths.CodexHome || probedExecutable != cfg.CodexExecutable {
		t.Fatalf("probe home=%q executable=%q", probedHome, probedExecutable)
	}
	if store.saveConfig != 0 || store.saveState != 0 {
		t.Fatalf("self-test mutated store: %+v", store)
	}
	if err := os.WriteFile(paths.Agents, []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := tester.Test(context.Background(), SelfTestInput{Paths: paths, Config: cfg, State: committed, Candidate: true}); err == nil || !strings.Contains(err.Error(), "AGENTS surface validation") {
		t.Fatalf("candidate install self-test skipped staged managed surface: %v", err)
	}
	if err := WriteManagedBlock(paths.Agents, []byte(assets.AgentsManagedContent)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(paths.StateDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := tester.Test(context.Background(), SelfTestInput{Paths: paths, Config: cfg, State: committed}); err == nil {
		t.Fatal("public state directory accepted")
	}
	if err := os.Chmod(paths.StateDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	tester.Probe = selfTestProbeFake{major: 11}
	if err := tester.Test(context.Background(), SelfTestInput{Paths: paths, Config: cfg, State: committed}); err == nil {
		t.Fatal("macOS 11 accepted")
	}
}

func TestInteractiveInstallShowsPreviewExactlyOnce(t *testing.T) {
	prompt := &fakePrompter{confirmed: true}
	installer := newInstaller(t, &fakeStore{}, &fakeScheduler{}, &fakeTasks{}, prompt)
	extra := 0
	installer.Previewer = func(Preview) error { extra++; return nil }
	if _, err := installer.Install(context.Background(), InstallRequest{ControlTaskID: "control-new"}); err != nil {
		t.Fatal(err)
	}
	if prompt.previews != 1 || extra != 0 {
		t.Fatalf("tty previews=%d fallback previews=%d", prompt.previews, extra)
	}
}

func TestInstallUsesOnlyExplicitTempHome(t *testing.T) {
	sentinel := t.TempDir()
	marker := filepath.Join(sentinel, "keep")
	if err := os.WriteFile(marker, []byte("sentinel"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", sentinel)
	installer := newInstaller(t, &fakeStore{}, &fakeScheduler{}, &fakeTasks{}, nil)
	if _, err := installer.Install(context.Background(), InstallRequest{ControlTaskID: "control-new", NonInteractive: true, Confirm: true}); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(sentinel)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "keep" {
		t.Fatalf("ambient HOME mutated: %v", entries)
	}
}

func TestInstallMissingCodexFailsBeforeMutation(t *testing.T) {
	home := t.TempDir()
	paths := PathsForHome(home)
	t.Setenv("PATH", filepath.Join(home, "missing-bin"))
	store := NewDiskStore(paths)
	installer := Installer{Paths: paths, Store: store, Scheduler: &fakeScheduler{}, ControlTasks: &fakeTasks{}, Binary: &fakeBinary{}, SelfTester: &fakeSelfTest{}, ResolveCodexExecutableSpec: codex.ResolveExecutableSpec}
	_, err := installer.Install(context.Background(), InstallRequest{ControlTaskID: "control-new", NonInteractive: true, Confirm: true})
	var failure *InstallFailure
	if !errors.As(err, &failure) || failure.Step != "resolve_codex_executable" || !strings.Contains(failure.Cause, "install the Codex CLI") {
		t.Fatalf("error=%v failure=%+v", err, failure)
	}
	if _, statErr := os.Stat(paths.StateDirectory); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("state directory created: %v", statErr)
	}
}

func TestReinstallReloadsExistingStateAfterLock(t *testing.T) {
	cfg := config.Default("control-existing")
	current := state.New()
	current.Generation = 1
	store := &fakeStore{config: cfg, state: current, configExists: true, stateExists: true}
	store.onAcquire = func() { store.state.Generation = 9 }
	installer := newInstaller(t, store, &fakeScheduler{}, &fakeTasks{}, nil)
	result, err := installer.Install(context.Background(), InstallRequest{NonInteractive: true, Confirm: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.State.Generation != 9 || store.state.Generation != 9 || store.saveState != 0 {
		t.Fatalf("result=%+v store=%+v", result.State, store)
	}
}

func TestReinstallRestoresBinaryModeWhenBytesMatch(t *testing.T) {
	home := t.TempDir()
	paths := PathsForHome(home)
	source := filepath.Join(home, "candidate")
	for _, path := range []string{source, paths.Binary} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("same"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(paths.Binary, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default("control-existing")
	codexExecutable := testCodexExecutable(t, home)
	cfg.CodexExecutable = codexExecutable
	spec, err := codex.DeriveExecutableSpec(home, codexExecutable, os.Getenv("PATH"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.CodexSpawnPath = spec.SpawnPath
	store := &fakeStore{config: cfg, state: state.New(), configExists: true, stateExists: true}
	installer := Installer{Paths: paths, Store: store, Scheduler: &fakeScheduler{}, ControlTasks: &fakeTasks{}, Binary: FileBinaryInstaller{Source: source}, SelfTester: &fakeSelfTest{}, CodexExecutable: codexExecutable}
	result, err := installer.Install(context.Background(), InstallRequest{NonInteractive: true, Confirm: true})
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(paths.Binary)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 || !containsResource(result.Resources, "binary") {
		t.Fatalf("mode=%o result=%+v", info.Mode().Perm(), result)
	}
}

func containsResource(resources []string, target string) bool {
	for _, resource := range resources {
		if resource == target {
			return true
		}
	}
	return false
}

type executableSpecTasks struct {
	*fakeTasks
	spec codex.ExecutableSpec
}

func (t *executableSpecTasks) SetCodexExecutableSpec(spec codex.ExecutableSpec) {
	t.spec = spec
}

func writeEnvNodeCodex(t *testing.T, root string) (string, string) {
	t.Helper()
	codexDirectory := filepath.Join(root, "codex-bin")
	nodeDirectory := filepath.Join(root, "node-bin")
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
	return executable, nodeDirectory
}

func TestReinstallPrefersPersistedExecutableOutsideCurrentPath(t *testing.T) {
	home := t.TempDir()
	paths := PathsForHome(home)
	persisted := testCodexExecutable(t, filepath.Join(home, "persisted"))
	fresh := testCodexExecutable(t, filepath.Join(home, "fresh"))
	t.Setenv("PATH", filepath.Join(home, "missing"))
	cfg := config.Default("control-existing")
	cfg.CodexExecutable = persisted
	store := &fakeStore{config: cfg, state: state.New(), configExists: true, stateExists: true}
	freshCalls := 0
	installer := Installer{Paths: paths, Store: store, Scheduler: &fakeScheduler{}, ControlTasks: &fakeTasks{}, Binary: &fakeBinary{}, SelfTester: &fakeSelfTest{}, CodexExecutable: fresh, ResolveCodexExecutableSpec: func(string, string) (codex.ExecutableSpec, error) {
		freshCalls++
		return codex.ExecutableSpec{}, errors.New("fresh resolution must not run")
	}}
	result, err := installer.Install(context.Background(), InstallRequest{NonInteractive: true, Confirm: true})
	if err != nil {
		t.Fatal(err)
	}
	if freshCalls != 0 || result.Config.CodexExecutable != persisted || result.Config.CodexSpawnPath == "" {
		t.Fatalf("fresh=%d config=%+v", freshCalls, result.Config)
	}
}

func TestInstallPersistsResolvedCodexSpawnContract(t *testing.T) {
	home := t.TempDir()
	paths := PathsForHome(home)
	executable, nodeDirectory := writeEnvNodeCodex(t, home)
	t.Setenv("PATH", strings.Join([]string{filepath.Dir(executable), nodeDirectory}, string(os.PathListSeparator)))
	store := &fakeStore{}
	tasks := &executableSpecTasks{fakeTasks: &fakeTasks{}}
	installer := Installer{
		Paths: paths, Store: store, Scheduler: &fakeScheduler{}, ControlTasks: tasks,
		Binary: &fakeBinary{}, SelfTester: &fakeSelfTest{},
		ResolveCodexExecutableSpec: codex.ResolveExecutableSpec,
	}
	result, err := installer.Install(context.Background(), InstallRequest{ControlTaskID: "control-new", NonInteractive: true, Confirm: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Config.CodexExecutable != executable || result.Config.CodexSpawnPath == "" {
		t.Fatalf("config=%+v", result.Config)
	}
	if !reflect.DeepEqual(result.Config.CodexSpawnPath, tasks.spec.SpawnPath) || tasks.spec.Path != executable {
		t.Fatalf("stored=%v control-task spec=%+v", result.Config.CodexSpawnPath, tasks.spec)
	}
	if result.Config.CodexSpawnPath != strings.Join([]string{filepath.Dir(executable), nodeDirectory}, string(os.PathListSeparator)) {
		t.Fatalf("spawn path=%v", result.Config.CodexSpawnPath)
	}
}

func TestReinstallDerivesMissingSpawnPathBeforeFreshResolution(t *testing.T) {
	home := t.TempDir()
	paths := PathsForHome(home)
	executable, nodeDirectory := writeEnvNodeCodex(t, home)
	t.Setenv("PATH", strings.Join([]string{filepath.Dir(executable), nodeDirectory}, string(os.PathListSeparator)))
	cfg := config.Default("control-existing")
	cfg.CodexExecutable = executable
	store := &fakeStore{config: cfg, state: state.New(), configExists: true, stateExists: true}
	freshCalls := 0
	installer := Installer{
		Paths: paths, Store: store, Scheduler: &fakeScheduler{}, ControlTasks: &fakeTasks{},
		Binary: &fakeBinary{}, SelfTester: &fakeSelfTest{},
		ResolveCodexExecutableSpec: func(string, string) (codex.ExecutableSpec, error) {
			freshCalls++
			return codex.ExecutableSpec{}, errors.New("fresh resolution must not run")
		},
	}
	result, err := installer.Install(context.Background(), InstallRequest{NonInteractive: true, Confirm: true})
	if err != nil {
		t.Fatal(err)
	}
	if freshCalls != 0 || result.Config.CodexExecutable != executable || result.Config.CodexSpawnPath == "" || store.saveConfig != 1 {
		t.Fatalf("fresh=%d result=%+v saves=%d", freshCalls, result, store.saveConfig)
	}
}

func TestFirstInstallMissingControlIDPrecedesDependencyResolution(t *testing.T) {
	store := &fakeStore{}
	scheduler := &fakeScheduler{}
	tasks := &fakeTasks{}
	installer := newInstaller(t, store, scheduler, tasks, nil)
	resolved := 0
	installer.CodexExecutable = ""
	installer.ResolveCodexExecutableSpec = func(string, string) (codex.ExecutableSpec, error) {
		resolved++
		return codex.ExecutableSpec{}, errors.New("must not resolve")
	}
	_, err := installer.Install(context.Background(), InstallRequest{NonInteractive: true, Confirm: true})
	if !errors.Is(err, ErrControlTaskIDRequired) {
		t.Fatalf("err=%v", err)
	}
	if resolved != 0 || store.locks != 0 || store.saveConfig != 0 || store.saveState != 0 || len(scheduler.calls) != 0 || len(tasks.reads) != 0 || installer.Binary.(*fakeBinary).calls != 0 {
		t.Fatalf("resolved=%d store=%+v scheduler=%v tasks=%+v binary=%d", resolved, store, scheduler.calls, tasks, installer.Binary.(*fakeBinary).calls)
	}
}

func TestInstallDryRunIsDeterministicAndMutationFree(t *testing.T) {
	store := &fakeStore{}
	scheduler := &fakeScheduler{}
	tasks := &fakeTasks{}
	installer := newInstaller(t, store, scheduler, tasks, nil)
	request := InstallRequest{ControlTaskID: "task-home", DryRun: true}
	first, err := installer.Install(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := installer.Install(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first.Preview, second.Preview) || first.ControlTaskDisposition != ControlTaskAdopted || !first.DryRun {
		t.Fatalf("first=%+v second=%+v", first, second)
	}
	if store.locks != 0 || store.saveConfig != 0 || store.saveState != 0 || len(scheduler.calls) != 0 || installer.Binary.(*fakeBinary).calls != 0 || len(tasks.welcomes) != 0 {
		t.Fatalf("dry run mutated store=%+v scheduler=%v tasks=%+v", store, scheduler.calls, tasks)
	}
}

func TestInstallDryRunCustomPreferencesAreMutationFree(t *testing.T) {
	store := &fakeStore{}
	scheduler := &fakeScheduler{}
	tasks := &fakeTasks{}
	installer := newInstaller(t, store, scheduler, tasks, nil)
	archive := false
	tokenDisplay := tokens.PositionEnd
	request := InstallRequest{
		ControlTaskID: "task-home",
		DryRun:        true,
		Patch: PreferencePatch{
			ArchiveEnabled: &archive,
			TokenDisplay:   &tokenDisplay,
		},
	}
	result, err := installer.Install(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if !result.DryRun || result.Config.ArchiveEnabled || result.Config.TokenDisplay != tokens.PositionEnd {
		t.Fatalf("result=%+v", result)
	}
	if store.locks != 0 || store.saveConfig != 0 || store.saveState != 0 || len(scheduler.calls) != 0 || installer.Binary.(*fakeBinary).calls != 0 || len(tasks.welcomes) != 0 {
		t.Fatalf("custom dry run mutated store=%+v scheduler=%v tasks=%+v", store, scheduler.calls, tasks)
	}
}

func TestInstallConfirmedFullPreferencePatchAppliesReviewedDryRunSnapshot(t *testing.T) {
	cfg := config.Default("task-home")
	cfg.HeartbeatSeconds = 420
	cfg.ArchiveEnabled = false
	cfg.ArchiveAfterDays = 31
	cfg.RenameEnabled = false
	cfg.AutoUpdateEnabled = false
	cfg.TokenDisplay = tokens.PositionEnd
	cfg.AgentsEnabled = false
	cfg.ClassifierModel = "reviewed-model"
	cfg.ClassifierEffort = config.EffortHigh
	cfg.ClassifierContextBudgetBytes = 2468
	store := &fakeStore{
		config:       cfg,
		state:        state.New(),
		configExists: true,
		stateExists:  true,
	}
	installer := newInstaller(t, store, &fakeScheduler{}, &fakeTasks{}, nil)

	heartbeatSeconds := 600
	dryRun, err := installer.Install(context.Background(), InstallRequest{
		ControlTaskID: "task-home",
		DryRun:        true,
		Patch: PreferencePatch{
			HeartbeatSeconds: &heartbeatSeconds,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	reviewed := preferencesFromConfig(dryRun.Config)

	store.config.AutoUpdateEnabled = true
	if reviewed.AutoUpdateEnabled == store.config.AutoUpdateEnabled {
		t.Fatal("intervening preference change did not differ from reviewed snapshot")
	}

	result, err := installer.Install(context.Background(), InstallRequest{
		ControlTaskID:  "task-home",
		NonInteractive: true,
		Confirm:        true,
		Patch: PreferencePatch{
			HeartbeatSeconds:             &reviewed.HeartbeatSeconds,
			ArchiveEnabled:               &reviewed.ArchiveEnabled,
			ArchiveAfterDays:             &reviewed.ArchiveAfterDays,
			RenameEnabled:                &reviewed.RenameEnabled,
			AutoUpdateEnabled:            &reviewed.AutoUpdateEnabled,
			TokenDisplay:                 &reviewed.TokenDisplay,
			AgentsEnabled:                &reviewed.AgentsEnabled,
			ClassifierModel:              &reviewed.ClassifierModel,
			ClassifierEffort:             &reviewed.ClassifierEffort,
			ClassifierContextBudgetBytes: &reviewed.ClassifierContextBudgetBytes,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := preferencesFromConfig(result.Config); got != reviewed {
		t.Fatalf("installed preferences=%+v reviewed=%+v", got, reviewed)
	}
	if got := preferencesFromConfig(store.config); got != reviewed {
		t.Fatalf("persisted preferences=%+v reviewed=%+v", got, reviewed)
	}
}

func TestInstallDryRunPreservesRealFilesAndIgnoresHistoricalThreadWatch(t *testing.T) {
	type snapshotEntry struct {
		Mode os.FileMode
		Data []byte
	}
	home := t.TempDir()
	paths := PathsForHome(home)
	cfg := config.Default("home-task")
	cfg.HeartbeatSeconds = 777
	cfg.ArchiveEnabled = false
	cfg.AutoUpdateEnabled = false
	cfg.TokenDisplay = tokens.PositionEnd
	cfg.CodexExecutable = testCodexExecutable(t, home)
	spec, err := codex.DeriveExecutableSpec(home, cfg.CodexExecutable, os.Getenv("PATH"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.CodexSpawnPath = spec.SpawnPath
	committed := state.New()
	committed.Generation = 42
	store := NewDiskStore(paths)
	if err := store.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveState(committed); err != nil {
		t.Fatal(err)
	}
	write := func(path string, data []byte, mode os.FileMode) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
	}
	write(paths.Binary, []byte("existing binary\x00bytes"), 0o711)
	write(paths.LaunchAgent, []byte("current launchagent bytes"), 0o640)
	write(paths.Agents, []byte("user AGENTS guidance\n"), 0o644)
	write(paths.Skill, []byte("historical skill bytes\n"), 0o444)
	legacyDirectory := filepath.Join(home, ".local", "share", "threadwatch")
	legacyState := filepath.Join(legacyDirectory, "state.json")
	legacyLock := filepath.Join(legacyDirectory, "run.lock")
	legacyPlist := filepath.Join(home, "Library", "LaunchAgents", "org.litman.threadwatch.plist")
	write(legacyState, []byte(`{"control_task_id":"legacy-task","heartbeat_seconds":5}`), 0o604)
	write(legacyLock, []byte("arbitrary historical lock\x00bytes"), 0o400)
	write(legacyPlist, []byte("arbitrary historical plist bytes"), 0o666)
	snapshot := func() map[string]snapshotEntry {
		t.Helper()
		got := make(map[string]snapshotEntry)
		err := filepath.WalkDir(home, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			relative, err := filepath.Rel(home, path)
			if err != nil {
				return err
			}
			item := snapshotEntry{Mode: info.Mode()}
			if info.Mode().IsRegular() {
				item.Data, err = os.ReadFile(path)
				if err != nil {
					return err
				}
			}
			got[relative] = item
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	before := snapshot()
	scheduler := &fakeScheduler{}
	tasks := &fakeTasks{tasks: map[string]ControlTask{
		"home-task":    {ID: "home-task"},
		"calling-task": {ID: "calling-task"},
		"legacy-task":  {ID: "legacy-task", Archived: true},
	}}
	binary := &fakeBinary{}
	selfTest := &fakeSelfTest{}
	installer := Installer{
		Paths: paths, Store: store, Scheduler: scheduler, ControlTasks: tasks,
		Binary: binary, SelfTester: selfTest, CodexExecutable: cfg.CodexExecutable, CodexSpawnPath: cfg.CodexSpawnPath,
	}
	result, err := installer.Install(context.Background(), InstallRequest{ControlTaskID: "calling-task", DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if !result.DryRun || result.ControlTaskDisposition != ControlTaskStayedHome || result.Config.ControlTaskID != "home-task" || !reflect.DeepEqual(result.Config, cfg) || !reflect.DeepEqual(result.State, committed) {
		t.Fatalf("result=%+v", result)
	}
	if !reflect.DeepEqual(tasks.reads, []string{"home-task"}) || len(tasks.unarchived) != 0 || len(tasks.archived) != 0 || len(tasks.welcomes) != 0 {
		t.Fatalf("task calls=%+v", tasks)
	}
	if len(scheduler.calls) != 0 || binary.calls != 0 || selfTest.calls != 0 {
		t.Fatalf("scheduler=%v binary=%d selftest=%d", scheduler.calls, binary.calls, selfTest.calls)
	}
	if after := snapshot(); !reflect.DeepEqual(after, before) {
		t.Fatalf("dry run changed files or modes\nbefore=%#v\nafter=%#v", before, after)
	}
}

func TestControlTaskSelectionMatrix(t *testing.T) {
	tests := []struct {
		name        string
		persisted   string
		persistedOK bool
		archived    bool
		supplied    string
		want        ControlTaskDisposition
		wantID      string
		wantErr     bool
		unarchive   bool
	}{
		{name: "first adoption", supplied: "new", want: ControlTaskAdopted, wantID: "new"},
		{name: "retained", persisted: "home", persistedOK: true, want: ControlTaskRetained, wantID: "home"},
		{name: "stayed home", persisted: "home", persistedOK: true, supplied: "other", want: ControlTaskStayedHome, wantID: "home"},
		{name: "unreadable replacement", persisted: "gone", supplied: "new", want: ControlTaskReplaced, wantID: "new"},
		{name: "persisted archived", persisted: "home", persistedOK: true, archived: true, want: ControlTaskRetained, wantID: "home", unarchive: true},
		{name: "supplied archived rejected", supplied: "new", archived: true, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tasks := &fakeTasks{tasks: map[string]ControlTask{}}
			if test.persistedOK {
				tasks.tasks[test.persisted] = ControlTask{ID: test.persisted, Archived: test.archived}
			} else if test.persisted != "" {
				tasks.readErr = map[string]error{test.persisted: errors.New("missing")}
			}
			if test.supplied != "" {
				tasks.tasks[test.supplied] = ControlTask{ID: test.supplied, Archived: test.archived && !test.persistedOK}
			}
			installer := newInstaller(t, &fakeStore{}, &fakeScheduler{}, tasks, nil)
			cfg := config.Config{}
			if test.persisted != "" {
				cfg = config.Default(test.persisted)
			}
			selection, err := installer.selectControlTask(context.Background(), cfg, test.persisted != "", test.supplied)
			if test.wantErr {
				if err == nil {
					t.Fatalf("selection=%+v", selection)
				}
				return
			}
			if err != nil || selection.Disposition != test.want || selection.ID != test.wantID || selection.Unarchive != test.unarchive {
				t.Fatalf("selection=%+v err=%v", selection, err)
			}
		})
	}
}

func TestInstallWelcomeAndUnarchiveDoNotInterfereWithOtherTasks(t *testing.T) {
	cfg := config.Default("home")
	store := &fakeStore{config: cfg, state: state.New(), configExists: true, stateExists: true}
	tasks := &fakeTasks{tasks: map[string]ControlTask{"home": {ID: "home", Archived: true}, "other": {ID: "other"}}}
	installer := newInstaller(t, store, &fakeScheduler{}, tasks, nil)
	result, err := installer.Install(context.Background(), InstallRequest{ControlTaskID: "other", NonInteractive: true, Confirm: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.ControlTaskDisposition != ControlTaskStayedHome || !result.Unarchived || !reflect.DeepEqual(tasks.unarchived, []string{"home"}) || len(tasks.welcomes) != 0 || len(tasks.archived) != 0 {
		t.Fatalf("result=%+v tasks=%+v", result, tasks)
	}
	if task := tasks.tasks["other"]; task.Archived {
		t.Fatalf("unrelated task changed: %+v", task)
	}
}

func TestBear60RepairPreservesOneRawBackupAndOnlyReplacesID(t *testing.T) {
	home := t.TempDir()
	paths := PathsForHome(home)
	if err := os.MkdirAll(paths.StateDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default(bear60ControllerID)
	cfg.CodexExecutable = testCodexExecutable(t, home)
	spec, err := codex.DeriveExecutableSpec(home, cfg.CodexExecutable, os.Getenv("PATH"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.CodexSpawnPath = spec.SpawnPath
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.Config, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewDiskStore(paths)
	if err := store.SaveState(state.New()); err != nil {
		t.Fatal(err)
	}
	run := func() InstallResult {
		t.Helper()
		tasks := &fakeTasks{tasks: map[string]ControlTask{"new-home": {ID: "new-home"}}}
		installer := Installer{Paths: paths, Store: store, Scheduler: &fakeScheduler{}, ControlTasks: tasks, Binary: &fakeBinary{}, SelfTester: &fakeSelfTest{}, CodexExecutable: cfg.CodexExecutable, CodexSpawnPath: cfg.CodexSpawnPath}
		result, err := installer.Install(context.Background(), InstallRequest{ControlTaskID: "new-home", NonInteractive: true, Confirm: true})
		if err != nil {
			t.Fatal(err)
		}
		if result.ControlTaskDisposition != ControlTaskRepaired || len(tasks.welcomes) != 1 {
			t.Fatalf("result=%+v welcomes=%d", result, len(tasks.welcomes))
		}
		return result
	}
	assertRepair := func() []byte {
		t.Helper()
		backup, err := os.ReadFile(paths.ConfigRepairBackup)
		if err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(paths.ConfigRepairBackup)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(backup, raw) || info.Mode().Perm() != 0o400 {
			t.Fatalf("backup preserved=%t mode=%04o", reflect.DeepEqual(backup, raw), info.Mode().Perm())
		}
		repaired, err := os.ReadFile(paths.Config)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Count(string(repaired), "new-home") != 1 || strings.Contains(string(repaired), bear60ControllerID) {
			t.Fatalf("repair did not replace exactly the control ID: %s", repaired)
		}
		decoded, err := config.Decode(repaired)
		if err != nil {
			t.Fatal(err)
		}
		want := cfg
		want.ControlTaskID = "new-home"
		if !reflect.DeepEqual(decoded, want) {
			t.Fatalf("decoded=%+v want=%+v", decoded, want)
		}
		return backup
	}
	run()
	backup := assertRepair()
	if err := os.WriteFile(paths.Config, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	run()
	if rerunBackup := assertRepair(); !reflect.DeepEqual(rerunBackup, backup) {
		t.Fatal("interrupted repair rerun changed the immutable backup")
	}
}

func TestInstallRequiresControlTaskIDBeforeMutation(t *testing.T) {
	store := &fakeStore{}
	scheduler := &fakeScheduler{}
	installer := newInstaller(t, store, scheduler, &fakeTasks{}, nil)
	_, err := installer.Install(context.Background(), InstallRequest{NonInteractive: true, Confirm: true})
	if !errors.Is(err, ErrControlTaskIDRequired) {
		t.Fatalf("err=%v", err)
	}
	if store.locks != 0 || len(scheduler.calls) != 0 || installer.Binary.(*fakeBinary).calls != 0 {
		t.Fatalf("mutation occurred store=%+v scheduler=%v binary=%d", store, scheduler.calls, installer.Binary.(*fakeBinary).calls)
	}
}

func TestInstallWelcomeOnlyForAdoptionAndReplacement(t *testing.T) {
	for _, test := range []struct {
		name      string
		persisted string
		readable  bool
		supplied  string
		want      ControlTaskDisposition
		welcomes  int
	}{
		{name: "adopted", supplied: "new", want: ControlTaskAdopted, welcomes: 1},
		{name: "retained", persisted: "home", readable: true, want: ControlTaskRetained},
		{name: "stayed home", persisted: "home", readable: true, supplied: "other", want: ControlTaskStayedHome},
		{name: "replaced", persisted: "gone", supplied: "new", want: ControlTaskReplaced, welcomes: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &fakeStore{}
			if test.persisted != "" {
				store.config = config.Default(test.persisted)
				store.state = state.New()
				store.configExists = true
				store.stateExists = true
			}
			tasks := &fakeTasks{tasks: map[string]ControlTask{"new": {ID: "new"}, "other": {ID: "other"}}}
			if test.readable {
				tasks.tasks[test.persisted] = ControlTask{ID: test.persisted}
			}
			installer := newInstaller(t, store, &fakeScheduler{}, tasks, nil)
			result, err := installer.Install(context.Background(), InstallRequest{ControlTaskID: test.supplied, NonInteractive: true, Confirm: true})
			if err != nil {
				t.Fatal(err)
			}
			if result.ControlTaskDisposition != test.want || len(tasks.welcomes) != test.welcomes {
				t.Fatalf("result=%+v welcomes=%d", result, len(tasks.welcomes))
			}
		})
	}
}

func TestInstallUnarchivesPersistedTaskWithoutWelcome(t *testing.T) {
	cfg := config.Default("home")
	store := &fakeStore{config: cfg, state: state.New(), configExists: true, stateExists: true}
	tasks := &fakeTasks{tasks: map[string]ControlTask{"home": {ID: "home", Archived: true}}}
	installer := newInstaller(t, store, &fakeScheduler{}, tasks, nil)
	result, err := installer.Install(context.Background(), InstallRequest{NonInteractive: true, Confirm: true})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Unarchived || len(tasks.unarchived) != 1 || len(tasks.welcomes) != 0 {
		t.Fatalf("result=%+v tasks=%+v", result, tasks)
	}
}

func TestInstallPostLockFailureRetainsStateDirectoryAndLockFile(t *testing.T) {
	home := t.TempDir()
	paths := PathsForHome(home)
	scheduler := &fakeScheduler{stageErrAt: 1, stageErr: errors.New("candidate staging failed")}
	installer := Installer{Paths: paths, Store: NewDiskStore(paths), Scheduler: scheduler, ControlTasks: &fakeTasks{}, Binary: &fakeBinary{}, SelfTester: &fakeSelfTest{}, CodexExecutable: testCodexExecutable(t, home)}
	_, err := installer.Install(context.Background(), InstallRequest{ControlTaskID: "control-new", NonInteractive: true, Confirm: true})
	var failure *InstallFailure
	if !errors.As(err, &failure) || failure.Step != "stage_candidate_scheduler" {
		t.Fatalf("err=%v failure=%+v", err, failure)
	}
	for _, path := range []string{paths.StateDirectory, filepath.Join(paths.StateDirectory, "threadbear.lock")} {
		if _, statErr := os.Stat(path); statErr != nil {
			t.Fatalf("retained recovery path %s: %v", path, statErr)
		}
	}
}

func TestInstallFailureIncludesStableStepAndCause(t *testing.T) {
	tasks := &fakeTasks{readErr: map[string]error{"control-new": errors.New("read denied; verify Codex authentication")}}
	installer := newInstaller(t, &fakeStore{}, &fakeScheduler{}, tasks, nil)
	_, err := installer.Install(context.Background(), InstallRequest{ControlTaskID: "control-new", NonInteractive: true, Confirm: true})
	var failure *InstallFailure
	if !errors.As(err, &failure) || failure.Step != "validate_supplied_control_task" || !strings.Contains(failure.Cause, "verify Codex authentication") {
		t.Fatalf("error=%v failure=%+v", err, failure)
	}
}

func TestInstallIgnoresThreadWatchArtifacts(t *testing.T) {
	home := t.TempDir()
	paths := PathsForHome(home)
	legacy := map[string][]byte{
		filepath.Join(home, ".local", "share", "threadwatch", "state.json"):            []byte(`{"schemaVersion":4,"controllerThreadId":"legacy-control"}`),
		filepath.Join(home, ".local", "share", "threadwatch", "run.lock"):              []byte("legacy lock"),
		filepath.Join(home, "Library", "LaunchAgents", "org.litman.threadwatch.plist"): []byte("legacy plist"),
	}
	for path, data := range legacy {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	store := &fakeStore{}
	scheduler := &fakeScheduler{}
	tasks := &fakeTasks{tasks: map[string]ControlTask{"new-home": {ID: "new-home"}, "legacy-control": {ID: "legacy-control", Archived: true}}}
	installer := newInstaller(t, store, scheduler, tasks, nil)
	installer.Paths = paths
	installer.CodexExecutable = testCodexExecutable(t, home)
	result, err := installer.Install(context.Background(), InstallRequest{ControlTaskID: "new-home", NonInteractive: true, Confirm: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Config.ControlTaskID != "new-home" || result.Config.HeartbeatSeconds != DefaultPreferences().HeartbeatSeconds {
		t.Fatalf("legacy artifacts influenced install: %+v", result.Config)
	}
	if !reflect.DeepEqual(scheduler.calls, []string{"stage", "stage", "enable", "healthy"}) || !reflect.DeepEqual(tasks.reads, []string{"new-home", "new-home"}) || len(tasks.unarchived) != 0 || len(tasks.archived) != 0 {
		t.Fatalf("legacy artifacts influenced scheduler/tasks: calls=%v tasks=%+v", scheduler.calls, tasks)
	}
	for path, want := range legacy {
		got, readErr := os.ReadFile(path)
		if readErr != nil || !bytes.Equal(got, want) {
			t.Fatalf("ThreadWatch artifact changed: %s got=%q err=%v", path, got, readErr)
		}
		info, statErr := os.Stat(path)
		if statErr != nil {
			t.Fatalf("stat ThreadWatch artifact %s: %v", path, statErr)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("ThreadWatch artifact mode changed: %s mode=%v", path, info.Mode())
		}
	}
}

func TestBear60RepairRejectsOtherConfigChangesBeforeMutation(t *testing.T) {
	cfg := config.Default(bear60ControllerID)
	store := &fakeStore{config: cfg, state: state.New(), configExists: true, stateExists: true}
	scheduler := &fakeScheduler{}
	tasks := &fakeTasks{tasks: map[string]ControlTask{"new-home": {ID: "new-home"}}}
	installer := newInstaller(t, store, scheduler, tasks, nil)
	seconds := cfg.HeartbeatSeconds + 1
	_, err := installer.Install(context.Background(), InstallRequest{ControlTaskID: "new-home", Patch: PreferencePatch{HeartbeatSeconds: &seconds}, NonInteractive: true, Confirm: true})
	var failure *InstallFailure
	if !errors.As(err, &failure) || failure.Step != "repair_control_task" || !strings.Contains(failure.Cause, "only control_task_id") {
		t.Fatalf("err=%v failure=%+v", err, failure)
	}
	if store.locks != 0 || store.saveConfig != 0 || store.saveState != 0 || len(scheduler.calls) != 0 || installer.Binary.(*fakeBinary).calls != 0 || len(tasks.welcomes) != 0 {
		t.Fatalf("repair changed unrelated state: store=%+v scheduler=%v tasks=%+v", store, scheduler.calls, tasks)
	}
}

func TestInstallRetriesWelcomeAfterDeliveryFailureWithoutDuplicates(t *testing.T) {
	store := &fakeStore{}
	scheduler := &fakeScheduler{}
	tasks := &fakeTasks{welcomeErr: errors.New("notice unavailable")}
	installer := newInstaller(t, store, scheduler, tasks, nil)
	first, err := installer.Install(context.Background(), InstallRequest{ControlTaskID: "home", NonInteractive: true, Confirm: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Warnings) != 1 || store.state.PendingWelcomeTaskID != "home" || len(tasks.welcomes) != 0 {
		t.Fatalf("first=%+v state=%+v tasks=%+v", first, store.state, tasks)
	}
	tasks.welcomeErr = nil
	second, err := installer.Install(context.Background(), InstallRequest{NonInteractive: true, Confirm: true})
	if err != nil {
		t.Fatal(err)
	}
	if second.ControlTaskDisposition != ControlTaskRetained || len(second.Warnings) != 0 || store.state.PendingWelcomeTaskID != "" || len(tasks.welcomes) != 1 {
		t.Fatalf("second=%+v state=%+v tasks=%+v", second, store.state, tasks)
	}
	third, err := installer.Install(context.Background(), InstallRequest{NonInteractive: true, Confirm: true})
	if err != nil {
		t.Fatal(err)
	}
	if third.ControlTaskDisposition != ControlTaskRetained || len(tasks.welcomes) != 1 {
		t.Fatalf("third=%+v tasks=%+v", third, tasks)
	}
}
