package install

import (
	"bytes"
	"context"
	"encoding/json"
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
	"github.com/ericlitman/threadbear/internal/tokens"
)

const bear60ControllerID = "019f8f9f-77fb-7240-b9ae-7963527b9af3"

type Paths struct {
	Home               string
	CodexHome          string
	Binary             string
	StateDirectory     string
	Config             string
	ConfigRepairBackup string
	State              string
	Agents             string
	Skill              string
	LaunchAgent        string
	LaunchAgentStdout  string
	LaunchAgentStderr  string
}

func PathsForHome(home string) Paths {
	return PathsForHomes(home, filepath.Join(home, ".codex"))
}

func PathsForHomes(home, codexHome string) Paths {
	stateDirectory := filepath.Join(home, ".local", "share", "threadbear")
	return Paths{
		Home: home, CodexHome: codexHome,
		Binary:             filepath.Join(home, ".local", "bin", "threadbear"),
		StateDirectory:     stateDirectory,
		Config:             filepath.Join(stateDirectory, "config.json"),
		ConfigRepairBackup: filepath.Join(stateDirectory, "config.json.bear-60-backup"),
		State:              filepath.Join(stateDirectory, "state.json"),
		Agents:             filepath.Join(codexHome, "AGENTS.md"),
		Skill:              filepath.Join(codexHome, "skills", "threadbear", "SKILL.md"),
		LaunchAgent:        filepath.Join(home, "Library", "LaunchAgents", config.LaunchAgentLabel+".plist"),
		LaunchAgentStdout:  filepath.Join(stateDirectory, "logs", "heartbeat.stdout.log"),
		LaunchAgentStderr:  filepath.Join(stateDirectory, "logs", "heartbeat.stderr.log"),
	}
}

type Lock interface{ Close() error }

type Store interface {
	AcquireLock() (Lock, error)
	LoadConfig() (config.Config, error)
	SaveConfig(config.Config) error
	LoadState() (state.State, error)
	SaveState(state.State) error
}

type DiskStore struct {
	store *state.Store
	paths Paths
}

func NewDiskStore(paths Paths) *DiskStore {
	return &DiskStore{store: state.NewStore(paths.StateDirectory), paths: paths}
}
func (s *DiskStore) AcquireLock() (Lock, error)           { return s.store.AcquireLock() }
func (s *DiskStore) LoadConfig() (config.Config, error)   { return s.store.LoadConfig() }
func (s *DiskStore) SaveConfig(value config.Config) error { return s.store.SaveConfig(value) }
func (s *DiskStore) LoadState() (state.State, error)      { return s.store.LoadState() }
func (s *DiskStore) SaveState(value state.State) error    { return s.store.SaveState(value) }
func (s *DiskStore) RepairControlTaskID(controlTaskID string) error {
	raw, err := os.ReadFile(s.paths.Config)
	if err != nil {
		return err
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return err
	}
	old, ok := decoded["control_task_id"]
	if !ok {
		return errors.New("config has no control_task_id")
	}
	var oldID string
	if err := json.Unmarshal(old, &oldID); err != nil {
		return err
	}
	if oldID != bear60ControllerID {
		return errors.New("config is not eligible for the BEAR-60 repair")
	}
	oldJSON, _ := json.Marshal(oldID)
	newJSON, _ := json.Marshal(controlTaskID)
	key := []byte(`"control_task_id"`)
	keyAt := bytes.Index(raw, key)
	if keyAt < 0 {
		return errors.New("config has no control_task_id")
	}
	valueAt := bytes.Index(raw[keyAt+len(key):], oldJSON)
	if valueAt < 0 {
		return errors.New("config control_task_id could not be replaced exactly")
	}
	valueAt += keyAt + len(key)
	repaired := append([]byte(nil), raw[:valueAt]...)
	repaired = append(repaired, newJSON...)
	repaired = append(repaired, raw[valueAt+len(oldJSON):]...)
	if info, err := os.Lstat(s.paths.ConfigRepairBackup); errors.Is(err, os.ErrNotExist) {
		file, createErr := os.OpenFile(s.paths.ConfigRepairBackup, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if createErr != nil {
			return createErr
		}
		if _, createErr = file.Write(raw); createErr == nil {
			createErr = file.Sync()
		}
		if createErr == nil {
			createErr = file.Chmod(0o400)
		}
		createErr = errors.Join(createErr, file.Close())
		if createErr != nil {
			return createErr
		}
		if createErr = syncInstallDirectory(s.paths.StateDirectory); createErr != nil {
			return createErr
		}
	} else if err != nil {
		return err
	} else {
		if !info.Mode().IsRegular() || info.Mode().Perm() != 0o400 {
			return errors.New("BEAR-60 backup is not an immutable private file")
		}
		backup, readErr := os.ReadFile(s.paths.ConfigRepairBackup)
		if readErr != nil {
			return readErr
		}
		if !bytes.Equal(backup, raw) {
			return errors.New("BEAR-60 backup does not match the raw pre-repair config")
		}
	}
	temporary, err := os.CreateTemp(s.paths.StateDirectory, ".config.json.repair-*")
	if err != nil {
		return err
	}
	name := temporary.Name()
	renamed := false
	defer func() {
		temporary.Close()
		if !renamed {
			os.Remove(name)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return err
	}
	if _, err := temporary.Write(repaired); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, s.paths.Config); err != nil {
		return err
	}
	renamed = true
	return syncInstallDirectory(s.paths.StateDirectory)
}

func syncInstallDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	return errors.Join(directory.Sync(), directory.Close())
}

type Scheduler interface {
	Stage(context.Context, config.Config) (bool, error)
	Enable(context.Context) (bool, error)
	VerifyHealthy(context.Context) error
	Loaded(context.Context) (bool, error)
	Remove(context.Context) error
}

type ControlTask struct {
	ID       string
	Archived bool
}

type ControlTasks interface {
	ReadControlTask(context.Context, string) (ControlTask, error)
	UnarchiveControlTask(context.Context, string) (bool, error)
	ArchiveControlTask(context.Context, string) (bool, error)
	PostWelcome(context.Context, string, string) error
}

type BinaryInstaller interface{ Install(string) error }
type SelfTester interface {
	Test(context.Context, SelfTestInput) error
}

type PreferencePatch struct {
	HeartbeatSeconds             *int
	ArchiveEnabled               *bool
	ArchiveAfterDays             *int
	RenameEnabled                *bool
	AutoUpdateEnabled            *bool
	TokenDisplay                 *tokens.Position
	AgentsEnabled                *bool
	ClassifierModel              *string
	ClassifierEffort             *config.ClassifierEffort
	ClassifierContextBudgetBytes *int
}

type InstallRequest struct {
	Preferences    *Preferences
	Patch          PreferencePatch
	ControlTaskID  string
	DryRun         bool
	NonInteractive bool
	Confirm        bool
}

type ControlTaskDisposition string

const (
	ControlTaskRetained   ControlTaskDisposition = "retained"
	ControlTaskStayedHome ControlTaskDisposition = "stayed_home"
	ControlTaskAdopted    ControlTaskDisposition = "adopted"
	ControlTaskReplaced   ControlTaskDisposition = "replaced"
	ControlTaskRepaired   ControlTaskDisposition = "repaired"
)

type InstallResult struct {
	Config                 config.Config
	State                  state.State
	Paths                  Paths
	Reinstalled            bool
	Preview                Preview
	Changed                bool
	Resources              []string
	Warnings               []string
	ControlTaskDisposition ControlTaskDisposition
	SuppliedControlTaskID  string
	Unarchived             bool
	DryRun                 bool
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

var ErrControlTaskIDRequired = errors.New("control task ID is required; in Codex, open the task that will be ThreadBear's home, copy its ID, then rerun with --control-task-id TASK_ID (see INSTALL.md)")

type Installer struct {
	Paths                      Paths
	Store                      Store
	Scheduler                  Scheduler
	ControlTasks               ControlTasks
	InstalledVersion           string
	Binary                     BinaryInstaller
	SelfTester                 SelfTester
	Prompter                   Prompter
	Previewer                  func(Preview) error
	CodexExecutable            string
	CodexSpawnPath             string
	ResolveCodexExecutable     func(string, string) (string, error)
	ResolveCodexExecutableSpec func(string, string) (codex.ExecutableSpec, error)
}

type controlTaskSelection struct {
	ID          string
	SuppliedID  string
	Disposition ControlTaskDisposition
	Unarchive   bool
	Welcome     bool
}

func (i Installer) Install(ctx context.Context, request InstallRequest) (InstallResult, error) {
	if err := i.validate(); err != nil {
		return InstallResult{}, Fail("validate", err)
	}
	previewConfig, previewState, previewConfigExists, previewStateExists, err := loadExisting(i.Store)
	if err != nil {
		return InstallResult{}, Fail("load_preview_state", err)
	}
	if !previewConfigExists && request.ControlTaskID == "" {
		return InstallResult{}, Fail("control_task_id_required", ErrControlTaskIDRequired)
	}
	executableSpec, err := i.resolveCodexExecutableSpec(previewConfig, previewConfigExists)
	if err != nil {
		return InstallResult{}, Fail("resolve_codex_executable", err)
	}
	i.CodexExecutable, i.CodexSpawnPath = executableSpec.Path, executableSpec.SpawnPath
	if consumer, ok := i.ControlTasks.(interface{ SetCodexExecutableSpec(codex.ExecutableSpec) }); ok {
		consumer.SetCodexExecutableSpec(executableSpec)
	}
	selection, err := i.selectControlTask(ctx, previewConfig, previewConfigExists, request.ControlTaskID)
	if err != nil {
		return InstallResult{}, err
	}
	previewPreferences := DefaultPreferences()
	if previewConfigExists {
		previewPreferences = preferencesFromConfig(previewConfig)
	}
	var selectedPreferences *Preferences
	if request.Preferences != nil {
		value := *request.Preferences
		selectedPreferences = &value
		previewPreferences = value
	}
	previewPreferences = request.Patch.Apply(previewPreferences)
	if request.DryRun {
		if err := previewPreferences.Validate(); err != nil {
			return InstallResult{}, Fail("preferences", err)
		}
	} else if request.NonInteractive {
		if !request.Confirm {
			return InstallResult{}, Fail("confirmation", errors.New("noninteractive install requires confirm"))
		}
	} else {
		if i.Prompter == nil {
			return InstallResult{}, Fail("preferences", errors.New("interactive install requires a prompter"))
		}
		if shower, ok := i.Prompter.(interface{ ShowBanner(string) }); ok {
			shower.ShowBanner(WelcomeBanner())
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
	previewConfigCandidate := previewPreferences.config(selection.ID, i.CodexExecutable, i.CodexSpawnPath)
	if err := validateRepairConfig(previewConfig, previewConfigCandidate, selection); err != nil {
		return InstallResult{}, Fail("repair_control_task", err)
	}
	preview, err := installPreview(i.Paths, previewPreferences, previewConfigExists, selection)
	if err != nil {
		return InstallResult{}, Fail("preview", err)
	}
	if request.DryRun {
		candidateState := previewState
		if !previewStateExists {
			candidateState = state.New()
		}
		return InstallResult{Config: previewConfigCandidate, State: candidateState, Paths: i.Paths, Reinstalled: previewConfigExists, Preview: preview, Resources: previewResources(preview), ControlTaskDisposition: selection.Disposition, SuppliedControlTaskID: selection.SuppliedID, Unarchived: selection.Unarchive, DryRun: true}, nil
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
		confirmed, confirmErr := i.Prompter.Confirm(true)
		if confirmErr != nil {
			return InstallResult{}, Fail("confirmation", confirmErr)
		}
		if !confirmed {
			return InstallResult{}, ErrCancelled
		}
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
	currentSelection, err := i.selectControlTask(ctx, currentConfig, configExists, request.ControlTaskID)
	if err != nil {
		return InstallResult{}, err
	}
	preferences := DefaultPreferences()
	if configExists {
		preferences = preferencesFromConfig(currentConfig)
	}
	if !stateExists {
		currentState = state.New()
	}
	if currentState.PendingWelcomeTaskID == currentSelection.ID {
		currentSelection.Welcome = true
	}
	if selectedPreferences != nil {
		preferences = *selectedPreferences
	}
	preferences = request.Patch.Apply(preferences)
	if err := preferences.Validate(); err != nil {
		return InstallResult{}, Fail("preferences", err)
	}
	currentPreview, err := installPreview(i.Paths, preferences, configExists, currentSelection)
	if err != nil {
		return InstallResult{}, Fail("recompute_preview", err)
	}
	if !reflect.DeepEqual(preview, currentPreview) {
		return InstallResult{}, Fail("stale_preview", errors.New("installation changed after preview; rerun install to review and confirm the current effects"))
	}
	nextConfig := preferences.config(currentSelection.ID, i.CodexExecutable, i.CodexSpawnPath)
	if err := validateRepairConfig(currentConfig, nextConfig, currentSelection); err != nil {
		return InstallResult{}, Fail("repair_control_task", err)
	}
	resources, err := i.stageCandidate(ctx, nextConfig, currentState)
	if err != nil {
		return InstallResult{}, err
	}
	if err := nextConfig.Validate(); err != nil {
		return InstallResult{}, Fail("validate_config", err)
	}
	stateChanged := !stateExists
	if currentSelection.Welcome && currentState.PendingWelcomeTaskID != currentSelection.ID {
		currentState.PendingWelcomeTaskID = currentSelection.ID
		stateChanged = true
	}
	if stateChanged {
		if err := i.Store.SaveState(currentState); err != nil {
			return InstallResult{}, Fail("persist_state", err)
		}
		resources = appendUnique(resources, "state")
	}
	if currentSelection.Disposition == ControlTaskRepaired {
		// BEAR-60: remove this exact repair in the first release after operator convergence.
		repairer, ok := i.Store.(interface{ RepairControlTaskID(string) error })
		if !ok {
			return InstallResult{}, Fail("repair_control_task", errors.New("store cannot preserve the raw pre-repair config"))
		}
		if err := repairer.RepairControlTaskID(currentSelection.ID); err != nil {
			return InstallResult{}, Fail("repair_control_task", err)
		}
		currentConfig.ControlTaskID = currentSelection.ID
		resources = appendUnique(resources, "config")
	}
	if !configExists || !reflect.DeepEqual(nextConfig, currentConfig) {
		if err := i.Store.SaveConfig(nextConfig); err != nil {
			return InstallResult{}, Fail("persist_config", err)
		}
		resources = appendUnique(resources, "config")
	}
	unarchived := false
	if currentSelection.Unarchive {
		changed, err := i.ControlTasks.UnarchiveControlTask(ctx, currentSelection.ID)
		if err != nil {
			return InstallResult{}, Fail("unarchive_control_task", err)
		}
		unarchived = changed
		if changed {
			resources = appendUnique(resources, "control_task")
		}
	}
	finalSchedulerChanged, err := i.Scheduler.Stage(ctx, nextConfig)
	if err != nil {
		return InstallResult{}, Fail("stage_final_scheduler", err)
	}
	enabledChanged, err := i.Scheduler.Enable(ctx)
	if err != nil {
		return InstallResult{}, Fail("enable_scheduler", err)
	}
	if err := i.Scheduler.VerifyHealthy(ctx); err != nil {
		return InstallResult{}, Fail("verify_scheduler_health", err)
	}
	if finalSchedulerChanged || enabledChanged {
		resources = appendUnique(resources, "launchagent")
	}
	if err := i.SelfTester.Test(ctx, SelfTestInput{Paths: i.Paths, Config: nextConfig, State: currentState}); err != nil {
		return InstallResult{}, Fail("installed_self_test", err)
	}
	if err := lock.Close(); err != nil {
		return InstallResult{}, Fail("release_lock", err)
	}
	lockHeld = false
	warnings := make([]string, 0, 1)
	if currentSelection.Welcome {
		if err := i.ControlTasks.PostWelcome(ctx, currentSelection.ID, welcomeNotice(i.InstalledVersion, preferences)); err != nil {
			warnings = append(warnings, fmt.Sprintf("welcome notice not posted: %v", err))
		} else if err := i.clearPendingWelcome(currentSelection.ID); err != nil {
			warnings = append(warnings, fmt.Sprintf("welcome notice completion not recorded: %v", err))
		} else {
			currentState.PendingWelcomeTaskID = ""
		}
	}
	return InstallResult{Config: nextConfig, State: currentState, Paths: i.Paths, Reinstalled: configExists, Preview: currentPreview, Changed: len(resources) > 0, Resources: resources, Warnings: warnings, ControlTaskDisposition: currentSelection.Disposition, SuppliedControlTaskID: currentSelection.SuppliedID, Unarchived: unarchived}, nil
}

func (i Installer) clearPendingWelcome(taskID string) error {
	lock, err := i.Store.AcquireLock()
	if err != nil {
		return err
	}
	defer lock.Close()
	current, err := i.Store.LoadState()
	if err != nil {
		return err
	}
	if current.PendingWelcomeTaskID != taskID {
		return nil
	}
	current.PendingWelcomeTaskID = ""
	return i.Store.SaveState(current)
}

func validateRepairConfig(current, next config.Config, selection controlTaskSelection) error {
	if selection.Disposition != ControlTaskRepaired {
		return nil
	}
	expected := current
	expected.ControlTaskID = selection.ID
	if !reflect.DeepEqual(expected, next) {
		return errors.New("the BEAR-60 repair can replace only control_task_id; rerun with the persisted preferences unchanged")
	}
	return nil
}

func (i Installer) selectControlTask(ctx context.Context, persisted config.Config, persistedExists bool, supplied string) (controlTaskSelection, error) {
	if strings.TrimSpace(supplied) != supplied {
		return controlTaskSelection{}, Fail("control_task", errors.New("--control-task-id must be canonical and have no surrounding whitespace"))
	}
	read := func(id string) (ControlTask, error) {
		if id == "" {
			return ControlTask{}, ErrControlTaskIDRequired
		}
		task, err := i.ControlTasks.ReadControlTask(ctx, id)
		if err != nil {
			return ControlTask{}, err
		}
		if task.ID != id || strings.TrimSpace(task.ID) != task.ID || task.ID == "" {
			return ControlTask{}, errors.New("App Server returned a noncanonical control task")
		}
		return task, nil
	}
	if persistedExists && persisted.ControlTaskID == bear60ControllerID && supplied != "" && supplied != persisted.ControlTaskID {
		task, err := read(supplied)
		if err != nil {
			return controlTaskSelection{}, Fail("validate_supplied_control_task", err)
		}
		if task.Archived {
			return controlTaskSelection{}, Fail("validate_supplied_control_task", errors.New("the supplied control task is archived; unarchive it in Codex before installing"))
		}
		return controlTaskSelection{ID: supplied, SuppliedID: supplied, Disposition: ControlTaskRepaired, Welcome: true}, nil
	}
	if persistedExists {
		task, persistedErr := read(persisted.ControlTaskID)
		if persistedErr == nil {
			disposition := ControlTaskRetained
			if supplied != "" && supplied != persisted.ControlTaskID {
				disposition = ControlTaskStayedHome
			}
			return controlTaskSelection{ID: persisted.ControlTaskID, SuppliedID: supplied, Disposition: disposition, Unarchive: task.Archived}, nil
		}
		if supplied == "" {
			return controlTaskSelection{}, Fail("control_task_id_required", ErrControlTaskIDRequired)
		}
		task, err := read(supplied)
		if err != nil {
			return controlTaskSelection{}, Fail("validate_supplied_control_task", err)
		}
		if task.Archived {
			return controlTaskSelection{}, Fail("validate_supplied_control_task", errors.New("the supplied control task is archived; unarchive it in Codex before installing"))
		}
		return controlTaskSelection{ID: supplied, SuppliedID: supplied, Disposition: ControlTaskReplaced, Welcome: true}, nil
	}
	if supplied == "" {
		return controlTaskSelection{}, Fail("control_task_id_required", ErrControlTaskIDRequired)
	}
	task, err := read(supplied)
	if err != nil {
		return controlTaskSelection{}, Fail("validate_supplied_control_task", err)
	}
	if task.Archived {
		return controlTaskSelection{}, Fail("validate_supplied_control_task", errors.New("the supplied control task is archived; unarchive it in Codex before installing"))
	}
	return controlTaskSelection{ID: supplied, SuppliedID: supplied, Disposition: ControlTaskAdopted, Welcome: true}, nil
}

func welcomeNotice(version string, preferences Preferences) string {
	if version == "" {
		version = "v1"
	}
	archive := "keep completed tasks visible until you archive them"
	if preferences.ArchiveEnabled {
		archive = fmt.Sprintf("tuck completed tasks away after %d quiet days", preferences.ArchiveAfterDays)
	}
	rename := "leave every task title entirely to you"
	if preferences.RenameEnabled {
		rename = "keep status and next actions easy to spot in task titles"
	}
	tokenDisplay := "keep output-token figures out of task titles while title updates are off"
	if preferences.RenameEnabled {
		tokenDisplay = friendlyTokenDisplay(preferences.TokenDisplay)
	}
	autoUpdate := "let you choose when to install available updates"
	if preferences.AutoUpdateEnabled {
		autoUpdate = "install verified updates automatically"
	}
	statusHints := "leave agent replies unchanged"
	if preferences.AgentsEnabled {
		statusHints = "add a one-line status hint to agent replies so most checks stay lightweight"
	}
	classifier := "use local task evidence first, then ask Codex for a careful second look only when a task is unclear"
	defaults := DefaultPreferences()
	if preferences.ClassifierModel != defaults.ClassifierModel ||
		preferences.ClassifierEffort != defaults.ClassifierEffort ||
		preferences.ClassifierContextBudgetBytes != defaults.ClassifierContextBudgetBytes {
		classifier = fmt.Sprintf(
			"%s (custom: %s with %s reasoning, up to %s of context)",
			classifier,
			preferences.ClassifierModel,
			preferences.ClassifierEffort,
			friendlyByteLimit(preferences.ClassifierContextBudgetBytes),
		)
	}
	return fmt.Sprintf(`🧵🐻 ThreadBear %s is home.

To keep your Codex tasks tidy, I will:
- check quietly %s
- %s
- %s
- %s
- %s
- %s
- %s

Want anything different? Say "check every ten minutes",
"stop archiving", or "put token counts at the end".

I will mind the threads. You go make the next thing.`,
		version,
		friendlyInterval(preferences.HeartbeatSeconds),
		autoUpdate,
		archive,
		rename,
		tokenDisplay,
		statusHints,
		classifier,
	)
}

func friendlyInterval(seconds int) string {
	if seconds%3600 == 0 {
		hours := seconds / 3600
		if hours == 1 {
			return "every hour"
		}
		return fmt.Sprintf("every %d hours", hours)
	}
	if seconds%60 == 0 {
		minutes := seconds / 60
		if minutes == 1 {
			return "every minute"
		}
		return fmt.Sprintf("every %d minutes", minutes)
	}
	if seconds == 1 {
		return "every second"
	}
	return fmt.Sprintf("every %d seconds", seconds)
}

func friendlyTokenDisplay(position tokens.Position) string {
	switch position {
	case tokens.PositionStart:
		return "show output tokens at the start, like 🚨 1.6m Fix checkout"
	case tokens.PositionEnd:
		return "show output tokens at the end, like 🚨 Fix checkout · out 1.6m"
	default:
		return "keep output-token figures out of task titles"
	}
}

func friendlyByteLimit(bytes int) string {
	if bytes%1000000 == 0 {
		return fmt.Sprintf("%d MB", bytes/1000000)
	}
	if bytes%1000 == 0 {
		return fmt.Sprintf("%d KB", bytes/1000)
	}
	return fmt.Sprintf("%d bytes", bytes)
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
	return nil
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

func installPreview(paths Paths, preferences Preferences, reinstall bool, selection controlTaskSelection) (Preview, error) {
	mode := "install"
	if reinstall {
		mode = "reinstall"
	}
	agents, _, err := ManagedMutationPreview(paths.Agents, preferences.AgentsEnabled, []byte(assets.AgentsManagedContent))
	if err != nil {
		return Preview{}, fmt.Errorf("preview AGENTS.md mutation: %w", err)
	}
	skill, _, err := ManagedMutationPreview(paths.Skill, true, []byte(assets.SkillManagedContent))
	if err != nil {
		return Preview{}, fmt.Errorf("preview skill mutation: %w", err)
	}
	lines := []string{
		"deterministic-first: unchanged heartbeats use zero model tokens",
		fmt.Sprintf("binary %s: stage and verify candidate", paths.Binary),
		fmt.Sprintf("state %s: private config and state", paths.StateDirectory),
		agents,
		skill,
		fmt.Sprintf("LaunchAgent %s: stage disabled, self-test, then enable", paths.LaunchAgent),
		fmt.Sprintf("control task %s: %s", selection.ID, selection.Disposition),
		fmt.Sprintf("preferences: heartbeat=%ds auto_update=%t archive=%t/%dd rename=%t tokens=%s agents=%t classifier=%s/%s budget=%d", preferences.HeartbeatSeconds, preferences.AutoUpdateEnabled, preferences.ArchiveEnabled, preferences.ArchiveAfterDays, preferences.RenameEnabled, preferences.TokenDisplay, preferences.AgentsEnabled, preferences.ClassifierModel, preferences.ClassifierEffort, preferences.ClassifierContextBudgetBytes),
	}
	if selection.SuppliedID != "" && selection.Disposition == ControlTaskStayedHome {
		lines = append(lines, fmt.Sprintf("supplied control task %s: ignored; existing readable task stayed home", selection.SuppliedID))
	}
	if selection.Unarchive {
		lines = append(lines, "persisted control task: will be unarchived on reinstall")
	}
	if selection.Disposition == ControlTaskRepaired {
		lines = append(lines, fmt.Sprintf("BEAR-60 repair backup: preserve %s once", paths.ConfigRepairBackup))
	}
	return Preview{Operation: mode, Lines: lines}, nil
}

func previewResources(preview Preview) []string {
	resources := []string{"agents", "binary", "config", "control_task", "launchagent", "skill", "state"}
	return resources
}

func ManagedMutationPreview(path string, enabled bool, content []byte) (string, bool, error) {
	original, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", false, err
	}
	var updated []byte
	if enabled {
		updated, err = UpdateManagedBlock(original, content)
	} else {
		updated, err = RemoveManagedBlock(original)
	}
	if err != nil {
		return "", false, err
	}
	changed := !bytes.Equal(original, updated)
	action := "no change"
	if changed {
		action = "write managed block"
		if !enabled {
			action = "remove managed block"
		}
	}
	return path + ": " + action, changed, nil
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
		return fmt.Errorf("AGENTS surface validation: %w; run threadbear update or threadbear configure", err)
	}
	if err := VerifyManagedSurface(input.Paths.Skill, true, []byte(assets.SkillManagedContent)); err != nil {
		return fmt.Errorf("skill surface validation: %w; run threadbear update or threadbear configure", err)
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
	if errors.Is(err, fs.ErrNotExist) {
		if !enabled {
			return nil
		}
		return fmt.Errorf("%w: expected managed file is missing", ErrManagedSurfaceStale)
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
		return fmt.Errorf("%w: expected managed block is missing or stale", ErrManagedSurfaceStale)
	}
	if !enabled && bytes.Contains(data, []byte(ManagedBlockStart)) {
		return fmt.Errorf("%w: disabled managed block remains", ErrManagedSurfaceStale)
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
	if p.AutoUpdateEnabled != nil {
		value.AutoUpdateEnabled = *p.AutoUpdateEnabled
	}
	if p.TokenDisplay != nil {
		value.TokenDisplay = *p.TokenDisplay
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
