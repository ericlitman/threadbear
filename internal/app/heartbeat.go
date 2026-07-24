package app

import (
	"context"
	"errors"

	"github.com/ericlitman/threadbear/internal/output"
)

type HeartbeatRunner interface {
	Run(context.Context, bool) (output.Result, error)
}

func NewWithHeartbeat(version string, runner HeartbeatRunner) *Service {
	service := New(version)
	service.handlers[CommandHeartbeat] = HeartbeatHandler(runner)
	return service
}

func HeartbeatHandler(runner HeartbeatRunner) Handler {
	return func(ctx context.Context, request Request) (output.Result, error) {
		if runner == nil {
			return output.ErrorResult{Operation: "heartbeat", ErrorCode: "not_implemented"}, ErrUnavailable
		}
		if request.Command != CommandHeartbeat {
			return output.ErrorResult{Operation: "heartbeat", ErrorCode: "invalid_request"}, errors.New("heartbeat handler received another command")
		}
		return runner.Run(ctx, request.DryRun)
	}
}
