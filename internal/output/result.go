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
	Version     int           `json:"version"`
	CycleID     string        `json:"cycle_id"`
	Changed     []TaskChange  `json:"changed"`
	ArchivedIDs []string      `json:"archived_ids"`
	RestoredIDs []string      `json:"restored_ids"`
	Retries     []RetryResult `json:"retries"`
	ErrorCode   string        `json:"error_code,omitempty"`
}

func (HeartbeatResult) result() {}

func (r HeartbeatResult) Empty() bool {
	return len(r.Changed) == 0 && len(r.ArchivedIDs) == 0 && len(r.RestoredIDs) == 0 && len(r.Retries) == 0 && r.ErrorCode == ""
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
	r.Retries = slices.Clone(r.Retries)
	sort.Slice(r.Changed, func(i, j int) bool {
		if r.Changed[i].TaskID == r.Changed[j].TaskID {
			return r.Changed[i].State < r.Changed[j].State
		}
		return r.Changed[i].TaskID < r.Changed[j].TaskID
	})
	slices.Sort(r.ArchivedIDs)
	slices.Sort(r.RestoredIDs)
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
	AgentsEnabled                bool   `json:"agents_enabled"`
	ClassifierModel              string `json:"classifier_model"`
	ClassifierEffort             string `json:"classifier_effort"`
	ClassifierContextBudgetBytes int    `json:"classifier_context_budget_bytes"`
}

type StatusResult struct {
	Version                int         `json:"version"`
	InstalledVersion       string      `json:"installed_version"`
	LaunchAgentHealthy     bool        `json:"launch_agent_healthy"`
	LastCompletedHeartbeat *time.Time  `json:"last_completed_heartbeat,omitempty"`
	ControlTaskID          string      `json:"control_task_id"`
	Preferences            Preferences `json:"preferences"`
	PendingRetries         int         `json:"pending_retries"`
	LastUpdateCheck        *time.Time  `json:"last_update_check,omitempty"`
}

func (StatusResult) result()     {}
func (StatusResult) Empty() bool { return false }
func (r StatusResult) Human() string {
	health := "unhealthy"
	if r.LaunchAgentHealthy {
		health = "healthy"
	}
	return fmt.Sprintf("ThreadBear %s · LaunchAgent %s · heartbeat %s · control task %s · heartbeat interval %ds · archive %t/%dd · rename %t · AGENTS %t · classifier %s/%s/%dB · retries %d · update check %s", r.InstalledVersion, health, formatTime(r.LastCompletedHeartbeat), r.ControlTaskID, r.Preferences.HeartbeatSeconds, r.Preferences.ArchiveEnabled, r.Preferences.ArchiveAfterDays, r.Preferences.RenameEnabled, r.Preferences.AgentsEnabled, r.Preferences.ClassifierModel, r.Preferences.ClassifierEffort, r.Preferences.ClassifierContextBudgetBytes, r.PendingRetries, formatTime(r.LastUpdateCheck))
}

type InspectResult struct {
	Version          int              `json:"version"`
	TaskID           string           `json:"task_id"`
	CapturedRevision string           `json:"captured_revision"`
	State            state.TaskStatus `json:"state"`
	Provenance       state.Provenance `json:"provenance"`
	ManagedAction    string           `json:"managed_action,omitempty"`
	Retry            *RetryResult     `json:"retry,omitempty"`
	ArchiveEligible  bool             `json:"archive_eligible"`
}

func (InspectResult) result()     {}
func (InspectResult) Empty() bool { return false }
func (r InspectResult) Human() string {
	action := r.ManagedAction
	if action == "" {
		action = "none"
	}
	retry := "none"
	if r.Retry != nil {
		retry = fmt.Sprintf("%s/%s", r.Retry.Operation, r.Retry.ErrorCode)
	}
	return fmt.Sprintf("%s %s · revision %s · provenance %s · next: %s · retry %s · archive eligible %t", r.State.Emoji(), r.TaskID, r.CapturedRevision, r.Provenance, action, retry, r.ArchiveEligible)
}

type PreviewResult struct {
	Version int      `json:"version"`
	Command string   `json:"command"`
	Effects []string `json:"effects"`
}

func (PreviewResult) result()     {}
func (PreviewResult) Empty() bool { return false }
func (r PreviewResult) Human() string {
	return fmt.Sprintf("ThreadBear preview for %s: %s", r.Command, strings.Join(r.Effects, ", "))
}

type ActionResult struct {
	Version     int      `json:"version"`
	Command     string   `json:"command"`
	Changed     bool     `json:"changed"`
	ResourceIDs []string `json:"resource_ids"`
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
	return fmt.Sprintf("ThreadBear %s: %s · resources %s", r.Command, outcome, resources)
}

type CheckResult struct {
	Name      string `json:"name"`
	OK        bool   `json:"ok"`
	ErrorCode string `json:"error_code,omitempty"`
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
		}
		checks = append(checks, check.Name+"="+result)
	}
	if len(checks) == 0 {
		return "ThreadBear self-test: " + status
	}
	return "ThreadBear self-test: " + status + " · " + strings.Join(checks, ",")
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
}

func (ErrorResult) result()     {}
func (ErrorResult) Empty() bool { return false }
func (r ErrorResult) Human() string {
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
	case *SelfTestResult:
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
	case HeartbeatResult:
		return result.normalized()
	case StatusResult:
		if result.Version == 0 {
			result.Version = CurrentResultVersion
		}
		return result
	case InspectResult:
		if result.Version == 0 {
			result.Version = CurrentResultVersion
		}
		return result
	case PreviewResult:
		if result.Version == 0 {
			result.Version = CurrentResultVersion
		}
		result.Effects = slices.Clone(result.Effects)
		slices.Sort(result.Effects)
		if result.Effects == nil {
			result.Effects = []string{}
		}
		return result
	case ActionResult:
		if result.Version == 0 {
			result.Version = CurrentResultVersion
		}
		result.ResourceIDs = slices.Clone(result.ResourceIDs)
		slices.Sort(result.ResourceIDs)
		if result.ResourceIDs == nil {
			result.ResourceIDs = []string{}
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
	switch result := value.(type) {
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
		if result.InstalledVersion == "" || result.ControlTaskID == "" || result.Preferences.HeartbeatSeconds <= 0 || result.Preferences.ArchiveAfterDays <= 0 || result.Preferences.ClassifierModel == "" || result.Preferences.ClassifierEffort == "" || result.Preferences.ClassifierContextBudgetBytes <= 0 || result.PendingRetries < 0 {
			return errors.New("status result is incomplete")
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
	case SelfTestResult:
		if len(result.Checks) == 0 {
			return errors.New("self-test result requires checks")
		}
		allOK := true
		for _, check := range result.Checks {
			if err := checkCode("check name", check.Name, false); err != nil {
				return err
			}
			if check.OK && check.ErrorCode != "" {
				return errors.New("successful check must not have an error_code")
			}
			if err := checkCode("check error_code", check.ErrorCode, check.OK); err != nil {
				return err
			}
			allOK = allOK && check.OK
		}
		if result.OK != allOK {
			return errors.New("self-test summary contradicts checks")
		}
	case VersionResult:
		if result.Product == "" || result.InstalledVersion == "" || result.Website == "" {
			return errors.New("version result is incomplete")
		}
	case ErrorResult:
		if err := checkCode("operation", result.Operation, false); err != nil {
			return err
		}
		return checkCode("error_code", result.ErrorCode, false)
	}
	return nil
}

func resultVersion(value Result) int {
	switch result := value.(type) {
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
	case SelfTestResult:
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
