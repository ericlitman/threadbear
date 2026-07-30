package titleplan

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"maps"
	"sort"
	"strings"
	"time"

	"github.com/ericlitman/threadbear/internal/codex"
	"github.com/ericlitman/threadbear/internal/config"
	"github.com/ericlitman/threadbear/internal/output"
	"github.com/ericlitman/threadbear/internal/state"
	statusresolver "github.com/ericlitman/threadbear/internal/status"
	"github.com/ericlitman/threadbear/internal/title"
	"github.com/ericlitman/threadbear/internal/tokens"
)

type Store interface {
	LoadConfig() (config.Config, error)
	LoadState() (state.State, error)
	SaveState(state.State) error
	LoadCycle() (state.CycleCheckpoint, error)
	RemoveCycle() error
	AcquireLock() (*state.Lock, error)
}

type Inventory interface {
	Task(context.Context, string, string) (codex.Task, bool, error)
}

type Request struct {
	Stage       bool
	Batch       bool
	OperationID string
	Report      bool
	Retired     bool
}

type Service struct {
	Store     Store
	Inventory Inventory
	Input     io.Reader
	ThreadID  func() string
	Now       func() time.Time
}

type nativeReport struct {
	OperationID string                   `json:"operation_id"`
	Outcome     state.NativeTitleOutcome `json:"outcome"`
	ErrorCode   string                   `json:"error_code,omitempty"`
}

func (s Service) Dispatch(ctx context.Context, request Request) (output.Result, error) {
	if s.Store == nil || s.Inventory == nil {
		return output.ErrorResult{Operation: "title-plan", ErrorCode: "dependency_unavailable"}, errors.New("title plan dependencies are unavailable")
	}
	cfg, err := s.Store.LoadConfig()
	if err != nil {
		return output.ErrorResult{Operation: "title-plan", ErrorCode: "config_read_failed"}, err
	}
	threadID := ""
	if s.ThreadID != nil {
		threadID = s.ThreadID()
	}
	if threadID == "" || threadID != cfg.ControlTaskID {
		return output.ErrorResult{Operation: "title-plan", ErrorCode: "unauthorized_control_task"}, errors.New("title plan access requires the configured control task")
	}
	if request.Retired {
		return output.TitleDispatchResult{Allow: false, Disposition: "retired"}, nil
	}
	lock, err := s.Store.AcquireLock()
	if errors.Is(err, state.ErrLocked) {
		return retryable(request, "heartbeat_active"), nil
	}
	if err != nil {
		return output.ErrorResult{Operation: "title-plan", ErrorCode: "lock_failed"}, err
	}
	defer lock.Close()
	committed, err := s.Store.LoadState()
	if err != nil {
		return output.ErrorResult{Operation: "title-plan", ErrorCode: "state_read_failed"}, err
	}
	ready, err := s.prepareCycle(committed)
	if err != nil {
		return output.ErrorResult{Operation: "title-plan", ErrorCode: "cycle_read_failed"}, err
	}
	if !ready {
		return retryable(request, "heartbeat_cycle_active"), nil
	}
	switch {
	case request.Stage:
		return s.stage(ctx, cfg, committed)
	case request.Batch:
		return s.batch(committed), nil
	case request.OperationID != "":
		return s.operation(ctx, cfg, committed, request.OperationID)
	case request.Report:
		return s.report(ctx, cfg, committed)
	default:
		return output.ErrorResult{Operation: "title-plan", ErrorCode: "invalid_request"}, errors.New("title plan mode is required")
	}
}

func retryable(request Request, code string) output.Result {
	switch {
	case request.Stage:
		return output.NewTitlePlanStageResult(false, code)
	case request.Batch:
		return output.NewTitlePlanBatchResult(false, code, nil)
	case request.OperationID != "":
		return output.NewTitlePlanOperationResult(false, code, request.OperationID)
	case request.Report:
		return output.NewTitlePlanReportResult(false, code, 0, 0)
	default:
		return output.ErrorResult{Operation: "title-plan", ErrorCode: code}
	}
}
func (s Service) prepareCycle(committed state.State) (bool, error) {
	cycle, err := s.Store.LoadCycle()
	if errors.Is(err, fs.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if committed.Generation > cycle.BaseGeneration {
		return true, s.Store.RemoveCycle()
	}
	return false, nil
}

func (s Service) stage(ctx context.Context, cfg config.Config, committed state.State) (output.Result, error) {
	footer, err := readExactFooter(s.Input)
	if err != nil {
		return output.ErrorResult{Operation: "title-plan", ErrorCode: "invalid_footer"}, err
	}
	parsed := statusresolver.ParseFooter(statusresolver.FooterInput{Message: footer, LatestTurnCompleted: true})
	if !parsed.Accepted {
		return output.ErrorResult{Operation: "title-plan", ErrorCode: "invalid_footer"}, errors.New("stage input is not an exact canonical footer")
	}
	current, found, err := s.Inventory.Task(ctx, cfg.ControlTaskID, cfg.ControlTaskID)
	if err != nil {
		return output.ErrorResult{Operation: "title-plan", ErrorCode: "revalidation_failed"}, err
	}
	if !found || current.TaskID != cfg.ControlTaskID || current.Archived || current.Source != "vscode" {
		return output.ErrorResult{Operation: "title-plan", ErrorCode: "control_task_unavailable"}, errors.New("configured control task is not active")
	}
	now := s.now()
	record, exists := committed.Tasks[cfg.ControlTaskID]
	if !exists {
		record = state.TaskRecord{TaskID: cfg.ControlTaskID, StateStartedAt: now, LastSubstantiveActivity: now}
	}
	record.CapturedRevision = current.Revision
	record.CapturedTitle = current.Title
	display := tokens.Display{}
	if cfg.TokenDisplay != tokens.PositionOff && record.TokenUsageFound {
		display = tokens.Display{Position: cfg.TokenDisplay, Value: tokens.Format(record.OutputTokens)}
	}
	action := parsed.Footer.Action
	if action == "none" {
		action = ""
	}
	rendered, err := title.Reconcile(record, parsed.Footer.Status, "", action, display)
	if err != nil {
		return output.ErrorResult{Operation: "title-plan", ErrorCode: "title_reconcile_failed"}, err
	}
	if record.Status != parsed.Footer.Status {
		record.StateStartedAt = now
	}
	record.Status = parsed.Footer.Status
	record.Provenance = state.ProvenanceFooter
	record.DurableSubject = rendered.DurableSubject
	record.ManagedAction = rendered.ManagedAction
	committed.Tasks[cfg.ControlTaskID] = record
	plan := state.PendingTitlePlan{OperationID: state.TitleOperationID(cfg.ControlTaskID, current.Revision, current.Title, rendered.Title), TaskID: cfg.ControlTaskID, ExpectedRevision: current.Revision, ExpectedTitle: current.Title, DesiredTitle: rendered.Title, DurableSubject: rendered.DurableSubject, ManagedAction: rendered.ManagedAction, ManagedTokenDisplay: rendered.ManagedTokenDisplay, ManagedTokenPosition: rendered.ManagedTokenPosition, NativeOutcome: state.NativeTitlePending}
	if existing, ok := committed.PendingTitlePlans[cfg.ControlTaskID]; ok && existing.OperationID == plan.OperationID {
		plan.NativeOutcome = existing.NativeOutcome
		plan.NativeReportedAt = existing.NativeReportedAt
		plan.NativeErrorCode = existing.NativeErrorCode
	}
	committed.PendingTitlePlans[cfg.ControlTaskID] = plan
	committed.Generation++
	if err := s.Store.SaveState(committed); err != nil {
		return output.ErrorResult{Operation: "title-plan", ErrorCode: "state_write_failed"}, err
	}
	return output.NewTitlePlanStageResult(true, ""), nil
}
func readExactFooter(input io.Reader) (string, error) {
	if input == nil {
		return "", errors.New("stage footer input is required")
	}
	data, err := io.ReadAll(io.LimitReader(input, 4097))
	if err != nil || len(data) > 4096 {
		if err == nil {
			err = errors.New("stage footer is too large")
		}
		return "", err
	}
	value := strings.TrimSuffix(string(data), "\n")
	if value == "" || strings.ContainsAny(value, "\r\n") || strings.TrimSpace(value) != value {
		return "", errors.New("stage footer must be one exact canonical line")
	}
	return value, nil
}
func (s Service) batch(committed state.State) output.Result {
	ids := make([]string, 0, len(committed.PendingTitlePlans))
	for _, plan := range committed.PendingTitlePlans {
		if plan.NativeOutcome != state.NativeTitleSucceeded {
			ids = append(ids, plan.OperationID)
		}
	}
	sort.Strings(ids)
	return output.NewTitlePlanBatchResult(true, "", ids)
}
func (s Service) operation(ctx context.Context, cfg config.Config, committed state.State, operationID string) (output.Result, error) {
	base := output.NewTitlePlanOperationResult(true, "", operationID)
	var plan state.PendingTitlePlan
	foundPlan := false
	for _, candidate := range committed.PendingTitlePlans {
		if candidate.OperationID == operationID {
			plan = candidate
			foundPlan = true
			break
		}
	}
	if !foundPlan {
		base.Disposition = "missing"
		return base, nil
	}
	if plan.NativeOutcome == state.NativeTitleSucceeded {
		base.Disposition = "rejected"
		return base, nil
	}
	task, found, err := s.Inventory.Task(ctx, cfg.ControlTaskID, plan.TaskID)
	if err != nil {
		return output.ErrorResult{Operation: "title-plan", ErrorCode: "revalidation_failed"}, err
	}
	if !found || task.TaskID != plan.TaskID || task.Archived || task.Source != "vscode" {
		base.Disposition = "missing"
		return base, nil
	}
	base.TaskID = plan.TaskID
	base.DesiredTitle = plan.DesiredTitle
	switch {
	case task.Title == plan.DesiredTitle && (plan.DesiredTitle != plan.ExpectedTitle || task.Revision != plan.ExpectedRevision):
		base.Disposition = "ready"
		base.Action = "report_success"
	case task.Revision == plan.ExpectedRevision && task.Title == plan.ExpectedTitle:
		base.Disposition = "ready"
		base.Action = "set"
	default:
		base.TaskID = ""
		base.DesiredTitle = ""
		base.Disposition = "drifted"
	}
	return base, nil
}

func (s Service) report(ctx context.Context, cfg config.Config, committed state.State) (output.Result, error) {
	committed.PendingTitlePlans = maps.Clone(committed.PendingTitlePlans)
	if s.Input == nil {
		return output.ErrorResult{Operation: "title-plan", ErrorCode: "report_read_failed"}, errors.New("title report input is required")
	}
	var envelope struct {
		Reports []nativeReport `json:"reports"`
	}
	decoder := json.NewDecoder(s.Input)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return output.ErrorResult{Operation: "title-plan", ErrorCode: "invalid_report"}, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return output.ErrorResult{Operation: "title-plan", ErrorCode: "invalid_report"}, errors.New("multiple report values")
	}
	if len(envelope.Reports) == 0 {
		return output.ErrorResult{Operation: "title-plan", ErrorCode: "invalid_report"}, errors.New("at least one report is required")
	}
	byOperation := make(map[string]string, len(committed.PendingTitlePlans))
	for taskID, plan := range committed.PendingTitlePlans {
		byOperation[plan.OperationID] = taskID
	}
	accepted, unchanged := 0, 0
	seen := make(map[string]struct{}, len(envelope.Reports))
	for _, report := range envelope.Reports {
		if report.OperationID == "" || strings.TrimSpace(report.OperationID) != report.OperationID {
			return output.ErrorResult{Operation: "title-plan", ErrorCode: "invalid_report"}, errors.New("report operation_id is invalid")
		}
		if _, duplicate := seen[report.OperationID]; duplicate {
			return output.ErrorResult{Operation: "title-plan", ErrorCode: "invalid_report"}, errors.New("duplicate operation report")
		}
		seen[report.OperationID] = struct{}{}
		taskID, ok := byOperation[report.OperationID]
		if !ok {
			return output.ErrorResult{Operation: "title-plan", ErrorCode: "unknown_operation"}, errors.New("reported title operation is not pending")
		}
		plan := committed.PendingTitlePlans[taskID]
		if report.Outcome != state.NativeTitleSucceeded && report.Outcome != state.NativeTitleFailed {
			return output.ErrorResult{Operation: "title-plan", ErrorCode: "invalid_report"}, errors.New("report outcome is invalid")
		}
		if report.Outcome == state.NativeTitleSucceeded && report.ErrorCode != "" || report.Outcome == state.NativeTitleFailed && !stableCode(report.ErrorCode) {
			return output.ErrorResult{Operation: "title-plan", ErrorCode: "invalid_report"}, errors.New("report error_code is invalid")
		}
		if plan.NativeOutcome == state.NativeTitleSucceeded {
			if report.Outcome != state.NativeTitleSucceeded {
				return output.ErrorResult{Operation: "title-plan", ErrorCode: "report_conflict"}, errors.New("successful title report cannot be downgraded")
			}
			unchanged++
			continue
		}
		if plan.NativeOutcome == state.NativeTitleFailed && report.Outcome == state.NativeTitleFailed {
			if plan.NativeErrorCode != report.ErrorCode {
				return output.ErrorResult{Operation: "title-plan", ErrorCode: "report_conflict"}, errors.New("failed title report conflicts with the recorded error")
			}
			unchanged++
			continue
		}
		task, found, err := s.Inventory.Task(ctx, cfg.ControlTaskID, taskID)
		if err != nil {
			return output.ErrorResult{Operation: "title-plan", ErrorCode: "revalidation_failed"}, err
		}
		if !found || task.TaskID != taskID || task.Archived || task.Source != "vscode" {
			return output.ErrorResult{Operation: "title-plan", ErrorCode: "revalidation_failed"}, errors.New("reported task is not an active Desktop task")
		}
		if report.Outcome == state.NativeTitleSucceeded {
			if task.Title != plan.DesiredTitle {
				return output.ErrorResult{Operation: "title-plan", ErrorCode: "revalidation_failed"}, errors.New("reported title success is not visible")
			}
			plan.NativeOutcome = state.NativeTitleSucceeded
			plan.NativeErrorCode = ""
		} else {
			if task.Revision != plan.ExpectedRevision || task.Title != plan.ExpectedTitle {
				return output.ErrorResult{Operation: "title-plan", ErrorCode: "revalidation_failed"}, errors.New("reported title failure drifted from its guard")
			}
			plan.NativeOutcome = state.NativeTitleFailed
			plan.NativeErrorCode = report.ErrorCode
		}
		now := s.now()
		plan.NativeReportedAt = &now
		committed.PendingTitlePlans[taskID] = plan
		accepted++
	}
	if accepted > 0 {
		committed.Generation++
		if err := s.Store.SaveState(committed); err != nil {
			return output.ErrorResult{Operation: "title-plan", ErrorCode: "state_write_failed"}, err
		}
	}
	return output.NewTitlePlanReportResult(true, "", accepted, unchanged), nil
}
func (s Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}
func stableCode(value string) bool {
	if value == "" {
		return false
	}
	separator := false
	for index, char := range value {
		alphanumeric := char >= 'a' && char <= 'z' || char >= '0' && char <= '9'
		if alphanumeric {
			separator = false
			continue
		}
		if char != '_' && char != '-' && char != '.' || index == 0 || separator {
			return false
		}
		separator = true
	}
	return !separator
}
