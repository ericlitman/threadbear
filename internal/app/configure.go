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
	Preview(bool) (string, error)
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
		preview := []string{"config: write validated preferences"}
		if next == current {
			return output.ActionResult{Command: "configure", Changed: false}, nil
		}
		resources := []string{"config"}
		agentsChanged := next.AgentsEnabled != current.AgentsEnabled
		if agentsChanged {
			resources = append(resources, "agents")
			if len(managedAgents) == 0 || managedAgents[0] == nil {
				return commandError("configure", "agents_unavailable", ErrUnavailable)
			}
			detail, err := managedAgents[0].Preview(next.AgentsEnabled)
			if err != nil {
				return commandError("configure", "agents_preview_failed", err)
			}
			preview = append(preview, detail)
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
		if agentsChanged {
			if len(managedAgents) == 0 || managedAgents[0] == nil {
				return commandError("configure", "agents_unavailable", ErrUnavailable)
			}
			if _, err := managedAgents[0].Apply(next.AgentsEnabled); err != nil {
				if schedulerChanged {
					_ = launchAgent.Apply(ctx, current)
				}
				return commandError("configure", "agents_apply_failed", err)
			}
		}
		if err := store.SaveConfig(next); err != nil {
			if agentsChanged {
				_, _ = managedAgents[0].Apply(current.AgentsEnabled)
			}
			if schedulerChanged {
				if rollbackErr := launchAgent.Apply(ctx, current); rollbackErr != nil {
					code := "launch_agent_rollback_failed"
					if errors.Is(rollbackErr, ErrLaunchAgentUnavailable) {
						code = "launch_agent_unavailable"
					}
					return commandError("configure", code, errors.Join(err, rollbackErr))
				}
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
