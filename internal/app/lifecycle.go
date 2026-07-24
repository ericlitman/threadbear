package app

import (
	"context"
	"errors"

	"github.com/ericlitman/threadbear/internal/config"
	"github.com/ericlitman/threadbear/internal/output"
)

func LifecycleHandler(store OperatorStore, launchAgent LaunchAgent, enable bool) Handler {
	command := "disable"
	if enable {
		command = "enable"
	}
	return func(ctx context.Context, request Request) (output.Result, error) {
		if string(request.Command) != command {
			return commandError(command, "invalid_request", ErrInvalidRequest)
		}
		if store == nil || launchAgent == nil {
			return commandError(command, "dependency_unavailable", ErrUnavailable)
		}
		lock, err := store.AcquireLock()
		if err != nil {
			return commandError(command, command+"_locked", err)
		}
		defer lock.Close()
		var changed bool
		if enable {
			changed, err = launchAgent.Enable(ctx)
		} else {
			changed, err = launchAgent.Disable(ctx)
		}
		if err != nil {
			if errors.Is(err, ErrLaunchAgentUnavailable) {
				return commandError(command, "launch_agent_unavailable", err)
			}
			return commandError(command, "launch_agent_"+command+"_failed", err)
		}
		return output.ActionResult{Command: command, Changed: changed, ResourceIDs: []string{config.LaunchAgentLabel}}, nil
	}
}
