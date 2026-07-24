package install

import (
	"context"
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
	calls     []string
	interval  int
	legacyErr error
	disabled  bool
	loaded    bool
	loadedErr error
}

func (s *fakeScheduler) DetectLegacyInterval(context.Context) (int, bool, error) {
	s.calls = append(s.calls, "detect")
	return s.interval, s.interval > 0, nil
}
func (s *fakeScheduler) StopLegacy(context.Context) error {
	s.calls = append(s.calls, "stop")
	return nil
}
func (s *fakeScheduler) VerifyLegacyStopped(context.Context) error {
	s.calls = append(s.calls, "verify")
	return s.legacyErr
}
func (s *fakeScheduler) Stage(context.Context, config.Config) (bool, error) {
	s.calls = append(s.calls, "stage")
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
	ensures         int
	requested       []string
	archived        []string
	archivedIDs     map[string]bool
	ensureChanged   bool
	failAfterCreate error
}

func (t *fakeTasks) EnsureControlTask(_ context.Context, id string) (string, bool, error) {
	t.ensures++
	t.requested = append(t.requested, id)
	if id != "" {
		return id, t.ensureChanged, nil
	}
	if t.failAfterCreate != nil {
		err := t.failAfterCreate
		t.failAfterCreate = nil
		return "control-new", true, err
	}
	return "control-new", true, nil
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
	collected Preferences
	collect   bool
	previews  int
	confirms  int
	confirmed bool
	choices   []bool
}

func (p *fakePrompter) Collect(v Preferences) (Preferences, error) {
	p.collect = true
	if p.collected.HeartbeatSeconds != 0 {
		return p.collected, nil
	}
	return v, nil
}
func (p *fakePrompter) ShowPreview(Preview) error { p.previews++; return nil }
func (p *fakePrompter) Confirm() (bool, error)    { p.confirms++; return p.confirmed, nil }
func (p *fakePrompter) Choose(string, bool) (bool, error) {
	v := p.choices[0]
	p.choices = p.choices[1:]
	return v, nil
}

type missingLegacy struct{}

func (missingLegacy) Load(string) ([]byte, error) { return nil, fs.ErrNotExist }

type bytesLegacy []byte

func (b bytesLegacy) Load(string) ([]byte, error) { return b, nil }

type renamingScheduler struct {
	fakeScheduler
	plistPath  string
	statePath  string
	finalState []byte
	store      *fakeStore
	stopped    bool
}

func (s *renamingScheduler) DetectLegacyInterval(context.Context) (int, bool, error) {
	s.calls = append(s.calls, "detect")
	for _, path := range []string{s.plistPath, s.plistPath + ".disabled-by-threadbear"} {
		if _, err := os.Stat(path); err == nil {
			return s.interval, true, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return 0, false, err
		}
	}
	return 0, false, nil
}

func (s *renamingScheduler) StopLegacy(context.Context) error {
	s.calls = append(s.calls, "stop")
	if s.store != nil && !s.store.lockHeld {
		return errors.New("ThreadBear lock was not held while stopping legacy")
	}
	if _, err := os.Stat(s.plistPath); err == nil {
		if err := os.Rename(s.plistPath, s.plistPath+".disabled-by-threadbear"); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if len(s.finalState) > 0 {
		if err := os.WriteFile(s.statePath, s.finalState, 0o600); err != nil {
			return err
		}
	}
	s.stopped = true
	return nil
}

type postStopFailLegacy struct {
	scheduler *renamingScheduler
	failed    bool
}

func (l *postStopFailLegacy) Load(path string) ([]byte, error) {
	if l.scheduler.stopped && !l.failed {
		l.failed = true
		return nil, errors.New("injected final migration read failure")
	}
	return os.ReadFile(path)
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
	return Installer{Paths: paths, Store: store, Scheduler: scheduler, ControlTasks: tasks, Binary: &fakeBinary{}, SelfTester: &fakeSelfTest{}, Legacy: missingLegacy{}, Prompter: prompt, CodexExecutable: testCodexExecutable(t, paths.Home)}
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
	result, err := installer.Install(context.Background(), InstallRequest{})
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
	if !reflect.DeepEqual(scheduler.calls, []string{"detect", "stage", "stop", "verify", "stage", "enable", "healthy"}) {
		t.Fatalf("calls=%v", scheduler.calls)
	}
	if tasks.ensures != 1 || store.saveConfig != 1 || store.saveState != 1 {
		t.Fatalf("tasks=%d saves=%d/%d", tasks.ensures, store.saveConfig, store.saveState)
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
	if _, err := installer.Install(context.Background(), InstallRequest{NonInteractive: true, Preferences: &custom}); err == nil {
		t.Fatal("missing confirmation accepted")
	}
	result, err := installer.Install(context.Background(), InstallRequest{NonInteractive: true, Confirm: true, Preferences: &custom})
	if err != nil {
		t.Fatal(err)
	}
	if result.Config.HeartbeatSeconds != 42 || result.Config.ArchiveEnabled || result.Config.ClassifierEffort != config.EffortHigh {
		t.Fatalf("config=%+v", result.Config)
	}
	cancelStore := &fakeStore{}
	prompt := &fakePrompter{confirmed: false}
	cancel := newInstaller(t, cancelStore, &fakeScheduler{}, &fakeTasks{}, prompt)
	if _, err := cancel.Install(context.Background(), InstallRequest{}); !errors.Is(err, ErrCancelled) {
		t.Fatalf("error=%v", err)
	}
	if cancelStore.locks != 0 || prompt.confirms != 1 || prompt.previews != 1 {
		t.Fatalf("mutated on cancel store=%+v prompt=%+v", cancelStore, prompt)
	}
}

func TestInstallRejectsActiveLegacyBeforeEnable(t *testing.T) {
	store := &fakeStore{}
	scheduler := &fakeScheduler{legacyErr: errors.New("still active")}
	tasks := &fakeTasks{}
	installer := newInstaller(t, store, scheduler, tasks, nil)
	_, err := installer.Install(context.Background(), InstallRequest{NonInteractive: true, Confirm: true})
	if err == nil {
		t.Fatal("active legacy accepted")
	}
	if tasks.ensures != 0 || store.saveConfig != 0 || !reflect.DeepEqual(scheduler.calls, []string{"detect", "stage", "stop", "verify"}) {
		t.Fatalf("legacy stop order tasks=%d saves=%d calls=%v", tasks.ensures, store.saveConfig, scheduler.calls)
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
	if !first.Reinstalled || !second.Reinstalled || tasks.ensures != 2 {
		t.Fatalf("results=%+v %+v ensures=%d", first, second, tasks.ensures)
	}
	if tasks.requested[0] != "control-existing" || store.state.Generation != 9 {
		t.Fatalf("requested=%v state=%+v", tasks.requested, store.state)
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

func TestMigrationUsesDetectedInterval(t *testing.T) {
	legacy := bytesLegacy(`{"schemaVersion":1,"controllerThreadId":"control-old","cycleCompletedAtMs":1700000000000,"retryIds":[],"threads":{}}`)
	store := &fakeStore{}
	scheduler := &fakeScheduler{interval: 77}
	tasks := &fakeTasks{}
	installer := newInstaller(t, store, scheduler, tasks, nil)
	installer.Legacy = legacy
	result, err := installer.Install(context.Background(), InstallRequest{NonInteractive: true, Confirm: true})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Migrated || result.Config.ControlTaskID != "control-old" || result.Config.HeartbeatSeconds != 77 {
		t.Fatalf("result=%+v", result)
	}
	if tasks.requested[0] != "control-old" {
		t.Fatalf("requested=%v", tasks.requested)
	}
}

func TestInstallFailureRerunAdoptsPersistedControlTask(t *testing.T) {
	store := &fakeStore{saveStateErr: errors.New("disk full")}
	scheduler := &fakeScheduler{}
	tasks := &fakeTasks{}
	installer := newInstaller(t, store, scheduler, tasks, nil)
	_, err := installer.Install(context.Background(), InstallRequest{NonInteractive: true, Confirm: true})
	if err == nil {
		t.Fatal("expected staged state failure")
	}
	if !store.configExists || store.config.ControlTaskID != "control-new" || store.stateExists {
		t.Fatalf("split persistence not staged safely: %+v", store)
	}
	result, err := installer.Install(context.Background(), InstallRequest{NonInteractive: true, Confirm: true})
	if err != nil {
		t.Fatal(err)
	}
	if tasks.ensures != 2 || tasks.requested[1] != "control-new" || result.Config.ControlTaskID != "control-new" {
		t.Fatalf("duplicate-prone rerun: requested=%v result=%+v", tasks.requested, result)
	}
	if !store.stateExists {
		t.Fatal("split config/state did not converge")
	}
}

func TestInstallSelfTestPrecedesEnableAndHealthVerification(t *testing.T) {
	store := &fakeStore{}
	scheduler := &fakeScheduler{}
	selfTest := &orderedSelfTest{scheduler: scheduler}
	installer := newInstaller(t, store, scheduler, &fakeTasks{}, nil)
	installer.SelfTester = selfTest
	if _, err := installer.Install(context.Background(), InstallRequest{NonInteractive: true, Confirm: true}); err != nil {
		t.Fatal(err)
	}
	want := []string{"detect", "stage", "selftest", "stop", "verify", "stage", "enable", "healthy", "selftest"}
	if !reflect.DeepEqual(scheduler.calls, want) {
		t.Fatalf("calls=%v want=%v", scheduler.calls, want)
	}
}

type orderedSelfTest struct{ scheduler *fakeScheduler }

func (s *orderedSelfTest) Test(context.Context, SelfTestInput) error {
	s.scheduler.calls = append(s.scheduler.calls, "selftest")
	return nil
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
	installer := Installer{Paths: paths, Store: store, Scheduler: &fakeScheduler{}, ControlTasks: &fakeTasks{}, Binary: FileBinaryInstaller{Source: source}, SelfTester: &fakeSelfTest{}, Legacy: missingLegacy{}, CodexExecutable: codexExecutable, CodexSpawnPath: spec.SpawnPath}
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
		if store.locks != 0 || tasks.ensures != 0 || len(preview.Lines) < 8 {
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
	if _, err := installer.Install(context.Background(), InstallRequest{NonInteractive: true, Confirm: true}); err != nil {
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

func TestInstallPersistsCreatedControlIDWhenLaterTaskSetupFails(t *testing.T) {
	store := &fakeStore{}
	tasks := &fakeTasks{failAfterCreate: errors.New("title failed")}
	installer := newInstaller(t, store, &fakeScheduler{}, tasks, nil)
	if _, err := installer.Install(context.Background(), InstallRequest{NonInteractive: true, Confirm: true}); err == nil {
		t.Fatal("expected task setup failure")
	}
	if !store.configExists || store.config.ControlTaskID != "control-new" {
		t.Fatalf("created ID not persisted: %+v", store)
	}
	if _, err := installer.Install(context.Background(), InstallRequest{NonInteractive: true, Confirm: true}); err != nil {
		t.Fatal(err)
	}
	if len(tasks.requested) != 2 || tasks.requested[1] != "control-new" {
		t.Fatalf("rerun did not adopt task: %v", tasks.requested)
	}
}

func TestControlTaskMutationMakesReinstallChanged(t *testing.T) {
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
	store := &fakeStore{config: cfg, state: committed, configExists: true, stateExists: true}
	codexExecutable := testCodexExecutable(t, home)
	spec, err := codex.DeriveExecutableSpec(home, codexExecutable, os.Getenv("PATH"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.CodexExecutable = codexExecutable
	cfg.CodexSpawnPath = spec.SpawnPath
	store.config = cfg
	installer := Installer{Paths: paths, Store: store, Scheduler: &fakeScheduler{}, ControlTasks: &fakeTasks{ensureChanged: true}, Binary: FileBinaryInstaller{Source: source}, SelfTester: &fakeSelfTest{}, Legacy: missingLegacy{}, CodexExecutable: codexExecutable, CodexSpawnPath: spec.SpawnPath}
	result, err := installer.Install(context.Background(), InstallRequest{NonInteractive: true, Confirm: true})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || !reflect.DeepEqual(result.Resources, []string{"control_task"}) {
		t.Fatalf("result=%+v", result)
	}
}

func TestInteractiveInstallShowsPreviewExactlyOnce(t *testing.T) {
	prompt := &fakePrompter{confirmed: true}
	installer := newInstaller(t, &fakeStore{}, &fakeScheduler{}, &fakeTasks{}, prompt)
	extra := 0
	installer.Previewer = func(Preview) error { extra++; return nil }
	if _, err := installer.Install(context.Background(), InstallRequest{}); err != nil {
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
	if _, err := installer.Install(context.Background(), InstallRequest{NonInteractive: true, Confirm: true}); err != nil {
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
	installer := Installer{Paths: paths, Store: store, Scheduler: &fakeScheduler{}, ControlTasks: &fakeTasks{}, Binary: &fakeBinary{}, SelfTester: &fakeSelfTest{}, Legacy: missingLegacy{}, ResolveCodexExecutableSpec: codex.ResolveExecutableSpec}
	_, err := installer.Install(context.Background(), InstallRequest{NonInteractive: true, Confirm: true})
	var failure *InstallFailure
	if !errors.As(err, &failure) || failure.Step != "resolve_codex_executable" || !strings.Contains(failure.Cause, "invoking PATH") {
		t.Fatalf("error=%v failure=%+v", err, failure)
	}
	if _, statErr := os.Stat(paths.StateDirectory); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("state directory created: %v", statErr)
	}
}

func TestInstallPostLockFailureRetainsStateDirectoryAndLockFile(t *testing.T) {
	home := t.TempDir()
	paths := PathsForHome(home)
	codexExecutable := testCodexExecutable(t, home)
	installer := Installer{Paths: paths, Store: NewDiskStore(paths), Scheduler: &fakeScheduler{}, ControlTasks: &fakeTasks{failAfterCreate: errors.New("title failed")}, Binary: &fakeBinary{}, SelfTester: &fakeSelfTest{}, Legacy: missingLegacy{}, CodexExecutable: codexExecutable}
	_, err := installer.Install(context.Background(), InstallRequest{NonInteractive: true, Confirm: true})
	if err == nil {
		t.Fatal("expected failure")
	}
	for _, path := range []string{paths.StateDirectory, filepath.Join(paths.StateDirectory, "threadbear.lock")} {
		if _, statErr := os.Stat(path); statErr != nil {
			t.Fatalf("retained recovery path %s: %v", path, statErr)
		}
	}
}

func TestMigrationCapturesOriginalIntervalBeforeLegacyPlistIsRenamed(t *testing.T) {
	home := t.TempDir()
	paths := PathsForHome(home)
	oldState := []byte(`{"schemaVersion":1,"controllerThreadId":"control-old","cycleCompletedAtMs":1700000000000,"retryIds":[],"threads":{}}`)
	finalState := []byte(`{"schemaVersion":1,"controllerThreadId":"control-final","cycleCompletedAtMs":1700000001000,"retryIds":[],"threads":{}}`)
	for _, path := range []string{paths.LegacyState, paths.LegacyLaunchAgent} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(paths.LegacyState, oldState, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.LegacyLaunchAgent, []byte("legacy plist with StartInterval 77"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := &fakeStore{}
	scheduler := &renamingScheduler{fakeScheduler: fakeScheduler{interval: 77}, plistPath: paths.LegacyLaunchAgent, statePath: paths.LegacyState, finalState: finalState, store: store}
	tasks := &fakeTasks{}
	selfTest := &lifecycleSelfTest{scheduler: &scheduler.fakeScheduler}
	installer := Installer{Paths: paths, Store: store, Scheduler: scheduler, ControlTasks: tasks, Binary: &fakeBinary{}, SelfTester: selfTest, Legacy: FileLegacyLoader{}, CodexExecutable: testCodexExecutable(t, home)}
	result, err := installer.Install(context.Background(), InstallRequest{NonInteractive: true, Confirm: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Config.HeartbeatSeconds != 77 || result.Config.ControlTaskID != "control-final" || store.config.ControlTaskID != "control-final" {
		t.Fatalf("final migration did not preserve interval/state: result=%+v store=%+v", result, store)
	}
	if _, err := os.Stat(paths.LegacyLaunchAgent); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("legacy plist was not renamed: %v", err)
	}
	if len(selfTest.inputs) != 2 || !selfTest.inputs[0].Candidate || selfTest.inputs[1].Candidate {
		t.Fatalf("self-test phases=%+v", selfTest.inputs)
	}
	if got := scheduler.calls; !reflect.DeepEqual(got[:5], []string{"detect", "stage", "stop", "verify", "stage"}) {
		t.Fatalf("binding order=%v", got)
	}
}

func TestPostStopFinalReadFailureIsActionableAndRerunConverges(t *testing.T) {
	home := t.TempDir()
	paths := PathsForHome(home)
	oldState := []byte(`{"schemaVersion":1,"controllerThreadId":"control-old","cycleCompletedAtMs":1700000000000,"retryIds":[],"threads":{}}`)
	finalState := []byte(`{"schemaVersion":1,"controllerThreadId":"control-final","cycleCompletedAtMs":1700000001000,"retryIds":[],"threads":{}}`)
	for _, path := range []string{paths.LegacyState, paths.LegacyLaunchAgent} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(paths.LegacyState, oldState, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.LegacyLaunchAgent, []byte("legacy plist with StartInterval 77"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := &fakeStore{}
	scheduler := &renamingScheduler{fakeScheduler: fakeScheduler{interval: 77}, plistPath: paths.LegacyLaunchAgent, statePath: paths.LegacyState, finalState: finalState, store: store}
	tasks := &fakeTasks{}
	loader := &postStopFailLegacy{scheduler: scheduler}
	installer := Installer{Paths: paths, Store: store, Scheduler: scheduler, ControlTasks: tasks, Binary: &fakeBinary{}, SelfTester: &fakeSelfTest{}, Legacy: loader, CodexExecutable: testCodexExecutable(t, home)}
	_, err := installer.Install(context.Background(), InstallRequest{NonInteractive: true, Confirm: true})
	var failure *InstallFailure
	if !errors.As(err, &failure) || failure.Step != "load_final_migration" || !strings.Contains(failure.Cause, "ThreadWatch was stopped") || !strings.Contains(failure.Cause, ".disabled-by-threadbear") || !strings.Contains(failure.Cause, "launchctl enable") || !strings.Contains(failure.Cause, "launchctl bootstrap") {
		t.Fatalf("failure=%v", err)
	}
	if store.configExists || store.stateExists || tasks.ensures != 0 {
		t.Fatalf("pre-stop candidate state was persisted: store=%+v tasks=%+v", store, tasks)
	}
	result, err := installer.Install(context.Background(), InstallRequest{NonInteractive: true, Confirm: true})
	if err != nil {
		t.Fatal(err)
	}
	if !store.configExists || !store.stateExists || result.Config.HeartbeatSeconds != 77 || result.Config.ControlTaskID != "control-final" {
		t.Fatalf("rerun did not converge: result=%+v store=%+v", result, store)
	}
}

func TestMigrationReloadsStateAfterPreviewAndLegacyStop(t *testing.T) {
	home := t.TempDir()
	paths := PathsForHome(home)
	oldState := `{"schemaVersion":1,"controllerThreadId":"control-old","cycleCompletedAtMs":1700000000000,"retryIds":[],"threads":{}}`
	newState := `{"schemaVersion":1,"controllerThreadId":"control-newer","cycleCompletedAtMs":1700000001000,"retryIds":[],"threads":{}}`
	if err := os.MkdirAll(filepath.Dir(paths.LegacyState), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.LegacyState, []byte(oldState), 0o600); err != nil {
		t.Fatal(err)
	}
	store := &fakeStore{}
	scheduler := &fakeScheduler{interval: 77}
	tasks := &fakeTasks{}
	installer := newInstaller(t, store, scheduler, tasks, nil)
	installer.Paths = paths
	installer.Legacy = FileLegacyLoader{}
	installer.Previewer = func(Preview) error { return os.WriteFile(paths.LegacyState, []byte(newState), 0o600) }
	result, err := installer.Install(context.Background(), InstallRequest{NonInteractive: true, Confirm: true})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Migrated || result.Config.ControlTaskID != "control-newer" || tasks.requested[0] != "control-newer" {
		t.Fatalf("result=%+v requested=%v", result, tasks.requested)
	}
	if !reflect.DeepEqual(scheduler.calls[:4], []string{"detect", "stage", "stop", "verify"}) {
		t.Fatalf("migration read order calls=%v", scheduler.calls)
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
	installer := Installer{Paths: paths, Store: store, Scheduler: &fakeScheduler{}, ControlTasks: &fakeTasks{}, Binary: FileBinaryInstaller{Source: source}, SelfTester: &fakeSelfTest{}, Legacy: missingLegacy{}, CodexExecutable: codexExecutable}
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

func TestInstallFailureIncludesStableStepAndCause(t *testing.T) {
	installer := newInstaller(t, &fakeStore{}, &fakeScheduler{}, &fakeTasks{failAfterCreate: errors.New("set title denied; verify Codex authentication")}, nil)
	_, err := installer.Install(context.Background(), InstallRequest{NonInteractive: true, Confirm: true})
	var failure *InstallFailure
	if !errors.As(err, &failure) || failure.Step != "ensure_control_task" || !strings.Contains(failure.Cause, "verify Codex authentication") {
		t.Fatalf("error=%v failure=%+v", err, failure)
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
	installer := Installer{Paths: paths, Store: store, Scheduler: &fakeScheduler{}, ControlTasks: &fakeTasks{}, Binary: &fakeBinary{}, SelfTester: &fakeSelfTest{}, Legacy: missingLegacy{}, CodexExecutable: fresh, ResolveCodexExecutableSpec: func(string, string) (codex.ExecutableSpec, error) {
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
		Binary: &fakeBinary{}, SelfTester: &fakeSelfTest{}, Legacy: missingLegacy{},
		ResolveCodexExecutableSpec: codex.ResolveExecutableSpec,
	}
	result, err := installer.Install(context.Background(), InstallRequest{NonInteractive: true, Confirm: true})
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
		Binary: &fakeBinary{}, SelfTester: &fakeSelfTest{}, Legacy: missingLegacy{},
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
