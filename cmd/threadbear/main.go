package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
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
	"github.com/ericlitman/threadbear/internal/watch"
)

var version = "dev"

func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
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
	paths := install.PathsForHome(home)
	store := state.NewStore(paths.StateDirectory)
	inventory := &lazyInventory{}
	clock := systemClock{}
	launch, err := launchagent.New(launchagent.Options{
		Home: home, BinaryPath: paths.Binary, PlistPath: paths.LaunchAgent,
		StdoutPath: paths.LaunchAgentStdout, StderrPath: paths.LaunchAgentStderr,
		LegacyPlistPath: paths.LegacyLaunchAgent, LegacyLockPath: paths.LegacyRunLock,
	})
	if err != nil {
		return nil, func() {}, err
	}
	runner, err := watch.New(watch.Dependencies{
		Store: store, Inventory: inventory, AppServer: appServerFactory{}, Clock: clock,
		InstalledVersion: installedVersion, NewCycleID: newCycleID, UpdateChecker: currentVersionChecker{},
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
	controlTasks := appServerControlTasks{}
	installFactory := func(interactive bool) (install.Installer, func() error, error) {
		installer := install.Installer{
			Paths: paths, Store: diskStore, Scheduler: scheduler, ControlTasks: controlTasks,
			Binary:     install.FileBinaryInstaller{Source: executable},
			SelfTester: install.CoreSelfTester{Probe: install.RuntimeProbe{}, Store: diskStore},
		}
		if !interactive {
			installer.Previewer = func(preview install.Preview) error {
				return output.Write(stderr, format, output.PreviewResult{Command: "install", Effects: []string{"agents", "binary", "config", "control_task", "launchagent", "skill", "state"}, Details: preview.Lines})
			}
			return installer, func() error { return nil }, nil
		}
		prompter, err := install.OpenTTYPrompter()
		if err != nil {
			return install.Installer{}, func() error { return nil }, err
		}
		installer.Prompter = prompter
		return installer, prompter.Close, nil
	}
	uninstallFactory := func(interactive bool) (install.Uninstaller, func() error, error) {
		uninstaller := install.Uninstaller{Paths: paths, Store: diskStore, Scheduler: scheduler, ControlTasks: controlTasks}
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
		ManagedAgents: managedAgents{path: paths.Agents}, Unarchiver: appServerUnarchiver{}, Heartbeat: runner,
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
			return prompter.Confirm()
		},
		Install: app.InstallHandler(installFactory), SelfTest: app.SelfTestHandler(runtimeSelfTest{paths: paths, launchAgent: launch}),
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
	mu    sync.Mutex
	index *codex.Index
}

func (i *lazyInventory) Inventory(ctx context.Context, controlTaskID string) (codex.Inventory, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.index == nil {
		index, err := codex.OpenDefaultIndex()
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

type appServerFactory struct{}

func (appServerFactory) Open(ctx context.Context) (watch.AppServer, error) {
	return openAppServer(ctx)
}

type currentVersionChecker struct{}

func (currentVersionChecker) Check(_ context.Context, installedVersion string) (watch.UpdateStatus, error) {
	return watch.UpdateStatus{LatestVersion: installedVersion}, nil
}

func newCycleID() string {
	data := make([]byte, 16)
	if _, err := rand.Read(data); err == nil {
		return hex.EncodeToString(data)
	}
	return strconv.FormatInt(time.Now().UTC().UnixNano(), 10)
}

type appServerUnarchiver struct{}

func (appServerUnarchiver) Unarchive(ctx context.Context, taskID string) error {
	client, err := openAppServer(ctx)
	if err != nil {
		return err
	}
	_, unarchiveErr := client.Unarchive(ctx, taskID)
	return errors.Join(unarchiveErr, client.Close())
}

func openAppServer(ctx context.Context) (*appserver.Client, error) {
	codexHome, err := codex.ResolveCodexHome()
	if err != nil {
		return nil, err
	}
	process := appserver.DefaultProcessSpec(codexHome)
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
		flags.BoolVar(&request.Candidate, "candidate", false, "validate this candidate without installed state")
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
func (s productionScheduler) Remove(ctx context.Context) error { return s.adapter.Remove(ctx) }

type appServerControlTasks struct{}

func (appServerControlTasks) EnsureControlTask(ctx context.Context, taskID string) (string, bool, error) {
	client, err := openAppServer(ctx)
	if err != nil {
		return "", false, err
	}
	ensuredID, changed, ensureErr := ensureControlTask(ctx, client, taskID)
	return ensuredID, changed, errors.Join(ensureErr, client.Close())
}

func (appServerControlTasks) ArchiveControlTask(ctx context.Context, taskID string) (bool, error) {
	client, err := openAppServer(ctx)
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
			return taskID, false, err
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

type managedAgents struct{ path string }

func (m managedAgents) Apply(enabled bool) (bool, error) {
	before, beforeErr := os.ReadFile(m.path)
	if beforeErr != nil && !errors.Is(beforeErr, os.ErrNotExist) {
		return false, beforeErr
	}
	var err error
	if enabled {
		err = install.WriteManagedBlock(m.path, []byte(assets.AgentsManagedContent))
	} else {
		err = install.DeleteManagedBlock(m.path)
	}
	if err != nil {
		return false, err
	}
	after, afterErr := os.ReadFile(m.path)
	if afterErr != nil && !errors.Is(afterErr, os.ErrNotExist) {
		return false, afterErr
	}
	return string(before) != string(after), nil
}

func (m managedAgents) Preview(enabled bool) (string, error) {
	return install.ManagedMutationPreview(m.path, enabled, []byte(assets.AgentsManagedContent))
}

type runtimeSelfTest struct {
	paths       install.Paths
	launchAgent app.LaunchAgent
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
	if _, codexErr := exec.LookPath("codex"); codexErr != nil {
		add("codex_executable", codexErr)
	} else {
		add("codex_executable", nil)
	}
	if candidate {
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
	codexHome := filepath.Join(s.paths.Home, ".codex")
	info, err := os.Stat(codexHome)
	if err == nil && !info.IsDir() {
		err = errors.New("Codex home is not a directory")
	}
	add("codex", err)
	err = install.VerifyManagedSurface(s.paths.Agents, cfg.AgentsEnabled, []byte(assets.AgentsManagedContent))
	add("agents", err)
	err = install.VerifyManagedSurface(s.paths.Skill, true, []byte(assets.SkillManagedContent))
	add("skill", err)
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
