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
	"github.com/ericlitman/threadbear/internal/codex"
	"github.com/ericlitman/threadbear/internal/config"
	"github.com/ericlitman/threadbear/internal/state"
)

type Paths struct {
	Home                 string
	CodexHome            string
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
	return PathsForHomes(home, filepath.Join(home, ".codex"))
}

func PathsForHomes(home, codexHome string) Paths {
	stateDirectory := filepath.Join(home, ".local", "share", "threadbear")
	return Paths{
		Home:                 home,
		CodexHome:            codexHome,
		Binary:               filepath.Join(home, ".local", "bin", "threadbear"),
		StateDirectory:       stateDirectory,
		Config:               filepath.Join(stateDirectory, "config.json"),
		State:                filepath.Join(stateDirectory, "state.json"),
		Agents:               filepath.Join(codexHome, "AGENTS.md"),
		Skill:                filepath.Join(codexHome, "skills", "threadbear", "SKILL.md"),
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
	Loaded(context.Context) (bool, error)
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

type InstallFailure struct {
	Step  string
	Cause string
	Err   error
}

func (e *InstallFailure) Error() string { return fmt.Sprintf("install %s: %s", e.Step, e.Cause) }
func (e *InstallFailure) Unwrap() error { return e.Err }

func Fail(step string, err error) error {
	if err == nil {
		return nil
	}
	return &InstallFailure{Step: step, Cause: err.Error(), Err: err}
}

type Installer struct {
	Paths                      Paths
	Store                      Store
	Scheduler                  Scheduler
	ControlTasks               ControlTasks
	Binary                     BinaryInstaller
	SelfTester                 SelfTester
	Legacy                     LegacyLoader
	Prompter                   Prompter
	Previewer                  func(Preview) error
	CodexExecutable            string
	CodexSpawnPath             string
	ResolveCodexExecutable     func(string, string) (string, error)
	ResolveCodexExecutableSpec func(string, string) (codex.ExecutableSpec, error)
}

func (i Installer) Install(ctx context.Context, request InstallRequest) (InstallResult, error) {
	if err := i.validate(); err != nil {
		return InstallResult{}, Fail("validate", err)
	}
	previewConfig, previewState, previewConfigExists, previewStateExists, err := loadExisting(i.Store)
	if err != nil {
		return InstallResult{}, Fail("load_preview_state", err)
	}
	executableSpec, err := i.resolveCodexExecutableSpec(previewConfig, previewConfigExists)
	if err != nil {
		return InstallResult{}, Fail("resolve_codex_executable", err)
	}
	i.CodexExecutable = executableSpec.Path
	i.CodexSpawnPath = executableSpec.SpawnPath
	if consumer, ok := i.ControlTasks.(interface{ SetCodexExecutableSpec(codex.ExecutableSpec) }); ok {
		consumer.SetCodexExecutableSpec(executableSpec)
	}
	migrationNeeded := !previewConfigExists || !previewStateExists
	legacyPresent, err := legacyInstallationPresent(i.Paths)
	if err != nil {
		return InstallResult{}, Fail("inspect_legacy_presence", err)
	}
	legacyInterval, legacyIntervalFound, err := i.detectLegacyInterval(ctx, migrationNeeded && legacyPresent)
	if err != nil {
		return InstallResult{}, Fail("detect_legacy_interval", err)
	}
	legacyStateExists := migrationNeeded && migrationCandidate(i.Paths.LegacyState)
	previewPreferences := DefaultPreferences()
	previewControlTaskID := "threadbear-install-candidate"
	if previewConfigExists {
		previewPreferences = preferencesFromConfig(previewConfig)
		previewControlTaskID = previewConfig.ControlTaskID
	} else if legacyStateExists && legacyIntervalFound {
		previewPreferences.HeartbeatSeconds = legacyInterval
	}
	var selectedPreferences *Preferences
	if request.Preferences != nil {
		value := *request.Preferences
		selectedPreferences = &value
		previewPreferences = value
	}
	previewPreferences = applyPreferencePatch(previewPreferences, request.Patch)
	if request.NonInteractive {
		if !request.Confirm {
			return InstallResult{}, Fail("confirmation", errors.New("noninteractive install requires confirm"))
		}
	} else {
		if i.Prompter == nil {
			return InstallResult{}, Fail("preferences", errors.New("interactive install requires a prompter"))
		}
		collected, collectErr := i.Prompter.Collect(previewPreferences)
		if collectErr != nil {
			return InstallResult{}, Fail("preferences", collectErr)
		}
		selectedPreferences = &collected
		previewPreferences = collected
	}
	if err := previewPreferences.Validate(); err != nil {
		return InstallResult{}, Fail("preferences", err)
	}
	preview, err := installPreview(i.Paths, previewPreferences, previewConfigExists, migrationNeeded && legacyStateExists)
	if err != nil {
		return InstallResult{}, Fail("preview", err)
	}
	if i.Prompter != nil {
		if err := i.Prompter.ShowPreview(preview); err != nil {
			return InstallResult{}, Fail("preview", err)
		}
	} else if i.Previewer != nil {
		if err := i.Previewer(preview); err != nil {
			return InstallResult{}, Fail("preview", err)
		}
	}
	if !request.NonInteractive {
		confirmed, confirmErr := i.Prompter.Confirm()
		if confirmErr != nil {
			return InstallResult{}, Fail("confirmation", confirmErr)
		}
		if !confirmed {
			return InstallResult{}, ErrCancelled
		}
	}
	candidateState := previewState
	if !previewStateExists {
		candidateState = state.New()
	}
	candidateConfig := previewPreferences.config(previewControlTaskID, i.CodexExecutable, i.CodexSpawnPath)
	resources, err := i.stageCandidate(ctx, candidateConfig, candidateState)
	if err != nil {
		return InstallResult{}, err
	}
	lock, err := i.Store.AcquireLock()
	if err != nil {
		return InstallResult{}, Fail("acquire_lock", err)
	}
	lockHeld := true
	defer func() {
		if lockHeld {
			_ = lock.Close()
		}
	}()
	currentConfig, currentState, configExists, stateExists, err := loadExisting(i.Store)
	if err != nil {
		return InstallResult{}, Fail("reload_existing", err)
	}
	legacyStopped := false
	if legacyPresent {
		if err := i.Scheduler.StopLegacy(ctx); err != nil {
			return InstallResult{}, Fail("stop_legacy_scheduler", err)
		}
		legacyStopped = true
		if err := i.Scheduler.VerifyLegacyStopped(ctx); err != nil {
			return InstallResult{}, i.stoppedLegacyFailure("verify_legacy_stopped", err)
		}
	}
	fail := func(step string, err error) error {
		if legacyStopped {
			return i.stoppedLegacyFailure(step, err)
		}
		return Fail(step, err)
	}
	finalMigration, migrated, err := i.loadMigrationState(!configExists || !stateExists)
	if err != nil {
		return InstallResult{}, fail("load_final_migration", err)
	}
	preferences := DefaultPreferences()
	controlTaskID := ""
	if configExists {
		preferences = preferencesFromConfig(currentConfig)
		controlTaskID = currentConfig.ControlTaskID
	}
	if migrated {
		if controlTaskID == "" {
			controlTaskID = finalMigration.ControlTaskID
		}
		if !stateExists {
			currentState = finalMigration.State
		}
		if !configExists && legacyIntervalFound {
			preferences.HeartbeatSeconds = legacyInterval
		}
	}
	if !stateExists && !migrated {
		currentState = state.New()
	}
	if selectedPreferences != nil {
		preferences = *selectedPreferences
	}
	preferences = applyPreferencePatch(preferences, request.Patch)
	if err := preferences.Validate(); err != nil {
		return InstallResult{}, fail("preferences", err)
	}
	if preferences != previewPreferences {
		return InstallResult{}, fail("stale_preview", errors.New("configuration changed after preview; rerun install to review and confirm the current preferences"))
	}
	controlTaskID, controlTaskChanged, ensureErr := i.ControlTasks.EnsureControlTask(ctx, controlTaskID)
	if !canonical(controlTaskID) {
		if ensureErr != nil {
			return InstallResult{}, fail("ensure_control_task", ensureErr)
		}
		return InstallResult{}, fail("ensure_control_task", errors.New("control task returned a noncanonical ID"))
	}
	nextConfig := preferences.config(controlTaskID, i.CodexExecutable, i.CodexSpawnPath)
	if err := nextConfig.Validate(); err != nil {
		return InstallResult{}, fail("validate_config", err)
	}
	if !configExists || !reflect.DeepEqual(nextConfig, currentConfig) {
		if err := i.Store.SaveConfig(nextConfig); err != nil {
			return InstallResult{}, fail("persist_config", err)
		}
		resources = appendUnique(resources, "config")
	}
	if ensureErr != nil {
		return InstallResult{}, fail("ensure_control_task", ensureErr)
	}
	if controlTaskChanged {
		resources = appendUnique(resources, "control_task")
	}
	if !stateExists {
		if err := i.Store.SaveState(currentState); err != nil {
			return InstallResult{}, fail("persist_state", err)
		}
		resources = appendUnique(resources, "state")
	}
	finalSchedulerChanged, err := i.Scheduler.Stage(ctx, nextConfig)
	if err != nil {
		return InstallResult{}, fail("stage_final_scheduler", err)
	}
	enabledChanged, err := i.Scheduler.Enable(ctx)
	if err != nil {
		return InstallResult{}, fail("enable_scheduler", err)
	}
	if err := i.Scheduler.VerifyHealthy(ctx); err != nil {
		return InstallResult{}, fail("verify_scheduler_health", err)
	}
	if finalSchedulerChanged || enabledChanged {
		resources = appendUnique(resources, "launchagent")
	}
	if err := i.SelfTester.Test(ctx, SelfTestInput{Paths: i.Paths, Config: nextConfig, State: currentState}); err != nil {
		return InstallResult{}, fail("installed_self_test", err)
	}
	if err := lock.Close(); err != nil {
		return InstallResult{}, fail("release_lock", err)
	}
	lockHeld = false
	return InstallResult{Config: nextConfig, State: currentState, Paths: i.Paths, Migrated: migrated, Reinstalled: configExists, Preview: preview, Changed: len(resources) > 0, Resources: resources}, nil
}

func (i Installer) stageCandidate(ctx context.Context, candidateConfig config.Config, candidateState state.State) ([]string, error) {
	if err := candidateConfig.Validate(); err != nil {
		return nil, Fail("validate_candidate_config", err)
	}
	if err := ValidateManagedFile(i.Paths.Agents); err != nil {
		return nil, Fail("validate_candidate_files", fmt.Errorf("validate AGENTS.md: %w", err))
	}
	if err := ValidateManagedFile(i.Paths.Skill); err != nil {
		return nil, Fail("validate_candidate_files", fmt.Errorf("validate skill: %w", err))
	}
	if err := rejectSymlinkComponents(i.Paths.Binary); err != nil {
		return nil, Fail("validate_candidate_binary", err)
	}
	resources := make([]string, 0, 4)
	schedulerChanged, err := i.Scheduler.Stage(ctx, candidateConfig)
	if err != nil {
		return nil, Fail("stage_candidate_scheduler", err)
	}
	if schedulerChanged {
		resources = append(resources, "launchagent")
	}
	binaryChanged, err := binaryNeedsInstall(i.Paths.Binary, i.Binary)
	if err != nil {
		return nil, Fail("inspect_candidate_binary", err)
	}
	if binaryChanged {
		if err := i.Binary.Install(i.Paths.Binary); err != nil {
			return nil, Fail("stage_candidate_binary", err)
		}
		resources = append(resources, "binary")
	}
	agentsChanged, err := applyManaged(i.Paths.Agents, candidateConfig.AgentsEnabled, []byte(assets.AgentsManagedContent))
	if err != nil {
		return nil, Fail("stage_candidate_agents", err)
	}
	if agentsChanged {
		resources = append(resources, "agents")
	}
	skillChanged, err := applyManaged(i.Paths.Skill, true, []byte(assets.SkillManagedContent))
	if err != nil {
		return nil, Fail("stage_candidate_skill", err)
	}
	if skillChanged {
		resources = append(resources, "skill")
	}
	if err := i.SelfTester.Test(ctx, SelfTestInput{Paths: i.Paths, Config: candidateConfig, State: candidateState, Candidate: true}); err != nil {
		return nil, Fail("candidate_self_test", err)
	}
	return resources, nil
}

func (i Installer) stoppedLegacyFailure(step string, err error) error {
	cause := fmt.Sprintf("ThreadWatch was stopped before ThreadBear installation completed: %v. To re-enable ThreadWatch, rename %s back to %s, then run `launchctl enable gui/$(id -u)/org.litman.threadwatch` and `launchctl bootstrap gui/$(id -u) %s`", err, i.Paths.LegacyLaunchAgent+".disabled-by-threadbear", i.Paths.LegacyLaunchAgent, i.Paths.LegacyLaunchAgent)
	return Fail(step, errors.New(cause))
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
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
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o700 {
		return true, nil
	}
	destination, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	return !bytes.Equal(source, destination), nil
}

func legacyInstallationPresent(paths Paths) (bool, error) {
	if _, err := os.Lstat(paths.LegacyLaunchAgent); err == nil {
		return true, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return false, fmt.Errorf("inspect legacy LaunchAgent plist: %w", err)
	}
	entries, err := os.ReadDir(paths.LegacyStateDirectory)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect legacy state directory: %w", err)
	}
	return len(entries) > 0, nil
}

func migrationCandidate(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
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

func (i Installer) resolveCodexExecutableSpec(persisted config.Config, persistedExists bool) (codex.ExecutableSpec, error) {
	capturedPath := codex.SanitizePath(os.Getenv("PATH"))
	if persistedExists && persisted.CodexExecutable != "" {
		spec := codex.ExecutableSpec{Path: persisted.CodexExecutable, SpawnPath: persisted.CodexSpawnPath}
		if spec.SpawnPath == "" {
			derived, err := codex.DeriveExecutableSpec(i.Paths.Home, spec.Path, capturedPath)
			if err != nil {
				return codex.ExecutableSpec{}, err
			}
			spec = derived
		}
		if err := codex.ProbeExecutable(i.Paths.Home, spec); err != nil {
			return codex.ExecutableSpec{}, err
		}
		return spec, nil
	}
	if i.CodexExecutable != "" || i.CodexSpawnPath != "" {
		spec := codex.ExecutableSpec{Path: i.CodexExecutable, SpawnPath: i.CodexSpawnPath}
		if spec.SpawnPath == "" {
			derived, err := codex.DeriveExecutableSpec(i.Paths.Home, spec.Path, capturedPath)
			if err != nil {
				return codex.ExecutableSpec{}, err
			}
			spec = derived
		}
		if err := codex.ProbeExecutable(i.Paths.Home, spec); err != nil {
			return codex.ExecutableSpec{}, err
		}
		return spec, nil
	}
	if i.ResolveCodexExecutableSpec != nil {
		spec, err := i.ResolveCodexExecutableSpec(i.Paths.Home, capturedPath)
		if err != nil {
			return codex.ExecutableSpec{}, err
		}
		if err := codex.ProbeExecutable(i.Paths.Home, spec); err != nil {
			return codex.ExecutableSpec{}, err
		}
		return spec, nil
	}
	if i.ResolveCodexExecutable != nil {
		executable, err := i.ResolveCodexExecutable(i.Paths.Home, capturedPath)
		if err != nil {
			return codex.ExecutableSpec{}, err
		}
		spec, err := codex.DeriveExecutableSpec(i.Paths.Home, executable, capturedPath)
		if err != nil {
			return codex.ExecutableSpec{}, err
		}
		if err := codex.ProbeExecutable(i.Paths.Home, spec); err != nil {
			return codex.ExecutableSpec{}, err
		}
		return spec, nil
	}
	return codex.ResolveExecutableSpec(i.Paths.Home, capturedPath)
}

func (i Installer) validate() error {
	if !filepath.IsAbs(i.Paths.Home) || !filepath.IsAbs(i.Paths.CodexHome) || i.Store == nil || i.Scheduler == nil || i.ControlTasks == nil || i.Binary == nil || i.SelfTester == nil {
		return errors.New("installer dependencies and absolute home are required")
	}
	if i.Legacy == nil {
		i.Legacy = FileLegacyLoader{}
	}
	return nil
}

func (i Installer) detectLegacyInterval(ctx context.Context, needed bool) (int, bool, error) {
	if !needed {
		return 0, false, nil
	}
	interval, detected, err := i.Scheduler.DetectLegacyInterval(ctx)
	if err != nil {
		return 0, false, fmt.Errorf("detect legacy interval: %w", err)
	}
	if detected && interval <= 0 {
		return 0, false, errors.New("detected legacy interval is invalid")
	}
	return interval, detected, nil
}

func (i Installer) loadMigrationState(needed bool) (Migration, bool, error) {
	if !needed {
		return Migration{}, false, nil
	}
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
	info, err := os.Stat(f.Source)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("binary source must be a regular file")
	}
	data, err := os.ReadFile(f.Source)
	if err != nil {
		return err
	}
	return writeAtomic(destination, data, 0o700)
}

type SelfTestInput struct {
	Paths     Paths
	Config    config.Config
	State     state.State
	Candidate bool
}

type SelfTestProbe interface {
	Platform() (string, string, int)
	ValidateCodex(context.Context, string, string, codex.ExecutableSpec) error
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
	if err := s.Probe.ValidateCodex(ctx, input.Paths.Home, input.Paths.CodexHome, codex.ExecutableSpec{Path: input.Config.CodexExecutable, SpawnPath: input.Config.CodexSpawnPath}); err != nil {
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
	if s.Store != nil && !input.Candidate {
		persistedConfig, err := s.Store.LoadConfig()
		if err != nil {
			return fmt.Errorf("read persisted config: %w", err)
		}
		if err := persistedConfig.Validate(); err != nil {
			return fmt.Errorf("persisted config validation: %w", err)
		}
		if !reflect.DeepEqual(persistedConfig, input.Config) {
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

func (RuntimeProbe) ValidateCodex(_ context.Context, home, codexHome string, spec codex.ExecutableSpec) error {
	if err := codex.ProbeExecutable(home, spec); err != nil {
		return err
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
