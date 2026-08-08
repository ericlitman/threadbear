package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"github.com/ericlitman/threadbear/assets"
	"golang.org/x/sys/unix"
)

const (
	blockStart           = "<!-- BEGIN THREADBEAR MANAGED BLOCK -->"
	blockEnd             = "<!-- END THREADBEAR MANAGED BLOCK -->"
	legacyAutomationID   = "threadbear-maintenance"
	legacyAutomationName = "ThreadBear maintenance"
	legacyAutomationKind = "heartbeat"
	legacyTitleTool      = "codex_appset_thread_title"
)

type lifecyclePaths struct {
	binary, agents, skill, launchAgent, updateReceipt string
}

type installOptions struct {
	DryRun, Confirmed, Reset, NoOnboard, Automatic bool
	SelectedVersion                                string
}

type uninstallOptions struct {
	DryRun, Confirmed bool
}

type legacyInstall struct{ MainTaskID string }

var postResetStatus = status

func codexHome() string {
	if value := strings.TrimSpace(os.Getenv("CODEX_HOME")); value != "" {
		return value
	}
	return filepath.Join(homeDir(), ".codex")
}

func homeDir() string { home, _ := os.UserHomeDir(); return home }

func stateDir() string { return filepath.Join(homeDir(), ".local", "share", "threadbear") }

func legacyHooksPath() string { return filepath.Join(codexHome(), "hooks.json") }

func installPaths() lifecyclePaths {
	return lifecyclePaths{
		binary:        filepath.Join(homeDir(), ".local", "bin", "threadbear"),
		agents:        filepath.Join(codexHome(), "AGENTS.md"),
		skill:         filepath.Join(codexHome(), "skills", "threadbear", "SKILL.md"),
		launchAgent:   updateAgentPath(),
		updateReceipt: filepath.Join(stateDir(), "update.json"),
	}
}

func install(ctx context.Context, options installOptions) (any, error) {
	if _, err := selfTest(); err != nil {
		return nil, err
	}
	if options.DryRun && options.Confirmed {
		return nil, errors.New("install preview cannot also be confirmed")
	}
	if options.Automatic && (options.DryRun || options.Reset) {
		return nil, errors.New("automatic install accepts neither preview nor legacy reset")
	}
	if options.SelectedVersion != "" && options.SelectedVersion != version {
		return nil, fmt.Errorf("installer selected version %q but candidate is %q", options.SelectedVersion, version)
	}
	if options.Automatic {
		options.NoOnboard = true
	}
	source, err := os.Executable()
	if err != nil {
		return nil, err
	}
	binary, err := os.ReadFile(source)
	if err != nil {
		return nil, err
	}
	p := installPaths()
	legacy, legacyFound, err := readLegacyInstall()
	if err != nil {
		return nil, err
	}
	preview := installResult(options, legacy, legacyFound, options.DryRun)
	if err := preflightInstall(ctx, p, binary, legacyFound); err != nil {
		return preview, err
	}
	if options.DryRun {
		return preview, nil
	}
	if !options.Confirmed {
		return preview, errors.New("install requires --noninteractive --confirm after its preview")
	}

	var updateLock *os.File
	// Manual install orders update -> stable boundary -> lifecycle. The updater
	// already owns update.lock before its automatic child reaches this path.
	if !options.Automatic {
		updateLock, err = lifecycleLock("update.lock")
		if err != nil {
			return preview, err
		}
		defer unlock(updateLock)
	}
	boundaryLock, err := lifecycleBoundaryLock()
	if err != nil {
		return preview, err
	}
	defer unlock(boundaryLock)
	if updateLock != nil {
		if err := currentLockPath(updateLock); err != nil {
			return preview, err
		}
	}
	var lock *os.File
	if options.Automatic {
		lock, err = existingLifecycleLock("lifecycle.lock")
	} else {
		lock, err = lifecycleLock("lifecycle.lock")
	}
	if err != nil {
		return preview, err
	}
	defer unlock(lock)
	legacy, legacyFound, err = readLegacyInstall()
	if err != nil {
		return preview, err
	}
	if legacyFound && !options.Reset {
		return preview, errors.New("legacy 2.2.1 state requires guided removal of threadbear-maintenance and install --reset")
	}
	if !legacyFound && options.Reset {
		return preview, errors.New("--reset is only valid for an exact legacy native.json installation")
	}
	if options.Automatic {
		if err := requireCurrentFormatInstall(p); err != nil {
			return preview, fmt.Errorf("automatic install refused because the current installation disappeared or is legacy: %w", err)
		}
	}
	if err := preflightInstall(ctx, p, binary, legacyFound); err != nil {
		return preview, err
	}
	agents, removeAgents, agentsChanged, err := editManagedBlock(p.agents, true, legacyFound || currentInstallPresent(p))
	if err != nil || removeAgents {
		return preview, errors.Join(err, errors.New("managed AGENTS block could not be prepared"))
	}
	if err := os.MkdirAll(newStore(stateDir()).subjectDir(), 0o700); err != nil {
		return installPartial(preview, "subject_state", false, options), err
	}
	if agentsChanged {
		if err := writeAtomic(p.agents, agents, 0o600); err != nil {
			return installPartial(preview, "managed_guidance", true, options), err
		}
	}
	if err := writeAtomic(p.skill, []byte(assets.SkillManagedContent), 0o600); err != nil {
		return installPartial(preview, "skill", true, options), err
	}
	if legacyFound {
		hooks, changed, remove, cleanupErr := removeLegacyHooks(legacyHooksPath(), p.binary)
		if cleanupErr != nil {
			return installPartial(preview, "legacy_hook", true, options), cleanupErr
		}
		if changed {
			if remove {
				cleanupErr = os.Remove(legacyHooksPath())
			} else {
				cleanupErr = writeAtomic(legacyHooksPath(), hooks, 0o600)
			}
			if cleanupErr != nil && !errors.Is(cleanupErr, os.ErrNotExist) {
				return installPartial(preview, "legacy_hook", true, options), cleanupErr
			}
		}
	}
	if err := installUpdateAgent(ctx, p.launchAgent, p.binary); err != nil {
		return installPartial(preview, "updater", true, options), err
	}
	// Replacement is last: any earlier failure leaves the previously installed
	// executable active, and a fresh non-RunAtLoad job cannot invoke a partial
	// installation.
	if err := writeAtomic(p.binary, binary, 0o755); err != nil {
		return installPartial(preview, "binary", true, options), err
	}

	checked, checkErr := statusAllowingLegacy(ctx, options.Reset)
	legacyCleanupCommitted := false
	if checkErr == nil && options.Reset {
		if checkErr = clearLegacyState(); checkErr == nil {
			legacyCleanupCommitted = true
			checked, checkErr = postResetStatus(ctx)
		}
	}
	result := installResult(options, legacyInstall{}, false, false)
	for key, value := range checked.(map[string]any) {
		if key == "ready" || key == "installed" || key == "automatic_updates_enabled" {
			result[key] = value
		}
	}
	result["restart_required"] = checkErr == nil
	if checkErr != nil {
		stage := "status"
		if options.Reset && !legacyCleanupCommitted {
			stage = "legacy_cleanup"
			result["legacy_reset_required"] = true
			result["legacy_main_task_id"] = legacy.MainTaskID
			result["legacy_automation_id"] = legacyAutomationID
			result["legacy_automation_name"] = legacyAutomationName
			result["legacy_automation_kind"] = legacyAutomationKind
			result["legacy_automation_target_thread_id"] = legacy.MainTaskID
		}
		partial := installPartial(result, stage, true, options)
		if legacyCleanupCommitted {
			partial["safe_rerun"] = confirmedInstallRerun(p, options.NoOnboard)
		}
		return partial, checkErr
	}
	return result, nil
}

func installResult(options installOptions, legacy legacyInstall, legacyFound, dry bool) map[string]any {
	onboarding := !options.NoOnboard && !options.Automatic
	result := map[string]any{
		"ready":                     dry,
		"installed":                 false,
		"version":                   version,
		"dry_run":                   dry,
		"legacy_reset_required":     legacyFound,
		"reset":                     options.Reset,
		"onboarding_requested":      onboarding,
		"automatic_updates_enabled": false,
		"restart_required":          false,
		"partial":                   false,
		"planned_changes":           installChanges(installPaths(), legacyFound),
	}
	if legacyFound {
		result["legacy_main_task_id"] = legacy.MainTaskID
		result["legacy_automation_id"] = legacyAutomationID
		result["legacy_automation_name"] = legacyAutomationName
		result["legacy_automation_kind"] = legacyAutomationKind
		result["legacy_automation_target_thread_id"] = legacy.MainTaskID
	}
	if onboarding {
		result["next_request"] = "threadbear onboard --dry-run --json"
	}
	return result
}

func installPartial(result map[string]any, stage string, restart bool, options installOptions) map[string]any {
	rerun := "repeat the same confirmed install command"
	if options.Automatic {
		rerun = quoteArgument(installPaths().binary) + " update --json"
	}
	return partialResult(result, stage, restart, rerun)
}

func confirmedInstallRerun(p lifecyclePaths, noOnboard bool) string {
	command := quoteArgument(p.binary) + " install"
	if noOnboard {
		command += " --no-onboard"
	}
	return command + " --noninteractive --confirm --json"
}

func installChanges(p lifecyclePaths, legacy bool) []string {
	changes := []string{}
	if legacy {
		changes = append(changes,
			"remove legacy state "+filepath.Join(stateDir(), "native.json"),
			"remove legacy locks native.lock, title.lock, and operation.lock under "+stateDir(),
			"remove exact legacy ThreadBear title hooks from "+legacyHooksPath())
	}
	changes = append(changes,
		"manage subject records under "+newStore(stateDir()).subjectDir(),
		"manage update receipt "+p.updateReceipt,
		"replace managed AGENTS block in "+p.agents,
		"write skill "+p.skill,
		"install "+updateAgentLabel+" LaunchAgent "+p.launchAgent,
		"write binary "+p.binary)
	return changes
}

func onboard(ctx context.Context, dryRun, confirmed bool) (any, error) {
	if dryRun && confirmed {
		return nil, errors.New("onboarding preview cannot also be confirmed")
	}
	if !dryRun && !confirmed {
		return nil, errors.New("onboarding requires --dry-run or --noninteractive --confirm")
	}
	p := installPaths()
	if err := requireCurrentFormatInstall(p); err != nil {
		return nil, fmt.Errorf("onboarding requires the current ThreadBear installation: %w", err)
	}
	return runOnboarding(ctx, confirmed, os.Getenv("CODEX_THREAD_ID"))
}

func uninstall(ctx context.Context, options uninstallOptions) (any, error) {
	if options.DryRun && options.Confirmed {
		return nil, errors.New("uninstall preview cannot also be confirmed")
	}
	preview := map[string]any{
		"ready": true, "dry_run": options.DryRun, "uninstalled": false,
		"icons_may_remain": true, "restart_required": false, "partial": false,
		"warning":         "Existing ThreadBear title icons may remain until renamed.",
		"planned_changes": uninstallChanges(installPaths()),
	}
	p := installPaths()
	partialAdmission, err := preflightUninstall(ctx, p)
	if err != nil {
		return preview, err
	}
	if options.DryRun {
		return preview, nil
	}
	if !options.Confirmed {
		return preview, errors.New("uninstall requires --noninteractive --confirm after its preview")
	}
	boundaryLock, err := lifecycleBoundaryLock()
	if err != nil {
		return preview, err
	}
	defer unlock(boundaryLock)
	lock, err := existingLifecycleLock("lifecycle.lock")
	if err != nil {
		if !partialAdmission || !errors.Is(err, os.ErrNotExist) {
			return preview, err
		}
		lock = nil
	}
	defer func() {
		if lock != nil {
			unlock(lock)
		}
	}()
	confirmedPartial, err := preflightUninstall(ctx, p)
	if err != nil {
		return preview, err
	}
	if confirmedPartial != partialAdmission {
		return preview, errors.New("uninstall state changed during admission")
	}
	agents, removeAgents, agentsChanged, err := editManagedBlock(p.agents, false, false)
	if err != nil {
		return preview, err
	}

	// Stop the only background entry point before removing any executable or
	// instruction surface. The binary is deliberately removed last.
	if err := removeUpdateAgent(ctx, p.launchAgent, p.binary); err != nil {
		return partialResult(preview, "updater", true, uninstallRerun(p)), err
	}
	if agentsChanged {
		if removeAgents {
			err = os.Remove(p.agents)
		} else {
			err = writeAtomic(p.agents, agents, 0o600)
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return partialResult(preview, "managed_guidance", true, uninstallRerun(p)), err
		}
	}
	if err := os.Remove(p.skill); err != nil && !errors.Is(err, os.ErrNotExist) {
		return partialResult(preview, "skill", true, uninstallRerun(p)), err
	}
	skillDir := filepath.Dir(p.skill)
	if info, _ := os.Lstat(skillDir); info == nil || info.Mode()&os.ModeSymlink == 0 {
		if err := os.Remove(skillDir); err != nil && !errors.Is(err, os.ErrNotExist) && !errors.Is(err, syscall.ENOTEMPTY) {
			return partialResult(preview, "skill", true, uninstallRerun(p)), err
		}
	}
	if err := removeOwnedState(); err != nil {
		return partialResult(preview, "state", true, uninstallRerun(p)), err
	}
	if err := os.Remove(filepath.Join(stateDir(), "lifecycle.lock")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return partialResult(preview, "state", true, uninstallRerun(p)), err
	}
	if lock != nil {
		unlock(lock)
		lock = nil
	}
	if err := os.Remove(stateDir()); err != nil && !errors.Is(err, os.ErrNotExist) && !errors.Is(err, syscall.ENOTEMPTY) {
		return partialResult(preview, "state", true, uninstallRerun(p)), err
	}
	if err := os.Remove(p.binary); err != nil && !errors.Is(err, os.ErrNotExist) {
		return partialResult(preview, "binary", true, uninstallRerun(p)), err
	}
	return map[string]any{
		"ready": true, "dry_run": false, "uninstalled": true,
		"icons_may_remain": true, "restart_required": true, "partial": false,
		"warning":         "Existing ThreadBear title icons may remain until renamed.",
		"planned_changes": uninstallChanges(p),
	}, nil
}

func uninstallRerun(p lifecyclePaths) string {
	return quoteArgument(p.binary) + " uninstall --noninteractive --confirm --json"
}

func partialResult(result map[string]any, stage string, restart bool, rerun string) map[string]any {
	result["ready"], result["dry_run"], result["partial"] = false, false, true
	result["stage"], result["restart_required"], result["safe_rerun"] = stage, restart, rerun
	return result
}

func uninstallChanges(p lifecyclePaths) []string {
	return []string{
		"boot out and remove " + updateAgentLabel + " LaunchAgent " + p.launchAgent,
		"remove managed AGENTS block from " + p.agents,
		"remove skill " + p.skill,
		"remove owned subject records under " + newStore(stateDir()).subjectDir(),
		"remove update receipt " + p.updateReceipt,
		"remove binary last " + p.binary,
	}
}

func status(ctx context.Context) (any, error) { return statusAllowingLegacy(ctx, false) }

func statusAllowingLegacy(ctx context.Context, allowLegacy bool) (any, error) {
	p := installPaths()
	stateErr := validateRuntimeState()
	legacy, legacyErr := legacyStatePresent()
	legacyClear := legacyErr == nil && !legacy
	if allowLegacy && legacy && legacyErr == nil {
		legacyClear = true
	}
	artifacts := map[string]bool{
		"binary": regularExecutable(p.binary), "agents": managedBlockExact(p.agents),
		"skill":    exactFile(p.skill, []byte(assets.SkillManagedContent)),
		"subjects": stateErr == nil, "legacy_state_absent": legacyClear,
	}
	var problems []error
	for name, healthy := range artifacts {
		if !healthy {
			problems = append(problems, fmt.Errorf("managed %s surface is missing or changed", name))
		}
	}
	updater, _, updaterErr := inspectUpdateAgent(ctx, p.launchAgent, p.binary)
	automaticUpdates := updaterErr == nil && updater.Exact && updater.Loaded
	if !legacyClear {
		legacyErr = errors.Join(legacyErr, errors.New("legacy or unsupported native.json state is present"))
	}
	coreErr := errors.Join(errors.Join(problems...), stateErr, legacyErr)
	ready := coreErr == nil
	binaryPresent, binaryPresenceErr := regularLeaf(p.binary, false)
	if binaryPresenceErr != nil {
		binaryPresent = false
	}
	result := map[string]any{
		"ready": ready, "installed": binaryPresent, "version": version,
		"automatic_updates_enabled": automaticUpdates,
		"artifacts":                 artifacts, "updater": updater,
	}
	if updaterErr != nil {
		result["updater_error"] = updaterErr.Error()
	}
	if receipt, err := readUpdateReceipt(p.updateReceipt); err == nil {
		result["latest_update"] = receipt
	} else if !errors.Is(err, os.ErrNotExist) {
		result["update_receipt_error"] = err.Error()
	}
	return result, coreErr
}

func selfTest() (any, error) {
	if runtime.GOOS != "darwin" || assets.AgentsManagedContent == "" || assets.SkillManagedContent == "" || version == "" {
		return nil, errors.New("candidate is incomplete or unsupported")
	}
	return map[string]any{"ready": true, "version": version}, nil
}

func lifecycleLock(name string) (*os.File, error) { return openLifecycleLock(name, true, true) }
func existingLifecycleLock(name string) (*os.File, error) {
	return openLifecycleLock(name, false, false)
}
func updateCheckLock() (*os.File, error) { return openLifecycleLock("update.lock", false, true) }

func lifecycleBoundaryLock() (*os.File, error) {
	path := filepath.Dir(stateDir())
	if err := os.MkdirAll(path, 0o700); err != nil {
		return nil, err
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	if err := unix.Flock(fd, unix.LOCK_EX); err != nil {
		return nil, errors.Join(err, file.Close())
	}
	info, statErr := file.Stat()
	current, pathErr := os.Lstat(path)
	if statErr != nil || pathErr != nil || !current.IsDir() || !os.SameFile(info, current) {
		unlock(file)
		return nil, errors.Join(errors.New("ThreadBear lifecycle boundary changed while the operation was waiting"), statErr, pathErr)
	}
	return file, nil
}

func currentLockPath(file *os.File) error {
	info, statErr := file.Stat()
	current, pathErr := os.Lstat(file.Name())
	if statErr != nil || pathErr != nil || !current.Mode().IsRegular() || current.Mode().Perm() != 0o600 || !os.SameFile(info, current) {
		return errors.Join(errors.New("ThreadBear lifecycle changed while the operation was waiting"), statErr, pathErr)
	}
	return nil
}

func openLifecycleLock(name string, createDir, createFile bool) (*os.File, error) {
	if createDir {
		if err := os.MkdirAll(stateDir(), 0o700); err != nil {
			return nil, err
		}
	}
	dir, err := unix.Open(stateDir(), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer unix.Close(dir)
	if err := unix.Fchmod(dir, 0o700); err != nil {
		return nil, err
	}
	flags := unix.O_RDWR | unix.O_NOFOLLOW
	if createFile {
		flags |= unix.O_CREAT
	}
	fd, err := unix.Openat(dir, name, flags, 0o600)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), filepath.Join(stateDir(), name))
	info, statErr := file.Stat()
	if statErr != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		return nil, errors.Join(errors.New("ThreadBear lifecycle lock is not a private regular file"), statErr, file.Close())
	}
	if err := unix.Flock(fd, unix.LOCK_EX); err != nil {
		return nil, errors.Join(err, file.Close())
	}
	if err := currentLockPath(file); err != nil {
		unlock(file)
		return nil, err
	}
	return file, nil
}

func readLegacyInstall() (legacyInstall, bool, error) {
	path := filepath.Join(stateDir(), "native.json")
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return legacyInstall{}, false, nil
	}
	if err != nil {
		return legacyInstall{}, false, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		return legacyInstall{}, false, errors.New("legacy native.json is not a private regular file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return legacyInstall{}, false, err
	}
	var value struct {
		Format     int                        `json:"format"`
		MainTaskID string                     `json:"main_task_id"`
		Tasks      map[string]json.RawMessage `json:"tasks"`
	}
	if json.Unmarshal(data, &value) != nil || value.Format != 4 || value.Tasks == nil || !taskIDPattern.MatchString(value.MainTaskID) {
		return legacyInstall{}, false, errors.New("native.json is not exact supported 2.2.1 state")
	}
	return legacyInstall{MainTaskID: value.MainTaskID}, true, nil
}

func legacyStatePresent() (bool, error) { _, found, err := readLegacyInstall(); return found, err }

func clearLegacyState() error {
	// Keep native.json until every other obsolete leaf is gone so --reset is
	// still admissible after any interrupted or failed cleanup.
	for _, name := range []string{"native.lock", "title.lock", "operation.lock", "update.json", "native.json"} {
		if err := os.Remove(filepath.Join(stateDir(), name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func requireCurrentFormatInstall(p lifecyclePaths) error {
	legacy, err := legacyStatePresent()
	if err != nil || legacy {
		return errors.Join(err, map[bool]error{true: errors.New("legacy native.json is present")}[legacy])
	}
	if !regularExecutable(p.binary) {
		return errors.New("installed binary is absent")
	}
	return validateRuntimeState()
}

func currentInstallPresent(p lifecyclePaths) bool {
	legacy, err := legacyStatePresent()
	if err != nil || legacy || !regularExecutable(p.binary) {
		return false
	}
	return validateRuntimeState() == nil
}

func removeOwnedState() error {
	if err := validateRemovableState(); err != nil {
		return err
	}
	subjectDir := newStore(stateDir()).subjectDir()
	if entries, err := os.ReadDir(subjectDir); err == nil {
		for _, entry := range entries {
			ext := filepath.Ext(entry.Name())
			if (ext == ".json" || ext == ".lock") && taskIDPattern.MatchString(strings.TrimSuffix(entry.Name(), ext)) {
				if err := os.Remove(filepath.Join(subjectDir, entry.Name())); err != nil {
					return err
				}
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Remove(subjectDir); err != nil && !errors.Is(err, os.ErrNotExist) && !errors.Is(err, syscall.ENOTEMPTY) {
		return err
	}
	for _, name := range []string{"update.json", "update.lock"} {
		if err := os.Remove(filepath.Join(stateDir(), name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func validateRuntimeState() error {
	found, err := privateDirectory(stateDir())
	if err != nil || !found {
		return errors.Join(err, errors.New("state root is missing or not private"))
	}
	found, err = privateDirectory(newStore(stateDir()).subjectDir())
	if err != nil || !found {
		return errors.Join(err, errors.New("subject store is missing or not private"))
	}
	found, err = privateRegular(filepath.Join(stateDir(), "lifecycle.lock"))
	if err != nil || !found {
		return errors.Join(err, errors.New("lifecycle fence is missing or not private"))
	}
	return nil
}

func preflightInstall(ctx context.Context, p lifecyclePaths, candidate []byte, legacy bool) error {
	if err := validateManagedParents(p); err != nil {
		return err
	}
	if found, err := privateDirectory(stateDir()); err != nil {
		return err
	} else if found {
		if _, err := privateDirectory(newStore(stateDir()).subjectDir()); err != nil {
			return err
		}
	}
	if legacy {
		if _, err := regularLeaf(legacyHooksPath(), false); err != nil {
			return err
		}
		if _, _, _, err := removeLegacyHooks(legacyHooksPath(), p.binary); err != nil {
			return err
		}
	}
	current, owned := currentInstallPresent(p), legacy
	owned = owned || current
	if exists, err := regularLeaf(p.binary, false); err != nil {
		return err
	} else if exists {
		data, readErr := os.ReadFile(p.binary)
		if readErr != nil || !regularExecutable(p.binary) || !owned && !bytes.Equal(data, candidate) {
			return errors.Join(readErr, errors.New("binary path contains non-ThreadBear content"))
		}
	}
	if _, err := regularLeaf(p.agents, false); err != nil {
		return err
	}
	if exists, err := regularLeaf(p.skill, false); err != nil {
		return err
	} else if exists && !owned && !exactFile(p.skill, []byte(assets.SkillManagedContent)) {
		return errors.New("skill path contains non-ThreadBear content")
	}
	if _, _, _, err := editManagedBlock(p.agents, true, owned); err != nil {
		return err
	}
	if _, _, err := inspectUpdateAgent(ctx, p.launchAgent, p.binary); err != nil {
		return err
	}
	if _, err := privateRegular(p.updateReceipt); err != nil {
		return err
	}
	for _, name := range []string{"lifecycle.lock", "update.lock"} {
		if _, err := privateRegular(filepath.Join(stateDir(), name)); err != nil {
			return err
		}
	}
	return nil
}

func preflightUninstall(ctx context.Context, p lifecyclePaths) (bool, error) {
	currentErr := preflightCurrentUninstall(ctx, p)
	if currentErr == nil {
		return false, nil
	}
	if partialErr := preflightPartialUninstall(ctx, p); partialErr != nil {
		return false, errors.Join(currentErr, fmt.Errorf("partial uninstall admission refused: %w", partialErr))
	}
	return true, nil
}

func preflightCurrentUninstall(ctx context.Context, p lifecyclePaths) error {
	if err := validateManagedParents(p); err != nil {
		return err
	}
	if err := requireCurrentFormatInstall(p); err != nil {
		return fmt.Errorf("uninstall requires a valid current installation: %w", err)
	}
	if err := validateOwnedState(); err != nil {
		return err
	}
	for _, path := range []string{p.agents, p.skill} {
		if _, err := regularLeaf(path, false); err != nil {
			return err
		}
	}
	if _, _, _, err := editManagedBlock(p.agents, false, false); err != nil {
		return err
	}
	_, _, err := inspectUpdateAgent(ctx, p.launchAgent, p.binary)
	return err
}

func preflightPartialUninstall(ctx context.Context, p lifecyclePaths) error {
	if err := validateManagedParents(p); err != nil {
		return err
	}
	if err := runningInstalledBinary(p.binary); err != nil {
		return err
	}
	if legacy, err := legacyStatePresent(); err != nil || legacy {
		return errors.Join(err, map[bool]error{true: errors.New("legacy native.json is present")}[legacy])
	}
	if err := validateRemovableState(); err != nil {
		return err
	}
	if _, err := regularLeaf(p.agents, false); err != nil {
		return err
	}
	if exists, err := regularLeaf(p.skill, false); err != nil {
		return err
	} else if exists && !exactFile(p.skill, []byte(assets.SkillManagedContent)) {
		return errors.New("partial uninstall found replacement content at the skill path")
	}
	if _, _, _, err := editManagedBlock(p.agents, false, false); err != nil {
		return err
	}
	_, _, err := inspectUpdateAgent(ctx, p.launchAgent, p.binary)
	return err
}

func runningInstalledBinary(path string) error {
	running, err := os.Executable()
	if err != nil {
		return err
	}
	runningInfo, runningErr := os.Stat(running)
	installedInfo, installedErr := os.Lstat(path)
	if runningErr != nil || installedErr != nil || !installedInfo.Mode().IsRegular() || installedInfo.Mode().Perm()&0o111 == 0 || !os.SameFile(runningInfo, installedInfo) {
		return errors.Join(runningErr, installedErr, errors.New("uninstall rerun must execute the exact installed binary"))
	}
	return nil
}

func validateManagedParents(p lifecyclePaths) error {
	paths := []string{p.binary, p.agents, p.skill, p.launchAgent, p.updateReceipt,
		newStore(stateDir()).subjectDir(), filepath.Join(stateDir(), "lifecycle.lock"), filepath.Join(stateDir(), "update.lock")}
	for _, path := range paths {
		anchor := homeDir()
		if rel, err := filepath.Rel(anchor, path); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			anchor = filepath.Dir(codexHome())
		}
		current := anchor
		rel, err := filepath.Rel(anchor, filepath.Dir(path))
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return errors.New("managed path is outside its trusted parent")
		}
		for _, part := range append([]string{"."}, strings.Split(rel, string(os.PathSeparator))...) {
			if part != "." {
				current = filepath.Join(current, part)
			}
			info, err := os.Lstat(current)
			if errors.Is(err, os.ErrNotExist) {
				break
			}
			if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return errors.Join(err, fmt.Errorf("managed parent is not a real directory: %s", current))
			}
		}
	}
	return nil
}

func regularLeaf(path string, private bool) (bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil || !info.Mode().IsRegular() || private && info.Mode().Perm() != 0o600 {
		return false, errors.Join(err, fmt.Errorf("managed path is not a%s regular file: %s", map[bool]string{true: " private", false: ""}[private], path))
	}
	return true, nil
}

func privateRegular(path string) (bool, error) { return regularLeaf(path, true) }
func privateDirectory(path string) (bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
		return false, errors.Join(err, fmt.Errorf("managed path is not a private directory: %s", path))
	}
	return true, nil
}

func validateOwnedState() error {
	if err := validateRuntimeState(); err != nil {
		return err
	}
	if err := validateOwnedSubjectLeaves(); err != nil {
		return err
	}
	if _, err := privateRegular(filepath.Join(stateDir(), "update.lock")); err != nil {
		return err
	}
	_, err := privateRegular(installPaths().updateReceipt)
	return err
}

func validateRemovableState() error {
	found, err := privateDirectory(stateDir())
	if err != nil || !found {
		return err
	}
	found, err = privateDirectory(newStore(stateDir()).subjectDir())
	if err != nil {
		return err
	}
	if found {
		if err := validateOwnedSubjectLeaves(); err != nil {
			return err
		}
	}
	for _, path := range []string{
		filepath.Join(stateDir(), "lifecycle.lock"),
		filepath.Join(stateDir(), "update.lock"),
		installPaths().updateReceipt,
	} {
		if _, err := privateRegular(path); err != nil {
			return err
		}
	}
	return nil
}

func validateOwnedSubjectLeaves() error {
	dir := newStore(stateDir()).subjectDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		ext := filepath.Ext(entry.Name())
		if ext != ".json" && ext != ".lock" || !taskIDPattern.MatchString(strings.TrimSuffix(entry.Name(), ext)) {
			continue
		}
		info, err := os.Lstat(filepath.Join(dir, entry.Name()))
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
			return errors.Join(err, fmt.Errorf("owned subject path is not a private regular file: %s", entry.Name()))
		}
	}
	return nil
}

func regularExecutable(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0
}
func exactFile(path string, want []byte) bool {
	data, err := os.ReadFile(path)
	return err == nil && bytes.Equal(data, want)
}

func managedBlockExact(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	block := blockStart + "\n" + strings.TrimSpace(assets.AgentsManagedContent) + "\n" + blockEnd
	return strings.Count(string(data), blockStart) == 1 && strings.Count(string(data), blockEnd) == 1 && strings.Contains(string(data), block)
}

func editManagedBlock(path string, add, replace bool) ([]byte, bool, bool, error) {
	data, err := os.ReadFile(path)
	missing := errors.Is(err, os.ErrNotExist)
	if err != nil && !missing {
		return nil, false, false, err
	}
	text := string(data)
	start, end := strings.Index(text, blockStart), strings.Index(text, blockEnd)
	if strings.Count(text, blockStart) > 1 || strings.Count(text, blockEnd) > 1 || (start < 0) != (end < 0) || (end >= 0 && end < start) {
		return nil, false, false, errors.New("invalid ThreadBear managed block")
	}
	block := blockStart + "\n" + strings.TrimSpace(assets.AgentsManagedContent) + "\n" + blockEnd
	before := text
	if add {
		if start >= 0 {
			if text[start:end+len(blockEnd)] != block && !replace {
				return nil, false, false, errors.New("managed AGENTS block was modified; refusing to replace it")
			}
			text = text[:start] + block + text[end+len(blockEnd):]
		} else if text == "" {
			text = block
		} else {
			text += "\n" + block
		}
	} else {
		if start < 0 {
			return data, false, false, nil
		}
		beforeBlock, after := text[:start], text[end+len(blockEnd):]
		if strings.HasSuffix(beforeBlock, "\n") {
			beforeBlock = strings.TrimSuffix(beforeBlock, "\n")
		}
		text = beforeBlock + after
	}
	remove := text == ""
	return []byte(text), remove, text != before, nil
}

// removeLegacyHooks exists only for the explicit 2.2.1 --reset transition.
// Current-format install, status, and uninstall are deliberately hook-blind.
func removeLegacyHooks(path, binary string) ([]byte, bool, bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, false, nil
	}
	if err != nil {
		return nil, false, false, err
	}
	root, events := map[string]json.RawMessage{}, map[string]json.RawMessage{}
	if json.Unmarshal(data, &root) != nil || root == nil {
		return nil, false, false, errors.New("legacy hooks.json must contain an object")
	}
	if raw, ok := root["hooks"]; ok && (json.Unmarshal(raw, &events) != nil || events == nil) {
		return nil, false, false, errors.New("legacy hooks.json hooks must be an object")
	}
	removed := false
	for _, event := range []string{"PreToolUse", "PostToolUse"} {
		var groups []json.RawMessage
		if raw, ok := events[event]; ok && (json.Unmarshal(raw, &groups) != nil || groups == nil) {
			return nil, false, false, fmt.Errorf("legacy hooks.json %s must be an array", event)
		}
		kept := groups[:0]
		for _, group := range groups {
			if ownedLegacyHookGroup(group, binary) {
				removed = true
				continue
			}
			kept = append(kept, group)
		}
		if len(kept) == 0 {
			delete(events, event)
		} else {
			events[event], _ = json.Marshal(kept)
		}
	}
	if !removed {
		return data, false, false, nil
	}
	if len(events) == 0 {
		delete(root, "hooks")
	} else {
		root["hooks"], _ = json.Marshal(events)
	}
	if len(root) == 0 {
		return nil, true, true, nil
	}
	updated, err := json.MarshalIndent(root, "", "  ")
	updated = append(updated, '\n')
	return updated, true, false, err
}

func ownedLegacyHookGroup(group json.RawMessage, binary string) bool {
	var value struct {
		Matcher string `json:"matcher"`
		Hooks   []struct {
			Type, Command string
		} `json:"hooks"`
	}
	if json.Unmarshal(group, &value) != nil || value.Matcher != legacyTitleTool || len(value.Hooks) != 1 {
		return false
	}
	return value.Hooks[0].Type == "command" && value.Hooks[0].Command == quoteArgument(binary)+" hook"
}

func quoteArgument(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".threadbear-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err = file.Chmod(mode); err == nil {
		_, err = file.Write(data)
	}
	if err == nil {
		err = file.Sync()
	}
	if err = errors.Join(err, file.Close()); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}
