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

type TitleDispatchTarget struct {
	Type          string `json:"type"`
	DirectoryName string `json:"directoryName"`
}
type TitleDispatchChild struct {
	Model    string              `json:"model"`
	Thinking string              `json:"thinking"`
	Target   TitleDispatchTarget `json:"target"`
	Prompt   string              `json:"prompt"`
}
type TitleDispatchResult struct {
	Version     int                 `json:"version"`
	Allow       bool                `json:"allow"`
	Disposition string              `json:"disposition"`
	Child       *TitleDispatchChild `json:"child,omitempty"`
}

func (TitleDispatchResult) result()     {}
func (TitleDispatchResult) Empty() bool { return false }
func (r TitleDispatchResult) Human() string {
	data, _ := json.Marshal(r)
	return string(data)
}

type TitleActuatorResult struct {
	Version int    `json:"version"`
	Program string `json:"program"`
}

func (TitleActuatorResult) result()     {}
func (TitleActuatorResult) Empty() bool { return false }
func (r TitleActuatorResult) Human() string {
	data, _ := json.Marshal(r)
	return string(data)
}

type TitlePlanItem struct {
	OperationID      string `json:"operation_id"`
	TaskID           string `json:"task_id"`
	ExpectedRevision string `json:"expected_revision"`
	ExpectedTitle    string `json:"expected_title"`
	DesiredTitle     string `json:"desired_title"`
}
type TitlePlanDisposition struct {
	TaskID  string `json:"task_id"`
	Outcome string `json:"outcome"`
}
type TitlePlanResult struct {
	Version      int                    `json:"version"`
	Mode         string                 `json:"mode"`
	Plans        []TitlePlanItem        `json:"plans"`
	Dispositions []TitlePlanDisposition `json:"dispositions"`
}

func (TitlePlanResult) result()     {}
func (TitlePlanResult) Empty() bool { return false }
func (r TitlePlanResult) Human() string {
	data, _ := json.Marshal(r.normalized())
	return string(data)
}
func (r TitlePlanResult) normalized() TitlePlanResult {
	if r.Version == 0 {
		r.Version = CurrentResultVersion
	}
	r.Plans = slices.Clone(r.Plans)
	r.Dispositions = slices.Clone(r.Dispositions)
	sort.Slice(r.Plans, func(i, j int) bool {
		if r.Plans[i].TaskID != r.Plans[j].TaskID {
			return r.Plans[i].TaskID < r.Plans[j].TaskID
		}
		return r.Plans[i].OperationID < r.Plans[j].OperationID
	})
	sort.Slice(r.Dispositions, func(i, j int) bool {
		if r.Dispositions[i].TaskID != r.Dispositions[j].TaskID {
			return r.Dispositions[i].TaskID < r.Dispositions[j].TaskID
		}
		return r.Dispositions[i].Outcome < r.Dispositions[j].Outcome
	})
	if r.Plans == nil {
		r.Plans = []TitlePlanItem{}
	}
	if r.Dispositions == nil {
		r.Dispositions = []TitlePlanDisposition{}
	}
	return r
}

type TitleReportResult struct {
	Version     int      `json:"version"`
	AcceptedIDs []string `json:"accepted_ids"`
	RejectedIDs []string `json:"rejected_ids"`
}

func (TitleReportResult) result()     {}
func (TitleReportResult) Empty() bool { return false }
func (r TitleReportResult) Human() string {
	data, _ := json.Marshal(r.normalized())
	return string(data)
}
func (r TitleReportResult) normalized() TitleReportResult {
	if r.Version == 0 {
		r.Version = CurrentResultVersion
	}
	r.AcceptedIDs = slices.Clone(r.AcceptedIDs)
	r.RejectedIDs = slices.Clone(r.RejectedIDs)
	slices.Sort(r.AcceptedIDs)
	slices.Sort(r.RejectedIDs)
	if r.AcceptedIDs == nil {
		r.AcceptedIDs = []string{}
	}
	if r.RejectedIDs == nil {
		r.RejectedIDs = []string{}
	}
	return r
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
	Version          int           `json:"version"`
	CycleID          string        `json:"cycle_id"`
	Changed          []TaskChange  `json:"changed"`
	ArchivedIDs      []string      `json:"archived_ids"`
	RestoredIDs      []string      `json:"restored_ids"`
	ManagedResources []string      `json:"managed_resources,omitempty"`
	Retries          []RetryResult `json:"retries"`
	ErrorCode        string        `json:"error_code,omitempty"`
}

func (HeartbeatResult) result() {}

func (r HeartbeatResult) Empty() bool {
	return len(r.Changed) == 0 && len(r.ArchivedIDs) == 0 && len(r.RestoredIDs) == 0 && len(r.ManagedResources) == 0 && len(r.Retries) == 0 && r.ErrorCode == ""
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
	Version                int            `json:"version"`
	InstalledVersion       string         `json:"installed_version"`
	LaunchAgentHealthy     bool           `json:"launch_agent_healthy"`
	LaunchAgentStatus      string         `json:"launch_agent_status"`
	LastCompletedHeartbeat *time.Time     `json:"last_completed_heartbeat,omitempty"`
	ControlTaskID          string         `json:"control_task_id"`
	Preferences            Preferences    `json:"preferences"`
	PendingRetries         int            `json:"pending_retries"`
	PendingTitlePlans      int            `json:"pending_title_plans"`
	NativeTitleSuccesses   int            `json:"native_title_successes"`
	LastUpdateCheck        *time.Time     `json:"last_update_check,omitempty"`
	LastUpdateFailure      *state.Failure `json:"last_update_failure,omitempty"`
	LastReconcileFailure   *state.Failure `json:"last_reconcile_failure,omitempty"`
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
	return fmt.Sprintf("ThreadBear %s · LaunchAgent %s · heartbeat %s · control task %s · heartbeat interval %ds · archive %t/%dd · rename %t · auto-update %t · token display %s · AGENTS %t · classifier %s/%s/%dB · retries %d · title plans %d (%d native-success pending canonical persistence) · update check %s · update failure %s · reconcile failure %s", r.InstalledVersion, health, formatTime(r.LastCompletedHeartbeat), r.ControlTaskID, r.Preferences.HeartbeatSeconds, r.Preferences.ArchiveEnabled, r.Preferences.ArchiveAfterDays, r.Preferences.RenameEnabled, r.Preferences.AutoUpdateEnabled, r.Preferences.TokenDisplay, r.Preferences.AgentsEnabled, r.Preferences.ClassifierModel, r.Preferences.ClassifierEffort, r.Preferences.ClassifierContextBudgetBytes, r.PendingRetries, r.PendingTitlePlans, r.NativeTitleSuccesses, formatTime(r.LastUpdateCheck), formatFailure(r.LastUpdateFailure), formatFailure(r.LastReconcileFailure))
}

func formatFailure(failure *state.Failure) string {
	if failure == nil {
		return "none"
	}
	return fmt.Sprintf("%s@%s", failure.Code, failure.Timestamp.UTC().Format(time.RFC3339))
}

type InspectResult struct {
	Version              int                      `json:"version"`
	TaskID               string                   `json:"task_id"`
	CapturedRevision     string                   `json:"captured_revision"`
	State                state.TaskStatus         `json:"state"`
	Provenance           state.Provenance         `json:"provenance"`
	ManagedAction        string                   `json:"managed_action,omitempty"`
	Retry                *RetryResult             `json:"retry,omitempty"`
	ArchiveEligible      bool                     `json:"archive_eligible"`
	TokenDisplayPosition tokens.Position          `json:"token_display_position"`
	ManagedTokenPosition tokens.Position          `json:"managed_token_position"`
	ManagedTokenDisplay  string                   `json:"managed_token_display"`
	TokenUsageFound      bool                     `json:"token_usage_found"`
	PendingTitlePlan     bool                     `json:"pending_title_plan"`
	NativeTitleOutcome   state.NativeTitleOutcome `json:"native_title_outcome,omitempty"`
	CanonicalPersistence string                   `json:"canonical_persistence,omitempty"`
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
	return fmt.Sprintf("%s %s · revision %s · provenance %s · next: %s · token configured %s · token applied %s/%s · token usage found %t · retry %s · title pending %t/%s/%s · archive eligible %t", r.State.Emoji(), r.TaskID, r.CapturedRevision, r.Provenance, action, r.TokenDisplayPosition, r.ManagedTokenPosition, display, r.TokenUsageFound, retry, r.PendingTitlePlan, r.NativeTitleOutcome, r.CanonicalPersistence, r.ArchiveEligible)
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
	Preview                []string `json:"preview"`
}

func (LifecycleResult) result()     {}
func (LifecycleResult) Empty() bool { return false }
func (r LifecycleResult) Human() string {
	if r.Command == "uninstall" {
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
	case *TitleActuatorResult:
		if result == nil {
			return nil, errors.New("result is required")
		}
		return *result, nil
	case *TitlePlanResult:
		if result == nil {
			return nil, errors.New("result is required")
		}
		return *result, nil
	case *TitleReportResult:
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
	case TitleActuatorResult:
		if result.Version == 0 {
			result.Version = CurrentResultVersion
		}
		return result
	case HeartbeatResult:
		return result.normalized()
	case TitlePlanResult:
		return result.normalized()
	case TitleReportResult:
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
		if result.Allow {
			if result.Disposition != "dispatch" || result.Child == nil || result.Child.Model != "gpt-5.6-luna" || result.Child.Thinking != "medium" || result.Child.Target.Type != "projectless" || result.Child.Target.DirectoryName != "threadbear-title-actuator" || len([]byte(result.Child.Prompt)) > 6000 || !strings.HasPrefix(result.Child.Prompt, "THREADBEAR_TITLE_ACTUATOR_V1\n") {
				return errors.New("title dispatch allow envelope is invalid")
			}
		} else if result.Child != nil || !validTitleDispatchDisposition(result.Disposition) {
			return errors.New("title dispatch no-op envelope is invalid")
		}
	case TitleActuatorResult:
		if result.Program == "" {
			return errors.New("title actuator program is required")
		}
	case HeartbeatResult:
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
	case TitlePlanResult:
		if result.Mode != "wait" && result.Mode != "batch" && result.Mode != "operation" {
			return errors.New("title-plan result has invalid mode")
		}
		seenPlans := make(map[string]struct{}, len(result.Plans))
		for _, plan := range result.Plans {
			if err := checkID("title-plan operation_id", plan.OperationID); err != nil {
				return err
			}
			if err := checkID("title-plan task_id", plan.TaskID); err != nil {
				return err
			}
			if err := checkID("title-plan expected_revision", plan.ExpectedRevision); err != nil {
				return err
			}
			if plan.DesiredTitle == "" {
				return errors.New("title-plan desired_title is required")
			}
			if plan.OperationID != state.TitleOperationID(plan.TaskID, plan.ExpectedRevision, plan.ExpectedTitle, plan.DesiredTitle) {
				return errors.New("title-plan operation_id is invalid")
			}
			if _, exists := seenPlans[plan.TaskID]; exists {
				return errors.New("title-plan contains duplicate task plans")
			}
			seenPlans[plan.TaskID] = struct{}{}
		}
		seenDispositions := make(map[string]struct{}, len(result.Dispositions))
		for _, disposition := range result.Dispositions {
			if err := checkID("title-plan disposition task_id", disposition.TaskID); err != nil {
				return err
			}
			if _, exists := seenPlans[disposition.TaskID]; exists {
				return errors.New("title-plan task appears in plans and dispositions")
			}
			if _, exists := seenDispositions[disposition.TaskID]; exists {
				return errors.New("title-plan contains duplicate dispositions")
			}
			seenDispositions[disposition.TaskID] = struct{}{}
			switch disposition.Outcome {
			case "canonical_persisted", "drifted", "missing", "native_succeeded_pending_canonical", "no_op":
			default:
				return errors.New("title-plan disposition has invalid outcome")
			}
		}
	case TitleReportResult:
		seen := make(map[string]bool, len(result.AcceptedIDs)+len(result.RejectedIDs))
		for _, taskID := range result.AcceptedIDs {
			if err := checkID("title-report accepted task_id", taskID); err != nil {
				return err
			}
			if seen[taskID] {
				return errors.New("title-report contains duplicate task IDs")
			}
			seen[taskID] = true
		}
		for _, taskID := range result.RejectedIDs {
			if err := checkID("title-report rejected task_id", taskID); err != nil {
				return err
			}
			if seen[taskID] {
				return errors.New("title-report contains duplicate task IDs")
			}
			seen[taskID] = true
		}
	case StatusResult:
		if result.LaunchAgentStatus != "" && result.LaunchAgentStatus != "healthy" && result.LaunchAgentStatus != "unhealthy" && result.LaunchAgentStatus != "unavailable" {
			return errors.New("status result has invalid launch_agent_status")
		}
		if result.InstalledVersion == "" || result.ControlTaskID == "" || result.Preferences.HeartbeatSeconds <= 0 || result.Preferences.ArchiveAfterDays <= 0 || result.Preferences.ClassifierModel == "" || result.Preferences.ClassifierEffort == "" || result.Preferences.ClassifierContextBudgetBytes <= 0 || result.PendingRetries < 0 || result.PendingTitlePlans < 0 || result.NativeTitleSuccesses < 0 || result.NativeTitleSuccesses > result.PendingTitlePlans {
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
		if result.PendingTitlePlan {
			if !result.NativeTitleOutcome.Valid() || result.CanonicalPersistence == "" {
				return errors.New("inspect pending title state is incomplete")
			}
		} else if result.NativeTitleOutcome != "" || result.CanonicalPersistence != "" {
			return errors.New("inspect has title state without a pending plan")
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
	case TitleActuatorResult:
		return result.Version
	case HeartbeatResult:
		return result.Version
	case TitlePlanResult:
		return result.Version
	case TitleReportResult:
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

func validTitleDispatchDisposition(value string) bool {
	switch value {
	case "source_missing", "source_invalid", "config_unavailable", "config_invalid", "state_unavailable", "state_invalid", "control_task", "rename_disabled", "agents_disabled":
		return true
	default:
		return false
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
