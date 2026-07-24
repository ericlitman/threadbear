package install

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"

	"github.com/ericlitman/threadbear/assets"
	"github.com/ericlitman/threadbear/internal/config"
	"github.com/ericlitman/threadbear/internal/state"
)

type Paths struct {
	Home                 string
	Binary               string
	StateDirectory       string
	Config               string
	State                string
	Agents               string
	Skill                string
	LaunchAgent          string
	LaunchAgentStdout    string
	LaunchAgentStderr    string
	LegacyStateDirectory string
	LegacyState          string
	LegacyRunLock        string
	LegacyLaunchAgent    string
}

func PathsForHome(home string) Paths {
	stateDirectory := filepath.Join(home, ".local", "share", "threadbear")
	return Paths{
		Home:                 home,
		Binary:               filepath.Join(home, ".local", "bin", "threadbear"),
		StateDirectory:       stateDirectory,
		Config:               filepath.Join(stateDirectory, "config.json"),
		State:                filepath.Join(stateDirectory, "state.json"),
		Agents:               filepath.Join(home, ".codex", "AGENTS.md"),
		Skill:                filepath.Join(home, ".codex", "skills", "threadbear", "SKILL.md"),
		LaunchAgent:          filepath.Join(home, "Library", "LaunchAgents", config.LaunchAgentLabel+".plist"),
		LaunchAgentStdout:    filepath.Join(stateDirectory, "logs", "heartbeat.stdout.log"),
		LaunchAgentStderr:    filepath.Join(stateDirectory, "logs", "heartbeat.stderr.log"),
		LegacyStateDirectory: filepath.Join(home, ".local", "share", "threadwatch"),
		LegacyState:          filepath.Join(home, ".local", "share", "threadwatch", "state.json"),
		LegacyRunLock:        filepath.Join(home, ".local", "share", "threadwatch", "run.lock"),
		LegacyLaunchAgent:    filepath.Join(home, "Library", "LaunchAgents", "org.litman.threadwatch.plist"),
	}
}

type Lock interface {
	Close() error
}

type Store interface {
	AcquireLock() (Lock, error)
	LoadConfig() (config.Config, error)
	SaveConfig(config.Config) error
	LoadState() (state.State, error)
	SaveState(state.State) error
}

type DiskStore struct {
	store *state.Store
}

func NewDiskStore(paths Paths) *DiskStore {
	return &DiskStore{store: state.NewStore(paths.StateDirectory)}
}

func (s *DiskStore) AcquireLock() (Lock, error) {
	return s.store.AcquireLock()
}

func (s *DiskStore) LoadConfig() (config.Config, error) {
	return s.store.LoadConfig()
}

func (s *DiskStore) SaveConfig(value config.Config) error {
	return s.store.SaveConfig(value)
}

func (s *DiskStore) LoadState() (state.State, error) {
	return s.store.LoadState()
}

func (s *DiskStore) SaveState(value state.State) error {
	return s.store.SaveState(value)
}

type Scheduler interface {
	DetectLegacyInterval(context.Context) (int, bool, error)
	StopLegacy(context.Context) error
	VerifyLegacyStopped(context.Context) error
	Stage(context.Context, config.Config) (bool, error)
	Enable(context.Context) (bool, error)
	VerifyHealthy(context.Context) error
	Remove(context.Context) error
}

type ControlTasks interface {
	EnsureControlTask(context.Context, string) (string, bool, error)
	ArchiveControlTask(context.Context, string) (bool, error)
}

type BinaryInstaller interface {
	Install(string) error
}

type SelfTester interface {
	Test(context.Context, SelfTestInput) error
}

type LegacyLoader interface {
	Load(string) ([]byte, error)
}

type PreferencePatch struct {
	HeartbeatSeconds             *int
	ArchiveEnabled               *bool
	ArchiveAfterDays             *int
	RenameEnabled                *bool
	AgentsEnabled                *bool
	ClassifierModel              *string
	ClassifierEffort             *config.ClassifierEffort
	ClassifierContextBudgetBytes *int
}

type InstallRequest struct {
	Preferences    *Preferences
	Patch          PreferencePatch
	NonInteractive bool
	Confirm        bool
}

type InstallResult struct {
	Config      config.Config
	State       state.State
	Paths       Paths
	Migrated    bool
	Reinstalled bool
	Preview     Preview
	Changed     bool
	Resources   []string
}

type Installer struct {
	Paths        Paths
	Store        Store
	Scheduler    Scheduler
	ControlTasks ControlTasks
	Binary       BinaryInstaller
	SelfTester   SelfTester
	Legacy       LegacyLoader
	Prompter     Prompter
	Previewer    func(Preview) error
}

func (i Installer) Install(ctx context.Context, request InstallRequest) (InstallResult, error) {
	if err := i.validate(); err != nil {
		return InstallResult{}, err
	}
	currentConfig, currentState, configExists, stateExists, err := loadExisting(i.Store)
	if err != nil {
		return InstallResult{}, err
	}
	preferences := DefaultPreferences()
	controlTaskID := ""
	migrated := false
	if configExists {
		preferences = preferencesFromConfig(currentConfig)
		controlTaskID = currentConfig.ControlTaskID
	}
	migration := Migration{}
	if !configExists || !stateExists {
		migration, migrated, err = i.loadMigration(ctx)
		if err != nil {
			return InstallResult{}, err
		}
		if migrated {
			if controlTaskID == "" {
				controlTaskID = migration.ControlTaskID
			}
			if !stateExists {
				currentState = migration.State
			}
			if !configExists && migration.HeartbeatSeconds > 0 {
				preferences.HeartbeatSeconds = migration.HeartbeatSeconds
			}
		}
	}
	if !stateExists && !migrated {
		currentState = state.New()
	}
	if request.Preferences != nil {
		preferences = *request.Preferences
	}
	preferences = applyPreferencePatch(preferences, request.Patch)
	if request.NonInteractive {
		if !request.Confirm {
			return InstallResult{}, errors.New("noninteractive install requires confirm")
		}
	} else {
		if i.Prompter == nil {
			return InstallResult{}, errors.New("interactive install requires a prompter")
		}
		preferences, err = i.Prompter.Collect(preferences)
		if err != nil {
			return InstallResult{}, err
		}
	}
	if err := preferences.Validate(); err != nil {
		return InstallResult{}, err
	}
	preview, err := installPreview(i.Paths, preferences, configExists, migrated)
	if err != nil {
		return InstallResult{}, err
	}
	if i.Prompter != nil {
		if err := i.Prompter.ShowPreview(preview); err != nil {
			return InstallResult{}, err
		}
	} else if i.Previewer != nil {
		if err := i.Previewer(preview); err != nil {
			return InstallResult{}, err
		}
	}
	if !request.NonInteractive {
		confirmed, err := i.Prompter.Confirm()
		if err != nil {
			return InstallResult{}, err
		}
		if !confirmed {
			return InstallResult{}, ErrCancelled
		}
	}
	lock, err := i.Store.AcquireLock()
	if err != nil {
		return InstallResult{}, err
	}
	defer lock.Close()
	if err := ValidateManagedFile(i.Paths.Agents); err != nil {
		return InstallResult{}, fmt.Errorf("validate AGENTS.md: %w", err)
	}
	if err := ValidateManagedFile(i.Paths.Skill); err != nil {
		return InstallResult{}, fmt.Errorf("validate skill: %w", err)
	}
	if err := rejectSymlinkComponents(i.Paths.Binary); err != nil {
		return InstallResult{}, err
	}
	controlTaskID, controlTaskChanged, ensureErr := i.ControlTasks.EnsureControlTask(ctx, controlTaskID)
	if !canonical(controlTaskID) {
		if ensureErr != nil {
			return InstallResult{}, fmt.Errorf("ensure control task: %w", ensureErr)
		}
		return InstallResult{}, errors.New("control task returned a noncanonical ID")
	}
	nextConfig := preferences.config(controlTaskID)
	if err := nextConfig.Validate(); err != nil {
		return InstallResult{}, err
	}
	resources := make([]string, 0, 7)
	if !configExists || nextConfig != currentConfig {
		if err := i.Store.SaveConfig(nextConfig); err != nil {
			return InstallResult{}, fmt.Errorf("persist control task identity and config: %w", err)
		}
		resources = append(resources, "config")
	}
	if ensureErr != nil {
		return InstallResult{}, fmt.Errorf("ensure control task: %w", ensureErr)
	}
	if controlTaskChanged {
		resources = append(resources, "control_task")
	}
	if !stateExists || !reflectStateEqual(currentState, mustLoadState(i.Store)) {
		if err := i.Store.SaveState(currentState); err != nil {
			return InstallResult{}, fmt.Errorf("save state: %w", err)
		}
		resources = append(resources, "state")
	}
	binaryChanged, err := binaryNeedsInstall(i.Paths.Binary, i.Binary)
	if err != nil {
		return InstallResult{}, err
	}
	if binaryChanged {
		if err := i.Binary.Install(i.Paths.Binary); err != nil {
			return InstallResult{}, fmt.Errorf("install binary: %w", err)
		}
		resources = append(resources, "binary")
	}
	agentsChanged, err := applyManaged(i.Paths.Agents, preferences.AgentsEnabled, []byte(assets.AgentsManagedContent))
	if err != nil {
		return InstallResult{}, fmt.Errorf("update AGENTS.md: %w", err)
	}
	if agentsChanged {
		resources = append(resources, "agents")
	}
	skillChanged, err := applyManaged(i.Paths.Skill, true, []byte(assets.SkillManagedContent))
	if err != nil {
		return InstallResult{}, fmt.Errorf("update skill: %w", err)
	}
	if skillChanged {
		resources = append(resources, "skill")
	}
	schedulerChanged, err := i.Scheduler.Stage(ctx, nextConfig)
	if err != nil {
		return InstallResult{}, fmt.Errorf("stage scheduler: %w", err)
	}
	if err := i.SelfTester.Test(ctx, SelfTestInput{Paths: i.Paths, Config: nextConfig, State: currentState}); err != nil {
		return InstallResult{}, fmt.Errorf("self-test: %w", err)
	}
	if err := i.Scheduler.StopLegacy(ctx); err != nil {
		return InstallResult{}, fmt.Errorf("stop legacy scheduler: %w", err)
	}
	if err := i.Scheduler.VerifyLegacyStopped(ctx); err != nil {
		return InstallResult{}, fmt.Errorf("verify legacy scheduler stopped: %w", err)
	}
	enabledChanged, err := i.Scheduler.Enable(ctx)
	if err != nil {
		return InstallResult{}, fmt.Errorf("enable scheduler: %w", err)
	}
	if err := i.Scheduler.VerifyHealthy(ctx); err != nil {
		return InstallResult{}, fmt.Errorf("verify scheduler health: %w", err)
	}
	if schedulerChanged || enabledChanged {
		resources = append(resources, "launchagent")
	}
	return InstallResult{Config: nextConfig, State: currentState, Paths: i.Paths, Migrated: migrated, Reinstalled: configExists, Preview: preview, Changed: len(resources) > 0, Resources: resources}, nil
}

func mustLoadState(store Store) state.State {
	value, err := store.LoadState()
	if err != nil {
		return state.State{}
	}
	return value
}

func reflectStateEqual(left, right state.State) bool {
	return reflect.DeepEqual(left, right)
}

func binaryNeedsInstall(path string, installer BinaryInstaller) (bool, error) {
	file, ok := installer.(FileBinaryInstaller)
	if !ok {
		return true, nil
	}
	source, err := os.ReadFile(file.Source)
	if err != nil {
		return false, err
	}
	destination, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return !bytes.Equal(source, destination), nil
}

func applyManaged(path string, enabled bool, content []byte) (bool, error) {
	before, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return false, err
	}
	if enabled {
		err = WriteManagedBlock(path, content)
	} else {
		err = DeleteManagedBlock(path)
	}
	if err != nil {
		return false, err
	}
	after, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return false, err
	}
	return !bytes.Equal(before, after), nil
}

func applyPreferencePatch(value Preferences, patch PreferencePatch) Preferences {
	if patch.HeartbeatSeconds != nil {
		value.HeartbeatSeconds = *patch.HeartbeatSeconds
	}
	if patch.ArchiveEnabled != nil {
		value.ArchiveEnabled = *patch.ArchiveEnabled
	}
	if patch.ArchiveAfterDays != nil {
		value.ArchiveAfterDays = *patch.ArchiveAfterDays
	}
	if patch.RenameEnabled != nil {
		value.RenameEnabled = *patch.RenameEnabled
	}
	if patch.AgentsEnabled != nil {
		value.AgentsEnabled = *patch.AgentsEnabled
	}
	if patch.ClassifierModel != nil {
		value.ClassifierModel = *patch.ClassifierModel
	}
	if patch.ClassifierEffort != nil {
		value.ClassifierEffort = *patch.ClassifierEffort
	}
	if patch.ClassifierContextBudgetBytes != nil {
		value.ClassifierContextBudgetBytes = *patch.ClassifierContextBudgetBytes
	}
	return value
}

func (i Installer) validate() error {
	if !filepath.IsAbs(i.Paths.Home) || i.Store == nil || i.Scheduler == nil || i.ControlTasks == nil || i.Binary == nil || i.SelfTester == nil {
		return errors.New("installer dependencies and absolute home are required")
	}
	if i.Legacy == nil {
		i.Legacy = FileLegacyLoader{}
	}
	return nil
}

func (i Installer) loadMigration(ctx context.Context) (Migration, bool, error) {
	loader := i.Legacy
	if loader == nil {
		loader = FileLegacyLoader{}
	}
	data, err := loader.Load(i.Paths.LegacyState)
	if errors.Is(err, fs.ErrNotExist) {
		return Migration{}, false, nil
	}
	if err != nil {
		return Migration{}, false, fmt.Errorf("read ThreadWatch state: %w", err)
	}
	migration, err := DecodeThreadWatch(data)
	if err != nil {
		return Migration{}, false, err
	}
	interval, detected, err := i.Scheduler.DetectLegacyInterval(ctx)
	if err != nil {
		return Migration{}, false, fmt.Errorf("detect legacy interval: %w", err)
	}
	if detected && interval <= 0 {
		return Migration{}, false, errors.New("detected legacy interval is invalid")
	}
	if detected {
		migration.HeartbeatSeconds = interval
	}
	return migration, true, nil
}

func loadExisting(store Store) (config.Config, state.State, bool, bool, error) {
	currentConfig, configErr := store.LoadConfig()
	currentState, stateErr := store.LoadState()
	configMissing := errors.Is(configErr, fs.ErrNotExist)
	stateMissing := errors.Is(stateErr, fs.ErrNotExist)
	if configErr != nil && !configMissing {
		return config.Config{}, state.State{}, false, false, fmt.Errorf("load existing config: %w", configErr)
	}
	if stateErr != nil && !stateMissing {
		return config.Config{}, state.State{}, false, false, fmt.Errorf("load existing state: %w", stateErr)
	}
	return currentConfig, currentState, !configMissing, !stateMissing, nil
}

func installPreview(paths Paths, preferences Preferences, reinstall, migration bool) (Preview, error) {
	mode := "install"
	if reinstall {
		mode = "reinstall"
	} else if migration {
		mode = "migration install"
	}
	agents, err := ManagedMutationPreview(paths.Agents, preferences.AgentsEnabled, []byte(assets.AgentsManagedContent))
	if err != nil {
		return Preview{}, fmt.Errorf("preview AGENTS.md mutation: %w", err)
	}
	skill, err := ManagedMutationPreview(paths.Skill, true, []byte(assets.SkillManagedContent))
	if err != nil {
		return Preview{}, fmt.Errorf("preview skill mutation: %w", err)
	}
	return Preview{Operation: mode, Lines: []string{
		"ThreadBear uses deterministic checks first and invokes the classifier only for unresolved changed tasks",
		"binary: " + paths.Binary,
		"state: " + paths.StateDirectory,
		agents,
		skill,
		"LaunchAgent staged disabled, self-tested, then enabled: " + paths.LaunchAgent,
		"one persistent Codex control task",
		fmt.Sprintf("heartbeat=%ds archive=%t/%dd rename=%t agents=%t classifier=%s/%s context=%d bytes", preferences.HeartbeatSeconds, preferences.ArchiveEnabled, preferences.ArchiveAfterDays, preferences.RenameEnabled, preferences.AgentsEnabled, preferences.ClassifierModel, preferences.ClassifierEffort, preferences.ClassifierContextBudgetBytes),
	}}, nil
}

func ManagedMutationPreview(path string, enabled bool, content []byte) (string, error) {
	original, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	var updated []byte
	if enabled {
		updated, err = UpdateManagedBlock(original, content)
	} else {
		updated, err = RemoveManagedBlock(original)
	}
	if err != nil {
		return "", err
	}
	action := "no change"
	if !bytes.Equal(original, updated) {
		action = "write managed block"
		if !enabled {
			action = "remove managed block"
		}
	}
	return path + ": " + action, nil
}

type FileLegacyLoader struct{}

func (FileLegacyLoader) Load(path string) ([]byte, error) {
	return os.ReadFile(path)
}

type FileBinaryInstaller struct {
	Source string
}

func (f FileBinaryInstaller) Install(destination string) error {
	if !filepath.IsAbs(f.Source) || !filepath.IsAbs(destination) {
		return errors.New("binary paths must be absolute")
	}
	if err := rejectSymlinkComponents(f.Source); err != nil {
		return err
	}
	data, err := os.ReadFile(f.Source)
	if err != nil {
		return err
	}
	return writeAtomic(destination, data, 0o700)
}

type SelfTestInput struct {
	Paths  Paths
	Config config.Config
	State  state.State
}

type SelfTestProbe interface {
	Platform() (string, string, int)
	ValidateCodex(context.Context, string) error
}

type CoreSelfTester struct {
	Probe SelfTestProbe
	Store Store
}

func (s CoreSelfTester) Test(ctx context.Context, input SelfTestInput) error {
	if s.Probe == nil {
		return errors.New("self-test probe is required")
	}
	platform, architecture, major := s.Probe.Platform()
	if platform != "darwin" || major < 12 || architecture != "arm64" && architecture != "amd64" {
		return fmt.Errorf("unsupported platform %s/%s", platform, architecture)
	}
	if err := s.Probe.ValidateCodex(ctx, filepath.Join(input.Paths.Home, ".codex")); err != nil {
		return fmt.Errorf("Codex validation: %w", err)
	}
	if err := input.Config.Validate(); err != nil {
		return fmt.Errorf("config validation: %w", err)
	}
	if err := input.State.Validate(); err != nil {
		return fmt.Errorf("state validation: %w", err)
	}
	if err := VerifyManagedSurface(input.Paths.Agents, input.Config.AgentsEnabled, []byte(assets.AgentsManagedContent)); err != nil {
		return fmt.Errorf("AGENTS surface validation: %w", err)
	}
	if err := VerifyManagedSurface(input.Paths.Skill, true, []byte(assets.SkillManagedContent)); err != nil {
		return fmt.Errorf("skill surface validation: %w", err)
	}
	if s.Store != nil {
		persistedConfig, err := s.Store.LoadConfig()
		if err != nil {
			return fmt.Errorf("read persisted config: %w", err)
		}
		if err := persistedConfig.Validate(); err != nil {
			return fmt.Errorf("persisted config validation: %w", err)
		}
		if persistedConfig != input.Config {
			return errors.New("persisted config does not match staged config")
		}
		persistedState, err := s.Store.LoadState()
		if err != nil {
			return fmt.Errorf("read persisted state: %w", err)
		}
		if err := persistedState.Validate(); err != nil {
			return fmt.Errorf("persisted state validation: %w", err)
		}
		if !reflect.DeepEqual(persistedState, input.State) {
			return errors.New("persisted state does not match staged state")
		}
	}
	if err := rejectSymlinkComponents(input.Paths.StateDirectory); err != nil {
		return err
	}
	info, err := os.Stat(input.Paths.StateDirectory)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0o077 != 0 {
		return errors.New("state directory is unavailable or not private")
	}
	probe, err := os.CreateTemp(input.Paths.StateDirectory, ".self-test-*")
	if err != nil {
		return errors.New("state directory is not writable")
	}
	probePath := probe.Name()
	if err := errors.Join(probe.Close(), os.Remove(probePath)); err != nil {
		return errors.New("state directory writable probe failed")
	}
	if err := rejectSymlinkComponents(input.Paths.Binary); err != nil {
		return err
	}
	info, err = os.Stat(input.Paths.Binary)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o100 == 0 {
		return errors.New("installed binary is unavailable or not executable")
	}
	if err := rejectSymlinkComponents(input.Paths.LaunchAgent); err != nil {
		return err
	}
	plist, err := os.ReadFile(input.Paths.LaunchAgent)
	if err != nil {
		return errors.New("staged LaunchAgent plist is unavailable")
	}
	info, err = os.Stat(input.Paths.LaunchAgent)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		return errors.New("staged LaunchAgent plist is not a private regular file")
	}
	interval := fmt.Sprintf("<integer>%d</integer>", input.Config.HeartbeatSeconds)
	if !bytes.Contains(plist, []byte(config.LaunchAgentLabel)) || !bytes.Contains(plist, []byte("<key>StartInterval</key>")) || !bytes.Contains(plist, []byte(interval)) {
		return errors.New("staged LaunchAgent plist does not match the requested scheduler")
	}
	return nil
}

func VerifyManagedSurface(path string, enabled bool, content []byte) error {
	if err := ValidateManagedFile(path); err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) && !enabled {
		return nil
	}
	if err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return errors.New("managed surface is unavailable")
	}
	expected := ManagedBlock(content)
	found := bytes.Contains(data, expected)
	if enabled && !found {
		return errors.New("expected managed block is missing or stale")
	}
	if !enabled && bytes.Contains(data, []byte(ManagedBlockStart)) {
		return errors.New("disabled managed block remains")
	}
	return nil
}

type RuntimeProbe struct{}

func (RuntimeProbe) Platform() (string, string, int) {
	major := 0
	if runtime.GOOS == "darwin" {
		output, err := exec.Command("/usr/bin/sw_vers", "-productVersion").Output()
		if err == nil {
			major, _ = strconv.Atoi(strings.Split(strings.TrimSpace(string(output)), ".")[0])
		}
	}
	return runtime.GOOS, runtime.GOARCH, major
}

func (RuntimeProbe) ValidateCodex(_ context.Context, codexHome string) error {
	if _, err := exec.LookPath("codex"); err != nil {
		return errors.New("Codex executable is unavailable")
	}
	info, err := os.Stat(codexHome)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("Codex home is not a directory")
	}
	return nil
}

func (p PreferencePatch) Apply(value Preferences) Preferences {
	if p.HeartbeatSeconds != nil {
		value.HeartbeatSeconds = *p.HeartbeatSeconds
	}
	if p.ArchiveEnabled != nil {
		value.ArchiveEnabled = *p.ArchiveEnabled
	}
	if p.ArchiveAfterDays != nil {
		value.ArchiveAfterDays = *p.ArchiveAfterDays
	}
	if p.RenameEnabled != nil {
		value.RenameEnabled = *p.RenameEnabled
	}
	if p.AgentsEnabled != nil {
		value.AgentsEnabled = *p.AgentsEnabled
	}
	if p.ClassifierModel != nil {
		value.ClassifierModel = *p.ClassifierModel
	}
	if p.ClassifierEffort != nil {
		value.ClassifierEffort = *p.ClassifierEffort
	}
	if p.ClassifierContextBudgetBytes != nil {
		value.ClassifierContextBudgetBytes = *p.ClassifierContextBudgetBytes
	}
	return value
}
