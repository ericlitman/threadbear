package watch

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"

	"github.com/ericlitman/threadbear/internal/codex"
	"github.com/ericlitman/threadbear/internal/codex/appserver"
	"github.com/ericlitman/threadbear/internal/config"
	"github.com/ericlitman/threadbear/internal/output"
	"github.com/ericlitman/threadbear/internal/state"
	"github.com/ericlitman/threadbear/internal/status"
	"github.com/ericlitman/threadbear/internal/title"
	"github.com/ericlitman/threadbear/internal/tokens"
)

const updateInterval = 24 * time.Hour

type Store interface {
	LoadConfig() (config.Config, error)
	LoadState() (state.State, error)
	SaveState(state.State) error
	LoadCycle() (state.CycleCheckpoint, error)
	SaveCycle(state.CycleCheckpoint) error
	RemoveCycle() error
	AcquireLock() (*state.Lock, error)
}

type InventoryReader interface {
	Inventory(context.Context, string) (codex.Inventory, error)
}

type AppServer interface {
	ReadLatestTurn(context.Context, string, string) (appserver.RecentEvidence, error)
	ReadPreviousTurn(context.Context, string, string) (*appserver.EvidenceTurn, error)
	ReadPersistedAssistantMessage(context.Context, string, string) (appserver.PersistedMessageResult, error)
	SetTitle(context.Context, string, string) error
	Archive(context.Context, string) error
	InsertNotice(context.Context, string, string) error
	Close() error
}

type AppServerFactory interface {
	Open(context.Context) (AppServer, error)
}

type Classifier interface {
	ClassifyWithPrevious(context.Context, []status.TaskEvidence, status.PreviousEvidenceLoader) []status.Classification
}

type ClassifierFactory func(AppServer, config.Config) (Classifier, error)

type UpdateStatus struct {
	LatestVersion string
	Newer         bool
}

type UpdateChecker interface {
	Check(context.Context, string) (UpdateStatus, error)
}

type TokenReader interface {
	ReadRollout(string, tokens.Snapshot) (tokens.Snapshot, error)
}

type filesystemTokenReader struct{}

func (filesystemTokenReader) ReadRollout(path string, previous tokens.Snapshot) (tokens.Snapshot, error) {
	return tokens.ReadRollout(path, previous)
}

type Clock interface {
	Now() time.Time
}

type Dependencies struct {
	Store            Store
	Inventory        InventoryReader
	AppServer        AppServerFactory
	NewClassifier    ClassifierFactory
	UpdateChecker    UpdateChecker
	TokenReader      TokenReader
	Clock            Clock
	InstalledVersion string
	NewCycleID       func() string
}

type Runner struct {
	deps Dependencies
}

func New(deps Dependencies) (*Runner, error) {
	if deps.Store == nil || deps.Inventory == nil || deps.Clock == nil || deps.NewCycleID == nil {
		return nil, errors.New("heartbeat dependencies are incomplete")
	}
	if deps.TokenReader == nil {
		deps.TokenReader = filesystemTokenReader{}
	}
	return &Runner{deps: deps}, nil
}

func (r *Runner) Run(ctx context.Context, dryRun bool) (output.Result, error) {
	lock, err := r.deps.Store.AcquireLock()
	if err != nil {
		return output.HeartbeatResult{CycleID: "lock", ErrorCode: "heartbeat_locked"}, err
	}
	defer lock.Close()

	cfg, err := r.deps.Store.LoadConfig()
	if err != nil {
		return output.HeartbeatResult{CycleID: "config", ErrorCode: "config_read_failed"}, err
	}
	committed, err := r.loadState()
	if err != nil {
		return output.HeartbeatResult{CycleID: "state", ErrorCode: "state_read_failed"}, err
	}
	now := r.deps.Clock.Now().UTC()
	inventory, err := r.deps.Inventory.Inventory(ctx, cfg.ControlTaskID)
	if err != nil {
		return output.HeartbeatResult{CycleID: "inventory", ErrorCode: "inventory_failed"}, err
	}
	comparison := codex.CompareInventory(inventory, committed)
	comparison.Changed = dueChanged(comparison.Changed, committed, now)
	archiveDue := archiveDueTasks(inventory, committed, cfg, now)
	comparison.Changed = mergeChanged(comparison.Changed, archiveDue)
	comparison.Changed = mergeChanged(comparison.Changed, tokenDisplayDueTasks(inventory, committed, cfg))
	updateDue := committed.LastUpdateCheck == nil || !now.Before(committed.LastUpdateCheck.Add(updateInterval))

	checkpoint, checkpointExists, err := r.loadCheckpoint(committed)
	if err != nil {
		return output.HeartbeatResult{CycleID: "cycle", ErrorCode: "cycle_read_failed"}, err
	}
	if dryRun {
		return dryRunResult(comparison, updateDue), nil
	}
	if comparison.Unchanged() && !updateDue && !checkpointExists {
		return output.HeartbeatResult{}, nil
	}

	if !checkpointExists {
		checkpoint = state.NewCycle(r.deps.NewCycleID(), committed.Generation, now)
		for _, task := range inventory.Tasks {
			activity := now
			if previous, ok := committed.Tasks[task.TaskID]; ok {
				activity = previous.LastSubstantiveActivity
			}
			if _, archived := committed.Archives[task.TaskID]; archived {
				activity = now
			}
			checkpoint.Inventory[task.TaskID] = state.CapturedTask{TaskID: task.TaskID, Revision: task.Revision, Title: task.Title, RolloutPath: task.RolloutPath, Archived: task.Archived, LastSubstantiveActivity: activity}
			if previous, ok := committed.Tasks[task.TaskID]; ok && previous.Retry == nil && previous.CapturedRevision == task.Revision && previous.CapturedTitle == task.Title && tokenDisplayDue(previous, cfg) {
				checkpoint.Results[task.TaskID] = state.ClassificationResult{
					TaskID: task.TaskID, Revision: task.Revision, Status: previous.Status, Provenance: previous.Provenance,
					DurableSubject: previous.DurableSubject, ManagedAction: previous.ManagedAction,
				}
			}
		}
		for _, task := range archiveDue {
			previous := committed.Tasks[task.TaskID]
			if previous.CapturedRevision == task.Revision && previous.CapturedTitle == task.Title {
				checkpoint.Results[task.TaskID] = state.ClassificationResult{TaskID: task.TaskID, Revision: task.Revision, Status: previous.Status, Provenance: previous.Provenance, DurableSubject: previous.DurableSubject, ManagedAction: previous.ManagedAction}
			}
		}
		if err := r.deps.Store.SaveCycle(checkpoint); err != nil {
			return output.HeartbeatResult{CycleID: checkpoint.CycleID, ErrorCode: "cycle_write_failed"}, err
		}
	}

	result := output.HeartbeatResult{CycleID: checkpoint.CycleID}
	pendingUpdate := UpdateStatus{}
	if updateDue {
		committed.LastUpdateCheck = &now
		if r.deps.UpdateChecker == nil {
			result.ErrorCode = "update_checker_unavailable"
		} else {
			checked, checkErr := r.deps.UpdateChecker.Check(ctx, r.deps.InstalledVersion)
			if checkErr == nil && checked.Newer && checked.LatestVersion != "" && !contains(committed.DeliveredNoticeVersions, checked.LatestVersion) && !checkpointHasNotice(checkpoint, checked.LatestVersion) {
				pendingUpdate = checked
			}
		}
	}
	needClient := len(comparison.Changed) > 0 || pendingUpdate.Newer || len(checkpoint.Operations) > 0
	var client AppServer
	closeClient := func() {}
	if needClient {
		client, closeClient, err = r.lazyClient(ctx)
		if err != nil {
			result.ErrorCode = "app_server_start_failed"
			return result, err
		}
	}
	defer closeClient()

	taskByID := make(map[string]codex.Task, len(inventory.Tasks))
	for _, task := range inventory.Tasks {
		taskByID[task.TaskID] = task
	}
	if err := r.recoverNotices(ctx, cfg, &checkpoint, client, &result); err != nil {
		return result, err
	}
	r.recoverOperations(&checkpoint, inventory)
	pruneRemovedCaptured(&checkpoint, inventory)

	if pendingUpdate.Newer {
		if err := r.deliverUpdate(ctx, cfg, pendingUpdate, &checkpoint, client, &result); err != nil {
			return result, err
		}
	}

	unresolved := make([]status.TaskEvidence, 0)
	for _, task := range comparison.Changed {
		captured, ok := checkpoint.Inventory[task.TaskID]
		if !ok || captured.Revision != task.Revision || captured.Title != task.Title {
			continue
		}
		if _, ok := checkpoint.Results[task.TaskID]; ok {
			continue
		}
		if client == nil {
			r.setDiagnostic(&checkpoint, task.TaskID, "evidence", "app_server_unavailable")
			checkpoint.Results[task.TaskID] = unknownResult(task, state.ProvenanceUnknown)
			continue
		}
		evidence, readErr := client.ReadLatestTurn(ctx, task.TaskID, task.RolloutPath)
		if readErr != nil {
			r.setDiagnostic(&checkpoint, task.TaskID, "evidence", "evidence_read_failed")
			checkpoint.Results[task.TaskID] = unknownResult(task, state.ProvenanceUnknown)
			continue
		}
		choice := selectTask(task, evidence, now)
		captured.LastSubstantiveActivity = laterActivity(captured.LastSubstantiveActivity, choice.Activity, now)
		checkpoint.Inventory[task.TaskID] = captured
		if choice.Resolution.Resolved {
			checkpoint.Results[task.TaskID] = state.ClassificationResult{TaskID: task.TaskID, Revision: task.Revision, Status: choice.Resolution.Status, Provenance: choice.Resolution.Provenance, ManagedAction: choice.Resolution.ManagedAction}
		} else {
			unresolved = append(unresolved, taskEvidence(choice))
		}
	}
	if err := r.deps.Store.SaveCycle(checkpoint); err != nil {
		result.ErrorCode = "cycle_write_failed"
		return result, err
	}

	if len(unresolved) > 0 {
		if cfg.ClassifierContextBudgetBytes <= 0 {
			for _, task := range unresolved {
				checkpoint.Results[task.TaskID] = state.ClassificationResult{TaskID: task.TaskID, Revision: task.Revision, Status: state.StatusUnknown, Provenance: state.ProvenanceUnknown}
				r.setDiagnostic(&checkpoint, task.TaskID, "classifier", "invalid_context_budget")
			}
		} else if r.deps.NewClassifier == nil || client == nil {
			for _, task := range unresolved {
				checkpoint.Results[task.TaskID] = state.ClassificationResult{TaskID: task.TaskID, Revision: task.Revision, Status: state.StatusUnknown, Provenance: state.ProvenanceUnknown}
				r.setDiagnostic(&checkpoint, task.TaskID, "classifier", "classifier_unavailable")
			}
		} else {
			classifier, classifierErr := r.deps.NewClassifier(client, cfg)
			if classifierErr != nil {
				for _, task := range unresolved {
					checkpoint.Results[task.TaskID] = state.ClassificationResult{TaskID: task.TaskID, Revision: task.Revision, Status: state.StatusUnknown, Provenance: state.ProvenanceUnknown}
					r.setDiagnostic(&checkpoint, task.TaskID, "classifier", "classifier_unavailable")
				}
			} else {
				classified := classifier.ClassifyWithPrevious(ctx, unresolved, func(loadCtx context.Context, requested []status.TaskEvidence) []status.PreviousEvidenceResult {
					loaded := make([]status.PreviousEvidenceResult, 0, len(requested))
					for _, request := range requested {
						task := taskByID[request.TaskID]
						previous, previousErr := client.ReadPreviousTurn(loadCtx, request.TaskID, task.RolloutPath)
						if previousErr != nil || previous == nil {
							loaded = append(loaded, status.PreviousEvidenceResult{TaskID: request.TaskID, Revision: request.Revision, ErrorCode: "previous_evidence_read_failed"})
							continue
						}
						evidence := status.TurnEvidence{User: previous.UserMessage, FinalAgent: previous.AgentMessage}
						loaded = append(loaded, status.PreviousEvidenceResult{TaskID: request.TaskID, Revision: request.Revision, Evidence: &evidence})
					}
					return loaded
				})
				for _, classification := range classified {
					checkpoint.Results[classification.TaskID] = classification.StateResult()
					if classification.Diagnostic != nil {
						r.setDiagnostic(&checkpoint, classification.TaskID, "classifier", classification.Diagnostic.Code)
					}
				}
			}
		}
		if err := r.deps.Store.SaveCycle(checkpoint); err != nil {
			result.ErrorCode = "cycle_write_failed"
			return result, err
		}
	}

	records := r.prepareRecords(cfg, committed, checkpoint, now, &result)
	if err := r.prepareOperations(cfg, records, &checkpoint, now); err != nil {
		result.ErrorCode = "operation_prepare_failed"
		return result, err
	}
	if err := r.deps.Store.SaveCycle(checkpoint); err != nil {
		result.ErrorCode = "cycle_write_failed"
		return result, err
	}

	if err := r.applyOperations(ctx, cfg, client, &checkpoint, records, &result, now); err != nil {
		result.ErrorCode = "mutation_outcome_unknown"
		return result, err
	}
	appendRetryResults(&result, checkpoint.Diagnostics)
	next := r.commitState(cfg, committed, checkpoint, records, now)
	if err := r.deps.Store.SaveState(next); err != nil {
		result.ErrorCode = "state_write_failed"
		return result, err
	}
	if err := r.deps.Store.RemoveCycle(); err != nil {
		result.ErrorCode = "cycle_remove_failed"
		return result, err
	}
	if len(result.Retries) > 0 && result.ErrorCode == "" {
		result.ErrorCode = "partial_failure"
	}
	return result, nil
}

func (r *Runner) loadState() (state.State, error) {
	value, err := r.deps.Store.LoadState()
	if errors.Is(err, fs.ErrNotExist) {
		return state.New(), nil
	}
	return value, err
}

func (r *Runner) loadCheckpoint(committed state.State) (state.CycleCheckpoint, bool, error) {
	cycle, err := r.deps.Store.LoadCycle()
	if errors.Is(err, fs.ErrNotExist) {
		return state.CycleCheckpoint{}, false, nil
	}
	if err != nil {
		return state.CycleCheckpoint{}, false, err
	}
	if committed.Generation > cycle.BaseGeneration {
		if err := r.deps.Store.RemoveCycle(); err != nil {
			return state.CycleCheckpoint{}, false, err
		}
		return state.CycleCheckpoint{}, false, nil
	}
	if committed.Generation != cycle.BaseGeneration {
		return state.CycleCheckpoint{}, false, errors.New("cycle generation is ahead of committed state")
	}
	return cycle, true, nil
}

func dueChanged(tasks []codex.Task, committed state.State, now time.Time) []codex.Task {
	result := make([]codex.Task, 0, len(tasks))
	for _, task := range tasks {
		previous, ok := committed.Tasks[task.TaskID]
		if !ok || previous.CapturedRevision != task.Revision || previous.CapturedTitle != task.Title || previous.Retry == nil || !now.Before(previous.Retry.NextAttemptAt) {
			result = append(result, task)
		}
	}
	return result
}

func archiveDueTasks(inventory codex.Inventory, committed state.State, cfg config.Config, now time.Time) []codex.Task {
	if !cfg.ArchiveEnabled {
		return nil
	}
	result := make([]codex.Task, 0)
	for _, task := range inventory.Tasks {
		record, ok := committed.Tasks[task.TaskID]
		if !ok || record.Retry != nil && now.Before(record.Retry.NextAttemptAt) {
			continue
		}
		if archiveEligible(record, now, cfg.ArchiveAfterDays) {
			result = append(result, task)
		}
	}
	return result
}

func tokenDisplayDueTasks(inventory codex.Inventory, committed state.State, cfg config.Config) []codex.Task {
	if !cfg.RenameEnabled {
		return nil
	}
	result := make([]codex.Task, 0)
	for _, task := range inventory.Tasks {
		if previous, ok := committed.Tasks[task.TaskID]; ok && tokenDisplayDue(previous, cfg) {
			result = append(result, task)
		}
	}
	return result
}

func tokenDisplayDue(record state.TaskRecord, cfg config.Config) bool {
	applied := record.TokenDisplayPosition
	if applied == "" {
		applied = tokens.PositionOff
	}
	return cfg.RenameEnabled && applied != cfg.TokenDisplay
}

func mergeChanged(left, right []codex.Task) []codex.Task {
	byID := make(map[string]codex.Task, len(left)+len(right))
	for _, task := range left {
		byID[task.TaskID] = task
	}
	for _, task := range right {
		byID[task.TaskID] = task
	}
	result := make([]codex.Task, 0, len(byID))
	for _, task := range byID {
		result = append(result, task)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].TaskID < result[j].TaskID })
	return result
}

func tokenSnapshot(record state.TaskRecord) tokens.Snapshot {
	return tokens.Snapshot{
		RolloutPath:  record.TokenRolloutPath,
		Offset:       record.TokenReadOffset,
		Size:         record.TokenRolloutSize,
		OutputTokens: record.OutputTokens,
		TotalTokens:  record.TotalTokens,
		Found:        record.TokenUsageFound,
	}
}

func applyTokenSnapshot(record *state.TaskRecord, snapshot tokens.Snapshot) {
	record.TokenRolloutPath = snapshot.RolloutPath
	record.TokenReadOffset = snapshot.Offset
	record.TokenRolloutSize = snapshot.Size
	record.OutputTokens = snapshot.OutputTokens
	record.TotalTokens = snapshot.TotalTokens
	record.TokenUsageFound = snapshot.Found
}

func clearTokenSnapshot(record *state.TaskRecord) {
	record.TokenRolloutPath = ""
	record.TokenReadOffset = 0
	record.TokenRolloutSize = 0
	record.OutputTokens = 0
	record.TotalTokens = 0
	record.TokenUsageFound = false
}

func dryRunResult(comparison codex.Comparison, updateDue bool) output.Result {
	effects := make([]string, 0, len(comparison.Changed)+len(comparison.RemovedIDs)+1)
	for _, task := range comparison.Changed {
		effects = append(effects, "classify."+task.TaskID)
	}
	for _, taskID := range comparison.RemovedIDs {
		effects = append(effects, "remove."+taskID)
	}
	if updateDue {
		effects = append(effects, "update_check")
	}
	return output.PreviewResult{Command: "heartbeat", Effects: effects}
}

func (r *Runner) lazyClient(ctx context.Context) (AppServer, func(), error) {
	if r.deps.AppServer == nil {
		return nil, func() {}, nil
	}
	client, err := r.deps.AppServer.Open(ctx)
	if err != nil {
		return nil, func() {}, err
	}
	return client, func() { client.Close() }, nil
}

func unknownResult(task codex.Task, provenance state.Provenance) state.ClassificationResult {
	return state.ClassificationResult{TaskID: task.TaskID, Revision: task.Revision, Status: state.StatusUnknown, Provenance: provenance}
}

func (r *Runner) setDiagnostic(checkpoint *state.CycleCheckpoint, taskID, operation, code string) {
	checkpoint.Diagnostics[taskID] = state.CycleDiagnostic{TaskID: taskID, Operation: operation, ErrorCode: stableCode(code)}
}

func stableCode(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown_failure"
	}
	return value
}

func (r *Runner) deliverUpdate(ctx context.Context, cfg config.Config, update UpdateStatus, checkpoint *state.CycleCheckpoint, client AppServer, result *output.HeartbeatResult) error {
	key := "notice:" + update.LatestVersion
	checkpoint.Operations[key] = state.CycleOperation{Kind: state.OperationNotice, Stage: state.StagePrepared, NoticeVersion: update.LatestVersion}
	if err := r.deps.Store.SaveCycle(*checkpoint); err != nil {
		result.ErrorCode = "cycle_write_failed"
		return err
	}
	return r.settleNotice(ctx, cfg, key, checkpoint, client, result)
}

func (r *Runner) recoverNotices(ctx context.Context, cfg config.Config, checkpoint *state.CycleCheckpoint, client AppServer, result *output.HeartbeatResult) error {
	keys := make([]string, 0)
	for key, operation := range checkpoint.Operations {
		if operation.Kind == state.OperationNotice && operation.Stage != state.StageVerified {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		if err := r.settleNotice(ctx, cfg, key, checkpoint, client, result); err != nil {
			return err
		}
	}
	return nil
}

func (r *Runner) settleNotice(ctx context.Context, cfg config.Config, key string, checkpoint *state.CycleCheckpoint, client AppServer, result *output.HeartbeatResult) error {
	if client == nil {
		result.ErrorCode = "app_server_unavailable"
		return errors.New("App Server is unavailable")
	}
	op := checkpoint.Operations[key]
	text := noticeText(op.NoticeVersion)
	delivered, err := noticeDelivered(ctx, client, cfg.ControlTaskID, text)
	if err != nil {
		result.ErrorCode = "update_notice_verify_failed"
		return err
	}
	if !delivered {
		op.Stage = state.StageApplying
		checkpoint.Operations[key] = op
		if err := r.deps.Store.SaveCycle(*checkpoint); err != nil {
			result.ErrorCode = "cycle_write_failed"
			return err
		}
		if err := client.InsertNotice(ctx, cfg.ControlTaskID, text); err != nil {
			result.ErrorCode = "update_notice_failed"
			return err
		}
		op.Stage = state.StageApplied
		checkpoint.Operations[key] = op
		if err := r.deps.Store.SaveCycle(*checkpoint); err != nil {
			result.ErrorCode = "cycle_write_failed"
			return err
		}
		delivered, err = noticeDelivered(ctx, client, cfg.ControlTaskID, text)
		if err != nil || !delivered {
			result.ErrorCode = "update_notice_verify_failed"
			if err != nil {
				return err
			}
			return errors.New("update notice was not visible after insertion")
		}
	}
	op.Stage = state.StageVerified
	checkpoint.Operations[key] = op
	if err := r.deps.Store.SaveCycle(*checkpoint); err != nil {
		result.ErrorCode = "cycle_write_failed"
		return err
	}
	result.Changed = append(result.Changed, output.TaskChange{TaskID: cfg.ControlTaskID, State: state.StatusUnknown})
	return nil
}

func noticeText(version string) string {
	return fmt.Sprintf("🧵🐻 ThreadBear %s is ready. Run threadbear update, or tell me “update ThreadBear.”", version)
}

func noticeDelivered(ctx context.Context, client AppServer, controlTaskID, text string) (bool, error) {
	result, err := client.ReadPersistedAssistantMessage(ctx, controlTaskID, text)
	return result.Found, err
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func checkpointHasNotice(checkpoint state.CycleCheckpoint, version string) bool {
	for _, operation := range checkpoint.Operations {
		if operation.Kind == state.OperationNotice && operation.NoticeVersion == version {
			return true
		}
	}
	return false
}

func pruneRemovedCaptured(checkpoint *state.CycleCheckpoint, inventory codex.Inventory) {
	for taskID := range checkpoint.Inventory {
		if _, exists := findTask(inventory, taskID); exists || hasVerifiedArchive(*checkpoint, taskID) {
			continue
		}
		delete(checkpoint.Inventory, taskID)
		delete(checkpoint.Results, taskID)
		delete(checkpoint.Diagnostics, taskID)
	}
}

func hasVerifiedArchive(checkpoint state.CycleCheckpoint, taskID string) bool {
	for _, operation := range checkpoint.Operations {
		if operation.Kind == state.OperationArchive && operation.Stage == state.StageVerified && operation.TaskID == taskID {
			return true
		}
	}
	return false
}

func (r *Runner) recoverOperations(checkpoint *state.CycleCheckpoint, inventory codex.Inventory) {
	for key, operation := range checkpoint.Operations {
		if operation.Stage == state.StageVerified {
			continue
		}
		task, exists := findTask(inventory, operation.TaskID)
		switch operation.Kind {
		case state.OperationTitle:
			if exists && task.Title == operation.DesiredTitle {
				operation.Stage = state.StageVerified
				operation.VerifiedRevision = task.Revision
				operation.VerifiedTitle = task.Title
				checkpoint.Operations[key] = operation
			}
		case state.OperationArchive:
			if !exists && operation.Stage == state.StageApplied {
				operation.Stage = state.StageVerified
				checkpoint.Operations[key] = operation
			} else if !exists {
				delete(checkpoint.Operations, key)
				delete(checkpoint.Inventory, operation.TaskID)
				delete(checkpoint.Results, operation.TaskID)
				delete(checkpoint.Diagnostics, operation.TaskID)
			}
		}
	}
}

func (r *Runner) prepareRecords(cfg config.Config, committed state.State, checkpoint state.CycleCheckpoint, now time.Time, result *output.HeartbeatResult) map[string]state.TaskRecord {
	records := make(map[string]state.TaskRecord, len(checkpoint.Inventory))
	for taskID, captured := range checkpoint.Inventory {
		classification, ok := checkpoint.Results[taskID]
		previous, hadPrevious := committed.Tasks[taskID]
		if !ok {
			if hadPrevious {
				previous.CapturedRevision = captured.Revision
				previous.CapturedTitle = captured.Title
				records[taskID] = previous
			}
			continue
		}
		activity := laterActivity(previous.LastSubstantiveActivity, captured.LastSubstantiveActivity, now)
		if _, restored := committed.Archives[taskID]; restored {
			activity = now
			result.RestoredIDs = append(result.RestoredIDs, taskID)
		}
		started := previous.StateStartedAt
		if !hadPrevious || previous.Status != classification.Status || started.IsZero() {
			started = now
		}
		durableSubject := classification.DurableSubject
		managedAction := ""
		lastAppliedTitle := ""
		if hadPrevious {
			durableSubject = previous.DurableSubject
			managedAction = previous.ManagedAction
			lastAppliedTitle = previous.LastAppliedTitle
		}
		records[taskID] = state.TaskRecord{
			TaskID: taskID, CapturedRevision: captured.Revision, CapturedTitle: captured.Title,
			Status: classification.Status, Provenance: classification.Provenance,
			StateStartedAt: started, LastSubstantiveActivity: activity,
			DurableSubject: durableSubject, ManagedAction: managedAction,
			LastAppliedTitle: lastAppliedTitle,
		}
		record := records[taskID]
		if hadPrevious {
			record.ManagedTokenDisplay = previous.ManagedTokenDisplay
			record.ManagedTokenPosition = previous.ManagedTokenPosition
			record.TokenDisplayPosition = previous.TokenDisplayPosition
			record.TokenRolloutPath = previous.TokenRolloutPath
			record.TokenReadOffset = previous.TokenReadOffset
			record.TokenRolloutSize = previous.TokenRolloutSize
			record.OutputTokens = previous.OutputTokens
			record.TotalTokens = previous.TotalTokens
			record.TokenUsageFound = previous.TokenUsageFound
		}
		if cfg.RenameEnabled {
			record.TokenDisplayPosition = cfg.TokenDisplay
			if cfg.TokenDisplay != tokens.PositionOff {
				if captured.RolloutPath == "" {
					clearTokenSnapshot(&record)
				} else {
					snapshot, readErr := r.deps.TokenReader.ReadRollout(captured.RolloutPath, tokenSnapshot(record))
					if readErr != nil {
						clearTokenSnapshot(&record)
					} else {
						applyTokenSnapshot(&record, snapshot)
					}
				}
			}
		}
		records[taskID] = record
	}
	return records
}

func (r *Runner) prepareOperations(cfg config.Config, records map[string]state.TaskRecord, checkpoint *state.CycleCheckpoint, now time.Time) error {
	ids := make([]string, 0, len(records))
	for taskID := range records {
		ids = append(ids, taskID)
	}
	sort.Strings(ids)
	for _, taskID := range ids {
		record := records[taskID]
		classification, classified := checkpoint.Results[taskID]
		if cfg.RenameEnabled && classified {
			display := tokens.Display{}
			if record.TokenUsageFound && cfg.TokenDisplay != tokens.PositionOff {
				display = tokens.Display{Position: cfg.TokenDisplay, Value: tokens.Format(record.OutputTokens)}
			}
			rendered, err := title.Reconcile(record, record.Status, classification.DurableSubject, classification.ManagedAction, display)
			if err != nil {
				r.setDiagnostic(checkpoint, taskID, "title", "title_reconcile_failed")
			} else {
				record.DurableSubject = rendered.DurableSubject
				record.ManagedAction = rendered.ManagedAction
				record.ManagedTokenDisplay = rendered.ManagedTokenDisplay
				record.ManagedTokenPosition = rendered.ManagedTokenPosition
				records[taskID] = record
				if rendered.Title != record.CapturedTitle {
					key := "title:" + taskID
					if _, exists := checkpoint.Operations[key]; !exists {
						checkpoint.Operations[key] = state.CycleOperation{Kind: state.OperationTitle, Stage: state.StagePrepared, TaskID: taskID, ExpectedRevision: record.CapturedRevision, ExpectedTitle: record.CapturedTitle, DesiredTitle: rendered.Title}
					}
				}
			}
		}
		if cfg.ArchiveEnabled && archiveEligible(record, now, cfg.ArchiveAfterDays) {
			key := "archive:" + taskID
			if _, exists := checkpoint.Operations[key]; !exists {
				checkpoint.Operations[key] = state.CycleOperation{Kind: state.OperationArchive, Stage: state.StagePrepared, TaskID: taskID, ExpectedRevision: record.CapturedRevision, ExpectedTitle: record.CapturedTitle}
			}
		}
	}
	return nil
}

func (r *Runner) applyOperations(ctx context.Context, cfg config.Config, client AppServer, checkpoint *state.CycleCheckpoint, records map[string]state.TaskRecord, result *output.HeartbeatResult, now time.Time) error {
	keys := make([]string, 0, len(checkpoint.Operations))
	for key := range checkpoint.Operations {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		left, right := checkpoint.Operations[keys[i]], checkpoint.Operations[keys[j]]
		if left.Kind == right.Kind {
			return keys[i] < keys[j]
		}
		return left.Kind == state.OperationTitle
	})
	for _, key := range keys {
		op := checkpoint.Operations[key]
		if op.Kind == state.OperationNotice || op.Stage == state.StageVerified {
			continue
		}
		if client == nil {
			r.setDiagnostic(checkpoint, op.TaskID, string(op.Kind), "app_server_unavailable")
			continue
		}
		current, fresh, err := revalidate(ctx, r.deps.Inventory, cfg.ControlTaskID, op)
		if err != nil {
			r.setDiagnostic(checkpoint, op.TaskID, string(op.Kind), "revalidation_failed")
			continue
		}
		if !fresh {
			continue
		}
		switch op.Kind {
		case state.OperationTitle:
			op.Stage = state.StageApplying
			checkpoint.Operations[key] = op
			if err := r.deps.Store.SaveCycle(*checkpoint); err != nil {
				return err
			}
			if err := client.SetTitle(ctx, op.TaskID, op.DesiredTitle); err != nil {
				op.Stage = state.StagePrepared
				checkpoint.Operations[key] = op
				r.setDiagnostic(checkpoint, op.TaskID, "title", "title_write_failed")
				continue
			}
			op.Stage = state.StageApplied
			checkpoint.Operations[key] = op
			if err := r.deps.Store.SaveCycle(*checkpoint); err != nil {
				return err
			}
			verified, verifyErr := r.deps.Inventory.Inventory(ctx, cfg.ControlTaskID)
			if verifyErr != nil {
				return verifyErr
			}
			row, ok := findTask(verified, op.TaskID)
			if !ok || row.Title != op.DesiredTitle {
				return errors.New("title mutation was not visible after application")
			}
			op.Stage = state.StageVerified
			op.VerifiedRevision = row.Revision
			op.VerifiedTitle = row.Title
			checkpoint.Operations[key] = op
			record := records[op.TaskID]
			record.CapturedRevision = row.Revision
			record.CapturedTitle = row.Title
			record.LastAppliedTitle = row.Title
			records[op.TaskID] = record
			result.Changed = append(result.Changed, output.TaskChange{TaskID: op.TaskID, State: record.Status})
			archiveKey := "archive:" + op.TaskID
			if archive, exists := checkpoint.Operations[archiveKey]; exists && archive.Stage == state.StagePrepared {
				archive.ExpectedRevision = row.Revision
				archive.ExpectedTitle = row.Title
				checkpoint.Operations[archiveKey] = archive
			}
		case state.OperationArchive:
			evidence, evidenceErr := client.ReadLatestTurn(ctx, op.TaskID, current.RolloutPath)
			if evidenceErr != nil {
				r.setDiagnostic(checkpoint, op.TaskID, "archive", "archive_activity_read_failed")
				continue
			}
			record := records[op.TaskID]
			observedActivity := record.LastSubstantiveActivity
			if evidence.RecencyAt != nil && *evidence.RecencyAt > 0 {
				observedActivity = time.Unix(*evidence.RecencyAt, 0).UTC()
			}
			if observedActivity.After(record.LastSubstantiveActivity) {
				record.LastSubstantiveActivity = observedActivity
				records[op.TaskID] = record
				captured := checkpoint.Inventory[op.TaskID]
				captured.LastSubstantiveActivity = observedActivity
				checkpoint.Inventory[op.TaskID] = captured
				delete(checkpoint.Operations, key)
				if err := r.deps.Store.SaveCycle(*checkpoint); err != nil {
					return err
				}
				continue
			}
			op.Stage = state.StageApplying
			checkpoint.Operations[key] = op
			if err := r.deps.Store.SaveCycle(*checkpoint); err != nil {
				return err
			}
			if archiveErr := client.Archive(ctx, op.TaskID); archiveErr != nil {
				verified, verifyErr := r.deps.Inventory.Inventory(ctx, cfg.ControlTaskID)
				if verifyErr != nil {
					return verifyErr
				}
				if _, exists := findTask(verified, op.TaskID); exists {
					op.Stage = state.StagePrepared
					checkpoint.Operations[key] = op
					r.setDiagnostic(checkpoint, op.TaskID, "archive", "archive_write_failed")
					continue
				}
				return archiveErr
			}
			op.Stage = state.StageApplied
			checkpoint.Operations[key] = op
			if err := r.deps.Store.SaveCycle(*checkpoint); err != nil {
				return err
			}
			verified, verifyErr := r.deps.Inventory.Inventory(ctx, cfg.ControlTaskID)
			if verifyErr != nil {
				return verifyErr
			}
			if _, exists := findTask(verified, op.TaskID); exists {
				return errors.New("archive mutation was not visible after application")
			}
			op.Stage = state.StageVerified
			checkpoint.Operations[key] = op
			result.ArchivedIDs = append(result.ArchivedIDs, op.TaskID)
		}

		if err := r.deps.Store.SaveCycle(*checkpoint); err != nil {
			return err
		}
	}
	return nil
}

func appendRetryResults(result *output.HeartbeatResult, diagnostics map[string]state.CycleDiagnostic) {
	for _, diagnostic := range diagnostics {
		result.Retries = append(result.Retries, output.RetryResult{TaskID: diagnostic.TaskID, Operation: diagnostic.Operation, ErrorCode: diagnostic.ErrorCode})
	}
}

func (r *Runner) commitState(cfg config.Config, committed state.State, checkpoint state.CycleCheckpoint, records map[string]state.TaskRecord, now time.Time) state.State {
	next := committed
	next.Generation++
	next.LastCompletedHeartbeat = &now
	for _, operation := range checkpoint.Operations {
		if operation.Kind != state.OperationTitle || operation.Stage != state.StageVerified {
			continue
		}
		record, ok := records[operation.TaskID]
		if !ok {
			continue
		}
		record.CapturedRevision = operation.VerifiedRevision
		record.CapturedTitle = operation.VerifiedTitle
		record.LastAppliedTitle = operation.VerifiedTitle
		records[operation.TaskID] = record
	}
	next.Tasks = make(map[string]state.TaskRecord, len(records))
	for taskID, record := range records {
		next.Tasks[taskID] = record
	}
	for taskID := range next.Archives {
		if _, restored := checkpoint.Inventory[taskID]; restored {
			delete(next.Archives, taskID)
		}
	}
	for key, operation := range checkpoint.Operations {
		_ = key
		if operation.Stage != state.StageVerified {
			continue
		}
		switch operation.Kind {
		case state.OperationArchive:
			record := records[operation.TaskID]
			delete(next.Tasks, operation.TaskID)
			next.Archives[operation.TaskID] = state.ArchiveRecord{TaskID: operation.TaskID, ArchivedAt: now, CapturedRevision: record.CapturedRevision, StateGeneration: next.Generation}
		case state.OperationNotice:
			if !contains(next.DeliveredNoticeVersions, operation.NoticeVersion) {
				next.DeliveredNoticeVersions = append(next.DeliveredNoticeVersions, operation.NoticeVersion)
			}
		}
	}
	for taskID, diagnostic := range checkpoint.Diagnostics {
		record, ok := next.Tasks[taskID]
		if !ok {
			continue
		}
		attempts := uint32(1)
		if previous, exists := committed.Tasks[taskID]; exists && previous.Retry != nil {
			attempts = previous.Retry.Attempts + 1
		}
		record.Retry = &state.Retry{Operation: diagnostic.Operation, ErrorCode: diagnostic.ErrorCode, Attempts: attempts, LastAttemptAt: now, NextAttemptAt: now.Add(time.Duration(max(1, cfg.HeartbeatSeconds)) * time.Second)}
		next.Tasks[taskID] = record
	}
	for taskID := range checkpoint.Results {
		if _, failed := checkpoint.Diagnostics[taskID]; failed {
			continue
		}
		record, ok := next.Tasks[taskID]
		if ok {
			record.Retry = nil
			next.Tasks[taskID] = record
		}
	}
	return next
}
