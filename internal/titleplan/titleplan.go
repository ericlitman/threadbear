package titleplan

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"sort"
	"strings"
	"time"

	"github.com/ericlitman/threadbear/internal/codex"
	"github.com/ericlitman/threadbear/internal/config"
	"github.com/ericlitman/threadbear/internal/output"
	"github.com/ericlitman/threadbear/internal/state"
)

type Store interface {
	LoadConfig() (config.Config, error)
	LoadState() (state.State, error)
	SaveState(state.State) error
	LoadCycle() (state.CycleCheckpoint, error)
	AcquireLock() (*state.Lock, error)
}
type Inventory interface {
	Inventory(context.Context, string) (codex.Inventory, error)
}
type Heartbeat interface {
	Run(context.Context, bool) (output.Result, error)
}
type SingleTaskPlanner interface {
	PlanTitle(context.Context, string) error
}
type TerminalWaiter interface {
	Wait(context.Context, string) error
}

type Service struct {
	Store     Store
	Inventory Inventory
	Heartbeat Heartbeat
	Planner   SingleTaskPlanner
	Waiter    TerminalWaiter
	Reports   io.Reader
	Now       func() time.Time
}
type NativeReport struct {
	OperationID   string `json:"operation_id"`
	TaskID        string `json:"task_id"`
	NativeSuccess *bool  `json:"native_success"`
	ErrorCode     string `json:"error_code,omitempty"`
}
type reportEnvelope struct {
	Reports []NativeReport `json:"reports"`
}

var errCycleInProgress = errors.New("cycle_in_progress")

func (s Service) Plan(ctx context.Context, taskID, operationID string, batch, report bool) (output.Result, error) {
	modes := 0
	if taskID != "" {
		modes++
	}
	if operationID != "" {
		modes++
	}
	if batch {
		modes++
	}
	if report {
		modes++
	}
	if strings.TrimSpace(taskID) != taskID || strings.TrimSpace(operationID) != operationID || modes != 1 {
		return output.ErrorResult{Operation: "title-plan", ErrorCode: "invalid_request"}, errors.New("title-plan requires exactly one strict mode")
	}
	if report {
		if s.Store == nil || s.Now == nil {
			return output.ErrorResult{Operation: "title-plan", ErrorCode: "dependency_unavailable"}, errors.New("title report dependencies unavailable")
		}
		return s.report()
	}
	if s.Store == nil || s.Inventory == nil || s.Now == nil {
		return output.ErrorResult{Operation: "title-plan", ErrorCode: "dependency_unavailable"}, errors.New("title-plan dependencies unavailable")
	}
	mode := "batch"
	if taskID != "" {
		mode = "wait"
	} else if operationID != "" {
		mode = "operation"
	}
	cfg, err := s.Store.LoadConfig()
	if err != nil {
		return output.ErrorResult{Operation: "title-plan", ErrorCode: "config_read_failed"}, err
	}
	if !cfg.RenameEnabled {
		return s.disabled(mode, taskID)
	}
	if taskID != "" && taskID == cfg.ControlTaskID {
		return output.TitlePlanResult{Mode: mode, Plans: []output.TitlePlanItem{}, Dispositions: disabledDispositions(taskID)}, nil
	}
	if taskID != "" {
		if s.Waiter == nil || s.Planner == nil {
			return output.ErrorResult{Operation: "title-plan", ErrorCode: "wait_unavailable"}, errors.New("single-task title planner unavailable")
		}
		if err := s.Waiter.Wait(ctx, taskID); err != nil {
			return output.ErrorResult{Operation: "title-plan", ErrorCode: "wait_failed"}, err
		}
		if err := s.Planner.PlanTitle(ctx, taskID); err != nil {
			return output.ErrorResult{Operation: "title-plan", ErrorCode: "planning_failed"}, err
		}
	} else if batch {
		if s.Heartbeat == nil {
			return output.ErrorResult{Operation: "title-plan", ErrorCode: "batch_unavailable"}, errors.New("batch heartbeat unavailable")
		}
		if _, err := s.Heartbeat.Run(ctx, false); err != nil {
			return output.ErrorResult{Operation: "title-plan", ErrorCode: "planning_failed"}, err
		}
	} else if operationID == "" {
		return output.ErrorResult{Operation: "title-plan", ErrorCode: "invalid_request"}, errors.New("title-plan mode is invalid")
	}
	lock, err := s.Store.AcquireLock()
	if err != nil {
		return output.ErrorResult{Operation: "title-plan", ErrorCode: "title_plan_locked"}, err
	}
	defer lock.Close()
	cfg, err = s.Store.LoadConfig()
	if err != nil {
		return output.ErrorResult{Operation: "title-plan", ErrorCode: "config_read_failed"}, err
	}
	if !cfg.RenameEnabled {
		return output.TitlePlanResult{Mode: mode, Plans: []output.TitlePlanItem{}, Dispositions: disabledDispositions(taskID)}, nil
	}
	if err := refuseCycle(s.Store); err != nil {
		return output.ErrorResult{Operation: "title-plan", ErrorCode: "cycle_in_progress"}, err
	}
	committed, err := s.Store.LoadState()
	if err != nil {
		return output.ErrorResult{Operation: "title-plan", ErrorCode: "state_read_failed"}, err
	}
	observed, err := s.Inventory.Inventory(ctx, cfg.ControlTaskID)
	if err != nil {
		return output.ErrorResult{Operation: "title-plan", ErrorCode: "inventory_failed"}, err
	}
	byID := make(map[string]codex.Task, len(observed.Tasks))
	for _, task := range observed.Tasks {
		byID[task.TaskID] = task
	}
	result := output.TitlePlanResult{Mode: mode, Plans: []output.TitlePlanItem{}, Dispositions: []output.TitlePlanDisposition{}}
	ids := make([]string, 0, len(committed.PendingTitlePlans))
	for id, plan := range committed.PendingTitlePlans {
		if taskID != "" && id != taskID {
			continue
		}
		if operationID != "" && plan.OperationID != operationID {
			continue
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	changed := false
	now := s.Now().UTC()
	for _, id := range ids {
		plan, task := committed.PendingTitlePlans[id], byID[id]
		sameTitleRefresh := plan.ExpectedTitle == plan.DesiredTitle
		switch {
		case task.TaskID == "":
			delete(committed.PendingTitlePlans, id)
			result.Dispositions = append(result.Dispositions, output.TitlePlanDisposition{TaskID: id, Outcome: "missing"})
			changed = true
		case task.Title == plan.DesiredTitle && (!sameTitleRefresh || plan.NativeOutcome == state.NativeTitleSucceeded):
			applyCanonical(&committed, plan, task)
			result.Dispositions = append(result.Dispositions, output.TitlePlanDisposition{TaskID: id, Outcome: "canonical_persisted"})
			changed = true
		case task.Revision != plan.ExpectedRevision || task.Title != plan.ExpectedTitle:
			delete(committed.PendingTitlePlans, id)
			result.Dispositions = append(result.Dispositions, output.TitlePlanDisposition{TaskID: id, Outcome: "drifted"})
			changed = true
		case plan.NativeOutcome == state.NativeTitleSucceeded && plan.NativeReportedAt != nil && !now.Before(plan.NativeReportedAt.Add(state.NativeTitleCanonicalTimeout)):
			plan.NativeOutcome, plan.NativeErrorCode, plan.NativeReportedAt = state.NativeTitleFailed, "canonical_not_persisted", &now
			committed.PendingTitlePlans[id] = plan
			result.Plans = append(result.Plans, titlePlanItem(plan))
			changed = true
		case plan.NativeOutcome == state.NativeTitleSucceeded:
			result.Dispositions = append(result.Dispositions, output.TitlePlanDisposition{TaskID: id, Outcome: "native_succeeded_pending_canonical"})
		default:
			result.Plans = append(result.Plans, titlePlanItem(plan))
		}
	}
	if taskID != "" && len(ids) == 0 {
		result.Dispositions = append(result.Dispositions, output.TitlePlanDisposition{TaskID: taskID, Outcome: "no_op"})
	}
	if changed {
		committed.Generation++
		if err := s.Store.SaveState(committed); err != nil {
			return output.ErrorResult{Operation: "title-plan", ErrorCode: "state_write_failed"}, err
		}
	}
	return result, nil
}

func titlePlanItem(plan state.PendingTitlePlan) output.TitlePlanItem {
	return output.TitlePlanItem{OperationID: plan.OperationID, TaskID: plan.TaskID, ExpectedRevision: plan.ExpectedRevision, ExpectedTitle: plan.ExpectedTitle, DesiredTitle: plan.DesiredTitle}
}

func disabledDispositions(taskID string) []output.TitlePlanDisposition {
	if taskID == "" {
		return []output.TitlePlanDisposition{}
	}
	return []output.TitlePlanDisposition{{TaskID: taskID, Outcome: "no_op"}}
}

func refuseCycle(store Store) error {
	_, err := store.LoadCycle()
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return errCycleInProgress
}

func (s Service) disabled(mode, taskID string) (output.Result, error) {
	lock, err := s.Store.AcquireLock()
	if err != nil {
		return output.ErrorResult{Operation: "title-plan", ErrorCode: "title_plan_locked"}, err
	}
	defer lock.Close()
	cfg, err := s.Store.LoadConfig()
	if err != nil {
		return output.ErrorResult{Operation: "title-plan", ErrorCode: "config_read_failed"}, err
	}
	result := output.TitlePlanResult{Mode: mode, Plans: []output.TitlePlanItem{}, Dispositions: disabledDispositions(taskID)}
	if cfg.RenameEnabled {
		return result, nil
	}
	if err := refuseCycle(s.Store); err != nil {
		if errors.Is(err, errCycleInProgress) {
			return result, nil
		}
		return output.ErrorResult{Operation: "title-plan", ErrorCode: "cycle_read_failed"}, err
	}
	committed, err := s.Store.LoadState()
	if err != nil {
		return output.ErrorResult{Operation: "title-plan", ErrorCode: "state_read_failed"}, err
	}
	if len(committed.PendingTitlePlans) == 0 {
		return result, nil
	}
	committed.PendingTitlePlans = make(map[string]state.PendingTitlePlan)
	committed.Generation++
	if err := s.Store.SaveState(committed); err != nil {
		return output.ErrorResult{Operation: "title-plan", ErrorCode: "state_write_failed"}, err
	}
	return result, nil
}

func (s Service) report() (output.Result, error) {
	if s.Reports == nil {
		return output.ErrorResult{Operation: "title-plan", ErrorCode: "report_unavailable"}, errors.New("report input unavailable")
	}
	decoder := json.NewDecoder(s.Reports)
	decoder.DisallowUnknownFields()
	var envelope reportEnvelope
	if err := decoder.Decode(&envelope); err != nil {
		return output.ErrorResult{Operation: "title-plan", ErrorCode: "invalid_report"}, err
	}
	if envelope.Reports == nil {
		return output.ErrorResult{Operation: "title-plan", ErrorCode: "invalid_report"}, errors.New("reports must be an array")
	}
	for _, report := range envelope.Reports {
		if report.NativeSuccess == nil {
			return output.ErrorResult{Operation: "title-plan", ErrorCode: "invalid_report"}, errors.New("native_success is required")
		}
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return output.ErrorResult{Operation: "title-plan", ErrorCode: "invalid_report"}, errors.New("report must contain one JSON value")
	}
	lock, err := s.Store.AcquireLock()
	if err != nil {
		return output.ErrorResult{Operation: "title-plan", ErrorCode: "title_plan_locked"}, err
	}
	defer lock.Close()
	cfg, err := s.Store.LoadConfig()
	if err != nil {
		return output.ErrorResult{Operation: "title-plan", ErrorCode: "config_read_failed"}, err
	}
	if !cfg.RenameEnabled {
		return output.ErrorResult{Operation: "title-plan", ErrorCode: "rename_disabled"}, errors.New("rename_disabled")
	}
	if err := refuseCycle(s.Store); err != nil {
		return output.ErrorResult{Operation: "title-plan", ErrorCode: "cycle_in_progress"}, err
	}
	committed, err := s.Store.LoadState()
	if err != nil {
		return output.ErrorResult{Operation: "title-plan", ErrorCode: "state_read_failed"}, err
	}
	result := output.TitleReportResult{AcceptedIDs: []string{}, RejectedIDs: []string{}}
	now := s.Now().UTC()
	counts := make(map[string]int, len(envelope.Reports))
	for _, report := range envelope.Reports {
		counts[report.TaskID]++
	}
	seenRejected := make(map[string]struct{}, len(envelope.Reports))
	changed := false
	for _, report := range envelope.Reports {
		plan, ok := committed.PendingTitlePlans[report.TaskID]
		validID := report.TaskID != "" && strings.TrimSpace(report.TaskID) == report.TaskID
		succeeded := *report.NativeSuccess
		validOutcome := succeeded != (report.ErrorCode != "")
		if counts[report.TaskID] != 1 || !validID || !ok || plan.OperationID != report.OperationID || !validOutcome {
			if validID {
				if _, seen := seenRejected[report.TaskID]; !seen {
					result.RejectedIDs = append(result.RejectedIDs, report.TaskID)
					seenRejected[report.TaskID] = struct{}{}
				}
			}
			continue
		}
		if plan.NativeOutcome == state.NativeTitleSucceeded {
			if succeeded {
				result.AcceptedIDs = append(result.AcceptedIDs, report.TaskID)
			} else {
				result.RejectedIDs = append(result.RejectedIDs, report.TaskID)
			}
			continue
		}
		if plan.NativeOutcome == state.NativeTitleFailed && !succeeded && plan.NativeErrorCode == report.ErrorCode {
			result.AcceptedIDs = append(result.AcceptedIDs, report.TaskID)
			continue
		}
		candidate := plan
		candidate.NativeReportedAt = &now
		if succeeded {
			candidate.NativeOutcome, candidate.NativeErrorCode = state.NativeTitleSucceeded, ""
		} else {
			candidate.NativeOutcome, candidate.NativeErrorCode = state.NativeTitleFailed, report.ErrorCode
		}
		if err := candidate.Validate(); err != nil {
			result.RejectedIDs = append(result.RejectedIDs, report.TaskID)
			continue
		}
		committed.PendingTitlePlans[report.TaskID] = candidate
		result.AcceptedIDs = append(result.AcceptedIDs, report.TaskID)
		changed = true
	}
	sort.Strings(result.AcceptedIDs)
	sort.Strings(result.RejectedIDs)
	if changed {
		committed.Generation++
		if err := s.Store.SaveState(committed); err != nil {
			return output.ErrorResult{Operation: "title-plan", ErrorCode: "state_write_failed"}, err
		}
	}
	return result, nil
}

func applyCanonical(committed *state.State, plan state.PendingTitlePlan, task codex.Task) {
	if record, ok := committed.Tasks[plan.TaskID]; ok {
		if task.Revision == plan.ExpectedRevision {
			record.CapturedRevision = task.Revision
		}
		record.CapturedTitle, record.LastAppliedTitle = task.Title, task.Title
		record.DurableSubject, record.ManagedAction = plan.DurableSubject, plan.ManagedAction
		record.ManagedTokenDisplay, record.ManagedTokenPosition = plan.ManagedTokenDisplay, plan.ManagedTokenPosition
		committed.Tasks[plan.TaskID] = record
	}
	delete(committed.PendingTitlePlans, plan.TaskID)
}
