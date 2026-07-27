package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ericlitman/threadbear/assets"
	"github.com/ericlitman/threadbear/internal/app"
	"github.com/ericlitman/threadbear/internal/codex"
	"github.com/ericlitman/threadbear/internal/codex/appserver"
	"github.com/ericlitman/threadbear/internal/config"
	"github.com/ericlitman/threadbear/internal/install"
	"github.com/ericlitman/threadbear/internal/launchagent"
	"github.com/ericlitman/threadbear/internal/output"
	"github.com/ericlitman/threadbear/internal/state"
	statusresolver "github.com/ericlitman/threadbear/internal/status"
	"github.com/ericlitman/threadbear/internal/tokens"
	updatepkg "github.com/ericlitman/threadbear/internal/update"
	"github.com/ericlitman/threadbear/internal/watch"
)

var version = "dev"
var resolveCodexExecutableSpec = codex.ResolveExecutableSpec

func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 3 && args[0] == "managed-assets" && args[1] == "--candidate" && args[2] == "--json" {
		if err := json.NewEncoder(stdout).Encode(embeddedManagedAssets()); err != nil {
			fmt.Fprintf(stderr, "ThreadBear couldn't export managed assets: %v\n", err)
			return 1
		}
		return 0
	}
	request, err := parseRequest(args)
	format := output.FormatHuman
	if request.JSON {
		format = output.FormatJSON
	}
	if err != nil {
		result := output.ErrorResult{Operation: "parse", ErrorCode: "invalid_arguments"}
		if errors.Is(err, app.ErrUnknownCommand) {
			result.Operation = "dispatch"
			result.ErrorCode = "unknown_command"
		}
		if writeErr := output.Write(stdout, format, result); writeErr != nil {
			fmt.Fprintf(stderr, "ThreadBear couldn't write its result: %v\n", writeErr)
			return 1
		}
		return 2
	}
	service, closeService, err := newOperatorService(version, stdout, stderr, format, request)
	if err != nil {
		result := output.ErrorResult{Operation: string(request.Command), ErrorCode: "dependency_unavailable"}
		if writeErr := output.Write(stdout, format, result); writeErr != nil {
			fmt.Fprintf(stderr, "ThreadBear couldn't write its result: %v\n", writeErr)
		}
		return 1
	}
	defer closeService()
	result, dispatchErr := service.Dispatch(ctx, request)
	if err := output.Write(stdout, format, result); err != nil {
		fmt.Fprintf(stderr, "ThreadBear couldn't write its result: %v\n", err)
		return 1
	}
	if dispatchErr != nil {
		return 1
	}
	return 0
}

func newOperatorService(installedVersion string, stdout, stderr io.Writer, format output.Format, request app.Request) (*app.Service, func(), error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, func() {}, err
	}
	codexHome, err := codex.ResolveCodexHome()
	if err != nil {
		return nil, func() {}, err
	}
	paths := install.PathsForHomes(home, codexHome)
	store := state.NewStore(paths.StateDirectory)
	inventory := &lazyInventory{codexHome: codexHome}
	appServers := appServerRuntime{store: store, home: home, codexHome: codexHome}
	clock := systemClock{}
	launch, err := launchagent.New(launchagent.Options{
		Home: home, CodexHome: codexHome, BinaryPath: paths.Binary, PlistPath: paths.LaunchAgent,
		StdoutPath: paths.LaunchAgentStdout, StderrPath: paths.LaunchAgentStderr,
		LegacyPlistPath: paths.LegacyLaunchAgent, LegacyLockPath: paths.LegacyRunLock,
	})
	if err != nil {
		return nil, func() {}, err
	}
	managed := managedAgents{surfaces: install.ManagedSurfaceSet{AgentsPath: paths.Agents, SkillPath: paths.Skill}}
	runner, err := watch.New(watch.Dependencies{
		Store: store, Inventory: inventory, AppServer: appServerFactory{runtime: appServers}, Clock: clock, ManagedSurfaces: managed,
		InstalledVersion: installedVersion, NewCycleID: newCycleID, UpdateChecker: heartbeatUpdateChecker{checker: updatepkg.Checker{}},
		NewClassifier: func(client watch.AppServer, cfg config.Config) (watch.Classifier, error) {
			runner, ok := client.(statusresolver.EphemeralRunner)
			if !ok {
				return nil, errors.New("App Server does not support ephemeral classification")
			}
			return statusresolver.NewClassifier(runner, statusresolver.ClassifierConfig{Model: cfg.ClassifierModel, Effort: string(cfg.ClassifierEffort), ContextBudgetBytes: cfg.ClassifierContextBudgetBytes})
		},
	})
	if err != nil {
		return nil, func() {}, err
	}
	executable, err := os.Executable()
	if err != nil {
		return nil, func() {}, err
	}
	diskStore := install.NewDiskStore(paths)
	scheduler := productionScheduler{adapter: launch}
	installFactory := func(interactive bool) (install.Installer, func() error, error) {
		executableSpec := &codex.ExecutableSpec{}
		controlTasks := &appServerControlTasks{open: func(ctx context.Context) (*appserver.Client, error) {
			return openAppServerPinned(ctx, *executableSpec, codexHome, home)
		}, executableSpec: executableSpec}
		installer := install.Installer{
			Paths: paths, Store: diskStore, Scheduler: scheduler, ControlTasks: controlTasks,
			Binary:                     install.FileBinaryInstaller{Source: executable},
			SelfTester:                 install.CoreSelfTester{Probe: install.RuntimeProbe{}, Store: diskStore},
			ResolveCodexExecutableSpec: resolveCodexExecutableSpec,
			InstalledVersion:           installedVersion,
		}
		if !interactive {
			installer.Previewer = func(preview install.Preview) error {
				return output.Write(stderr, format, output.PreviewResult{Command: "install", Effects: []string{"agents", "binary", "config", "control_task", "launchagent", "skill", "state"}, Details: preview.Lines})
			}
			return installer, func() error { return nil }, nil
		}
		prompter, err := install.OpenTTYPrompter()
		if err != nil {
			return install.Installer{}, func() error { return nil }, install.Fail("open_prompter", err)
		}
		installer.Prompter = prompter
		return installer, prompter.Close, nil
	}
	uninstallFactory := func(interactive bool) (install.Uninstaller, func() error, error) {
		uninstaller := install.Uninstaller{Paths: paths, Store: diskStore, Scheduler: scheduler, ControlTasks: appServerControlTasks{open: appServers.open}}
		if !interactive {
			uninstaller.Previewer = func(preview install.Preview) error {
				return output.Write(stderr, format, output.PreviewResult{Command: "uninstall", Effects: []string{"agents", "binary", "control_task", "launchagent", "skill", "state"}, Details: preview.Lines})
			}
			return uninstaller, func() error { return nil }, nil
		}
		prompter, err := install.OpenTTYPrompter()
		if err != nil {
			return install.Uninstaller{}, func() error { return nil }, err
		}
		uninstaller.Prompter = prompter
		return uninstaller, prompter.Close, nil
	}
	service := app.NewWithOperatorCommands(installedVersion, app.OperatorDependencies{
		Store: store, Inventory: inventory, Clock: clock, LaunchAgent: launch,
		ManagedAgents: managed, Unarchiver: appServerUnarchiver{runtime: appServers}, Heartbeat: runner,
		Preview: func(preview output.PreviewResult) error {
			if request.NonInteractive {
				return output.Write(stderr, format, preview)
			}
			return writeMutationPreview(stderr, format, preview)
		},
		Confirm: func() (bool, error) {
			prompter, err := install.OpenTTYPrompter()
			if err != nil {
				return false, err
			}
			defer prompter.Close()
			return prompter.Confirm(true)
		},
		Install: app.InstallHandler(installFactory), SelfTest: app.SelfTestHandler(runtimeSelfTest{paths: paths, launchAgent: launch}),
		Update: updatepkg.Replacer{
			ExecutablePath: paths.Binary, InstalledVersion: installedVersion, ManagedSurfaces: managed.surfaces,
			Preview: func(preview updatepkg.ReplacementPreview) error {
				result := output.PreviewResult{Command: "update", Effects: preview.Resources, Details: preview.Details}
				if request.NonInteractive {
					return output.Write(stderr, format, result)
				}
				return writeMutationPreview(stderr, format, result)
			},
			AgentsEnabled: func() (bool, error) {
				cfg, err := store.LoadConfig()
				return cfg.AgentsEnabled, err
			},
			CurrentAssets: embeddedManagedAssets(),
		},
		Uninstall: app.UninstallHandler(uninstallFactory),
	})
	return service, func() { inventory.Close() }, nil
}

func resolveStateDirectory() (string, error) {
	if !strings.HasPrefix(config.StateDirectory, "~/") {
		return filepath.Abs(config.StateDirectory)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, strings.TrimPrefix(config.StateDirectory, "~/")), nil
}

type lazyInventory struct {
	mu        sync.Mutex
	index     *codex.Index
	codexHome string
}

func (i *lazyInventory) Inventory(ctx context.Context, controlTaskID string) (codex.Inventory, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.index == nil {
		sqliteHome, err := codex.ResolveSQLiteHome(i.codexHome)
		if err != nil {
			return codex.Inventory{}, err
		}
		index, err := codex.OpenIndex(sqliteHome)
		if err != nil {
			return codex.Inventory{}, err
		}
		i.index = index
	}
	return i.index.Inventory(ctx, controlTaskID)
}

func (i *lazyInventory) Close() error {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.index == nil {
		return nil
	}
	err := i.index.Close()
	i.index = nil
	return err
}

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

type appServerRuntime struct {
	store     interface{ LoadConfig() (config.Config, error) }
	home      string
	codexHome string
}

func (r appServerRuntime) process() (appserver.ProcessSpec, error) {
	cfg, err := r.store.LoadConfig()
	if err != nil {
		return appserver.ProcessSpec{}, err
	}
	spec, err := configuredExecutableSpec(r.home, cfg, os.Getenv("PATH"))
	if err != nil {
		return appserver.ProcessSpec{}, err
	}
	return appserver.PinnedProcessSpec(spec.Path, r.codexHome, r.home, spec.SpawnPath)
}

func configuredExecutableSpec(home string, cfg config.Config, pathValue string) (codex.ExecutableSpec, error) {
	if cfg.CodexExecutable == "" {
		return resolveCodexExecutableSpec(home, pathValue)
	}
	if cfg.CodexSpawnPath == "" {
		return codex.DeriveExecutableSpec(home, cfg.CodexExecutable, pathValue)
	}
	spec := codex.ExecutableSpec{Path: cfg.CodexExecutable, SpawnPath: cfg.CodexSpawnPath}
	if err := codex.ValidateExecutableSpec(spec); err != nil {
		return codex.ExecutableSpec{}, err
	}
	return spec, nil
}

func (r appServerRuntime) open(ctx context.Context) (*appserver.Client, error) {
	process, err := r.process()
	if err != nil {
		return nil, err
	}
	capabilities, err := appserver.DiscoverCapabilities(ctx, process)
	if err != nil {
		return nil, err
	}
	return appserver.Start(ctx, process, capabilities)
}

type appServerFactory struct{ runtime appServerRuntime }

func (f appServerFactory) Open(ctx context.Context) (watch.AppServer, error) {
	return f.runtime.open(ctx)
}

type heartbeatUpdateChecker struct{ checker updatepkg.Checker }

func (c heartbeatUpdateChecker) Check(ctx context.Context, installedVersion string) (watch.UpdateStatus, error) {
	status, err := c.checker.Check(ctx, installedVersion)
	return watch.UpdateStatus{LatestVersion: status.LatestVersion, Newer: status.Newer}, err
}

func newCycleID() string {
	data := make([]byte, 16)
	if _, err := rand.Read(data); err == nil {
		return hex.EncodeToString(data)
	}
	return strconv.FormatInt(time.Now().UTC().UnixNano(), 10)
}

type appServerUnarchiver struct{ runtime appServerRuntime }

func (u appServerUnarchiver) Unarchive(ctx context.Context, taskID string) error {
	client, err := u.runtime.open(ctx)
	if err != nil {
		return err
	}
	_, unarchiveErr := client.Unarchive(ctx, taskID)
	return errors.Join(unarchiveErr, client.Close())
}

func openAppServerPinned(ctx context.Context, spec codex.ExecutableSpec, codexHome, home string) (*appserver.Client, error) {
	process, err := appserver.PinnedProcessSpec(spec.Path, codexHome, home, spec.SpawnPath)
	if err != nil {
		return nil, err
	}
	capabilities, err := appserver.DiscoverCapabilities(ctx, process)
	if err != nil {
		return nil, err
	}
	return appserver.Start(ctx, process, capabilities)
}

func parseRequest(args []string) (app.Request, error) {
	if len(args) == 0 {
		return app.Request{}, fmt.Errorf("choose a command: install, heartbeat, status, inspect, configure, enable, disable, restore, self-test, update, uninstall, or version")
	}
	request := app.Request{Command: app.Command(args[0]), JSON: containsFlag(args[1:], "--json")}
	if !request.Command.Valid() {
		return request, fmt.Errorf("%w: %q", app.ErrUnknownCommand, request.Command)
	}
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.BoolVar(&request.JSON, "json", request.JSON, "write stable JSON output")
	switch request.Command {
	case app.CommandHeartbeat:
		flags.BoolVar(&request.DryRun, "dry-run", false, "inventory and analyze without models or mutations")
	case app.CommandInstall:
		registerConfigureFlags(flags, &request.Configure)
		registerLifecycleFlags(flags, &request)
		flags.StringVar(&request.Version, "version", "", "verified bootstrap version")
	case app.CommandConfigure:
		registerConfigureFlags(flags, &request.Configure)
		registerNonInteractiveFlags(flags, &request)
		flags.BoolVar(&request.DryRun, "dry-run", false, "preview configuration effects")
		flags.BoolVar(&request.Confirm, "confirm", false, "confirm the previewed changes")
	case app.CommandSelfTest:
		flags.BoolVar(&request.Candidate, "candidate", false, "validate this candidate before installation")
	case app.CommandUpdate:
		flags.StringVar(&request.Version, "version", "", "exact release version without a leading v")
	case app.CommandUninstall:
		registerLifecycleFlags(flags, &request)
		flags.BoolVar(&request.ArchiveControlTask, "archive-control-task", false, "archive the control task")
		flags.BoolVar(&request.DeleteState, "delete-state", false, "delete persistent state")
	}
	commandArgs := args[1:]
	if request.Command == app.CommandInspect || request.Command == app.CommandRestore {
		commandArgs = flagsBeforePositionals(commandArgs, "--json")
	}
	if err := flags.Parse(commandArgs); err != nil {
		return request, err
	}
	remaining := flags.Args()
	switch request.Command {
	case app.CommandInspect, app.CommandRestore:
		if len(remaining) != 1 {
			return request, fmt.Errorf("%s requires exactly one task ID", request.Command)
		}
		request.TaskID = remaining[0]
	default:
		if len(remaining) != 0 {
			return request, fmt.Errorf("%s does not accept positional arguments", request.Command)
		}
	}
	if err := request.Validate(); err != nil {
		return request, err
	}
	return request, nil
}

func containsFlag(args []string, target string) bool {
	for _, arg := range args {
		if arg == target {
			return true
		}
	}
	return false
}

func flagsBeforePositionals(args []string, known ...string) []string {
	isKnown := func(value string) bool {
		for _, flagName := range known {
			if value == flagName {
				return true
			}
		}
		return false
	}
	ordered := make([]string, 0, len(args))
	for _, arg := range args {
		if isKnown(arg) {
			ordered = append(ordered, arg)
		}
	}
	for _, arg := range args {
		if !isKnown(arg) {
			ordered = append(ordered, arg)
		}
	}
	return ordered
}

func registerConfigureFlags(flags *flag.FlagSet, patch *app.ConfigPatch) {
	flags.Var(optionalBool{target: &patch.ArchiveEnabled}, "archive", "enable or disable automatic archiving")
	flags.Var(optionalBool{target: &patch.RenameEnabled}, "rename", "enable or disable managed titles")
	flags.Func("token-display", "show output tokens in managed titles: off, start, or end", func(value string) error {
		parsed := tokens.Position(value)
		patch.TokenDisplay = &parsed
		return nil
	})
	flags.Var(optionalBool{target: &patch.AgentsEnabled}, "agents", "enable or disable managed AGENTS content")
	flags.Func("heartbeat-seconds", "set the heartbeat interval", func(value string) error {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return err
		}
		patch.HeartbeatSeconds = &parsed
		return nil
	})
	flags.Func("archive-after-days", "set complete-task archive age", func(value string) error {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return err
		}
		patch.ArchiveAfterDays = &parsed
		return nil
	})
	flags.Func("classifier-model", "set classifier model", func(value string) error {
		patch.ClassifierModel = &value
		return nil
	})
	flags.Func("classifier-effort", "set classifier reasoning effort", func(value string) error {
		parsed := config.ClassifierEffort(value)
		patch.ClassifierEffort = &parsed
		return nil
	})
	flags.Func("classifier-context-budget-bytes", "set classifier context budget", func(value string) error {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return err
		}
		patch.ClassifierContextBudgetBytes = &parsed
		return nil
	})
}

func registerLifecycleFlags(flags *flag.FlagSet, request *app.Request) {
	registerNonInteractiveFlags(flags, request)
	flags.BoolVar(&request.Confirm, "confirm", false, "assert confirmation for noninteractive use")
}

func registerNonInteractiveFlags(flags *flag.FlagSet, request *app.Request) {
	flags.BoolVar(&request.NonInteractive, "noninteractive", false, "do not prompt")
	flags.BoolVar(&request.NonInteractive, "non-interactive", false, "do not prompt")
}

type optionalBool struct {
	target **bool
}

func (f optionalBool) String() string {
	if f.target == nil || *f.target == nil {
		return ""
	}
	return strconv.FormatBool(**f.target)
}

func (f optionalBool) Set(value string) error {
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return err
	}
	*f.target = &parsed
	return nil
}

func (optionalBool) IsBoolFlag() bool { return true }

const controlTaskTitle = "🧵🐻 ThreadBear 🐻🧵"

var _ install.Scheduler = productionScheduler{}
var _ install.ControlTasks = appServerControlTasks{}

type productionScheduler struct{ adapter *launchagent.Adapter }

func (s productionScheduler) DetectLegacyInterval(context.Context) (int, bool, error) {
	return s.adapter.DetectLegacyInterval()
}
func (s productionScheduler) StopLegacy(ctx context.Context) error { return s.adapter.StopLegacy(ctx) }
func (s productionScheduler) VerifyLegacyStopped(ctx context.Context) error {
	return s.adapter.VerifyLegacyStopped(ctx)
}
func (s productionScheduler) Stage(ctx context.Context, cfg config.Config) (bool, error) {
	return s.adapter.Stage(ctx, cfg)
}
func (s productionScheduler) Enable(ctx context.Context) (bool, error) {
	return s.adapter.Enable(ctx)
}
func (s productionScheduler) VerifyHealthy(ctx context.Context) error {
	healthy, err := s.adapter.Healthy(ctx)
	if err != nil {
		return err
	}
	if !healthy {
		return errors.New("LaunchAgent is not healthy")
	}
	return nil
}
func (s productionScheduler) Loaded(ctx context.Context) (bool, error) {
	return s.adapter.Loaded(ctx)
}
func (s productionScheduler) Remove(ctx context.Context) error { return s.adapter.Remove(ctx) }

type appServerControlTasks struct {
	open           func(context.Context) (*appserver.Client, error)
	executableSpec *codex.ExecutableSpec
}

func (a *appServerControlTasks) SetCodexExecutableSpec(spec codex.ExecutableSpec) {
	if a.executableSpec != nil {
		*a.executableSpec = spec
	}
}

func (a appServerControlTasks) EnsureControlTask(ctx context.Context, taskID string) (string, bool, error) {
	client, err := a.open(ctx)
	if err != nil {
		return "", false, err
	}
	ensuredID, changed, ensureErr := ensureControlTask(ctx, client, taskID)
	return ensuredID, changed, errors.Join(ensureErr, client.Close())
}

func (a appServerControlTasks) PostWelcome(ctx context.Context, taskID, text string) error {
	client, err := a.open(ctx)
	if err != nil {
		return err
	}
	return errors.Join(client.InsertNotice(ctx, taskID, text), client.Close())
}

func (a appServerControlTasks) ArchiveControlTask(ctx context.Context, taskID string) (bool, error) {
	client, err := a.open(ctx)
	if err != nil {
		return false, err
	}
	changed, archiveErr := archiveControlTask(ctx, client, taskID)
	return changed, errors.Join(archiveErr, client.Close())
}

type controlTaskClient interface {
	ReadThread(context.Context, string) (appserver.Thread, error)
	StartPersistentThread(context.Context) (appserver.Thread, error)
	Unarchive(context.Context, string) (appserver.Thread, error)
	SetTitle(context.Context, string, string) error
	Archive(context.Context, string) error
}

func ensureControlTask(ctx context.Context, client controlTaskClient, taskID string) (string, bool, error) {
	changed := false
	var thread appserver.Thread
	var err error
	if taskID == "" {
		thread, err = client.StartPersistentThread(ctx)
		if err != nil {
			return "", false, err
		}
		taskID = thread.ID
		changed = true
	} else {
		thread, err = client.ReadThread(ctx, taskID)
		if err != nil {
			// An adopted control task can be gone: migrated from ThreadWatch
			// after the thread was deleted, or a Codex home that no longer
			// holds it. Losing the old thread is not a reason to refuse the
			// install - and refusing here is especially costly, because by
			// this point the legacy scheduler has already been stopped. Start
			// a fresh control task and carry on.
			thread, err = client.StartPersistentThread(ctx)
			if err != nil {
				return taskID, false, err
			}
			taskID = thread.ID
			changed = true
		}
	}
	name := thread.Name
	if thread.Status.Type == "archived" {
		thread, err = client.Unarchive(ctx, taskID)
		if err != nil {
			return taskID, changed, err
		}
		if thread.Name != "" {
			name = thread.Name
		}
		changed = true
	}
	if name != controlTaskTitle {
		if err := client.SetTitle(ctx, taskID, controlTaskTitle); err != nil {
			return taskID, changed, err
		}
		changed = true
	}
	return taskID, changed, nil
}

func archiveControlTask(ctx context.Context, client controlTaskClient, taskID string) (bool, error) {
	thread, err := client.ReadThread(ctx, taskID)
	if err != nil {
		return false, err
	}
	if thread.Status.Type == "archived" {
		return false, nil
	}
	if err := client.Archive(ctx, taskID); err != nil {
		return false, err
	}
	return true, nil
}

func writeMutationPreview(fallback io.Writer, format output.Format, preview output.PreviewResult) error {
	tty, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
	if err == nil {
		defer tty.Close()
		return output.Write(tty, format, preview)
	}
	return output.Write(fallback, format, preview)
}

type managedAgents struct{ surfaces install.ManagedSurfaceSet }

func embeddedManagedAssets() install.ManagedAssets {
	return install.ManagedAssets{Agents: assets.AgentsManagedContent, Skill: assets.SkillManagedContent}
}

func (m managedAgents) Apply(enabled bool) (bool, error) {
	result, err := m.surfaces.Reconcile(enabled, embeddedManagedAssets())
	return result.Changed, err
}

func (m managedAgents) Repair(enabled bool) ([]string, error) {
	result, err := m.surfaces.Reconcile(enabled, embeddedManagedAssets())
	return result.Resources, err
}

func (m managedAgents) Preview(enabled bool) (app.ManagedAgentsPreview, error) {
	mutations, err := m.surfaces.Preview(enabled, embeddedManagedAssets())
	preview := app.ManagedAgentsPreview{}
	for _, mutation := range mutations {
		if mutation.Changed {
			preview.Changed = true
			preview.Resources = append(preview.Resources, mutation.Resource)
			preview.Details = append(preview.Details, mutation.Detail)
		}
	}
	return preview, err
}

func (m managedAgents) Snapshot() (any, error) {
	return m.surfaces.Snapshot()
}

func (m managedAgents) Restore(value any) error {
	snapshot, ok := value.(install.ManagedSnapshot)
	if !ok {
		return errors.New("invalid managed surface snapshot")
	}
	return m.surfaces.Restore(snapshot)
}

func managedSurfaceCheck(name string, err error) output.CheckResult {
	check := output.CheckResult{Name: name, OK: err == nil}
	if err == nil {
		return check
	}
	switch {
	case errors.Is(err, install.ErrMalformedManagedBlock):
		check.ErrorCode = "managed_surface_malformed"
		check.Remedy = "replace or move aside the malformed managed file so it has no invalid ThreadBear markers, then rerun update or configure"
	case errors.Is(err, install.ErrManagedSurfaceStale):
		check.ErrorCode = "managed_surface_stale"
		check.Remedy = "run threadbear update or threadbear configure"
	case errors.Is(err, install.ErrUnsafeManagedPath):
		check.ErrorCode = "managed_surface_unsafe_path"
		check.Remedy = "replace the unsafe or symlinked managed path with a regular file, then rerun update or configure"
	default:
		check.ErrorCode = "managed_surface_unavailable"
		check.Remedy = "fix managed file access or permissions, then rerun update or configure"
	}
	return check
}

type runtimeSelfTest struct {
	paths       install.Paths
	launchAgent app.LaunchAgent
}

func validateInstalledState(paths install.Paths) error {
	info, err := os.Stat(paths.StateDirectory)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("state directory is not a directory")
	}
	store := install.NewDiskStore(paths)
	cfg, configErr := store.LoadConfig()
	committed, stateErr := store.LoadState()
	if errors.Is(configErr, os.ErrNotExist) && errors.Is(stateErr, os.ErrNotExist) {
		return nil
	}
	if configErr != nil {
		return fmt.Errorf("load installed config: %w", configErr)
	}
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("validate installed config: %w", err)
	}
	if stateErr != nil {
		return fmt.Errorf("load installed state: %w", stateErr)
	}
	if err := committed.Validate(); err != nil {
		return fmt.Errorf("validate installed state: %w", err)
	}
	return nil
}

func (s runtimeSelfTest) Run(ctx context.Context, candidate bool) output.SelfTestResult {
	checks := make([]output.CheckResult, 0, 8)
	add := func(name string, err error) {
		check := output.CheckResult{Name: name, OK: err == nil}
		if err != nil {
			check.ErrorCode = "unavailable"
		}
		checks = append(checks, check)
	}
	platformErr := error(nil)
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" && runtime.GOARCH != "amd64" {
		platformErr = errors.New("unsupported platform")
	} else if major, err := macOSMajor(); err != nil || major < 12 {
		platformErr = errors.New("macOS 12 or newer is required")
	}
	add("platform", platformErr)
	executable, err := os.Executable()
	if !candidate {
		executable = s.paths.Binary
	}
	if err == nil {
		var info os.FileInfo
		info, err = os.Stat(executable)
		if err == nil && (!info.Mode().IsRegular() || info.Mode().Perm()&0o100 == 0) {
			err = errors.New("executable is unhealthy")
		}
	}
	add("executable", err)
	var codexSpec codex.ExecutableSpec
	var codexErr error
	if candidate {
		codexSpec, codexErr = resolveCodexExecutableSpec(s.paths.Home, os.Getenv("PATH"))
	} else {
		var installedConfig config.Config
		installedConfig, codexErr = install.NewDiskStore(s.paths).LoadConfig()
		if codexErr == nil {
			codexSpec, codexErr = configuredExecutableSpec(s.paths.Home, installedConfig, os.Getenv("PATH"))
		}
	}
	if codexErr == nil {
		codexErr = codex.VerifyExecutableSpec(s.paths.Home, codexSpec)
	}
	add("codex_executable", codexErr)
	if candidate {
		add("installed_state", validateInstalledState(s.paths))
		ok := true
		for _, check := range checks {
			ok = ok && check.OK
		}
		return output.SelfTestResult{OK: ok, Checks: checks}
	}
	stateInfo, stateErr := os.Stat(s.paths.StateDirectory)
	if stateErr == nil && (!stateInfo.IsDir() || stateInfo.Mode().Perm()&0o077 != 0) {
		stateErr = errors.New("state directory is not private")
	}
	if stateErr == nil {
		probe, probeErr := os.CreateTemp(s.paths.StateDirectory, ".self-test-*")
		if probeErr == nil {
			probeErr = errors.Join(probe.Close(), os.Remove(probe.Name()))
		}
		stateErr = probeErr
	}
	add("state_directory", stateErr)
	store := install.NewDiskStore(s.paths)
	cfg, err := store.LoadConfig()
	if err == nil {
		err = cfg.Validate()
	}
	add("config", err)
	committed, err := store.LoadState()
	if err == nil {
		err = committed.Validate()
	}
	add("state", err)
	info, err := os.Stat(s.paths.CodexHome)
	if err == nil && !info.IsDir() {
		err = errors.New("Codex home is not a directory")
	}
	add("codex", err)
	err = install.VerifyManagedSurface(s.paths.Agents, cfg.AgentsEnabled, []byte(assets.AgentsManagedContent))
	checks = append(checks, managedSurfaceCheck("agents", err))
	err = install.VerifyManagedSurface(s.paths.Skill, true, []byte(assets.SkillManagedContent))
	checks = append(checks, managedSurfaceCheck("skill", err))
	healthy, err := s.launchAgent.Healthy(ctx)
	if err == nil && !healthy {
		err = errors.New("LaunchAgent is unhealthy")
	}
	add("launchagent", err)
	ok := true
	for _, check := range checks {
		ok = ok && check.OK
	}
	return output.SelfTestResult{OK: ok, Checks: checks}
}

func macOSMajor() (int, error) {
	data, err := exec.Command("/usr/bin/sw_vers", "-productVersion").Output()
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.Split(strings.TrimSpace(string(data)), ".")[0])
}
