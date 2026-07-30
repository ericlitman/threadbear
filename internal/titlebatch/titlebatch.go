package titlebatch

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
	"github.com/ericlitman/threadbear/internal/status"
	"github.com/ericlitman/threadbear/internal/title"
	"github.com/ericlitman/threadbear/internal/tokens"
)

const maxCanonicalAttempts = 3

type Store interface {
	LoadConfig() (config.Config, error)
	LoadState() (state.State, error)
	SaveState(state.State) error
	LoadCycle() (state.CycleCheckpoint, error)
	AcquireLock() (*state.Lock, error)
}

type Inventory interface {
	Inventory(context.Context, string) (codex.Inventory, error)
	Task(context.Context, string) (codex.Task, error)
}

type Service struct {
	Store          Store
	Inventory      Inventory
	Input          io.Reader
	Now            func() time.Time
	SourceIdentity func() string
}

type stageEnvelope struct {
	Footer string `json:"footer"`
}

type nativeReport struct {
	OperationID string `json:"operation_id"`
	Outcome     string `json:"outcome"`
	ErrorCode   string `json:"error_code,omitempty"`
}

type reportEnvelope struct {
	Reports []nativeReport `json:"reports"`
}

func (s Service) List(ctx context.Context) (output.Result, error) {
	return s.plan(ctx, "")
}

func (s Service) Operation(ctx context.Context, operationID string) (output.Result, error) {
	return s.plan(ctx, operationID)
}

func (s Service) Stage(ctx context.Context) (output.Result, error) {
	var envelope stageEnvelope
	if err := decodeStrict(s.Input, &envelope); err != nil {
		return commandError("invalid_stage", err)
	}
	lock, cfg, err := s.lockedConfig()
	if err != nil {
		return commandError(codeFor(err), err)
	}
	defer lock.Close()
	if err := refuseCycle(s.Store); err != nil {
		return commandError("cycle_in_progress", err)
	}
	committed, err := s.Store.LoadState()
	if err != nil {
		return commandError("state_read_failed", err)
	}
	task, err := s.Inventory.Task(ctx, cfg.ControlTaskID)
	if err == nil && task.Archived {
		err = errors.New("source task is archived")
	}
	if err != nil {
		return commandError("source_missing", err)
	}
	record, owned := committed.Tasks[cfg.ControlTaskID]
	if !owned {
		record = state.TaskRecord{TaskID: task.TaskID, CapturedRevision: task.Revision, CapturedTitle: task.Title, Status: state.StatusUnknown, Provenance: state.ProvenanceUnknown, StateStartedAt: s.now(), LastSubstantiveActivity: s.now(), TokenDisplayPosition: cfg.TokenDisplay}
	}
	parsed := status.ParseFooter(status.FooterInput{Message: envelope.Footer, LatestTurnCompleted: true})
	if !parsed.Accepted {
		return commandError("invalid_footer", errors.New("terminal footer is not accepted"))
	}
	record.CapturedRevision = task.Revision
	record.CapturedTitle = task.Title
	display := tokens.Display{}
	if cfg.TokenDisplay != tokens.PositionOff && record.ManagedTokenDisplay != "" {
		display = tokens.Display{Position: cfg.TokenDisplay, Value: record.ManagedTokenDisplay}
	}
	action := parsed.Footer.Action
	if parsed.Footer.Owner == status.OwnerNone {
		action = ""
	}
	rendered, err := title.Reconcile(record, parsed.Footer.Status, "", action, display)
	if err != nil {
		return commandError("title_render_failed", err)
	}
	record.Status, record.Provenance = parsed.Footer.Status, state.ProvenanceFooter
	record.DurableSubject, record.ManagedAction = rendered.DurableSubject, rendered.ManagedAction
	record.CapturedRevision, record.CapturedTitle = task.Revision, task.Title
	committed.Tasks[task.TaskID] = record
	plan := state.PendingTitlePlan{
		OperationID: state.TitleOperationID(task.TaskID, task.Revision, task.Title, rendered.Title),
		TaskID:      task.TaskID, ExpectedRevision: task.Revision, ExpectedTitle: task.Title, DesiredTitle: rendered.Title,
		DurableSubject: rendered.DurableSubject, ManagedAction: rendered.ManagedAction,
		ManagedTokenDisplay: rendered.ManagedTokenDisplay, ManagedTokenPosition: rendered.ManagedTokenPosition,
		NativeOutcome: state.NativeTitlePending, ExpectedFooter: envelope.Footer,
	}
	if err := plan.Validate(); err != nil {
		return commandError("invalid_plan", err)
	}
	committed.PendingTitlePlans[task.TaskID] = plan
	committed.Generation++
	if err := s.Store.SaveState(committed); err != nil {
		return commandError("state_write_failed", err)
	}
	return output.TitleBatchResult{Mode: "stage", Plans: []output.TitleBatchItem{}, Dispositions: []output.TitleBatchDisposition{{Outcome: "staged"}}}, nil
}

func (s Service) Report() (output.Result, error) {
	var envelope reportEnvelope
	if err := decodeStrict(s.Input, &envelope); err != nil {
		return commandError("invalid_report", err)
	}
	lock, _, err := s.lockedConfig()
	if err != nil {
		return commandError(codeFor(err), err)
	}
	defer lock.Close()
	if err := refuseCycle(s.Store); err != nil {
		return commandError("cycle_in_progress", err)
	}
	committed, err := s.Store.LoadState()
	if err != nil {
		return commandError("state_read_failed", err)
	}
	result := output.TitleBatchReportResult{AcceptedIDs: []string{}, FailedIDs: []string{}, DriftedIDs: []string{}, RejectedIDs: []string{}}
	counts := map[string]int{}
	for _, report := range envelope.Reports {
		counts[report.OperationID]++
	}
	now := s.now()
	changed := false
	for _, report := range envelope.Reports {
		plan, taskID, ok := findPlan(committed, report.OperationID)
		if !ok || report.OperationID == "" || counts[report.OperationID] != 1 || !validReport(report) {
			if report.OperationID != "" {
				result.RejectedIDs = appendUnique(result.RejectedIDs, report.OperationID)
			}
			continue
		}
		if plan.NativeOutcome == state.NativeTitleSucceeded {
			if report.Outcome == "accepted" {
				result.AcceptedIDs = append(result.AcceptedIDs, report.OperationID)
			} else {
				result.RejectedIDs = appendUnique(result.RejectedIDs, report.OperationID)
			}
			continue
		}
		candidate := plan
		candidate.NativeReportedAt = &now
		switch report.Outcome {
		case "accepted":
			candidate.NativeOutcome = state.NativeTitleSucceeded
			candidate.NativeErrorCode = ""
			candidate.CanonicalAttempts++
			result.AcceptedIDs = append(result.AcceptedIDs, report.OperationID)
		case "failed":
			candidate.NativeOutcome = state.NativeTitleFailed
			candidate.NativeErrorCode = report.ErrorCode
			result.FailedIDs = append(result.FailedIDs, report.OperationID)
		case "drifted":
			candidate.NativeOutcome = state.NativeTitleFailed
			candidate.NativeErrorCode = "native_guard_drift"
			result.DriftedIDs = append(result.DriftedIDs, report.OperationID)
		}
		if err := candidate.Validate(); err != nil {
			result.RejectedIDs = appendUnique(result.RejectedIDs, report.OperationID)
			continue
		}
		committed.PendingTitlePlans[taskID] = candidate
		changed = true
	}
	if changed {
		committed.Generation++
		if err := s.Store.SaveState(committed); err != nil {
			return commandError("state_write_failed", err)
		}
	}
	return result, nil
}

func (s Service) plan(ctx context.Context, operationID string) (output.Result, error) {
	lock, cfg, err := s.lockedConfig()
	if err != nil {
		return commandError(codeFor(err), err)
	}
	defer lock.Close()
	if err := refuseCycle(s.Store); err != nil {
		return commandError("cycle_in_progress", err)
	}
	committed, err := s.Store.LoadState()
	if err != nil {
		return commandError("state_read_failed", err)
	}
	observed, err := s.Inventory.Inventory(ctx, cfg.ControlTaskID)
	if err != nil {
		return commandError("inventory_failed", err)
	}
	if sourcePlan, pending := committed.PendingTitlePlans[cfg.ControlTaskID]; pending && sourcePlan.ExpectedFooter != "" {
		sourceTask, readErr := s.Inventory.Task(ctx, cfg.ControlTaskID)
		if readErr != nil {
			return commandError("inventory_failed", readErr)
		}
		observed.Tasks = append(observed.Tasks, sourceTask)
	}
	mode := "list"
	if operationID != "" {
		mode = "operation"
	}
	result := output.TitleBatchResult{Mode: mode, Plans: []output.TitleBatchItem{}, Dispositions: []output.TitleBatchDisposition{}}
	ids := make([]string, 0, len(committed.PendingTitlePlans))
	for taskID, plan := range committed.PendingTitlePlans {
		if operationID == "" || plan.OperationID == operationID {
			ids = append(ids, taskID)
		}
	}
	sort.Strings(ids)
	changed := false
	now := s.now()
	for _, taskID := range ids {
		plan := committed.PendingTitlePlans[taskID]
		task, exists := findTask(observed, taskID)
		switch {
		case exists && task.Archived:
			delete(committed.PendingTitlePlans, taskID)
			result.Dispositions = append(result.Dispositions, disposition(plan, "archived"))
			changed = true
		case !exists:
			delete(committed.PendingTitlePlans, taskID)
			result.Dispositions = append(result.Dispositions, disposition(plan, "missing"))
			changed = true
		case plan.ExpectedFooter != "" && task.Title == plan.DesiredTitle && plan.NativeOutcome == state.NativeTitleSucceeded:
			if plan.CanonicalCheckedAt == nil {
				plan.CanonicalCheckedAt = &now
				committed.PendingTitlePlans[taskID] = plan
				changed = true
			}
			result.Dispositions = append(result.Dispositions, disposition(plan, "canonical_verified_awaiting_footer"))
		case plan.ExpectedFooter != "" && task.Revision != plan.ExpectedRevision:
			result.Dispositions = append(result.Dispositions, disposition(plan, "awaiting_footer_verification"))
		case task.Title == plan.DesiredTitle && plan.NativeOutcome == state.NativeTitleSucceeded:
			applyCanonical(&committed, plan, task)
			result.Dispositions = append(result.Dispositions, disposition(plan, "canonical_verified"))
			changed = true
		case task.Revision != plan.ExpectedRevision || task.Title != plan.ExpectedTitle:
			delete(committed.PendingTitlePlans, taskID)
			result.Dispositions = append(result.Dispositions, disposition(plan, "drifted"))
			changed = true
		case plan.NativeOutcome == state.NativeTitleSucceeded && plan.NativeReportedAt != nil && now.Before(plan.NativeReportedAt.Add(state.NativeTitleCanonicalTimeout)):
			result.Dispositions = append(result.Dispositions, disposition(plan, "native_succeeded_pending_canonical"))
		case plan.NativeOutcome == state.NativeTitleSucceeded && plan.CanonicalAttempts >= maxCanonicalAttempts:
			result.Dispositions = append(result.Dispositions, disposition(plan, "canonical_verification_failed"))
		case plan.NativeOutcome == state.NativeTitleSucceeded:
			plan.NativeOutcome = state.NativeTitleFailed
			plan.NativeErrorCode = "canonical_not_verified"
			plan.CanonicalCheckedAt = &now
			committed.PendingTitlePlans[taskID] = plan
			result.Plans = append(result.Plans, item(plan))
			changed = true
		default:
			result.Plans = append(result.Plans, item(plan))
		}
	}
	if operationID != "" && len(ids) == 0 {
		result.Dispositions = append(result.Dispositions, output.TitleBatchDisposition{OperationID: operationID, Outcome: "missing"})
	}
	if changed {
		committed.Generation++
		if err := s.Store.SaveState(committed); err != nil {
			return commandError("state_write_failed", err)
		}
	}
	return result, nil
}

func (s Service) lockedConfig() (*state.Lock, config.Config, error) {
	if s.Store == nil || s.Inventory == nil || s.Now == nil || s.SourceIdentity == nil {
		return nil, config.Config{}, errors.New("dependency_unavailable")
	}
	lock, err := s.Store.AcquireLock()
	if err != nil {
		return nil, config.Config{}, err
	}
	cfg, err := s.Store.LoadConfig()
	if err != nil {
		lock.Close()
		return nil, config.Config{}, err
	}
	sourceTaskID := s.SourceIdentity()
	if !canonicalUUID(sourceTaskID) || sourceTaskID != cfg.ControlTaskID {
		lock.Close()
		return nil, config.Config{}, errors.New("source_identity_mismatch")
	}
	if !cfg.AgentsEnabled || !cfg.RenameEnabled {
		lock.Close()
		return nil, config.Config{}, errors.New("title_batch_disabled")
	}
	return lock, cfg, nil
}

func refuseCycle(store Store) error {
	_, err := store.LoadCycle()
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return errors.New("cycle_in_progress")
}

func decodeStrict(input io.Reader, target any) error {
	if input == nil {
		return errors.New("input unavailable")
	}
	decoder := json.NewDecoder(input)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("input must contain one JSON value")
	}
	return nil
}

func validReport(report nativeReport) bool {
	if report.OperationID == "" || strings.TrimSpace(report.OperationID) != report.OperationID {
		return false
	}
	switch report.Outcome {
	case "accepted", "drifted":
		return report.ErrorCode == ""
	case "failed":
		return report.ErrorCode != "" && strings.TrimSpace(report.ErrorCode) == report.ErrorCode
	default:
		return false
	}
}

func item(plan state.PendingTitlePlan) output.TitleBatchItem {
	return output.TitleBatchItem{OperationID: plan.OperationID, TaskID: plan.TaskID, ExpectedRevision: plan.ExpectedRevision, ExpectedTitle: plan.ExpectedTitle, DesiredTitle: plan.DesiredTitle}
}

func disposition(plan state.PendingTitlePlan, outcome string) output.TitleBatchDisposition {
	return output.TitleBatchDisposition{OperationID: plan.OperationID, TaskID: plan.TaskID, Outcome: outcome}
}

func findTask(inventory codex.Inventory, taskID string) (codex.Task, bool) {
	for _, task := range inventory.Tasks {
		if task.TaskID == taskID {
			return task, true
		}
	}
	return codex.Task{}, false
}

func findPlan(committed state.State, operationID string) (state.PendingTitlePlan, string, bool) {
	for taskID, plan := range committed.PendingTitlePlans {
		if plan.OperationID == operationID {
			return plan, taskID, true
		}
	}
	return state.PendingTitlePlan{}, "", false
}

func applyCanonical(committed *state.State, plan state.PendingTitlePlan, task codex.Task) {
	if record, ok := committed.Tasks[plan.TaskID]; ok {
		record.CapturedRevision, record.CapturedTitle, record.LastAppliedTitle = task.Revision, task.Title, task.Title
		record.DurableSubject, record.ManagedAction = plan.DurableSubject, plan.ManagedAction
		record.ManagedTokenDisplay, record.ManagedTokenPosition = plan.ManagedTokenDisplay, plan.ManagedTokenPosition
		committed.Tasks[plan.TaskID] = record
	}
	delete(committed.PendingTitlePlans, plan.TaskID)
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func (s Service) now() time.Time { return s.Now().UTC() }

func canonicalUUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for index, char := range value {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			if char != '-' {
				return false
			}
			continue
		}
		if char < '0' || char > '9' {
			if char < 'a' || char > 'f' {
				return false
			}
		}
	}
	return true
}

func commandError(code string, err error) (output.Result, error) {
	return output.ErrorResult{Operation: "title-batch", ErrorCode: code}, err
}

func codeFor(err error) string {
	switch err.Error() {
	case "source_identity_mismatch":
		return "source_identity_mismatch"
	case "title_batch_disabled":
		return "title_batch_disabled"
	case "dependency_unavailable":
		return "dependency_unavailable"
	default:
		return "title_batch_locked"
	}
}
