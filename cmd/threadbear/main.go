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
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ericlitman/threadbear/internal/app"
	"github.com/ericlitman/threadbear/internal/codex"
	"github.com/ericlitman/threadbear/internal/codex/appserver"
	"github.com/ericlitman/threadbear/internal/config"
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
	service, closeService, err := newOperatorService(version)
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

func newOperatorService(installedVersion string) (*app.Service, func(), error) {
	stateDirectory, err := resolveStateDirectory()
	if err != nil {
		return nil, func() {}, err
	}
	store := state.NewStore(stateDirectory)
	inventory := &lazyInventory{}
	clock := systemClock{}
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
	service := app.NewWithOperatorCommands(installedVersion, app.OperatorDependencies{
		Store: store, Inventory: inventory, Clock: clock, LaunchAgent: unavailableLaunchAgent{},
		Unarchiver: appServerUnarchiver{}, Heartbeat: runner,
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

type unavailableLaunchAgent struct{}

func (unavailableLaunchAgent) Healthy(context.Context) (bool, error) {
	return false, app.ErrLaunchAgentUnavailable
}
func (unavailableLaunchAgent) Apply(context.Context, config.Config) error {
	return app.ErrLaunchAgentUnavailable
}
func (unavailableLaunchAgent) Enable(context.Context) (bool, error) {
	return false, app.ErrLaunchAgentUnavailable
}
func (unavailableLaunchAgent) Disable(context.Context) (bool, error) {
	return false, app.ErrLaunchAgentUnavailable
}

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
	if request.Command == app.CommandHeartbeat {
		flags.BoolVar(&request.DryRun, "dry-run", false, "inventory and analyze without models or mutations")
	}
	if request.Command == app.CommandConfigure {
		registerConfigureFlags(flags, &request.Configure)
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
