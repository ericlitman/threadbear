package state

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ericlitman/threadbear/internal/tokens"
)

const (
	CurrentStateSchemaVersion   = 3
	CurrentCycleSchemaVersion   = 3
	NativeTitleCanonicalTimeout = 2 * time.Minute
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
	ProvenanceBootstrapTitle  Provenance = "bootstrap_title"
	ProvenanceLuna            Provenance = "luna"
	ProvenanceUnknown         Provenance = "unknown"
)

func (p Provenance) Valid() bool {
	switch p {
	case ProvenanceRuntime, ProvenanceStructuredError, ProvenanceAutomation, ProvenanceInterruption, ProvenanceFooter, ProvenanceBootstrapTitle, ProvenanceLuna, ProvenanceUnknown:
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
	TaskID                  string          `json:"task_id"`
	CapturedRevision        string          `json:"captured_revision"`
	CapturedTitle           string          `json:"captured_title"`
	Status                  TaskStatus      `json:"status"`
	Provenance              Provenance      `json:"provenance"`
	StateStartedAt          time.Time       `json:"state_started_at"`
	LastSubstantiveActivity time.Time       `json:"last_substantive_activity"`
	EvidenceFingerprint     string          `json:"evidence_fingerprint,omitempty"`
	DurableSubject          string          `json:"durable_subject,omitempty"`
	ManagedAction           string          `json:"managed_action,omitempty"`
	LastAppliedTitle        string          `json:"last_applied_title,omitempty"`
	ManagedTokenDisplay     string          `json:"managed_token_display,omitempty"`
	ManagedTokenPosition    tokens.Position `json:"managed_token_position,omitempty"`
	TokenDisplayPosition    tokens.Position `json:"token_display_position,omitempty"`
	TokenRolloutPath        string          `json:"token_rollout_path,omitempty"`
	TokenReadOffset         int64           `json:"token_read_offset,omitempty"`
	TokenRolloutSize        int64           `json:"token_rollout_size,omitempty"`
	OutputTokens            uint64          `json:"output_tokens,omitempty"`
	TotalTokens             uint64          `json:"total_tokens,omitempty"`
	TokenUsageFound         bool            `json:"token_usage_found,omitempty"`
	Retry                   *Retry          `json:"retry,omitempty"`
}

type Failure struct {
	Code      string    `json:"code"`
	Timestamp time.Time `json:"timestamp"`
}

type ArchiveRecord struct {
	TaskID           string    `json:"task_id"`
	ArchivedAt       time.Time `json:"archived_at"`
	CapturedRevision string    `json:"captured_revision"`
	StateGeneration  uint64    `json:"state_generation"`
}

type NativeTitleOutcome string

const (
	NativeTitlePending   NativeTitleOutcome = "pending"
	NativeTitleSucceeded NativeTitleOutcome = "succeeded"
	NativeTitleFailed    NativeTitleOutcome = "failed"
)

func (o NativeTitleOutcome) Valid() bool {
	switch o {
	case NativeTitlePending, NativeTitleSucceeded, NativeTitleFailed:
		return true
	default:
		return false
	}
}

func TitleOperationID(taskID, expectedRevision, expectedTitle, desiredTitle string) string {
	sum := sha256.Sum256([]byte(taskID + "\x00" + expectedRevision + "\x00" + expectedTitle + "\x00" + desiredTitle))
	return hex.EncodeToString(sum[:16])
}

type PendingTitlePlan struct {
	OperationID          string             `json:"operation_id"`
	TaskID               string             `json:"task_id"`
	ExpectedRevision     string             `json:"expected_revision"`
	ExpectedTitle        string             `json:"expected_title"`
	DesiredTitle         string             `json:"desired_title"`
	DurableSubject       string             `json:"durable_subject,omitempty"`
	ManagedAction        string             `json:"managed_action,omitempty"`
	ManagedTokenDisplay  string             `json:"managed_token_display,omitempty"`
	ManagedTokenPosition tokens.Position    `json:"managed_token_position,omitempty"`
	NativeOutcome        NativeTitleOutcome `json:"native_outcome"`
	NativeReportedAt     *time.Time         `json:"native_reported_at,omitempty"`
	NativeErrorCode      string             `json:"native_error_code,omitempty"`
	ExpectedFooter       string             `json:"expected_footer,omitempty"`
	CanonicalAttempts    uint32             `json:"canonical_attempts,omitempty"`
	CanonicalCheckedAt   *time.Time         `json:"canonical_checked_at,omitempty"`
}

func (p PendingTitlePlan) Validate() error {
	if !canonicalIdentifier(p.OperationID) || !canonicalIdentifier(p.TaskID) || !canonicalIdentifier(p.ExpectedRevision) || p.DesiredTitle == "" {
		return errors.New("pending title plan identity is incomplete")
	}
	if p.OperationID != TitleOperationID(p.TaskID, p.ExpectedRevision, p.ExpectedTitle, p.DesiredTitle) {
		return errors.New("pending title plan operation_id is invalid")
	}
	if (p.ManagedTokenDisplay == "") != (p.ManagedTokenPosition == "") {
		return errors.New("pending title token ownership is incomplete")
	}
	if p.ManagedTokenDisplay != "" && p.ManagedTokenPosition != tokens.PositionStart && p.ManagedTokenPosition != tokens.PositionEnd {
		return errors.New("pending title token position is invalid")
	}
	if strings.TrimSpace(p.ExpectedFooter) != p.ExpectedFooter {
		return errors.New("pending title footer is invalid")
	}
	if p.CanonicalCheckedAt != nil && p.CanonicalCheckedAt.IsZero() {
		return errors.New("pending title canonical check is invalid")
	}
	switch p.NativeOutcome {
	case NativeTitlePending:
		if p.NativeReportedAt != nil || p.NativeErrorCode != "" {
			return errors.New("pending native title outcome has report metadata")
		}
	case NativeTitleSucceeded:
		if p.NativeReportedAt == nil || p.NativeErrorCode != "" {
			return errors.New("successful native title outcome is incomplete")
		}
	case NativeTitleFailed:
		if p.NativeReportedAt == nil || !stableCode(p.NativeErrorCode) {
			return errors.New("failed native title outcome is incomplete")
		}
	default:
		return errors.New("native title outcome is invalid")
	}
	return nil
}

type SweepPhase string

const (
	SweepPhaseStarting      SweepPhase = "starting"
	SweepPhaseDeterministic SweepPhase = "deterministic"
	SweepPhaseSemantic      SweepPhase = "semantic"
	SweepPhaseMutating      SweepPhase = "mutating"
	SweepPhaseConverged     SweepPhase = "converged"
	SweepPhaseRetryable     SweepPhase = "retryable"
)

type SweepProgress struct {
	Phase                        SweepPhase `json:"phase"`
	InventoryTasks               int        `json:"inventory_tasks"`
	ChangedTasks                 int        `json:"changed_tasks"`
	LatestTurnReads              int        `json:"latest_turn_reads"`
	MechanicallyResolved         int        `json:"mechanically_resolved"`
	LunaCandidates               int        `json:"luna_candidates"`
	FirstPassBatchesTotal        int        `json:"first_pass_batches_total"`
	FirstPassBatchesCompleted    int        `json:"first_pass_batches_completed"`
	PreviousPassBatchesTotal     int        `json:"previous_pass_batches_total"`
	PreviousPassBatchesCompleted int        `json:"previous_pass_batches_completed"`
	ModelDurationMilliseconds    int64      `json:"model_duration_ms"`
	MutationDurationMilliseconds int64      `json:"mutation_duration_ms"`
	RetryCount                   int        `json:"retry_count"`
	RateLimitCount               int        `json:"rate_limit_count"`
	StartedAt                    time.Time  `json:"started_at"`
	FirstProgressAt              *time.Time `json:"first_progress_at,omitempty"`
	UpdatedAt                    time.Time  `json:"updated_at"`
	CompletedAt                  *time.Time `json:"completed_at,omitempty"`
}

func (p SweepProgress) Validate() error {
	switch p.Phase {
	case SweepPhaseStarting, SweepPhaseDeterministic, SweepPhaseSemantic, SweepPhaseMutating, SweepPhaseConverged, SweepPhaseRetryable:
	default:
		return errors.New("sweep phase is invalid")
	}
	counts := []int{p.InventoryTasks, p.ChangedTasks, p.LatestTurnReads, p.MechanicallyResolved, p.LunaCandidates, p.FirstPassBatchesTotal, p.FirstPassBatchesCompleted, p.PreviousPassBatchesTotal, p.PreviousPassBatchesCompleted, p.RetryCount, p.RateLimitCount}
	for _, count := range counts {
		if count < 0 {
			return errors.New("sweep counts must be nonnegative")
		}
	}
	if p.FirstPassBatchesCompleted > p.FirstPassBatchesTotal || p.PreviousPassBatchesCompleted > p.PreviousPassBatchesTotal {
		return errors.New("sweep completed batches exceed planned batches")
	}
	if p.ModelDurationMilliseconds < 0 || p.MutationDurationMilliseconds < 0 || p.StartedAt.IsZero() || p.UpdatedAt.IsZero() {
		return errors.New("sweep timing is invalid")
	}
	if p.FirstProgressAt != nil && p.FirstProgressAt.IsZero() {
		return errors.New("sweep first progress time is invalid")
	}
	completed := p.Phase == SweepPhaseConverged || p.Phase == SweepPhaseRetryable
	if completed != (p.CompletedAt != nil) {
		return errors.New("sweep completion time does not match phase")
	}
	if p.CompletedAt != nil && p.CompletedAt.IsZero() {
		return errors.New("sweep completion time is invalid")
	}
	return nil
}

type State struct {
	SchemaVersion           int                         `json:"schema_version"`
	Generation              uint64                      `json:"generation"`
	BootstrapComplete       bool                        `json:"bootstrap_complete"`
	LastCompletedHeartbeat  *time.Time                  `json:"last_completed_heartbeat,omitempty"`
	LastSweep               *SweepProgress              `json:"last_sweep,omitempty"`
	LastUpdateCheck         *time.Time                  `json:"last_update_check,omitempty"`
	LastAnnouncedVersion    string                      `json:"last_announced_version,omitempty"`
	LastReconciledVersion   string                      `json:"last_reconciled_version,omitempty"`
	PendingWelcomeTaskID    string                      `json:"pending_welcome_task_id,omitempty"`
	LastUpdateFailure       *Failure                    `json:"last_update_failure,omitempty"`
	LastReconcileFailure    *Failure                    `json:"last_reconcile_failure,omitempty"`
	Tasks                   map[string]TaskRecord       `json:"tasks"`
	PendingTitlePlans       map[string]PendingTitlePlan `json:"pending_title_plans"`
	Archives                map[string]ArchiveRecord    `json:"archives"`
	DeliveredNoticeVersions []string                    `json:"delivered_notice_versions"`
}

func New() State {
	return State{
		SchemaVersion:           CurrentStateSchemaVersion,
		Tasks:                   make(map[string]TaskRecord),
		PendingTitlePlans:       make(map[string]PendingTitlePlan),
		Archives:                make(map[string]ArchiveRecord),
		DeliveredNoticeVersions: []string{},
	}
}

func (s State) Validate() error {
	if s.SchemaVersion != CurrentStateSchemaVersion {
		return fmt.Errorf("%w: got %d, want %d", ErrUnsupportedSchema, s.SchemaVersion, CurrentStateSchemaVersion)
	}
	if s.Tasks == nil || s.PendingTitlePlans == nil || s.Archives == nil || s.DeliveredNoticeVersions == nil {
		return errors.New("state collections must not be null")
	}
	if s.LastAnnouncedVersion != "" && strings.TrimSpace(s.LastAnnouncedVersion) != s.LastAnnouncedVersion {
		return errors.New("last_announced_version must not contain surrounding whitespace")
	}
	if s.LastReconciledVersion != "" && strings.TrimSpace(s.LastReconciledVersion) != s.LastReconciledVersion {
		return errors.New("last_reconciled_version must not contain surrounding whitespace")
	}
	if s.PendingWelcomeTaskID != "" && !canonicalIdentifier(s.PendingWelcomeTaskID) {
		return errors.New("pending_welcome_task_id must be canonical")
	}
	if s.LastSweep != nil {
		if err := s.LastSweep.Validate(); err != nil {
			return fmt.Errorf("last sweep: %w", err)
		}
	}
	for name, failure := range map[string]*Failure{"last_update_failure": s.LastUpdateFailure, "last_reconcile_failure": s.LastReconcileFailure} {
		if failure != nil && (!stableCode(failure.Code) || failure.Timestamp.IsZero()) {
			return fmt.Errorf("%s is incomplete", name)
		}
	}
	for key, task := range s.Tasks {
		if !canonicalIdentifier(key) || task.TaskID != key {
			return fmt.Errorf("task map key %q does not match task_id %q", key, task.TaskID)
		}
		if err := task.Validate(); err != nil {
			return fmt.Errorf("task %s: %w", key, err)
		}
	}
	for key, plan := range s.PendingTitlePlans {
		if key != plan.TaskID || !canonicalIdentifier(key) {
			return fmt.Errorf("pending title plan key %q does not match task_id %q", key, plan.TaskID)
		}
		if err := plan.Validate(); err != nil {
			return fmt.Errorf("pending title plan %s: %w", key, err)
		}
		task, ok := s.Tasks[key]
		sourceAwaitingFooter := plan.ExpectedFooter != "" && plan.NativeOutcome == NativeTitleSucceeded && plan.CanonicalCheckedAt != nil
		if !ok || !sourceAwaitingFooter && (task.CapturedRevision != plan.ExpectedRevision || task.CapturedTitle != plan.ExpectedTitle) {
			return fmt.Errorf("pending title plan %s does not match captured task", key)
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
	if t.EvidenceFingerprint != "" && !canonicalIdentifier(t.EvidenceFingerprint) {
		return errors.New("evidence fingerprint is invalid")
	}
	if (t.ManagedTokenDisplay == "") != (t.ManagedTokenPosition == "") {
		return errors.New("managed token display and position must be set together")
	}
	if t.ManagedTokenDisplay != "" && (t.ManagedTokenPosition != tokens.PositionStart && t.ManagedTokenPosition != tokens.PositionEnd) {
		return errors.New("managed token position is invalid")
	}
	if t.TokenDisplayPosition != "" && !t.TokenDisplayPosition.Valid() {
		return errors.New("token display position is invalid")
	}
	if t.TokenReadOffset < 0 || t.TokenRolloutSize < 0 || t.TokenReadOffset > t.TokenRolloutSize {
		return errors.New("token rollout cursor is invalid")
	}
	if t.TokenRolloutPath == "" && (t.TokenReadOffset != 0 || t.TokenRolloutSize != 0 || t.TokenUsageFound || t.OutputTokens != 0 || t.TotalTokens != 0) {
		return errors.New("token rollout cache requires a path")
	}
	if !t.TokenUsageFound && (t.OutputTokens != 0 || t.TotalTokens != 0) {
		return errors.New("token totals require a discovered token event")
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
	RolloutPath             string    `json:"rollout_path,omitempty"`
	Archived                bool      `json:"archived"`
	LastSubstantiveActivity time.Time `json:"last_substantive_activity"`
	EvidenceFingerprint     string    `json:"evidence_fingerprint,omitempty"`
}

type ClassificationResult struct {
	TaskID         string     `json:"task_id"`
	Revision       string     `json:"revision"`
	Status         TaskStatus `json:"status"`
	Provenance     Provenance `json:"provenance"`
	DurableSubject string     `json:"durable_subject,omitempty"`
	ManagedAction  string     `json:"managed_action,omitempty"`
}

type CycleDiagnostic struct {
	TaskID    string `json:"task_id"`
	Operation string `json:"operation"`
	ErrorCode string `json:"error_code"`
}

type OperationKind string

type OperationStage string

const (
	OperationTitle        OperationKind  = "title"
	OperationArchive      OperationKind  = "archive"
	OperationNotice       OperationKind  = "notice"
	OperationAnnouncement OperationKind  = "announcement"
	StagePrepared         OperationStage = "prepared"
	StageApplying         OperationStage = "applying"
	StageApplied          OperationStage = "applied"
	StageNativePending    OperationStage = "native_pending"
	StageVerified         OperationStage = "verified"
)

type CycleOperation struct {
	Kind                 OperationKind   `json:"kind"`
	Stage                OperationStage  `json:"stage"`
	TaskID               string          `json:"task_id,omitempty"`
	NoticeVersion        string          `json:"notice_version,omitempty"`
	PreviousVersion      string          `json:"previous_version,omitempty"`
	ExpectedRevision     string          `json:"expected_revision,omitempty"`
	ExpectedTitle        string          `json:"expected_title,omitempty"`
	DesiredTitle         string          `json:"desired_title,omitempty"`
	DurableSubject       string          `json:"durable_subject,omitempty"`
	ManagedAction        string          `json:"managed_action,omitempty"`
	ManagedTokenDisplay  string          `json:"managed_token_display,omitempty"`
	ManagedTokenPosition tokens.Position `json:"managed_token_position,omitempty"`
	ForceWrite           bool            `json:"force_write,omitempty"`
	VerifiedRevision     string          `json:"verified_revision,omitempty"`
	VerifiedTitle        string          `json:"verified_title,omitempty"`
}

type CycleCheckpoint struct {
	SchemaVersion          int                             `json:"schema_version"`
	CycleID                string                          `json:"cycle_id"`
	BaseGeneration         uint64                          `json:"base_generation"`
	CapturedAt             time.Time                       `json:"captured_at"`
	Inventory              map[string]CapturedTask         `json:"inventory"`
	Results                map[string]ClassificationResult `json:"results"`
	Diagnostics            map[string]CycleDiagnostic      `json:"diagnostics"`
	Operations             map[string]CycleOperation       `json:"operations"`
	Progress               *SweepProgress                  `json:"progress,omitempty"`
	PreviousRequested      map[string]string               `json:"previous_requested"`
	ClassifierCleanupToken string                          `json:"classifier_cleanup_token,omitempty"`
}

func NewCycle(cycleID string, baseGeneration uint64, capturedAt time.Time) CycleCheckpoint {
	return CycleCheckpoint{
		SchemaVersion:     CurrentCycleSchemaVersion,
		CycleID:           cycleID,
		BaseGeneration:    baseGeneration,
		CapturedAt:        capturedAt.UTC(),
		Inventory:         make(map[string]CapturedTask),
		Results:           make(map[string]ClassificationResult),
		Diagnostics:       make(map[string]CycleDiagnostic),
		Operations:        make(map[string]CycleOperation),
		PreviousRequested: make(map[string]string),
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
	if strings.TrimSpace(c.ClassifierCleanupToken) != c.ClassifierCleanupToken {
		return errors.New("classifier cleanup token must not contain surrounding whitespace")
	}
	if c.Inventory == nil || c.Results == nil || c.Diagnostics == nil || c.Operations == nil {
		return errors.New("cycle collections must not be null")
	}
	if c.Progress != nil {
		if err := c.Progress.Validate(); err != nil {
			return fmt.Errorf("cycle progress: %w", err)
		}
	}
	for taskID, revision := range c.PreviousRequested {
		task, ok := c.Inventory[taskID]
		if !ok || task.Revision != revision {
			return fmt.Errorf("previous request %q does not match captured revision", taskID)
		}
	}
	for key, task := range c.Inventory {
		if !canonicalIdentifier(key) || task.TaskID != key || !canonicalIdentifier(task.Revision) || task.LastSubstantiveActivity.IsZero() || task.EvidenceFingerprint != "" && !canonicalIdentifier(task.EvidenceFingerprint) {
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
	for key, diagnostic := range c.Diagnostics {
		if !canonicalIdentifier(key) || diagnostic.TaskID != key || !stableCode(diagnostic.Operation) || !stableCode(diagnostic.ErrorCode) {
			return fmt.Errorf("cycle diagnostic %q is invalid", key)
		}
	}
	for key, operation := range c.Operations {
		if !canonicalIdentifier(key) || !operation.Valid() {
			return fmt.Errorf("cycle operation %q is invalid", key)
		}
	}
	return nil
}

func (o CycleOperation) Valid() bool {
	if o.Stage != StagePrepared && o.Stage != StageApplying && o.Stage != StageApplied && o.Stage != StageNativePending && o.Stage != StageVerified {
		return false
	}
	switch o.Kind {
	case OperationTitle:
		valid := canonicalIdentifier(o.TaskID) && canonicalIdentifier(o.ExpectedRevision) && o.DesiredTitle != "" && o.NoticeVersion == "" && o.PreviousVersion == ""
		valid = valid && (o.ManagedTokenDisplay == "") == (o.ManagedTokenPosition == "")
		valid = valid && (o.ManagedTokenDisplay == "" || o.ManagedTokenPosition == tokens.PositionStart || o.ManagedTokenPosition == tokens.PositionEnd)
		if o.Stage == StageVerified {
			valid = valid && canonicalIdentifier(o.VerifiedRevision) && o.VerifiedTitle != ""
		}
		return valid
	case OperationArchive:
		return canonicalIdentifier(o.TaskID) && canonicalIdentifier(o.ExpectedRevision) && o.NoticeVersion == "" && o.PreviousVersion == "" && !o.ForceWrite
	case OperationNotice:
		return o.TaskID == "" && canonicalIdentifier(o.NoticeVersion) && o.PreviousVersion == "" && !o.ForceWrite
	case OperationAnnouncement:
		return o.TaskID == "" && canonicalIdentifier(o.NoticeVersion) && canonicalIdentifier(o.PreviousVersion) && o.NoticeVersion != o.PreviousVersion && !o.ForceWrite
	default:
		return false
	}
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
