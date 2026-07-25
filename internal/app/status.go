package app

import (
	"context"
	"errors"
	"io/fs"
	"sort"
	"time"

	"github.com/ericlitman/threadbear/internal/codex"
	"github.com/ericlitman/threadbear/internal/config"
	"github.com/ericlitman/threadbear/internal/output"
	"github.com/ericlitman/threadbear/internal/state"
)

var ErrLaunchAgentUnavailable = errors.New("LaunchAgent adapter is unavailable")

type OperatorStore interface {
	LoadConfig() (config.Config, error)
	SaveConfig(config.Config) error
	LoadState() (state.State, error)
	SaveState(state.State) error
	LoadCycle() (state.CycleCheckpoint, error)
	SaveCycle(state.CycleCheckpoint) error
	AcquireLock() (*state.Lock, error)
}

type OperatorInventory interface {
	Inventory(context.Context, string) (codex.Inventory, error)
}

type OperatorClock interface {
	Now() time.Time
}

type LaunchAgent interface {
	Healthy(context.Context) (bool, error)
	Apply(context.Context, config.Config) error
	Enable(context.Context) (bool, error)
	Disable(context.Context) (bool, error)
}

type Unarchiver interface {
	Unarchive(context.Context, string) error
}

type OperatorDependencies struct {
	Store         OperatorStore
	Inventory     OperatorInventory
	Clock         OperatorClock
	LaunchAgent   LaunchAgent
	ManagedAgents ManagedAgents
	Preview       func(output.PreviewResult) error
	Confirm       func() (bool, error)
	Unarchiver    Unarchiver
	Heartbeat     HeartbeatRunner
	Install       Handler
	SelfTest      Handler
	Update        Updater
	Uninstall     Handler
}

func NewWithOperatorCommands(version string, deps OperatorDependencies) *Service {
	service := New(version)
	service.handlers[CommandHeartbeat] = OperatorHeartbeatHandler(deps.Store, deps.Inventory, deps.Clock, deps.Heartbeat)
	service.handlers[CommandStatus] = StatusHandler(version, deps.Store, deps.LaunchAgent)
	service.handlers[CommandInspect] = InspectHandler(deps.Store, deps.Inventory, deps.Clock)
	service.handlers[CommandConfigure] = ConfigureHandler(deps.Store, deps.LaunchAgent, deps.Preview, deps.Confirm, deps.ManagedAgents)
	service.handlers[CommandEnable] = LifecycleHandler(deps.Store, deps.LaunchAgent, true)
	service.handlers[CommandDisable] = LifecycleHandler(deps.Store, deps.LaunchAgent, false)
	service.handlers[CommandRestore] = RestoreHandler(deps.Store, deps.Inventory, deps.Unarchiver, deps.Clock)
	if deps.Install != nil {
		service.handlers[CommandInstall] = deps.Install
	}
	if deps.SelfTest != nil {
		service.handlers[CommandSelfTest] = deps.SelfTest
	}
	if deps.Update != nil {
		service.handlers[CommandUpdate] = UpdateHandler(deps.Store, deps.Update)
	}
	if deps.Uninstall != nil {
		service.handlers[CommandUninstall] = deps.Uninstall
	}
	return service
}

func StatusHandler(version string, store OperatorStore, launchAgent LaunchAgent) Handler {
	return func(ctx context.Context, request Request) (output.Result, error) {
		if request.Command != CommandStatus {
			return commandError("status", "invalid_request", ErrInvalidRequest)
		}
		if store == nil || launchAgent == nil {
			return commandError("status", "dependency_unavailable", ErrUnavailable)
		}
		cfg, err := store.LoadConfig()
		if err != nil {
			return commandError("status", "config_read_failed", err)
		}
		committed, err := store.LoadState()
		if err != nil {
			return commandError("status", "state_read_failed", err)
		}
		healthy, err := launchAgent.Healthy(ctx)
		launchStatus := "unhealthy"
		if healthy {
			launchStatus = "healthy"
		}
		if errors.Is(err, ErrLaunchAgentUnavailable) {
			launchStatus = "unavailable"
			err = nil
		}
		if err != nil {
			return commandError("status", "launch_agent_health_failed", err)
		}
		pendingTaskIDs := make(map[string]struct{})
		for taskID, task := range committed.Tasks {
			if task.Retry != nil {
				pendingTaskIDs[taskID] = struct{}{}
			}
		}
		cycle, cycleExists, err := loadOperatorCycleForGeneration(store, committed.Generation)
		if err != nil {
			return commandError("status", "cycle_read_failed", err)
		}
		if cycleExists {
			for taskID := range cycle.Diagnostics {
				pendingTaskIDs[taskID] = struct{}{}
			}
		}
		return output.StatusResult{
			InstalledVersion:       version,
			LaunchAgentHealthy:     healthy,
			LaunchAgentStatus:      launchStatus,
			LastCompletedHeartbeat: committed.LastCompletedHeartbeat,
			ControlTaskID:          cfg.ControlTaskID,
			Preferences: output.Preferences{
				HeartbeatSeconds:             cfg.HeartbeatSeconds,
				ArchiveEnabled:               cfg.ArchiveEnabled,
				ArchiveAfterDays:             cfg.ArchiveAfterDays,
				RenameEnabled:                cfg.RenameEnabled,
				AgentsEnabled:                cfg.AgentsEnabled,
				ClassifierModel:              cfg.ClassifierModel,
				ClassifierEffort:             string(cfg.ClassifierEffort),
				ClassifierContextBudgetBytes: cfg.ClassifierContextBudgetBytes,
			},
			PendingRetries:  len(pendingTaskIDs),
			LastUpdateCheck: committed.LastUpdateCheck,
		}, nil
	}
}

func OperatorHeartbeatHandler(store OperatorStore, inventory OperatorInventory, clock OperatorClock, runner HeartbeatRunner) Handler {
	return func(ctx context.Context, request Request) (output.Result, error) {
		if request.Command != CommandHeartbeat {
			return commandError("heartbeat", "invalid_request", ErrInvalidRequest)
		}
		if request.DryRun {
			return heartbeatDryRun(ctx, store, inventory, clock)
		}
		if runner == nil {
			return commandError("heartbeat", "not_implemented", ErrUnavailable)
		}
		return runner.Run(ctx, false)
	}
}

func heartbeatDryRun(ctx context.Context, store OperatorStore, inventory OperatorInventory, clock OperatorClock) (output.Result, error) {
	if store == nil || inventory == nil || clock == nil {
		return commandError("heartbeat", "dependency_unavailable", ErrUnavailable)
	}
	cfg, err := store.LoadConfig()
	if err != nil {
		return commandError("heartbeat", "config_read_failed", err)
	}
	committed, err := store.LoadState()
	if errors.Is(err, fs.ErrNotExist) {
		committed = state.New()
	} else if err != nil {
		return commandError("heartbeat", "state_read_failed", err)
	}
	cycle, cycleExists, err := loadOperatorCycleForGeneration(store, committed.Generation)
	if err != nil {
		return commandError("heartbeat", "cycle_read_failed", err)
	}
	observed, err := inventory.Inventory(ctx, cfg.ControlTaskID)
	if err != nil {
		return commandError("heartbeat", "inventory_failed", err)
	}
	now := clock.Now().UTC()
	comparison := codex.CompareInventory(observed, committed)
	changed := dueOperatorCandidates(comparison.Changed, committed, now)
	archiveIDs := archiveEligibleOperatorTasks(observed, committed, cfg, now)
	effects := make([]string, 0, len(changed)+len(comparison.RemovedIDs)+len(archiveIDs)+1)
	for _, task := range changed {
		if cycleExists && cycleResolvesTask(cycle, task) {
			continue
		}
		effects = append(effects, "classify."+task.TaskID)
	}
	for _, taskID := range comparison.RemovedIDs {
		effects = append(effects, "remove."+taskID)
	}
	for _, taskID := range archiveIDs {
		effects = append(effects, "archive."+taskID)
	}
	if committed.LastUpdateCheck == nil || !now.Before(committed.LastUpdateCheck.Add(24*time.Hour)) {
		effects = append(effects, "update_check")
	}
	sort.Strings(effects)
	return output.PreviewResult{Command: "heartbeat", Effects: effects}, nil
}

func dueOperatorCandidates(tasks []codex.Task, committed state.State, now time.Time) []codex.Task {
	result := make([]codex.Task, 0, len(tasks))
	for _, task := range tasks {
		previous, ok := committed.Tasks[task.TaskID]
		if !ok || previous.CapturedRevision != task.Revision || previous.CapturedTitle != task.Title || previous.Retry == nil || !now.Before(previous.Retry.NextAttemptAt) {
			result = append(result, task)
		}
	}
	return result
}

func archiveEligibleOperatorTasks(inventory codex.Inventory, committed state.State, cfg config.Config, now time.Time) []string {
	if !cfg.ArchiveEnabled {
		return nil
	}
	result := make([]string, 0)
	for _, task := range inventory.Tasks {
		record, ok := committed.Tasks[task.TaskID]
		if !ok || record.CapturedRevision != task.Revision || record.CapturedTitle != task.Title || record.Retry != nil && now.Before(record.Retry.NextAttemptAt) {
			continue
		}
		if archiveEligibleForInspect(record, now, cfg.ArchiveAfterDays) {
			result = append(result, task.TaskID)
		}
	}
	sort.Strings(result)
	return result
}

func cycleResolvesTask(cycle state.CycleCheckpoint, task codex.Task) bool {
	captured, capturedExists := cycle.Inventory[task.TaskID]
	classification, classified := cycle.Results[task.TaskID]
	return capturedExists && classified && captured.Revision == task.Revision && captured.Title == task.Title && classification.Revision == task.Revision
}

func commandError(operation, code string, err error) (output.Result, error) {
	return output.ErrorResult{Operation: operation, ErrorCode: code}, err
}
