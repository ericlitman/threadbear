package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ericlitman/threadbear/internal/config"
	"github.com/ericlitman/threadbear/internal/output"
	"github.com/ericlitman/threadbear/internal/tokens"
)

type Command string

const (
	CommandInstall   Command = "install"
	CommandHeartbeat Command = "heartbeat"
	CommandTitlePlan Command = "title-plan"
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

var allCommands = [...]Command{
	CommandInstall,
	CommandHeartbeat,
	CommandTitlePlan,
	CommandStatus,
	CommandInspect,
	CommandConfigure,
	CommandEnable,
	CommandDisable,
	CommandRestore,
	CommandSelfTest,
	CommandUpdate,
	CommandUninstall,
	CommandVersion,
}

func AllCommands() []Command {
	commands := make([]Command, len(allCommands))
	copy(commands, allCommands[:])
	return commands
}

var (
	ErrUnknownCommand = errors.New("unknown command")
	ErrInvalidRequest = errors.New("invalid command request")
	ErrUnavailable    = errors.New("command is not implemented yet")
)

type ConfigPatch struct {
	HeartbeatSeconds             *int
	ArchiveEnabled               *bool
	ArchiveAfterDays             *int
	RenameEnabled                *bool
	AutoUpdateEnabled            *bool
	TokenDisplay                 *tokens.Position
	AgentsEnabled                *bool
	ClassifierModel              *string
	ClassifierEffort             *config.ClassifierEffort
	ClassifierContextBudgetBytes *int
}

func (p ConfigPatch) Empty() bool {
	return p.HeartbeatSeconds == nil && p.ArchiveEnabled == nil && p.ArchiveAfterDays == nil && p.RenameEnabled == nil && p.AutoUpdateEnabled == nil && p.TokenDisplay == nil && p.AgentsEnabled == nil && p.ClassifierModel == nil && p.ClassifierEffort == nil && p.ClassifierContextBudgetBytes == nil
}

type Request struct {
	Command            Command
	JSON               bool
	DryRun             bool
	Confirm            bool
	NonInteractive     bool
	Candidate          bool
	ArchiveControlTask bool
	Version            string
	TaskID             string
	TitlePlanWait      string
	TitlePlanBatch     bool
	TitlePlanReport    bool
	ControlTaskID      string
	Configure          ConfigPatch
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
	if strings.TrimSpace(r.TitlePlanWait) != r.TitlePlanWait {
		return fmt.Errorf("%w: title-plan wait task ID must not contain surrounding whitespace", ErrInvalidRequest)
	}
	if r.Command == CommandTitlePlan {
		modes := 0
		if r.TitlePlanWait != "" {
			modes++
		}
		if r.TitlePlanBatch {
			modes++
		}
		if r.TitlePlanReport {
			modes++
		}
		if !r.JSON || modes != 1 {
			return fmt.Errorf("%w: title-plan requires --json and exactly one of --wait, --batch, or --report", ErrInvalidRequest)
		}
	} else if r.TitlePlanWait != "" || r.TitlePlanBatch || r.TitlePlanReport {
		return fmt.Errorf("%w: title-plan flags are title-plan-only", ErrInvalidRequest)
	}
	if strings.TrimSpace(r.ControlTaskID) != r.ControlTaskID {
		return fmt.Errorf("%w: control task ID must not contain surrounding whitespace", ErrInvalidRequest)
	}
	if r.ControlTaskID != "" && r.Command != CommandInstall {
		return fmt.Errorf("%w: --control-task-id is install-only", ErrInvalidRequest)
	}
	if r.DryRun && r.Command != CommandHeartbeat && r.Command != CommandConfigure && r.Command != CommandInstall {
		return fmt.Errorf("%w: --dry-run is heartbeat-, configure-, or install-only", ErrInvalidRequest)
	}
	if r.Command != CommandConfigure && r.Command != CommandInstall && !r.Configure.Empty() {
		return fmt.Errorf("%w: preference patch is install- or configure-only", ErrInvalidRequest)
	}
	if r.Confirm && r.Command != CommandInstall && r.Command != CommandConfigure && r.Command != CommandUninstall {
		return fmt.Errorf("%w: --confirm is install-, configure-, or uninstall-only", ErrInvalidRequest)
	}
	if r.NonInteractive && r.Command != CommandInstall && r.Command != CommandConfigure && r.Command != CommandUninstall {
		return fmt.Errorf("%w: --noninteractive is install-, configure-, or uninstall-only", ErrInvalidRequest)
	}
	if r.Candidate && r.Command != CommandSelfTest {
		return fmt.Errorf("%w: --candidate is self-test-only", ErrInvalidRequest)
	}
	if r.ArchiveControlTask && r.Command != CommandUninstall {
		return fmt.Errorf("%w: uninstall choices are uninstall-only", ErrInvalidRequest)
	}
	if r.Version != "" && r.Command != CommandInstall && r.Command != CommandUpdate {
		return fmt.Errorf("%w: --version selection is install- or update-only", ErrInvalidRequest)
	}
	if r.Version != "" && !exactVersion(r.Version) {
		return fmt.Errorf("%w: version must be an exact version without a leading v", ErrInvalidRequest)
	}
	if value := r.Configure.HeartbeatSeconds; value != nil && *value <= 0 {
		return fmt.Errorf("%w: heartbeat seconds must be positive", ErrInvalidRequest)
	}
	if value := r.Configure.ArchiveAfterDays; value != nil && *value <= 0 {
		return fmt.Errorf("%w: archive days must be positive", ErrInvalidRequest)
	}
	if value := r.Configure.TokenDisplay; value != nil && !value.Valid() {
		return fmt.Errorf("%w: token display must be off, start, or end", ErrInvalidRequest)
	}
	if value := r.Configure.ClassifierModel; value != nil && (*value == "" || strings.TrimSpace(*value) != *value) {
		return fmt.Errorf("%w: classifier model is invalid", ErrInvalidRequest)
	}
	if value := r.Configure.ClassifierEffort; value != nil {
		switch *value {
		case config.EffortLow, config.EffortMedium, config.EffortHigh, config.EffortXHigh:
		default:
			return fmt.Errorf("%w: classifier effort is unsupported", ErrInvalidRequest)
		}
	}
	if value := r.Configure.ClassifierContextBudgetBytes; value != nil && *value <= 0 {
		return fmt.Errorf("%w: classifier context budget must be positive", ErrInvalidRequest)
	}
	return nil
}

func exactVersion(value string) bool {
	parts := strings.Split(value, ".")
	if len(parts) != 3 || strings.TrimSpace(value) != value {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
		for _, char := range part {
			if char < '0' || char > '9' {
				return false
			}
		}
	}
	return true
}

func (c Command) Valid() bool {
	for _, command := range allCommands {
		if c == command {
			return true
		}
	}
	return false
}

func operationName(command Command) string {
	if command == "" {
		return "dispatch"
	}
	return string(command)
}
