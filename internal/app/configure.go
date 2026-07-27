package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/ericlitman/threadbear/internal/config"
	"github.com/ericlitman/threadbear/internal/output"
)

type ManagedAgents interface {
	Apply(bool) (bool, error)
	Preview(bool) (ManagedAgentsPreview, error)
}

type ManagedAgentsSnapshotter interface {
	Snapshot() (any, error)
	Restore(any) error
}

type ManagedAgentsPreview struct {
	Detail    string
	Details   []string
	Resources []string
	Changed   bool
}

func ConfigureHandler(store OperatorStore, launchAgent LaunchAgent, previewer func(output.PreviewResult) error, confirmer func() (bool, error), managedAgents ...ManagedAgents) Handler {
	return func(ctx context.Context, request Request) (output.Result, error) {
		if request.Command != CommandConfigure {
			return commandError("configure", "invalid_request", ErrInvalidRequest)
		}
		if store == nil {
			return commandError("configure", "dependency_unavailable", ErrUnavailable)
		}
		lock, err := store.AcquireLock()
		if err != nil {
			return commandError("configure", "configure_locked", err)
		}
		defer lock.Close()
		current, err := store.LoadConfig()
		if err != nil {
			return commandError("configure", "config_read_failed", err)
		}
		next := applyConfigPatch(current, request.Configure)
		if err := next.Validate(); err != nil {
			return commandError("configure", "invalid_configuration", err)
		}
		configChanged := next != current
		agentsSettingChanged := next.AgentsEnabled != current.AgentsEnabled
		var agentsPreview ManagedAgentsPreview
		if len(managedAgents) > 0 && managedAgents[0] != nil {
			agentsPreview, err = managedAgents[0].Preview(next.AgentsEnabled)
			if err != nil {
				return commandError("configure", "agents_preview_failed", err)
			}
		} else if agentsSettingChanged {
			return commandError("configure", "agents_unavailable", ErrUnavailable)
		}
		if !configChanged && !agentsPreview.Changed {
			return output.ActionResult{Command: "configure", Changed: false}, nil
		}
		preview := make([]string, 0, 2)
		resources := make([]string, 0, 2)
		if configChanged {
			preview = append(preview, "config: write validated preferences")
			resources = append(resources, "config")
		}
		if agentsPreview.Changed {
			managedResources := agentsPreview.Resources
			if len(managedResources) == 0 {
				managedResources = []string{"agents"}
			}
			resources = append(resources, managedResources...)
			if len(agentsPreview.Details) > 0 {
				preview = append(preview, agentsPreview.Details...)
			} else if agentsPreview.Detail != "" {
				preview = append(preview, agentsPreview.Detail)
			}
		}
		if next.HeartbeatSeconds != current.HeartbeatSeconds {
			preview = append(preview, fmt.Sprintf("LaunchAgent interval: %ds -> %ds", current.HeartbeatSeconds, next.HeartbeatSeconds))
		}
		previewResult := output.PreviewResult{Command: "configure", Effects: resources, Details: preview}
		if request.DryRun {
			return previewResult, nil
		}
		if previewer != nil {
			if err := previewer(previewResult); err != nil {
				return commandError("configure", "preview_write_failed", err)
			}
		}
		if !request.Confirm {
			if request.NonInteractive {
				return commandError("configure", "confirmation_required", ErrInvalidRequest)
			}
			if confirmer == nil {
				return commandError("configure", "confirmation_required", ErrInvalidRequest)
			}
			confirmed, err := confirmer()
			if err != nil {
				return commandError("configure", "confirmation_failed", err)
			}
			if !confirmed {
				return commandError("configure", "cancelled", ErrInvalidRequest)
			}
		}
		var managedSnapshot any
		if agentsPreview.Changed {
			if snapshotter, ok := managedAgents[0].(ManagedAgentsSnapshotter); ok {
				managedSnapshot, err = snapshotter.Snapshot()
				if err != nil {
					return commandError("configure", "managed_snapshot_failed", err)
				}
			}
		}
		schedulerChanged := next.HeartbeatSeconds != current.HeartbeatSeconds
		if schedulerChanged {
			if launchAgent == nil {
				return commandError("configure", "launch_agent_unavailable", ErrUnavailable)
			}
			if err := launchAgent.Apply(ctx, next); err != nil {
				if errors.Is(err, ErrLaunchAgentUnavailable) {
					return commandError("configure", "launch_agent_unavailable", err)
				}
				return commandError("configure", "launch_agent_apply_failed", err)
			}
			resources = append(resources, config.LaunchAgentLabel)
		}
		if agentsPreview.Changed {
			if _, applyErr := managedAgents[0].Apply(next.AgentsEnabled); applyErr != nil {
				var managedRollbackErr error
				if snapshotter, ok := managedAgents[0].(ManagedAgentsSnapshotter); ok && managedSnapshot != nil {
					managedRollbackErr = snapshotter.Restore(managedSnapshot)
				}
				var launchRollbackErr error
				if schedulerChanged {
					launchRollbackErr = launchAgent.Apply(ctx, current)
				}
				rollbackErr := errors.Join(applyErr, managedRollbackErr, launchRollbackErr)
				if launchRollbackErr != nil {
					return commandError("configure", "launch_agent_rollback_failed", rollbackErr)
				}
				if managedRollbackErr != nil {
					return commandError("configure", "managed_rollback_failed", rollbackErr)
				}
				return commandError("configure", "agents_apply_failed", rollbackErr)
			}
		}
		if configChanged {
			err = store.SaveConfig(next)
		}
		if err != nil {
			var managedRollbackErr error
			if agentsPreview.Changed {
				if snapshotter, ok := managedAgents[0].(ManagedAgentsSnapshotter); ok && managedSnapshot != nil {
					managedRollbackErr = snapshotter.Restore(managedSnapshot)
				} else {
					_, managedRollbackErr = managedAgents[0].Apply(current.AgentsEnabled)
				}
			}
			var launchRollbackErr error
			if schedulerChanged {
				launchRollbackErr = launchAgent.Apply(ctx, current)
			}
			rollbackErr := errors.Join(err, managedRollbackErr, launchRollbackErr)
			if launchRollbackErr != nil {
				return commandError("configure", "launch_agent_rollback_failed", rollbackErr)
			}
			if managedRollbackErr != nil {
				return commandError("configure", "managed_rollback_failed", rollbackErr)
			}
			return commandError("configure", "config_write_failed", err)
		}
		return output.ActionResult{Command: "configure", Changed: true, ResourceIDs: resources}, nil
	}
}

func applyConfigPatch(value config.Config, patch ConfigPatch) config.Config {
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
	if patch.AutoUpdateEnabled != nil {
		value.AutoUpdateEnabled = *patch.AutoUpdateEnabled
	}
	if patch.TokenDisplay != nil {
		value.TokenDisplay = *patch.TokenDisplay
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
