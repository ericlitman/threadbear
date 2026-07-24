package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ericlitman/threadbear/internal/config"
	"github.com/ericlitman/threadbear/internal/output"
)

type Command string

const (
	CommandInstall   Command = "install"
	CommandHeartbeat Command = "heartbeat"
	CommandStatus    Command = "status"
	CommandInspect   Command = "inspect"
	CommandConfigure Command = "configure"
	CommandEnable    Command = "enable"
	CommandDisable   Command = "disable"
	CommandRestore   Command = "restore"
	CommandSelfTest  Command = "self-test"
	CommandUpdate    Command = "update"
	CommandUninstall Command = "uninstall"
	CommandVersion   Command = "version"
)

var (
	ErrUnknownCommand = errors.New("unknown command")
	ErrInvalidRequest = errors.New("invalid command request")
	ErrUnavailable    = errors.New("command is not implemented yet")
)

type Request struct {
	Command Command
	JSON    bool
	DryRun  bool
	TaskID  string
}

type Handler func(context.Context, Request) (output.Result, error)

type Service struct {
	handlers map[Command]Handler
}

func New(version string) *Service {
	service := NewWithHandlers(nil)
	service.handlers[CommandVersion] = func(context.Context, Request) (output.Result, error) {
		return output.VersionResult{
			Product:          config.ProductName,
			InstalledVersion: version,
			Website:          config.Website,
		}, nil
	}
	return service
}

func NewWithHandlers(handlers map[Command]Handler) *Service {
	copied := make(map[Command]Handler, len(handlers))
	for command, handler := range handlers {
		copied[command] = handler
	}
	return &Service{handlers: copied}
}

func (s *Service) Dispatch(ctx context.Context, request Request) (output.Result, error) {
	if err := request.Validate(); err != nil {
		code := "invalid_request"
		if errors.Is(err, ErrUnknownCommand) {
			code = "unknown_command"
		}
		return output.ErrorResult{Operation: operationName(request.Command), ErrorCode: code}, err
	}
	handler, ok := s.handlers[request.Command]
	if !ok {
		return output.ErrorResult{Operation: string(request.Command), ErrorCode: "not_implemented"}, ErrUnavailable
	}
	result, err := handler(ctx, request)
	if result == nil {
		if err == nil {
			err = errors.New("command handler returned no result")
		}
		return output.ErrorResult{Operation: string(request.Command), ErrorCode: "command_failed"}, err
	}
	return result, err
}

func (r Request) Validate() error {
	if !r.Command.Valid() {
		return fmt.Errorf("%w: %q", ErrUnknownCommand, r.Command)
	}
	taskID := strings.TrimSpace(r.TaskID)
	needsTask := r.Command == CommandInspect || r.Command == CommandRestore
	if taskID != r.TaskID {
		return fmt.Errorf("%w: task ID must not contain surrounding whitespace", ErrInvalidRequest)
	}
	if needsTask && taskID == "" {
		return fmt.Errorf("%w: %s requires a task ID", ErrInvalidRequest, r.Command)
	}
	if !needsTask && taskID != "" {
		return fmt.Errorf("%w: %s does not accept a task ID", ErrInvalidRequest, r.Command)
	}
	if r.DryRun && r.Command != CommandHeartbeat {
		return fmt.Errorf("%w: --dry-run is heartbeat-only", ErrInvalidRequest)
	}
	return nil
}

func (c Command) Valid() bool {
	switch c {
	case CommandInstall, CommandHeartbeat, CommandStatus, CommandInspect, CommandConfigure, CommandEnable, CommandDisable, CommandRestore, CommandSelfTest, CommandUpdate, CommandUninstall, CommandVersion:
		return true
	default:
		return false
	}
}

func operationName(command Command) string {
	if command == "" {
		return "dispatch"
	}
	return string(command)
}
