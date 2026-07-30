package output

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/ericlitman/threadbear/internal/state"
	"github.com/ericlitman/threadbear/internal/tokens"
)

const CurrentResultVersion = 1

type Format string

const (
	FormatHuman Format = "human"
	FormatJSON  Format = "json"
)

type Result interface {
	Human() string
	Empty() bool
	result()
}

type TitleDispatchResult struct {
	Version     int    `json:"version"`
	Allow       bool   `json:"allow"`
	Disposition string `json:"disposition"`
}

func (TitleDispatchResult) result()     {}
func (TitleDispatchResult) Empty() bool { return false }
func (r TitleDispatchResult) Human() string {
	data, _ := json.Marshal(r)
	return string(data)
}

type titlePlanMode uint8

const (
	titlePlanStage titlePlanMode = iota
	titlePlanBatch
	titlePlanOperation
	titlePlanReport
)

type TitlePlanResult struct {
	Version         int      `json:"version"`
	Ready           bool     `json:"ready"`
	Retryable       bool     `json:"retryable"`
	ErrorCode       string   `json:"error_code,omitempty"`
	ContinuationDue bool     `json:"continuation_due,omitempty"`
	OperationIDs    []string `json:"operation_ids,omitempty"`
	OperationID     string   `json:"operation_id,omitempty"`
	Disposition     string   `json:"disposition,omitempty"`
	Action          string   `json:"action,omitempty"`
	TaskID          string   `json:"task_id,omitempty"`
	DesiredTitle    string   `json:"desired_title,omitempty"`
	Accepted        int      `json:"accepted,omitempty"`
	Unchanged       int      `json:"unchanged,omitempty"`
	mode            titlePlanMode
}

func NewTitlePlanStageResult(ready bool, code string) TitlePlanResult {
	return TitlePlanResult{Ready: ready, Retryable: !ready, ErrorCode: code, mode: titlePlanStage}
}
func NewTitlePlanBatchResult(ready bool, code string, ids []string) TitlePlanResult {
	return TitlePlanResult{Ready: ready, Retryable: !ready, ErrorCode: code, OperationIDs: ids, mode: titlePlanBatch}
}
func NewTitlePlanOperationResult(ready bool, code, id string) TitlePlanResult {
	return TitlePlanResult{Ready: ready, Retryable: !ready, ErrorCode: code, OperationID: id, mode: titlePlanOperation}
}
func NewTitlePlanReportResult(ready bool, code string, accepted, unchanged int) TitlePlanResult {
	return TitlePlanResult{Ready: ready, Retryable: !ready, ErrorCode: code, Accepted: accepted, Unchanged: unchanged, mode: titlePlanReport}
}
func (TitlePlanResult) result()     {}
func (TitlePlanResult) Empty() bool { return false }
func (r TitlePlanResult) Human() string {
	data, _ := json.Marshal(r)
	return string(data)
}

type TaskChange struct {
	TaskID string           `json:"task_id"`
	State  state.TaskStatus `json:"state"`
}

type RetryResult struct {
	TaskID    string `json:"task_id"`
	Operation string `json:"operation"`
	ErrorCode string `json:"error_code"`
}

type HeartbeatResult struct {
	Version          int                  `json:"version"`
	CycleID          string               `json:"cycle_id"`
	Changed          []TaskChange         `json:"changed"`
	ArchivedIDs      []string             `json:"archived_ids"`
	RestoredIDs      []string             `json:"restored_ids"`
	ManagedResources []string             `json:"managed_resources,omitempty"`
	Retries          []RetryResult        `json:"retries"`
	ErrorCode        string               `json:"error_code,omitempty"`
	Progress         *state.SweepProgress `json:"progress,omitempty"`
}

func (HeartbeatResult) result() {}

func (r HeartbeatResult) Empty() bool {
	return len(r.Changed) == 0 && len(r.ArchivedIDs) == 0 && len(r.RestoredIDs) == 0 && len(r.ManagedResources) == 0 && len(r.Retries) == 0 && r.ErrorCode == "" && r.Progress == nil
}

func (r HeartbeatResult) Human() string {
	data, _ := json.Marshal(r.normalized())
	return string(data)
}

func (r HeartbeatResult) normalized() HeartbeatResult {
	if r.Version == 0 {
		r.Version = CurrentResultVersion
	}
	r.Changed = slices.Clone(r.Changed)
	r.ArchivedIDs = slices.Clone(r.ArchivedIDs)
	r.RestoredIDs = slices.Clone(r.RestoredIDs)
	r.ManagedResources = slices.Clone(r.ManagedResources)
	r.Retries = slices.Clone(r.Retries)
	sort.Slice(r.Changed, func(i, j int) bool {
		if r.Changed[i].TaskID == r.Changed[j].TaskID {
			return r.Changed[i].State < r.Changed[j].State
		}
		return r.Changed[i].TaskID < r.Changed[j].TaskID
	})
	slices.Sort(r.ArchivedIDs)
	slices.Sort(r.RestoredIDs)
	slices.Sort(r.ManagedResources)
	sort.Slice(r.Retries, func(i, j int) bool {
		if r.Retries[i].TaskID != r.Retries[j].TaskID {
			return r.Retries[i].TaskID < r.Retries[j].TaskID
		}
		return r.Retries[i].Operation < r.Retries[j].Operation
	})
	if r.Changed == nil {
		r.Changed = []TaskChange{}
	}
	if r.ArchivedIDs == nil {
		r.ArchivedIDs = []string{}
	}
	if r.RestoredIDs == nil {
		r.RestoredIDs = []string{}
	}
	if r.Retries == nil {
		r.Retries = []RetryResult{}
	}
	return r
}

type Preferences struct {
	HeartbeatSeconds             int    `json:"heartbeat_seconds"`
	ArchiveEnabled               bool   `json:"archive_enabled"`
	ArchiveAfterDays             int    `json:"archive_after_days"`
	RenameEnabled                bool   `json:"rename_enabled"`
	AutoUpdateEnabled            bool   `json:"auto_update_enabled"`
	TokenDisplay                 string `json:"token_display"`
	AgentsEnabled                bool   `json:"agents_enabled"`
	ClassifierModel              string `json:"classifier_model"`
	ClassifierEffort             string `json:"classifier_effort"`
	ClassifierContextBudgetBytes int    `json:"classifier_context_budget_bytes"`
}

type StatusResult struct {
	Version                int                  `json:"version"`
	InstalledVersion       string               `json:"installed_version"`
	LaunchAgentHealthy     bool                 `json:"launch_agent_healthy"`
	LaunchAgentStatus      string               `json:"launch_agent_status"`
	LastCompletedHeartbeat *time.Time           `json:"last_completed_heartbeat,omitempty"`
	ControlTaskID          string               `json:"control_task_id"`
	Preferences            Preferences          `json:"preferences"`
	PendingRetries         int                  `json:"pending_retries"`
	LastUpdateCheck        *time.Time           `json:"last_update_check,omitempty"`
	LastUpdateFailure      *state.Failure       `json:"last_update_failure,omitempty"`
	LastReconcileFailure   *state.Failure       `json:"last_reconcile_failure,omitempty"`
	FirstSweep             *state.SweepProgress `json:"first_sweep,omitempty"`
}

func (StatusResult) result()     {}
func (StatusResult) Empty() bool { return false }
func (r StatusResult) Human() string {
	health := r.LaunchAgentStatus
	if health == "" {
		health = "unhealthy"
		if r.LaunchAgentHealthy {
			health = "healthy"
		}
	}
	if health == "unavailable" {
		health = "scheduler adapter unavailable (pending install unit)"
	}
	message := fmt.Sprintf("ThreadBear %s · LaunchAgent %s · heartbeat %s · control task %s · heartbeat interval %ds · archive %t/%dd · rename %t · auto-update %t · token display %s · AGENTS %t · classifier %s/%s/%dB · retries %d · update check %s · update failure %s · reconcile failure %s", r.InstalledVersion, health, formatTime(r.LastCompletedHeartbeat), r.ControlTaskID, r.Preferences.HeartbeatSeconds, r.Preferences.ArchiveEnabled, r.Preferences.ArchiveAfterDays, r.Preferences.RenameEnabled, r.Preferences.AutoUpdateEnabled, r.Preferences.TokenDisplay, r.Preferences.AgentsEnabled, r.Preferences.ClassifierModel, r.Preferences.ClassifierEffort, r.Preferences.ClassifierContextBudgetBytes, r.PendingRetries, formatTime(r.LastUpdateCheck), formatFailure(r.LastUpdateFailure), formatFailure(r.LastReconcileFailure))
	if r.FirstSweep != nil {
		message += fmt.Sprintf(" · first sweep %s · deterministic %d/%d · Luna %d · batches %d/%d+%d/%d", r.FirstSweep.Phase, r.FirstSweep.MechanicallyResolved, r.FirstSweep.ChangedTasks, r.FirstSweep.LunaCandidates, r.FirstSweep.FirstPassBatchesCompleted, r.FirstSweep.FirstPassBatchesTotal, r.FirstSweep.PreviousPassBatchesCompleted, r.FirstSweep.PreviousPassBatchesTotal)
	}
	return message
}

func formatFailure(failure *state.Failure) string {
	if failure == nil {
		return "none"
	}
	return fmt.Sprintf("%s@%s", failure.Code, failure.Timestamp.UTC().Format(time.RFC3339))
}

type InspectResult struct {
	Version              int              `json:"version"`
	TaskID               string           `json:"task_id"`
	CapturedRevision     string           `json:"captured_revision"`
	State                state.TaskStatus `json:"state"`
	Provenance           state.Provenance `json:"provenance"`
	ManagedAction        string           `json:"managed_action,omitempty"`
	Retry                *RetryResult     `json:"retry,omitempty"`
	ArchiveEligible      bool             `json:"archive_eligible"`
	TokenDisplayPosition tokens.Position  `json:"token_display_position"`
	ManagedTokenPosition tokens.Position  `json:"managed_token_position"`
	ManagedTokenDisplay  string           `json:"managed_token_display"`
	TokenUsageFound      bool             `json:"token_usage_found"`
}

func (InspectResult) result()     {}
func (InspectResult) Empty() bool { return false }
func (r InspectResult) Human() string {
	r = r.normalized()
	action := r.ManagedAction
	if action == "" {
		action = "none"
	}
	display := r.ManagedTokenDisplay
	if display == "" {
		display = "none"
	}
	retry := "none"
	if r.Retry != nil {
		retry = fmt.Sprintf("%s/%s", r.Retry.Operation, r.Retry.ErrorCode)
	}
	return fmt.Sprintf("%s %s · revision %s · provenance %s · next: %s · token configured %s · token applied %s/%s · token usage found %t · retry %s · archive eligible %t", r.State.Emoji(), r.TaskID, r.CapturedRevision, r.Provenance, action, r.TokenDisplayPosition, r.ManagedTokenPosition, display, r.TokenUsageFound, retry, r.ArchiveEligible)
}

func (r InspectResult) normalized() InspectResult {
	if r.Version == 0 {
		r.Version = CurrentResultVersion
	}
	if r.TokenDisplayPosition == "" {
		r.TokenDisplayPosition = tokens.PositionOff
	}
	if r.ManagedTokenPosition == "" || r.ManagedTokenDisplay == "" {
		r.ManagedTokenPosition = tokens.PositionOff
		r.ManagedTokenDisplay = ""
	}
	return r
}

type PreviewResult struct {
	Version                  int      `json:"version"`
	Command                  string   `json:"command"`
	Effects                  []string `json:"effects"`
	Details                  []string `json:"details"`
	ControlTaskID            string   `json:"control_task_id,omitempty"`
	SuppliedControlTaskID    string   `json:"supplied_control_task_id,omitempty"`
	ControlTaskDisposition   string   `json:"control_task_disposition,omitempty"`
	WillUnarchiveControlTask bool     `json:"will_unarchive_control_task,omitempty"`
}

func (PreviewResult) result()     {}
func (PreviewResult) Empty() bool { return false }
func (r PreviewResult) Human() string {
	line := fmt.Sprintf("ThreadBear preview for %s: %s · %s", r.Command, strings.Join(r.Effects, ", "), strings.Join(r.Details, " · "))
	if r.ControlTaskDisposition != "" {
		line += fmt.Sprintf(" · control task %s=%s", r.ControlTaskID, r.ControlTaskDisposition)
	}
	if r.ControlTaskDisposition == "stayed_home" {
		line += " · ThreadBear stayed home"
	}
	if r.SuppliedControlTaskID != "" {
		line += " · supplied control task " + r.SuppliedControlTaskID
	}
	if r.WillUnarchiveControlTask {
		line += " · persisted control task will be unarchived"
	}
	return line
}

type ActionResult struct {
	Version     int      `json:"version"`
	Command     string   `json:"command"`
	Changed     bool     `json:"changed"`
	ResourceIDs []string `json:"resource_ids"`
	Preview     []string `json:"preview"`
}

func (ActionResult) result()     {}
func (ActionResult) Empty() bool { return false }
func (r ActionResult) Human() string {
	outcome := "already settled"
	if r.Changed {
		outcome = "updated"
	}
	resources := "none"
	if len(r.ResourceIDs) > 0 {
		resources = strings.Join(r.ResourceIDs, ",")
	}
	return fmt.Sprintf("ThreadBear %s outcome: %s · resources %s", r.Command, outcome, resources)
}

type LifecycleResult struct {
	Version                int      `json:"version"`
	Command                string   `json:"command"`
	Changed                bool     `json:"changed"`
	Resources              []string `json:"resources"`
	ControlTaskID          string   `json:"control_task_id"`
	Migrated               bool     `json:"migrated"`
	Warnings               []string `json:"warnings,omitempty"`
	Reinstalled            bool     `json:"reinstalled"`
	ControlTaskDisposition string   `json:"control_task_disposition,omitempty"`
	SuppliedControlTaskID  string   `json:"supplied_control_task_id,omitempty"`
	Unarchived             bool     `json:"unarchived"`
	ArchivedControlTask    bool     `json:"archived_control_task"`
	DeletedState           bool     `json:"deleted_state"`
	CleanedTitles          int      `json:"cleaned_titles"`
	Preview                []string `json:"preview"`
}

func (LifecycleResult) result()     {}
func (LifecycleResult) Empty() bool { return false }
func (r LifecycleResult) Human() string {
	if r.Command == "uninstall" {
		if r.CleanedTitles > 0 {
			return fmt.Sprintf("ThreadBear cleaned %d managed task title(s) and is uninstalled. Take care out there.", r.CleanedTitles)
		}
		return "ThreadBear is uninstalled. Take care out there."
	}
	resources := "none"
	if len(r.Resources) > 0 {
		resources = strings.Join(r.Resources, ",")
	}
	line := fmt.Sprintf("ThreadBear %s changed=%t · resources %s · control task %s=%s · reinstalled=%t · unarchived=%t · archived control task=%t · deleted state=%t", r.Command, r.Changed, resources, r.ControlTaskID, r.ControlTaskDisposition, r.Reinstalled, r.Unarchived, r.ArchivedControlTask, r.DeletedState)
	if r.ControlTaskDisposition == "stayed_home" {
		line = fmt.Sprintf("ThreadBear stayed home at persisted control task %s instead of supplied control task %s · changed=%t · resources %s · reinstalled=%t · unarchived=%t", r.ControlTaskID, r.SuppliedControlTaskID, r.Changed, resources, r.Reinstalled, r.Unarchived)
	} else if r.SuppliedControlTaskID != "" {
		line += " · supplied control task " + r.SuppliedControlTaskID
	}
	for _, warning := range r.Warnings {
		line += " · warning: " + warning
	}
	return line
}

type CheckResult struct {
	Name      string `json:"name"`
	OK        bool   `json:"ok"`
	ErrorCode string `json:"error_code,omitempty"`
	Remedy    string `json:"remedy,omitempty"`
}

type SelfTestResult struct {
	Version int           `json:"version"`
	OK      bool          `json:"ok"`
	Checks  []CheckResult `json:"checks"`
}

func (SelfTestResult) result()     {}
func (SelfTestResult) Empty() bool { return false }
func (r SelfTestResult) Human() string {
	status := "attention needed"
	if r.OK {
		status = "all paws accounted for"
	}
	checks := make([]string, 0, len(r.Checks))
	for _, check := range r.Checks {
		result := "ok"
		if !check.OK {
			result = check.ErrorCode
			if check.Remedy != "" {
				result += " (" + check.Remedy + ")"
			}
		}
		checks = append(checks, check.Name+"="+result)
	}
	if len(checks) == 0 {
		return "ThreadBear self-test: " + status
	}
	return "ThreadBear self-test: " + status + " · " + strings.Join(checks, ",")
}

type UpdateResult struct {
	Version          int      `json:"version"`
	Changed          bool     `json:"changed"`
	PreviousVersion  string   `json:"previous_version"`
	InstalledVersion string   `json:"installed_version"`
	Resources        []string `json:"resources,omitempty"`
	Warnings         []string `json:"warnings,omitempty"`
}

func (UpdateResult) result()     {}
func (UpdateResult) Empty() bool { return false }
func (r UpdateResult) Human() string {
	if !r.Changed {
		return fmt.Sprintf("ThreadBear is already at %s", r.InstalledVersion)
	}
	message := fmt.Sprintf("ThreadBear updated %s → %s", r.PreviousVersion, r.InstalledVersion)
	if len(r.Resources) > 0 {
		message += " · resources " + strings.Join(r.Resources, ",")
	}
	if len(r.Warnings) > 0 {
		message += " · warning " + strings.Join(r.Warnings, " · ")
	}
	return message
}

type VersionResult struct {
	Version          int    `json:"version"`
	Product          string `json:"product"`
	InstalledVersion string `json:"installed_version"`
	Website          string `json:"website"`
}

func (VersionResult) result()     {}
func (VersionResult) Empty() bool { return false }
func (r VersionResult) Human() string {
	return fmt.Sprintf("%s %s · %s", r.Product, r.InstalledVersion, r.Website)
}

type ErrorResult struct {
	Version   int    `json:"version"`
	Operation string `json:"operation"`
	ErrorCode string `json:"error_code"`
	Step      string `json:"step,omitempty"`
	Cause     string `json:"cause,omitempty"`
}

func (ErrorResult) result()     {}
func (ErrorResult) Empty() bool { return false }
func (r ErrorResult) Human() string {
	if r.Operation == "status" && r.ErrorCode == "not_installed" {
		return "ThreadBear is not installed"
	}
	if r.Operation == "install" && r.ErrorCode == "control_task_id_required" {
		return r.Cause
	}
	if r.Step != "" {
		return fmt.Sprintf("ThreadBear couldn't %s (%s) · failed step %s · %s", r.Operation, r.ErrorCode, r.Step, r.Cause)
	}
	return fmt.Sprintf("ThreadBear couldn't %s (%s)", r.Operation, r.ErrorCode)
}

func Write(writer io.Writer, format Format, value Result) error {
	if value == nil {
		return errors.New("result is required")
	}
	var err error
	value, err = dereferenceResult(value)
	if err != nil {
		return err
	}
	if err := validateResult(value); err != nil {
		return err
	}
	if value.Empty() {
		return nil
	}
	value = withVersion(value)
	if _, ok := value.(HeartbeatResult); ok {
		format = FormatJSON
	}
	switch format {
	case FormatHuman:
		_, err := io.WriteString(writer, value.Human()+"\n")
		return err
	case FormatJSON:
		data, err := json.Marshal(withVersion(value))
		if err != nil {
			return err
		}
		data = append(data, '\n')
		_, err = writer.Write(data)
		return err
	default:
		return fmt.Errorf("unknown output format %q", format)
	}
}

func dereferenceResult(value Result) (Result, error) {
	switch result := value.(type) {
	case *TitleDispatchResult:
		if result == nil {
			return nil, errors.New("result is required")
		}
		return *result, nil
	case *TitlePlanResult:
		if result == nil {
			return nil, errors.New("result is required")
		}
		return *result, nil
	case *HeartbeatResult:
		if result == nil {
			return nil, errors.New("result is required")
		}
		return *result, nil
	case *StatusResult:
		if result == nil {
			return nil, errors.New("result is required")
		}
		return *result, nil
	case *InspectResult:
		if result == nil {
			return nil, errors.New("result is required")
		}
		return *result, nil
	case *PreviewResult:
		if result == nil {
			return nil, errors.New("result is required")
		}
		return *result, nil
	case *ActionResult:
		if result == nil {
			return nil, errors.New("result is required")
		}
		return *result, nil
	case *LifecycleResult:
		if result == nil {
			return nil, errors.New("result is required")
		}
		return *result, nil
	case *SelfTestResult:
		if result == nil {
			return nil, errors.New("result is required")
		}
		return *result, nil
	case *UpdateResult:
		if result == nil {
			return nil, errors.New("result is required")
		}
		return *result, nil
	case *VersionResult:
		if result == nil {
			return nil, errors.New("result is required")
		}
		return *result, nil
	case *ErrorResult:
		if result == nil {
			return nil, errors.New("result is required")
		}
		return *result, nil
	default:
		return value, nil
	}
}

func withVersion(value Result) Result {
	switch result := value.(type) {
	case TitleDispatchResult:
		if result.Version == 0 {
			result.Version = CurrentResultVersion
		}
		return result
	case TitlePlanResult:
		if result.mode == titlePlanBatch {
			result.OperationIDs = slices.Clone(result.OperationIDs)
			if result.OperationIDs == nil {
				result.OperationIDs = []string{}
			}
		}
		if result.Version == 0 {
			result.Version = CurrentResultVersion
		}
		return result
	case HeartbeatResult:
		return result.normalized()
	case StatusResult:
		if result.Version == 0 {
			result.Version = CurrentResultVersion
		}
		if result.LaunchAgentStatus == "" {
			result.LaunchAgentStatus = "unhealthy"
			if result.LaunchAgentHealthy {
				result.LaunchAgentStatus = "healthy"
			}
		}
		return result
	case InspectResult:
		return result.normalized()
	case PreviewResult:
		if result.Version == 0 {
			result.Version = CurrentResultVersion
		}
		result.Effects = slices.Clone(result.Effects)
		result.Details = slices.Clone(result.Details)
		slices.Sort(result.Effects)
		if result.Effects == nil {
			result.Effects = []string{}
		}
		if result.Details == nil {
			result.Details = []string{}
		}
		return result
	case ActionResult:
		if result.Version == 0 {
			result.Version = CurrentResultVersion
		}
		result.ResourceIDs = slices.Clone(result.ResourceIDs)
		result.Preview = slices.Clone(result.Preview)
		slices.Sort(result.ResourceIDs)
		if result.ResourceIDs == nil {
			result.ResourceIDs = []string{}
		}
		if result.Preview == nil {
			result.Preview = []string{}
		}
		return result
	case LifecycleResult:
		if result.Version == 0 {
			result.Version = CurrentResultVersion
		}
		result.Resources = slices.Clone(result.Resources)
		result.Preview = slices.Clone(result.Preview)
		slices.Sort(result.Resources)
		if result.Resources == nil {
			result.Resources = []string{}
		}
		if result.Preview == nil {
			result.Preview = []string{}
		}
		return result
	case SelfTestResult:
		if result.Version == 0 {
			result.Version = CurrentResultVersion
		}
		result.Checks = slices.Clone(result.Checks)
		sort.Slice(result.Checks, func(i, j int) bool { return result.Checks[i].Name < result.Checks[j].Name })
		if result.Checks == nil {
			result.Checks = []CheckResult{}
		}
		return result
	case UpdateResult:
		if result.Version == 0 {
			result.Version = CurrentResultVersion
		}
		result.Resources = slices.Clone(result.Resources)
		result.Warnings = slices.Clone(result.Warnings)
		slices.Sort(result.Resources)
		return result
	case VersionResult:
		if result.Version == 0 {
			result.Version = CurrentResultVersion
		}
		return result
	case ErrorResult:
		if result.Version == 0 {
			result.Version = CurrentResultVersion
		}
		return result
	default:
		return value
	}
}

func formatTime(value *time.Time) string {
	if value == nil {
		return "never"
	}
	return value.UTC().Format(time.RFC3339)
}

func validateResult(value Result) error {
	version := resultVersion(value)
	if version != 0 && version != CurrentResultVersion {
		return fmt.Errorf("unsupported result version %d", version)
	}
	checkCode := func(field, code string, optional bool) error {
		if code == "" && optional {
			return nil
		}
		if !validCode(code) {
			return fmt.Errorf("%s must be a stable machine code", field)
		}
		return nil
	}
	checkID := func(field, value string) error {
		if value == "" || strings.TrimSpace(value) != value {
			return fmt.Errorf("%s must be a canonical identifier", field)
		}
		return nil
	}
	checkFailure := func(field string, failure *state.Failure) error {
		if failure == nil {
			return nil
		}
		if err := checkCode(field+" code", failure.Code, false); err != nil {
			return err
		}
		if failure.Timestamp.IsZero() {
			return fmt.Errorf("%s timestamp is required", field)
		}
		return nil
	}
	switch result := value.(type) {
	case TitleDispatchResult:
		if result.Allow || result.Disposition != "retired" {
			return errors.New("title dispatch compatibility envelope is invalid")
		}
	case TitlePlanResult:
		if result.Ready == result.Retryable || result.Retryable != (result.ErrorCode != "") {
			return errors.New("title plan readiness is inconsistent")
		}
		if err := checkCode("error_code", result.ErrorCode, result.Ready); err != nil {
			return err
		}
		operationPayload := result.OperationID != "" || result.Disposition != "" || result.Action != "" || result.TaskID != "" || result.DesiredTitle != ""
		if result.mode != titlePlanBatch && len(result.OperationIDs) > 0 || result.mode != titlePlanOperation && operationPayload || result.mode != titlePlanReport && (result.Accepted != 0 || result.Unchanged != 0) {
			return errors.New("title plan result leaks payload")
		}
		switch result.mode {
		case titlePlanStage:
		case titlePlanBatch:
			for _, id := range result.OperationIDs {
				if err := checkID("operation_id", id); err != nil {
					return err
				}
			}
		case titlePlanOperation:
			if err := checkID("operation_id", result.OperationID); err != nil {
				return err
			}
			if result.Retryable {
				break
			}
			if result.Disposition == "ready" {
				if result.Action != "set" && result.Action != "report_success" || result.TaskID == "" || result.DesiredTitle == "" {
					return errors.New("ready title operation is incomplete")
				}
			} else if result.Disposition != "drifted" && result.Disposition != "missing" && result.Disposition != "rejected" {
				return errors.New("title operation disposition is invalid")
			} else if result.Action != "" || result.TaskID != "" || result.DesiredTitle != "" {
				return errors.New("title operation disposition leaks payload")
			}
		case titlePlanReport:
			if result.Accepted < 0 || result.Unchanged < 0 {
				return errors.New("title report counts must be nonnegative")
			}
		default:
			return errors.New("title plan result mode is invalid")
		}
	case HeartbeatResult:
		if result.Progress != nil {
			if err := result.Progress.Validate(); err != nil {
				return err
			}
		}
		if !result.Empty() && result.CycleID == "" {
			return errors.New("changed heartbeat requires cycle_id")
		}
		if err := checkCode("error_code", result.ErrorCode, true); err != nil {
			return err
		}
		for _, change := range result.Changed {
			if err := checkID("changed task_id", change.TaskID); err != nil {
				return err
			}
			if !change.State.Valid() {
				return fmt.Errorf("changed task %s has invalid state %q", change.TaskID, change.State)
			}
		}
		for _, taskID := range append(slices.Clone(result.ArchivedIDs), result.RestoredIDs...) {
			if err := checkID("archive task_id", taskID); err != nil {
				return err
			}
		}
		for _, resource := range result.ManagedResources {
			if err := checkID("managed resource", resource); err != nil {
				return err
			}
		}
		for _, retry := range result.Retries {
			if err := checkID("retry task_id", retry.TaskID); err != nil {
				return err
			}
			if err := checkCode("retry operation", retry.Operation, false); err != nil {
				return err
			}
			if err := checkCode("retry error_code", retry.ErrorCode, false); err != nil {
				return err
			}
		}
	case StatusResult:
		if result.FirstSweep != nil {
			if err := result.FirstSweep.Validate(); err != nil {
				return err
			}
		}
		if result.LaunchAgentStatus != "" && result.LaunchAgentStatus != "healthy" && result.LaunchAgentStatus != "unhealthy" && result.LaunchAgentStatus != "unavailable" {
			return errors.New("status result has invalid launch_agent_status")
		}
		if result.InstalledVersion == "" || result.ControlTaskID == "" || result.Preferences.HeartbeatSeconds <= 0 || result.Preferences.ArchiveAfterDays <= 0 || result.Preferences.ClassifierModel == "" || result.Preferences.ClassifierEffort == "" || result.Preferences.ClassifierContextBudgetBytes <= 0 || result.PendingRetries < 0 {
			return errors.New("status result is incomplete")
		}
		if err := checkFailure("last_update_failure", result.LastUpdateFailure); err != nil {
			return err
		}
		if err := checkFailure("last_reconcile_failure", result.LastReconcileFailure); err != nil {
			return err
		}
		return checkID("control_task_id", result.ControlTaskID)
	case InspectResult:
		if err := checkID("task_id", result.TaskID); err != nil {
			return err
		}
		if err := checkID("captured_revision", result.CapturedRevision); err != nil {
			return err
		}
		if !result.State.Valid() || !result.Provenance.Valid() {
			return errors.New("inspect result has invalid state or provenance")
		}
		if result.TokenDisplayPosition != "" && !result.TokenDisplayPosition.Valid() {
			return errors.New("inspect result has invalid token display position")
		}
		if result.ManagedTokenDisplay == "" {
			if result.ManagedTokenPosition != "" && result.ManagedTokenPosition != tokens.PositionOff {
				return errors.New("inspect result has a managed token position without a display")
			}
		} else if result.ManagedTokenPosition != tokens.PositionStart && result.ManagedTokenPosition != tokens.PositionEnd {
			return errors.New("inspect result has a display without a managed token position")
		}
		if result.Retry != nil {
			if err := checkID("retry task_id", result.Retry.TaskID); err != nil {
				return err
			}
			if result.Retry.TaskID != result.TaskID {
				return errors.New("inspect retry task_id does not match task_id")
			}
			if err := checkCode("retry operation", result.Retry.Operation, false); err != nil {
				return err
			}
			if err := checkCode("retry error_code", result.Retry.ErrorCode, false); err != nil {
				return err
			}
		}
	case PreviewResult:
		if err := checkCode("command", result.Command, false); err != nil {
			return err
		}
		if result.ControlTaskDisposition != "" {
			if err := checkCode("control_task_disposition", result.ControlTaskDisposition, false); err != nil {
				return err
			}
			if err := checkID("control_task_id", result.ControlTaskID); err != nil {
				return err
			}
		}
		if result.SuppliedControlTaskID != "" {
			if err := checkID("supplied_control_task_id", result.SuppliedControlTaskID); err != nil {
				return err
			}
		}
		for _, effect := range result.Effects {
			if err := checkCode("effect", effect, false); err != nil {
				return err
			}
		}
	case ActionResult:
		if err := checkCode("command", result.Command, false); err != nil {
			return err
		}
		for _, resourceID := range result.ResourceIDs {
			if err := checkID("resource_id", resourceID); err != nil {
				return err
			}
		}
	case LifecycleResult:
		if err := checkCode("command", result.Command, false); err != nil {
			return err
		}
		for _, resource := range result.Resources {
			if err := checkID("resource", resource); err != nil {
				return err
			}
		}
		if result.ControlTaskID != "" {
			if err := checkID("control_task_id", result.ControlTaskID); err != nil {
				return err
			}
		}
		if result.ControlTaskDisposition != "" {
			if err := checkCode("control_task_disposition", result.ControlTaskDisposition, false); err != nil {
				return err
			}
		}
		if result.SuppliedControlTaskID != "" {
			return checkID("supplied_control_task_id", result.SuppliedControlTaskID)
		}
	case SelfTestResult:
		if len(result.Checks) == 0 {
			return errors.New("self-test result requires checks")
		}
		allOK := true
		for _, check := range result.Checks {
			if err := checkCode("check name", check.Name, false); err != nil {
				return err
			}
			if check.OK && (check.ErrorCode != "" || check.Remedy != "") {
				return errors.New("successful check must not have an error_code or remedy")
			}
			if strings.TrimSpace(check.Remedy) != check.Remedy {
				return errors.New("check remedy must not have surrounding whitespace")
			}
			if err := checkCode("check error_code", check.ErrorCode, check.OK); err != nil {
				return err
			}
			allOK = allOK && check.OK
		}
		if result.OK != allOK {
			return errors.New("self-test summary contradicts checks")
		}
	case UpdateResult:
		if result.InstalledVersion == "" || result.PreviousVersion == "" {
			return errors.New("update result is incomplete")
		}
		for _, resource := range result.Resources {
			if err := checkID("resource", resource); err != nil {
				return err
			}
		}
	case VersionResult:
		if result.Product == "" || result.InstalledVersion == "" || result.Website == "" {
			return errors.New("version result is incomplete")
		}
	case ErrorResult:
		if err := checkCode("operation", result.Operation, false); err != nil {
			return err
		}
		if err := checkCode("error_code", result.ErrorCode, false); err != nil {
			return err
		}
		if (result.Step == "") != (result.Cause == "") {
			return errors.New("error result step and cause must be provided together")
		}
		if result.Step != "" {
			if err := checkCode("step", result.Step, false); err != nil {
				return err
			}
			if strings.TrimSpace(result.Cause) != result.Cause || result.Cause == "" {
				return errors.New("error result cause must be nonempty without surrounding whitespace")
			}
		}
		return nil
	}
	return nil
}

func resultVersion(value Result) int {
	switch result := value.(type) {
	case TitleDispatchResult:
		return result.Version
	case TitlePlanResult:
		return result.Version
	case HeartbeatResult:
		return result.Version
	case StatusResult:
		return result.Version
	case InspectResult:
		return result.Version
	case PreviewResult:
		return result.Version
	case ActionResult:
		return result.Version
	case LifecycleResult:
		return result.Version
	case SelfTestResult:
		return result.Version
	case UpdateResult:
		return result.Version
	case VersionResult:
		return result.Version
	case ErrorResult:
		return result.Version
	default:
		return 0
	}
}

func validCode(value string) bool {
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
