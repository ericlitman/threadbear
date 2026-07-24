package state

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	CurrentStateSchemaVersion = 1
	CurrentCycleSchemaVersion = 1
)

var ErrUnsupportedSchema = errors.New("unsupported state schema")

type TaskStatus string

const (
	StatusRunning    TaskStatus = "running"
	StatusBlocked    TaskStatus = "blocked"
	StatusNeedsInput TaskStatus = "needs_input"
	StatusAutomation TaskStatus = "automation"
	StatusNextSteps  TaskStatus = "next_steps"
	StatusComplete   TaskStatus = "complete"
	StatusUnknown    TaskStatus = "unknown"
)

func (s TaskStatus) Valid() bool {
	switch s {
	case StatusRunning, StatusBlocked, StatusNeedsInput, StatusAutomation, StatusNextSteps, StatusComplete, StatusUnknown:
		return true
	default:
		return false
	}
}

func (s TaskStatus) Emoji() string {
	switch s {
	case StatusRunning:
		return "⏳"
	case StatusBlocked:
		return "🚨"
	case StatusNeedsInput:
		return "🙋"
	case StatusAutomation:
		return "🤖"
	case StatusNextSteps:
		return "➡️"
	case StatusComplete:
		return "✅"
	default:
		return "❔"
	}
}

type Provenance string

const (
	ProvenanceRuntime         Provenance = "runtime"
	ProvenanceStructuredError Provenance = "structured_error"
	ProvenanceAutomation      Provenance = "automation"
	ProvenanceInterruption    Provenance = "interruption"
	ProvenanceFooter          Provenance = "footer"
	ProvenanceLuna            Provenance = "luna"
	ProvenanceUnknown         Provenance = "unknown"
)

func (p Provenance) Valid() bool {
	switch p {
	case ProvenanceRuntime, ProvenanceStructuredError, ProvenanceAutomation, ProvenanceInterruption, ProvenanceFooter, ProvenanceLuna, ProvenanceUnknown:
		return true
	default:
		return false
	}
}

type Retry struct {
	Operation     string    `json:"operation"`
	ErrorCode     string    `json:"error_code"`
	Attempts      uint32    `json:"attempts"`
	LastAttemptAt time.Time `json:"last_attempt_at"`
	NextAttemptAt time.Time `json:"next_attempt_at"`
}

type TaskRecord struct {
	TaskID                  string     `json:"task_id"`
	CapturedRevision        string     `json:"captured_revision"`
	Status                  TaskStatus `json:"status"`
	Provenance              Provenance `json:"provenance"`
	StateStartedAt          time.Time  `json:"state_started_at"`
	LastSubstantiveActivity time.Time  `json:"last_substantive_activity"`
	DurableSubject          string     `json:"durable_subject,omitempty"`
	ManagedAction           string     `json:"managed_action,omitempty"`
	LastAppliedTitle        string     `json:"last_applied_title,omitempty"`
	Retry                   *Retry     `json:"retry,omitempty"`
}

type ArchiveRecord struct {
	TaskID           string    `json:"task_id"`
	ArchivedAt       time.Time `json:"archived_at"`
	CapturedRevision string    `json:"captured_revision"`
	StateGeneration  uint64    `json:"state_generation"`
}

type State struct {
	SchemaVersion           int                      `json:"schema_version"`
	Generation              uint64                   `json:"generation"`
	LastCompletedHeartbeat  *time.Time               `json:"last_completed_heartbeat,omitempty"`
	LastUpdateCheck         *time.Time               `json:"last_update_check,omitempty"`
	Tasks                   map[string]TaskRecord    `json:"tasks"`
	Archives                map[string]ArchiveRecord `json:"archives"`
	DeliveredNoticeVersions []string                 `json:"delivered_notice_versions"`
}

func New() State {
	return State{
		SchemaVersion:           CurrentStateSchemaVersion,
		Tasks:                   make(map[string]TaskRecord),
		Archives:                make(map[string]ArchiveRecord),
		DeliveredNoticeVersions: []string{},
	}
}

func (s State) Validate() error {
	if s.SchemaVersion != CurrentStateSchemaVersion {
		return fmt.Errorf("%w: got %d, want %d", ErrUnsupportedSchema, s.SchemaVersion, CurrentStateSchemaVersion)
	}
	if s.Tasks == nil || s.Archives == nil || s.DeliveredNoticeVersions == nil {
		return errors.New("state collections must not be null")
	}
	for key, task := range s.Tasks {
		if !canonicalIdentifier(key) || task.TaskID != key {
			return fmt.Errorf("task map key %q does not match task_id %q", key, task.TaskID)
		}
		if err := task.Validate(); err != nil {
			return fmt.Errorf("task %s: %w", key, err)
		}
	}
	for key, archive := range s.Archives {
		if !canonicalIdentifier(key) || archive.TaskID != key {
			return fmt.Errorf("archive map key %q does not match task_id %q", key, archive.TaskID)
		}
		if !canonicalIdentifier(archive.CapturedRevision) || archive.ArchivedAt.IsZero() {
			return fmt.Errorf("archive %s is incomplete", key)
		}
	}
	return nil
}

func (t TaskRecord) Validate() error {
	if t.TaskID == "" || t.CapturedRevision == "" {
		return errors.New("task_id and captured_revision are required")
	}
	if strings.TrimSpace(t.TaskID) != t.TaskID || strings.TrimSpace(t.CapturedRevision) != t.CapturedRevision {
		return errors.New("task_id and captured_revision must not contain surrounding whitespace")
	}
	if !t.Status.Valid() {
		return fmt.Errorf("status %q is invalid", t.Status)
	}
	if !t.Provenance.Valid() {
		return fmt.Errorf("provenance %q is invalid", t.Provenance)
	}
	if t.StateStartedAt.IsZero() || t.LastSubstantiveActivity.IsZero() {
		return errors.New("state and activity timestamps are required")
	}
	if t.Retry != nil {
		if !stableCode(t.Retry.Operation) || !stableCode(t.Retry.ErrorCode) || t.Retry.Attempts == 0 || t.Retry.LastAttemptAt.IsZero() || t.Retry.NextAttemptAt.IsZero() {
			return errors.New("retry is incomplete")
		}
		if t.Retry.NextAttemptAt.Before(t.Retry.LastAttemptAt) {
			return errors.New("retry next_attempt_at precedes last_attempt_at")
		}
	}
	return nil
}

type CapturedTask struct {
	TaskID                  string    `json:"task_id"`
	Revision                string    `json:"revision"`
	Title                   string    `json:"title"`
	Archived                bool      `json:"archived"`
	LastSubstantiveActivity time.Time `json:"last_substantive_activity"`
}

type ClassificationResult struct {
	TaskID         string     `json:"task_id"`
	Revision       string     `json:"revision"`
	Status         TaskStatus `json:"status"`
	Provenance     Provenance `json:"provenance"`
	DurableSubject string     `json:"durable_subject,omitempty"`
	ManagedAction  string     `json:"managed_action,omitempty"`
}

type CycleCheckpoint struct {
	SchemaVersion  int                             `json:"schema_version"`
	CycleID        string                          `json:"cycle_id"`
	BaseGeneration uint64                          `json:"base_generation"`
	CapturedAt     time.Time                       `json:"captured_at"`
	Inventory      map[string]CapturedTask         `json:"inventory"`
	Results        map[string]ClassificationResult `json:"results"`
}

func NewCycle(cycleID string, baseGeneration uint64, capturedAt time.Time) CycleCheckpoint {
	return CycleCheckpoint{
		SchemaVersion:  CurrentCycleSchemaVersion,
		CycleID:        cycleID,
		BaseGeneration: baseGeneration,
		CapturedAt:     capturedAt.UTC(),
		Inventory:      make(map[string]CapturedTask),
		Results:        make(map[string]ClassificationResult),
	}
}

func (c CycleCheckpoint) Validate() error {
	if c.SchemaVersion != CurrentCycleSchemaVersion {
		return fmt.Errorf("%w: got %d, want %d", ErrUnsupportedSchema, c.SchemaVersion, CurrentCycleSchemaVersion)
	}
	if c.CycleID == "" || c.CapturedAt.IsZero() {
		return errors.New("cycle_id and captured_at are required")
	}
	if strings.TrimSpace(c.CycleID) != c.CycleID {
		return errors.New("cycle_id must not contain surrounding whitespace")
	}
	if c.Inventory == nil || c.Results == nil {
		return errors.New("cycle collections must not be null")
	}
	for key, task := range c.Inventory {
		if !canonicalIdentifier(key) || task.TaskID != key || !canonicalIdentifier(task.Revision) || task.LastSubstantiveActivity.IsZero() {
			return fmt.Errorf("captured task %q is invalid", key)
		}
	}
	for key, result := range c.Results {
		task, ok := c.Inventory[key]
		if !canonicalIdentifier(key) || !ok || result.TaskID != key || !canonicalIdentifier(result.Revision) || result.Revision != task.Revision {
			return fmt.Errorf("classification result %q does not match captured revision", key)
		}
		if !result.Status.Valid() || !result.Provenance.Valid() {
			return fmt.Errorf("classification result %q is invalid", key)
		}
	}
	return nil
}

func canonicalIdentifier(value string) bool {
	return value != "" && strings.TrimSpace(value) == value
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
