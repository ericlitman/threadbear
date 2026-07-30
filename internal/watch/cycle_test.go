package watch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	appservice "github.com/ericlitman/threadbear/internal/app"
	"github.com/ericlitman/threadbear/internal/codex"
	"github.com/ericlitman/threadbear/internal/codex/appserver"
	"github.com/ericlitman/threadbear/internal/config"
	"github.com/ericlitman/threadbear/internal/output"
	"github.com/ericlitman/threadbear/internal/state"
	"github.com/ericlitman/threadbear/internal/status"
	"github.com/ericlitman/threadbear/internal/tokens"
	updatepkg "github.com/ericlitman/threadbear/internal/update"
)

type fakeClock struct{ now time.Time }

func (f fakeClock) Now() time.Time { return f.now }

type fakeIndex struct {
	tasks  []codex.Task
	calls  int
	hook   func(int, *fakeIndex)
	events *[]string
}

func (f *fakeIndex) Inventory(_ context.Context, controlTaskID string) (codex.Inventory, error) {
	f.calls++
	if f.events != nil {
		*f.events = append(*f.events, "inventory")
	}
	if f.hook != nil {
		f.hook(f.calls, f)
	}
	tasks := make([]codex.Task, 0, len(f.tasks))
	for _, task := range f.tasks {
		if task.TaskID != controlTaskID {
			tasks = append(tasks, task)
		}
	}
	return codex.Inventory{Tasks: tasks}, nil
}

func (f *fakeIndex) Task(_ context.Context, taskID string) (codex.Task, error) {
	task, ok := f.task(taskID)
	if !ok {
		return codex.Task{}, errors.New("missing task")
	}
	return task, nil
}

func (f *fakeIndex) task(taskID string) (codex.Task, bool) {
	return findTask(codex.Inventory{Tasks: f.tasks}, taskID)
}

func (f *fakeIndex) replace(task codex.Task) {
	for index := range f.tasks {
		if f.tasks[index].TaskID == task.TaskID {
			f.tasks[index] = task
			return
		}
	}
}

func (f *fakeIndex) remove(taskID string) {
	for index := range f.tasks {
		if f.tasks[index].TaskID == taskID {
			f.tasks = append(f.tasks[:index], f.tasks[index+1:]...)
			return
		}
	}
}

type fakeClient struct {
	index                  *fakeIndex
	latest                 map[string]appserver.RecentEvidence
	previous               map[string]*appserver.EvidenceTurn
	latestReads            []string
	previousReads          []string
	persistedReads         []string
	persisted              map[string][]string
	titles                 []string
	archives               []string
	notices                []string
	failTitle              map[string]bool
	failNotice             bool
	archiveErrorAfterApply bool
	events                 *[]string
}

func (f *fakeClient) ReadLatestTurn(_ context.Context, taskID, _ string) (appserver.RecentEvidence, error) {
	f.latestReads = append(f.latestReads, taskID)
	value, ok := f.latest[taskID]
	if !ok && taskID == "control" {
		return appserver.RecentEvidence{}, nil
	}
	if !ok {
		return appserver.RecentEvidence{}, errors.New("missing evidence")
	}
	return value, nil
}
func (f *fakeClient) ReadPreviousTurn(_ context.Context, taskID, _ string) (*appserver.EvidenceTurn, error) {
	f.previousReads = append(f.previousReads, taskID)
	return f.previous[taskID], nil
}
func (f *fakeClient) ReadPersistedAssistantMessage(_ context.Context, taskID, text string) (appserver.PersistedMessageResult, error) {
	f.persistedReads = append(f.persistedReads, taskID)
	for _, message := range f.persisted[taskID] {
		if message == text {
			return appserver.PersistedMessageResult{Found: true}, nil
		}
	}
	return appserver.PersistedMessageResult{}, nil
}
func (f *fakeClient) SetTitle(_ context.Context, taskID, value string) error {
	if f.events != nil {
		*f.events = append(*f.events, "set_title")
	}
	if f.failTitle[taskID] {
		return errors.New("synthetic title failure")
	}
	task, ok := f.index.task(taskID)
	if !ok {
		return errors.New("missing task")
	}
	task.Title = value
	task.Revision += "t"
	f.index.replace(task)
	f.titles = append(f.titles, taskID+":"+value)
	return nil
}
func (f *fakeClient) Archive(_ context.Context, taskID string) error {
	if f.events != nil {
		*f.events = append(*f.events, "archive")
	}
	f.archives = append(f.archives, taskID)
	f.index.remove(taskID)
	if f.archiveErrorAfterApply {
		return errors.New("archive response lost")
	}
	return nil
}
func (f *fakeClient) InsertNotice(_ context.Context, taskID string, text string) error {
	f.notices = append(f.notices, text)
	f.persisted[taskID] = append(f.persisted[taskID], text)
	if f.failNotice {
		return errors.New("notice response lost")
	}
	return nil
}
func (*fakeClient) Close() error { return nil }

type fakeFactory struct {
	client *fakeClient
	opens  int
}

func (f *fakeFactory) Open(context.Context) (AppServer, error) {
	f.opens++
	return f.client, nil
}

type fakeClassifierSessionFactory struct {
	opens          int
	closes         int
	cleanups       int
	orphanCleanups int
	openErr        error
	closeErr       error
	cleanupErr     error
}

type fakeClassifierSession struct{ factory *fakeClassifierSessionFactory }

func (f *fakeClassifierSessionFactory) Open(context.Context) (ClassifierSession, error) {
	f.opens++
	if f.openErr != nil {
		return nil, f.openErr
	}
	return &fakeClassifierSession{factory: f}, nil
}

func (f *fakeClassifierSessionFactory) Cleanup(string) error {
	f.cleanups++
	return f.cleanupErr
}

func (f *fakeClassifierSessionFactory) CleanupOrphans() error {
	f.orphanCleanups++
	return f.cleanupErr
}

func (s *fakeClassifierSession) RunEphemeral(context.Context, appserver.EphemeralRequest) (appserver.EphemeralResult, error) {
	return appserver.EphemeralResult{}, errors.New("unexpected direct ephemeral call")
}
func (*fakeClassifierSession) CleanupToken() string { return "test-classifier-cleanup-token" }
func (s *fakeClassifierSession) Close() error {
	s.factory.closes++
	return s.factory.closeErr
}

type fakeClassifier struct {
	calls         int
	requestPrev   []string
	results       map[string]status.Classification
	batchSizeOne  bool
	waitForCancel bool
	seen          []string
}

func (f *fakeClassifier) ClassifyWithProgress(ctx context.Context, tasks []status.TaskEvidence, load status.PreviousEvidenceLoader, _ status.ClassificationResume, observer status.ClassificationObserver) ([]status.Classification, error) {
	f.calls++
	if f.waitForCancel {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	total := 1
	if f.batchSizeOne {
		total = len(tasks)
	}
	if observer != nil {
		if err := observer(status.ClassificationBatchEvent{Pass: status.ClassificationPassFirst, Total: total}); err != nil {
			return nil, err
		}
	}
	if len(f.requestPrev) > 0 {
		requested := make([]status.TaskEvidence, 0, len(f.requestPrev))
		for _, task := range tasks {
			if contains(f.requestPrev, task.TaskID) {
				requested = append(requested, task)
			}
		}
		load(ctx, requested)
	}
	result := make([]status.Classification, 0, len(tasks))
	for _, task := range tasks {
		value, ok := f.results[task.TaskID]
		if !ok {
			value = status.Classification{TaskID: task.TaskID, Revision: task.Revision, Status: state.StatusComplete, Provenance: state.ProvenanceLuna, DurableSubject: task.TaskID}
		}
		result = append(result, value)
	}
	if observer != nil {
		if f.batchSizeOne {
			for index, classification := range result {
				f.seen = append(f.seen, classification.TaskID)
				if err := observer(status.ClassificationBatchEvent{Pass: status.ClassificationPassFirst, Total: total, Completed: index + 1, Classifications: []status.Classification{classification}}); err != nil {
					return nil, err
				}
			}
		} else {
			for _, classification := range result {
				f.seen = append(f.seen, classification.TaskID)
			}
			if err := observer(status.ClassificationBatchEvent{Pass: status.ClassificationPassFirst, Total: total, Completed: 1, Classifications: result}); err != nil {
				return nil, err
			}
		}
	} else {
		for _, classification := range result {
			f.seen = append(f.seen, classification.TaskID)
		}
	}
	return result, nil
}

type fakeUpdateChecker struct {
	calls  int
	result UpdateStatus
	err    error
}

func (f *fakeUpdateChecker) Check(context.Context, string) (UpdateStatus, error) {
	f.calls++
	return f.result, f.err
}

type fakeUpdater struct {
	calls   int
	targets []string
	result  updatepkg.Result
	err     error
	block   bool
}

func (f *fakeUpdater) Update(ctx context.Context, target string) (updatepkg.Result, error) {
	f.calls++
	f.targets = append(f.targets, target)
	if f.block {
		<-ctx.Done()
		return updatepkg.Result{}, ctx.Err()
	}
	return f.result, f.err
}

type fakeManagedSurfaces struct {
	calls     int
	agents    []bool
	enabled   []bool
	resources []string
	err       error
}

func (f *fakeManagedSurfaces) Repair(agentsEnabled bool) ([]string, error) {
	f.calls++
	f.agents = append(f.agents, agentsEnabled)
	f.enabled = append(f.enabled, agentsEnabled)
	if f.err != nil {
		return nil, f.err
	}
	return append([]string(nil), f.resources...), nil
}

type fakeTokenReader struct {
	calls     []string
	snapshots map[string]tokens.Snapshot
	errs      map[string]error
}

func (f *fakeTokenReader) ReadRollout(path string, _ tokens.Snapshot) (tokens.Snapshot, error) {
	f.calls = append(f.calls, path)
	if err := f.errs[path]; err != nil {
		return tokens.Snapshot{}, err
	}
	return f.snapshots[path], nil
}

type wrappedStore struct {
	store                 *state.Store
	configOverride        *config.Config
	failSaveState         bool
	failRemove            bool
	failCycleAfter        int
	failSemanticCompleted int
	failClassifierToken   bool
	cycleSaves            int
	events                *[]string
}

func (w *wrappedStore) LoadConfig() (config.Config, error) {
	if w.configOverride != nil {
		return *w.configOverride, nil
	}
	return w.store.LoadConfig()
}
func (w *wrappedStore) LoadState() (state.State, error)           { return w.store.LoadState() }
func (w *wrappedStore) LoadCycle() (state.CycleCheckpoint, error) { return w.store.LoadCycle() }
func (w *wrappedStore) AcquireLock() (*state.Lock, error)         { return w.store.AcquireLock() }
func (w *wrappedStore) SaveState(value state.State) error {
	if w.failSaveState {
		return errors.New("synthetic state crash")
	}
	return w.store.SaveState(value)
}
func (w *wrappedStore) SaveCycle(value state.CycleCheckpoint) error {
	w.cycleSaves++
	if w.events != nil {
		titleStage, archiveStage := "none", "none"
		if operation, ok := value.Operations["title:task-a"]; ok {
			titleStage = string(operation.Stage)
		}
		if operation, ok := value.Operations["archive:task-a"]; ok {
			archiveStage = string(operation.Stage)
		}
		*w.events = append(*w.events, "save:title="+titleStage+",archive="+archiveStage)
	}
	if w.failClassifierToken && value.ClassifierCleanupToken != "" {
		return errors.New("synthetic classifier token crash")
	}
	if w.failSemanticCompleted > 0 && value.Progress != nil && value.Progress.FirstPassBatchesCompleted >= w.failSemanticCompleted {
		return errors.New("synthetic semantic checkpoint crash")
	}
	if w.failCycleAfter > 0 && w.cycleSaves >= w.failCycleAfter {
		return errors.New("synthetic cycle crash")
	}
	return w.store.SaveCycle(value)
}
func (w *wrappedStore) RemoveCycle() error {
	if w.failRemove {
		return errors.New("synthetic remove crash")
	}
	return w.store.RemoveCycle()
}

func TestPruneRemovedCapturedClearsPreviousRequest(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	checkpoint := state.NewCycle("cycle", 0, now)
	checkpoint.Inventory["removed"] = state.CapturedTask{TaskID: "removed", Revision: "rev", Title: "Removed", LastSubstantiveActivity: now}
	checkpoint.PreviousRequested["removed"] = "rev"
	pruneRemovedCaptured(&checkpoint, codex.Inventory{})
	if _, ok := checkpoint.PreviousRequested["removed"]; ok {
		t.Fatal("removed task retained previous request")
	}
	if err := checkpoint.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestHeartbeatIdleZero(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	tasks := make([]codex.Task, 137)
	committed := state.New()
	committed.LastUpdateCheck = timePointer(now)
	for index := range tasks {
		id := taskID(index)
		tasks[index] = codex.Task{TaskID: id, Revision: "1", Title: "✅ " + id, Source: "vscode"}
		committed.Tasks[id] = record(tasks[index], state.StatusComplete, now)
	}
	runner, deps := testRunner(t, now, tasks, committed)
	result, err := appservice.NewWithHeartbeat("1.0.0", runner).Dispatch(context.Background(), appservice.Request{Command: appservice.CommandHeartbeat})
	if err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	if err := output.Write(&stdout, output.FormatJSON, result); err != nil {
		t.Fatal(err)
	}
	if stdout.Len() != 0 || deps.factory.opens != 0 || deps.classifierSessions.opens != 0 || deps.classifier.calls != 0 || deps.index.calls != 1 || deps.store.cycleSaves != 0 {
		t.Fatalf("stdout=%q opens=%d classifier_sessions=%d classifier=%d inventories=%d cycle_saves=%d", stdout.String(), deps.factory.opens, deps.classifierSessions.opens, deps.classifier.calls, deps.index.calls, deps.store.cycleSaves)
	}
}

func TestHeartbeatReportsAggregateDeterministicAndSemanticProgress(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	tasks := []codex.Task{{TaskID: "guided", Revision: "1", Title: "Guided", Source: "vscode"}, {TaskID: "legacy", Revision: "1", Title: "Legacy", Source: "vscode"}}
	committed := state.New()
	committed.LastUpdateCheck = timePointer(now)
	runner, deps := testRunner(t, now, tasks, committed)
	deps.client.latest["guided"] = completedEvidence(now, "finish", "done\n🧵🐻 complete")
	deps.client.latest["legacy"] = completedEvidence(now, "continue", "ambiguous result")
	value, err := runner.Run(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	result := value.(output.HeartbeatResult)
	if result.Progress == nil || result.Progress.InventoryTasks != 2 || result.Progress.LatestTurnReads != 2 || result.Progress.MechanicallyResolved != 1 || result.Progress.LunaCandidates != 1 || result.Progress.FirstPassBatchesTotal != 1 || result.Progress.FirstPassBatchesCompleted != 1 || result.Progress.Phase != state.SweepPhaseConverged {
		t.Fatalf("progress=%+v", result.Progress)
	}
	stored, err := deps.store.store.LoadState()
	if err != nil || stored.LastSweep == nil || stored.LastSweep.Phase != state.SweepPhaseConverged {
		t.Fatalf("last_sweep=%+v err=%v", stored.LastSweep, err)
	}
	data, err := json.Marshal(result.Progress)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{"guided", "legacy", "ambiguous result", "🧵🐻 complete"} {
		if strings.Contains(string(data), private) {
			t.Fatalf("progress leaked %q: %s", private, data)
		}
	}
}

func TestHeartbeatRendersOutputTokensAndLeavesUnchangedTitlesAlone(t *testing.T) {
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	task := codex.Task{TaskID: "task-a", Revision: "2", Title: "🚨 Release service", Source: "vscode", RolloutPath: "/synthetic/task-a.jsonl"}
	committed := state.New()
	committed.LastUpdateCheck = timePointer(now)
	previous := record(codex.Task{TaskID: task.TaskID, Revision: "1", Title: task.Title}, state.StatusBlocked, now)
	previous.TokenDisplayPosition = tokens.PositionStart
	committed.Tasks[task.TaskID] = previous
	runner, deps := testRunner(t, now, []codex.Task{task}, committed)
	deps.client.latest[task.TaskID] = appserver.RecentEvidence{ThreadStatus: appserver.ThreadStatus{Type: "idle"}, Latest: &appserver.EvidenceTurn{ID: "turn-a", Status: "failed", Error: &appserver.TurnError{Message: "synthetic failure"}}}
	deps.tokens.snapshots[task.RolloutPath] = tokens.Snapshot{RolloutPath: task.RolloutPath, Offset: 120, Size: 120, OutputTokens: 1_600_123, TotalTokens: 433_000_000, Found: true}
	if _, err := runner.Run(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	stored, _ := deps.store.store.LoadState()
	plan := stored.PendingTitlePlans[task.TaskID]
	if plan.DesiredTitle != "🚨 1.6m Release service" || plan.NativeOutcome != state.NativeTitlePending || len(deps.client.titles) != 0 || len(deps.tokens.calls) != 1 {
		t.Fatalf("plan=%+v writes=%v reads=%v", plan, deps.client.titles, deps.tokens.calls)
	}
	if _, err := runner.Run(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if len(deps.client.titles) != 0 || len(deps.tokens.calls) != 1 {
		t.Fatalf("writes=%v reads=%v", deps.client.titles, deps.tokens.calls)
	}
}

func TestHeartbeatClearsEmojiOnlyTitleReconcileRetry(t *testing.T) {
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	task := codex.Task{TaskID: "legacy", Revision: "1", Title: "✅", Source: "vscode"}
	committed := state.New()
	committed.LastUpdateCheck = timePointer(now)
	previous := record(task, state.StatusComplete, now)
	previous.Retry = &state.Retry{
		Operation: "title", ErrorCode: "title_reconcile_failed", Attempts: 1,
		LastAttemptAt: now.Add(-time.Minute), NextAttemptAt: now,
	}
	committed.Tasks[task.TaskID] = previous
	runner, deps := testRunner(t, now, []codex.Task{task}, committed)
	deps.client.latest[task.TaskID] = completedEvidence(now, "done", "🧵🐻 complete")

	value, err := runner.Run(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	stored, _ := deps.store.store.LoadState()
	if stored.Tasks[task.TaskID].Retry != nil || len(value.(output.HeartbeatResult).Retries) != 0 || len(deps.client.titles) != 0 {
		t.Fatalf("state=%+v result=%+v titles=%v", stored.Tasks[task.TaskID], value, deps.client.titles)
	}

	if _, err := runner.Run(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if len(deps.client.latestReads) != 1 {
		t.Fatalf("title reconciliation retried: latest_reads=%v", deps.client.latestReads)
	}
}

func TestHeartbeatRepositionsTokenDisplayWithoutReclassification(t *testing.T) {
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	task := codex.Task{TaskID: "task-a", Revision: "1", Title: "➡️ 1.6m Release service → review rollout", Source: "vscode", RolloutPath: "/synthetic/task-a.jsonl"}
	committed := state.New()
	committed.LastUpdateCheck = timePointer(now)
	previous := record(task, state.StatusNextSteps, now)
	previous.DurableSubject = "Release service"
	previous.ManagedAction = "review rollout"
	previous.ManagedTokenDisplay = "1.6m"
	previous.ManagedTokenPosition = tokens.PositionStart
	previous.TokenDisplayPosition = tokens.PositionStart
	previous.TokenUsageFound = true
	previous.OutputTokens = 1_600_123
	previous.TokenRolloutPath = task.RolloutPath
	previous.TokenReadOffset = 120
	previous.TokenRolloutSize = 120
	committed.Tasks[task.TaskID] = previous
	runner, deps := testRunner(t, now, []codex.Task{task}, committed)
	cfg := config.Default("control")
	cfg.TokenDisplay = tokens.PositionEnd
	deps.store.configOverride = &cfg
	deps.tokens.snapshots[task.RolloutPath] = tokens.Snapshot{RolloutPath: task.RolloutPath, Offset: 120, Size: 120, OutputTokens: 1_600_123, Found: true}
	if _, err := runner.Run(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	stored, _ := deps.store.store.LoadState()
	plan := stored.PendingTitlePlans[task.TaskID]
	if plan.DesiredTitle != "➡️ Release service → review rollout · 1.6m" || deps.classifier.calls != 0 || len(deps.client.latestReads) != 0 || len(deps.client.titles) != 0 {
		t.Fatalf("plan=%+v classifier=%d reads=%v writes=%v", plan, deps.classifier.calls, deps.client.latestReads, deps.client.titles)
	}
}

func TestHeartbeatCleansRepeatedOwnedPrefixWhenMovingDisplayToEnd(t *testing.T) {
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	task := codex.Task{TaskID: "task-a", Revision: "1", Title: "✅ 26k 26k 26k Execute BEAR-59", Source: "vscode", RolloutPath: "/synthetic/task-a.jsonl"}
	committed := state.New()
	committed.LastUpdateCheck = timePointer(now)
	previous := record(task, state.StatusComplete, now)
	previous.DurableSubject = "26k 26k Execute BEAR-59"
	previous.ManagedTokenDisplay = "20k"
	previous.ManagedTokenPosition = tokens.PositionStart
	previous.TokenDisplayPosition = tokens.PositionStart
	previous.TokenUsageFound = true
	previous.OutputTokens = 26_123
	previous.TokenRolloutPath = task.RolloutPath
	previous.TokenReadOffset = 120
	previous.TokenRolloutSize = 120
	committed.Tasks[task.TaskID] = previous
	runner, deps := testRunner(t, now, []codex.Task{task}, committed)
	cfg := config.Default("control")
	cfg.TokenDisplay = tokens.PositionEnd
	deps.store.configOverride = &cfg
	deps.tokens.snapshots[task.RolloutPath] = tokens.Snapshot{RolloutPath: task.RolloutPath, Offset: 120, Size: 120, OutputTokens: 26_123, Found: true}
	if _, err := runner.Run(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	stored, _ := deps.store.store.LoadState()
	plan := stored.PendingTitlePlans[task.TaskID]
	if plan.DesiredTitle != "✅ 26k 26k Execute BEAR-59 · 26k" || plan.DurableSubject != "26k 26k Execute BEAR-59" || plan.ManagedTokenDisplay != "26k" || plan.ManagedTokenPosition != tokens.PositionEnd || len(deps.client.titles) != 0 {
		t.Fatalf("plan=%+v writes=%v", plan, deps.client.titles)
	}
}

func TestHeartbeatUnreadableTokenTailRemovesFigureWithoutRetryNoise(t *testing.T) {
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	task := codex.Task{TaskID: "task-a", Revision: "2", Title: "🚨 1.6m Release service", Source: "vscode", RolloutPath: "/synthetic/unreadable.jsonl"}
	committed := state.New()
	committed.LastUpdateCheck = timePointer(now)
	previous := record(codex.Task{TaskID: task.TaskID, Revision: "1", Title: task.Title}, state.StatusBlocked, now)
	previous.DurableSubject = "Release service"
	previous.ManagedTokenDisplay = "1.6m"
	previous.ManagedTokenPosition = tokens.PositionStart
	previous.TokenDisplayPosition = tokens.PositionStart
	committed.Tasks[task.TaskID] = previous
	runner, deps := testRunner(t, now, []codex.Task{task}, committed)
	deps.tokens.errs[task.RolloutPath] = errors.New("synthetic unreadable tail")
	deps.client.latest[task.TaskID] = appserver.RecentEvidence{
		ThreadStatus: appserver.ThreadStatus{Type: "idle"},
		Latest:       &appserver.EvidenceTurn{ID: "turn-a", Status: "failed", Error: &appserver.TurnError{Message: "synthetic failure"}},
	}

	value, err := runner.Run(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	current, _ := deps.index.task(task.TaskID)
	stored, _ := deps.store.store.LoadState()
	if current.Title != task.Title || stored.PendingTitlePlans[task.TaskID].DesiredTitle != "🚨 Release service" || len(value.(output.HeartbeatResult).Retries) != 0 || len(deps.client.titles) != 0 {
		t.Fatalf("title=%q state=%+v result=%+v writes=%v", current.Title, stored.Tasks[task.TaskID], value, deps.client.titles)
	}
}

func TestHeartbeatMissingRolloutPathRemovesFigureWithoutRetryNoise(t *testing.T) {
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	task := codex.Task{TaskID: "task-a", Revision: "2", Title: "🚨 1.6m Release service", Source: "vscode"}
	committed := state.New()
	committed.LastUpdateCheck = timePointer(now)
	previous := record(codex.Task{TaskID: task.TaskID, Revision: "1", Title: task.Title}, state.StatusBlocked, now)
	previous.DurableSubject = "Release service"
	previous.ManagedTokenDisplay = "1.6m"
	previous.ManagedTokenPosition = tokens.PositionStart
	previous.TokenRolloutPath = "/synthetic/task-a.jsonl"
	previous.TokenReadOffset = 120
	previous.TokenRolloutSize = 120
	previous.OutputTokens = 1_600_123
	previous.TotalTokens = 433_000_000
	previous.TokenUsageFound = true
	committed.Tasks[task.TaskID] = previous
	runner, deps := testRunner(t, now, []codex.Task{task}, committed)
	deps.client.latest[task.TaskID] = appserver.RecentEvidence{
		ThreadStatus: appserver.ThreadStatus{Type: "idle"},
		Latest:       &appserver.EvidenceTurn{ID: "turn-a", Status: "failed", Error: &appserver.TurnError{Message: "synthetic failure"}},
	}

	value, err := runner.Run(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	current, _ := deps.index.task(task.TaskID)
	stored, err := deps.store.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	record := stored.Tasks[task.TaskID]
	if current.Title != task.Title || stored.PendingTitlePlans[task.TaskID].DesiredTitle != "🚨 Release service" || len(value.(output.HeartbeatResult).Retries) != 0 || len(deps.tokens.calls) != 0 || len(deps.client.titles) != 0 {
		t.Fatalf("title=%q state=%+v result=%+v token_reads=%v writes=%v", current.Title, record, value, deps.tokens.calls, deps.client.titles)
	}
	if record.ManagedTokenDisplay != "1.6m" || record.ManagedTokenPosition != tokens.PositionStart || record.TokenRolloutPath != "" || record.TokenReadOffset != 0 || record.TokenRolloutSize != 0 || record.OutputTokens != 0 || record.TotalTokens != 0 || record.TokenUsageFound {
		t.Fatalf("stale token snapshot retained: %+v", record)
	}
}

func TestHeartbeatDeterministicRuntimeAndPartialSiblingFailure(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	tasks := []codex.Task{{TaskID: "task-a", Revision: "2", Title: "✅ Active work", Source: "vscode"}, {TaskID: "task-b", Revision: "2", Title: "⏳ Finished work", Source: "vscode"}}
	committed := state.New()
	committed.LastUpdateCheck = timePointer(now)
	committed.Tasks["task-a"] = record(codex.Task{TaskID: "task-a", Revision: "1", Title: "✅ Active work"}, state.StatusComplete, now)
	committed.Tasks["task-b"] = record(codex.Task{TaskID: "task-b", Revision: "1", Title: "✅ Finished work"}, state.StatusComplete, now)
	runner, deps := testRunner(t, now, tasks, committed)
	activeAt := now.Unix()
	deps.client.latest["task-a"] = appserver.RecentEvidence{ThreadStatus: appserver.ThreadStatus{Type: "active"}, RecencyAt: &activeAt, Latest: &appserver.EvidenceTurn{ID: "a", Status: "inProgress"}}
	deps.client.latest["task-b"] = completedEvidence(now, "done", "🧵🐻 complete")
	value, err := runner.Run(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	result := value.(output.HeartbeatResult)
	stored, _ := deps.store.store.LoadState()
	if deps.classifier.calls != 0 || len(result.Changed) != 2 || stored.Tasks["task-a"].Status != state.StatusRunning || stored.PendingTitlePlans["task-a"].DesiredTitle != "⏳ Active work" || stored.PendingTitlePlans["task-b"].DesiredTitle != "✅ Finished work" {
		t.Fatalf("result=%+v state=%+v", result, stored)
	}
}

func TestArchiveEligibleChangedTaskIsReclassified(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	task := codex.Task{TaskID: "changed", Revision: "2", Title: "✅ Changed", Source: "vscode"}
	committed := state.New()
	committed.LastUpdateCheck = timePointer(now)
	previous := record(codex.Task{TaskID: task.TaskID, Revision: "1", Title: task.Title}, state.StatusComplete, now.Add(-15*24*time.Hour))
	previous.StateStartedAt = now.Add(-15 * 24 * time.Hour)
	committed.Tasks[task.TaskID] = previous
	runner, deps := testRunner(t, now, []codex.Task{task}, committed)
	activeAt := now.Unix()
	deps.client.latest[task.TaskID] = appserver.RecentEvidence{ThreadStatus: appserver.ThreadStatus{Type: "active"}, RecencyAt: &activeAt, Latest: &appserver.EvidenceTurn{ID: "turn", Status: "inProgress"}}
	if _, err := runner.Run(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	stored, _ := deps.store.store.LoadState()
	if stored.Tasks[task.TaskID].Status != state.StatusRunning || len(deps.client.archives) != 0 {
		t.Fatalf("state=%+v archives=%v", stored.Tasks[task.TaskID], deps.client.archives)
	}
}

func TestCyclePreservesUnchangedManagedAction(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	tasks := []codex.Task{{TaskID: "sibling", Revision: "2", Title: "Sibling", Source: "vscode"}, {TaskID: "unchanged", Revision: "1", Title: "➡️ Ship → deploy production", Source: "vscode"}}
	committed := state.New()
	committed.LastUpdateCheck = timePointer(now)
	committed.Tasks["sibling"] = record(codex.Task{TaskID: "sibling", Revision: "1", Title: "Sibling"}, state.StatusUnknown, now)
	unchanged := record(tasks[1], state.StatusNextSteps, now)
	unchanged.DurableSubject = "Ship"
	unchanged.ManagedAction = "deploy production"
	committed.Tasks["unchanged"] = unchanged
	runner, deps := testRunner(t, now, tasks, committed)
	deps.client.latest["sibling"] = completedEvidence(now, "done", "🧵🐻 complete")
	if _, err := runner.Run(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	current, _ := deps.index.task("unchanged")
	stored, _ := deps.store.store.LoadState()
	if current.Title != tasks[1].Title || stored.Tasks["unchanged"].ManagedAction != "deploy production" {
		t.Fatalf("title=%q record=%+v", current.Title, stored.Tasks["unchanged"])
	}
}

func TestCyclePreservesFutureRetryUntilDue(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	tasks := []codex.Task{{TaskID: "sibling", Revision: "2", Title: "Sibling", Source: "vscode"}, {TaskID: "retry", Revision: "1", Title: "Retry", Source: "vscode"}}
	committed := state.New()
	committed.LastUpdateCheck = timePointer(now)
	committed.Tasks["sibling"] = record(codex.Task{TaskID: "sibling", Revision: "1", Title: "Sibling"}, state.StatusUnknown, now)
	retryRecord := record(tasks[1], state.StatusUnknown, now.Add(-time.Hour))
	retryRecord.Retry = &state.Retry{Operation: "classifier", ErrorCode: "ephemeral_call_failed", Attempts: 1, LastAttemptAt: now.Add(-time.Minute), NextAttemptAt: now.Add(time.Hour)}
	committed.Tasks["retry"] = retryRecord
	runner, deps := testRunner(t, now, tasks, committed)
	deps.client.latest["sibling"] = completedEvidence(now, "done", "🧵🐻 complete")
	if _, err := runner.Run(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	stored, _ := deps.store.store.LoadState()
	if stored.Tasks["retry"].Retry == nil || contains(deps.client.latestReads, "retry") {
		t.Fatalf("retry=%+v reads=%v", stored.Tasks["retry"].Retry, deps.client.latestReads)
	}
	deps.clock.now = now.Add(time.Hour + time.Second)
	deps.client.latest["retry"] = completedEvidence(deps.clock.now, "done", "🧵🐻 complete")
	if _, err := runner.Run(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	stored, _ = deps.store.store.LoadState()
	if stored.Tasks["retry"].Retry != nil || !contains(deps.client.latestReads, "retry") {
		t.Fatalf("retry=%+v reads=%v", stored.Tasks["retry"].Retry, deps.client.latestReads)
	}
}

func TestArchiveCompletedAndExcludeOpenStates(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	tasks := []codex.Task{{TaskID: "complete", Revision: "1", Title: "✅ Complete", Source: "vscode"}, {TaskID: "next", Revision: "1", Title: "➡️ Next → act", Source: "vscode"}}
	committed := state.New()
	committed.LastUpdateCheck = timePointer(now)
	complete := record(tasks[0], state.StatusComplete, now.Add(-15*24*time.Hour))
	complete.StateStartedAt = now.Add(-15 * 24 * time.Hour)
	committed.Tasks["complete"] = complete
	next := record(tasks[1], state.StatusNextSteps, now.Add(-30*24*time.Hour))
	next.StateStartedAt = now.Add(-30 * 24 * time.Hour)
	committed.Tasks["next"] = next
	runner, deps := testRunner(t, now, tasks, committed)
	oldActivity := now.Add(-15 * 24 * time.Hour).Unix()
	deps.client.latest["complete"] = appserver.RecentEvidence{ThreadStatus: appserver.ThreadStatus{Type: "idle"}, RecencyAt: &oldActivity, Latest: &appserver.EvidenceTurn{ID: "turn", Status: "completed"}}
	value, err := runner.Run(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	result := value.(output.HeartbeatResult)
	if !reflect.DeepEqual(result.ArchivedIDs, []string{"complete"}) || strings.Join(deps.client.latestReads, ",") != "complete" {
		t.Fatalf("result=%+v latest=%v", result, deps.client.latestReads)
	}
	stored, _ := deps.store.store.LoadState()
	if _, ok := stored.Archives["complete"]; !ok {
		t.Fatal("archive ownership was not recorded")
	}
	if _, ok := stored.Tasks["next"]; !ok {
		t.Fatal("next-steps task was archived")
	}
}

func TestCycleRemovesOwnedActionWhenDeterministicStateHasNone(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	task := codex.Task{TaskID: "active", Revision: "2", Title: "➡️ Ship release → deploy production", Source: "vscode"}
	committed := state.New()
	committed.LastUpdateCheck = timePointer(now)
	rec := record(codex.Task{TaskID: task.TaskID, Revision: "1", Title: task.Title}, state.StatusNextSteps, now)
	rec.DurableSubject = "Ship release"
	rec.ManagedAction = "deploy production"
	committed.Tasks[task.TaskID] = rec
	runner, deps := testRunner(t, now, []codex.Task{task}, committed)
	activeAt := now.Unix()
	deps.client.latest[task.TaskID] = appserver.RecentEvidence{ThreadStatus: appserver.ThreadStatus{Type: "active"}, RecencyAt: &activeAt, Latest: &appserver.EvidenceTurn{ID: "turn", Status: "inProgress"}}
	if _, err := runner.Run(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	stored, _ := deps.store.store.LoadState()
	if stored.PendingTitlePlans[task.TaskID].DesiredTitle != "⏳ Ship release" {
		t.Fatalf("state=%+v", stored)
	}
}

func TestArchiveActivityChangeSkipsWithoutRetry(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	task := codex.Task{TaskID: "complete", Revision: "1", Title: "✅ Complete", Source: "vscode"}
	committed := state.New()
	committed.LastUpdateCheck = timePointer(now)
	record := record(task, state.StatusComplete, now.Add(-15*24*time.Hour))
	record.StateStartedAt = now.Add(-15 * 24 * time.Hour)
	committed.Tasks[task.TaskID] = record
	runner, deps := testRunner(t, now, []codex.Task{task}, committed)
	freshActivity := now.Unix()
	deps.client.latest[task.TaskID] = appserver.RecentEvidence{ThreadStatus: appserver.ThreadStatus{Type: "idle"}, RecencyAt: &freshActivity, Latest: &appserver.EvidenceTurn{ID: "turn", Status: "completed"}}
	value, err := runner.Run(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	result := value.(output.HeartbeatResult)
	if len(deps.client.archives) != 0 || len(result.Retries) != 0 {
		t.Fatalf("archives=%v result=%+v", deps.client.archives, result)
	}
	stored, _ := deps.store.store.LoadState()
	if !stored.Tasks[task.TaskID].LastSubstantiveActivity.Equal(now) {
		t.Fatalf("activity=%s", stored.Tasks[task.TaskID].LastSubstantiveActivity)
	}
}

func TestCycleLazyPreviousAndManualUnarchiveGrace(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	tasks := []codex.Task{{TaskID: "restored", Revision: "2", Title: "Work", Source: "vscode"}, {TaskID: "semantic", Revision: "2", Title: "Semantic", Source: "vscode"}}
	committed := state.New()
	committed.LastUpdateCheck = timePointer(now)
	committed.Archives["restored"] = state.ArchiveRecord{TaskID: "restored", ArchivedAt: now.Add(-time.Hour), CapturedRevision: "1", StateGeneration: 1}
	committed.Tasks["semantic"] = record(codex.Task{TaskID: "semantic", Revision: "1", Title: "Semantic"}, state.StatusUnknown, now.Add(-time.Hour))
	runner, deps := testRunner(t, now, tasks, committed)
	deps.client.latest["restored"] = completedEvidence(now, "restore", "🧵🐻 complete")
	deps.client.latest["semantic"] = completedEvidence(now, "continue", "no footer")
	deps.client.previous["semantic"] = &appserver.EvidenceTurn{ID: "previous", Status: "completed", UserMessage: "before", AgentMessage: "before answer"}
	deps.classifier.requestPrev = []string{"semantic"}
	deps.classifier.results["semantic"] = status.Classification{TaskID: "semantic", Revision: "2", Status: state.StatusComplete, Provenance: state.ProvenanceLuna, DurableSubject: "Semantic"}
	value, err := runner.Run(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	result := value.(output.HeartbeatResult)
	if strings.Join(deps.client.previousReads, ",") != "semantic" || !reflect.DeepEqual(result.RestoredIDs, []string{"restored"}) {
		t.Fatalf("previous=%v result=%+v", deps.client.previousReads, result)
	}
	stored, _ := deps.store.store.LoadState()
	if !stored.Tasks["restored"].LastSubstantiveActivity.Equal(now) {
		t.Fatalf("restored activity=%s", stored.Tasks["restored"].LastSubstantiveActivity)
	}
}

func TestClassifierBatchCheckpointRecoveryRepeatsOnlyUnpersistedWork(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	tasks := []codex.Task{{TaskID: "semantic-a", Revision: "1", Title: "A", Source: "vscode"}, {TaskID: "semantic-b", Revision: "1", Title: "B", Source: "vscode"}, {TaskID: "semantic-c", Revision: "1", Title: "C", Source: "vscode"}}
	committed := state.New()
	committed.LastUpdateCheck = timePointer(now)
	runner, deps := testRunner(t, now, tasks, committed)
	for _, task := range tasks {
		deps.client.latest[task.TaskID] = completedEvidence(now, "continue", "ambiguous")
	}
	deps.classifier.batchSizeOne = true
	deps.store.failSemanticCompleted = 2
	if _, err := runner.Run(context.Background(), false); err == nil {
		t.Fatal("semantic checkpoint failure did not stop heartbeat")
	}
	deps.store.failSemanticCompleted = 0
	value, err := runner.Run(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	progress := value.(output.HeartbeatResult).Progress
	if progress == nil || progress.LunaCandidates != 3 || progress.FirstPassBatchesTotal != 3 || progress.FirstPassBatchesCompleted != 3 {
		t.Fatalf("progress=%+v", progress)
	}
	counts := map[string]int{}
	for _, taskID := range deps.classifier.seen {
		counts[taskID]++
	}
	if counts["semantic-a"] != 1 || counts["semantic-b"] != 2 || counts["semantic-c"] != 1 {
		t.Fatalf("seen=%v", deps.classifier.seen)
	}
}

func TestCrashAfterClassifierReusesCheckpoint(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	tasks := []codex.Task{{TaskID: "semantic", Revision: "2", Title: "Semantic", Source: "vscode"}}
	committed := state.New()
	committed.LastUpdateCheck = timePointer(now)
	committed.Tasks["semantic"] = record(codex.Task{TaskID: "semantic", Revision: "1", Title: "Semantic"}, state.StatusUnknown, now.Add(-time.Hour))
	runner, deps := testRunner(t, now, tasks, committed)
	deps.client.latest["semantic"] = completedEvidence(now, "done", "no footer")
	deps.classifier.results["semantic"] = status.Classification{TaskID: "semantic", Revision: "2", Status: state.StatusComplete, Provenance: state.ProvenanceLuna, DurableSubject: "Semantic"}
	deps.store.failSaveState = true
	if _, err := runner.Run(context.Background(), false); err == nil {
		t.Fatal("first run did not crash")
	}
	if deps.classifier.calls != 1 {
		t.Fatalf("classifier calls=%d", deps.classifier.calls)
	}
	deps.store.failSaveState = false
	if _, err := runner.Run(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if deps.classifier.calls != 1 {
		t.Fatalf("classifier repeated after checkpoint: %d", deps.classifier.calls)
	}
	if _, err := deps.store.store.LoadCycle(); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("cycle remains: %v", err)
	}
}

func TestCrashAfterTitleAndArchiveResumeVerifiedOperations(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	task := codex.Task{TaskID: "semantic", Revision: "2", Title: "Semantic", Source: "vscode"}
	committed := state.New()
	committed.LastUpdateCheck = timePointer(now)
	committed.Tasks[task.TaskID] = record(codex.Task{TaskID: task.TaskID, Revision: "1", Title: "Semantic"}, state.StatusUnknown, now)
	runner, deps := testRunner(t, now, []codex.Task{task}, committed)
	deps.client.latest[task.TaskID] = completedEvidence(now, "done", "no footer")
	deps.classifier.results[task.TaskID] = status.Classification{TaskID: task.TaskID, Revision: "2", Status: state.StatusComplete, Provenance: state.ProvenanceLuna, DurableSubject: "Semantic"}
	deps.store.failSaveState = true
	if _, err := runner.Run(context.Background(), false); err == nil {
		t.Fatal("first run did not crash")
	}
	deps.store.failSaveState = false
	if _, err := runner.Run(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	stored, _ := deps.store.store.LoadState()
	if stored.PendingTitlePlans[task.TaskID].DesiredTitle != "✅ Semantic" || deps.classifier.calls != 1 || len(deps.client.titles) != 0 {
		t.Fatalf("state=%+v classifier=%d writes=%v", stored, deps.classifier.calls, deps.client.titles)
	}
}

func TestCrashBeforeArchiveDoesNotClaimExternalArchive(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	task := codex.Task{TaskID: "complete", Revision: "1", Title: "✅ Complete", Source: "vscode"}
	committed := state.New()
	committed.LastUpdateCheck = timePointer(now)
	record := record(task, state.StatusComplete, now.Add(-15*24*time.Hour))
	record.StateStartedAt = now.Add(-15 * 24 * time.Hour)
	committed.Tasks[task.TaskID] = record
	runner, deps := testRunner(t, now, []codex.Task{task}, committed)
	oldActivity := now.Add(-15 * 24 * time.Hour).Unix()
	deps.client.latest[task.TaskID] = appserver.RecentEvidence{ThreadStatus: appserver.ThreadStatus{Type: "idle"}, RecencyAt: &oldActivity, Latest: &appserver.EvidenceTurn{ID: "turn", Status: "completed"}}
	deps.store.failCycleAfter = 3
	if _, err := runner.Run(context.Background(), false); err == nil {
		t.Fatal("first run did not crash before archive")
	}
	deps.index.remove(task.TaskID)
	deps.store.failCycleAfter = 0
	if _, err := runner.Run(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	stored, _ := deps.store.store.LoadState()
	if _, owned := stored.Archives[task.TaskID]; owned {
		t.Fatal("external archive was attributed to ThreadBear")
	}
	if _, active := stored.Tasks[task.TaskID]; active {
		t.Fatal("externally archived task remained active")
	}
}

func TestCrashApplyingJournalRecoversWithoutRepeatingMutation(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	current := codex.Task{TaskID: "title", Revision: "1", Title: "Title", Source: "vscode"}
	committed := state.New()
	committed.LastUpdateCheck = timePointer(now)
	committed.Tasks[current.TaskID] = record(current, state.StatusComplete, now)
	runner, deps := testRunner(t, now, []codex.Task{current}, committed)
	cycle := state.NewCycle("cycle-1", committed.Generation, now)
	cycle.Inventory[current.TaskID] = state.CapturedTask{TaskID: current.TaskID, Revision: "1", Title: "Title", LastSubstantiveActivity: now}
	cycle.Results[current.TaskID] = state.ClassificationResult{TaskID: current.TaskID, Revision: "1", Status: state.StatusComplete, Provenance: state.ProvenanceFooter, DurableSubject: "Title"}
	cycle.Operations["title:title"] = state.CycleOperation{Kind: state.OperationTitle, Stage: state.StageApplying, TaskID: current.TaskID, ExpectedRevision: "1", ExpectedTitle: "Title", DesiredTitle: "✅ Title"}
	if err := deps.store.store.SaveCycle(cycle); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Run(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	stored, _ := deps.store.store.LoadState()
	if stored.PendingTitlePlans[current.TaskID].DesiredTitle != "✅ Title" || len(deps.client.titles) != 0 {
		t.Fatalf("state=%+v writes=%v", stored, deps.client.titles)
	}
}

func TestLegacyCycleRecoveryReconstructsManagedActionAndTokenOwnership(t *testing.T) {
	now := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	expected := codex.Task{TaskID: "title", Revision: "1", Title: "➡️ 1.2m Release service → old action", Source: "vscode", RolloutPath: "/synthetic/legacy-title.jsonl"}
	committed := state.New()
	committed.LastUpdateCheck = timePointer(now)
	rec := record(expected, state.StatusNextSteps, now)
	rec.DurableSubject = "Release service"
	rec.ManagedAction = "old action"
	rec.ManagedTokenDisplay = "1.2m"
	rec.ManagedTokenPosition = tokens.PositionStart
	rec.TokenDisplayPosition = tokens.PositionEnd
	committed.Tasks[expected.TaskID] = rec
	runner, deps := testRunner(t, now, []codex.Task{expected}, committed)
	cycle := state.NewCycle("legacy", committed.Generation, now)
	cycle.Inventory[expected.TaskID] = state.CapturedTask{TaskID: expected.TaskID, Revision: expected.Revision, Title: expected.Title, RolloutPath: expected.RolloutPath, LastSubstantiveActivity: now}
	cycle.Results[expected.TaskID] = state.ClassificationResult{TaskID: expected.TaskID, Revision: expected.Revision, Status: state.StatusNextSteps, Provenance: state.ProvenanceFooter, ManagedAction: "review rollout"}
	cycle.Operations["title:"+expected.TaskID] = state.CycleOperation{Kind: state.OperationTitle, Stage: state.StageApplying, TaskID: expected.TaskID, ExpectedRevision: expected.Revision, ExpectedTitle: expected.Title, DesiredTitle: "➡️ Release service → review rollout"}
	if err := deps.store.store.SaveCycle(cycle); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Run(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	stored, _ := deps.store.store.LoadState()
	plan := stored.PendingTitlePlans[expected.TaskID]
	if plan.DesiredTitle != "➡️ Release service → review rollout" || plan.DurableSubject != "Release service" || plan.ManagedAction != "review rollout" || len(deps.client.titles) != 0 {
		t.Fatalf("plan=%+v writes=%v", plan, deps.client.titles)
	}
}

func TestLegacyTitleOwnershipRecognizesOnlyExactOperationTokenZones(t *testing.T) {
	record := state.TaskRecord{
		TaskID: "title", CapturedRevision: "1", CapturedTitle: "➡️ 1.2m Release service → old action",
		LastAppliedTitle: "➡️ 1.2m Release service → old action", DurableSubject: "Release service", ManagedAction: "old action",
		ManagedTokenDisplay: "1.2m", ManagedTokenPosition: tokens.PositionStart,
	}
	classification := state.ClassificationResult{TaskID: "title", Revision: "1", Status: state.StatusNextSteps, Provenance: state.ProvenanceFooter, ManagedAction: "review rollout"}
	base := "➡️ Release service → review rollout"
	for _, test := range []struct {
		name     string
		desired  string
		display  string
		position tokens.Position
	}{
		{name: "none", desired: base},
		{name: "start", desired: "➡️ 26k Release service → review rollout", display: "26k", position: tokens.PositionStart},
		{name: "end", desired: base + " · out 1.6m", display: "1.6m", position: tokens.PositionEnd},
	} {
		t.Run(test.name, func(t *testing.T) {
			operation := state.CycleOperation{Kind: state.OperationTitle, Stage: state.StageApplying, TaskID: "title", ExpectedRevision: "1", ExpectedTitle: record.CapturedTitle, DesiredTitle: test.desired}
			got, err := reconstructTitleOwnership(record, classification, operation)
			if err != nil {
				t.Fatal(err)
			}
			if got.Title != test.desired || got.DurableSubject != "Release service" || got.ManagedAction != "review rollout" || got.ManagedTokenDisplay != test.display || got.ManagedTokenPosition != test.position {
				t.Fatalf("ownership = %+v", got)
			}
		})
	}
	for _, desired := range []string{
		"➡️ user Release service → review rollout",
		"➡️ 26k 30k Release service → review rollout",
		base + " · out 1.6m extra",
		"➡️ 26k Release service → review rollout · out 1.6m",
		"➡️ Release service → changed rollout",
	} {
		t.Run(desired, func(t *testing.T) {
			operation := state.CycleOperation{Kind: state.OperationTitle, Stage: state.StageApplying, TaskID: "title", ExpectedRevision: "1", ExpectedTitle: record.CapturedTitle, DesiredTitle: desired}
			if _, err := reconstructTitleOwnership(record, classification, operation); err == nil {
				t.Fatal("non-exact legacy title was accepted")
			}
		})
	}
	t.Run("non legacy operation", func(t *testing.T) {
		operation := state.CycleOperation{Kind: state.OperationTitle, Stage: state.StagePrepared, TaskID: "title", ExpectedRevision: "1", ExpectedTitle: record.CapturedTitle, DesiredTitle: base, DurableSubject: "Release service"}
		if _, err := reconstructTitleOwnership(record, classification, operation); err == nil {
			t.Fatal("fresh title operation was treated as legacy")
		}
	})
	t.Run("verified title mismatch", func(t *testing.T) {
		operation := state.CycleOperation{Kind: state.OperationTitle, Stage: state.StageVerified, TaskID: "title", ExpectedRevision: "1", ExpectedTitle: record.CapturedTitle, DesiredTitle: base, VerifiedTitle: base + " changed"}
		if _, err := reconstructTitleOwnership(record, classification, operation); err == nil {
			t.Fatal("verified title mismatch was accepted")
		}
	})
}

func TestCrashAfterStateRenameDoesNotCommitTwice(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	task := codex.Task{TaskID: "active", Revision: "2", Title: "✅ Active", Source: "vscode"}
	committed := state.New()
	committed.LastUpdateCheck = timePointer(now)
	committed.Tasks[task.TaskID] = record(codex.Task{TaskID: task.TaskID, Revision: "1", Title: task.Title}, state.StatusComplete, now)
	runner, deps := testRunner(t, now, []codex.Task{task}, committed)
	activeAt := now.Unix()
	deps.client.latest[task.TaskID] = appserver.RecentEvidence{ThreadStatus: appserver.ThreadStatus{Type: "active"}, RecencyAt: &activeAt, Latest: &appserver.EvidenceTurn{ID: "turn", Status: "inProgress"}}
	deps.store.failRemove = true
	if _, err := runner.Run(context.Background(), false); err == nil {
		t.Fatal("first run did not fail after state rename")
	}
	first, _ := deps.store.store.LoadState()
	deps.store.failRemove = false
	if _, err := runner.Run(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	second, _ := deps.store.store.LoadState()
	if first.Generation != second.Generation {
		t.Fatalf("generation committed twice: %d -> %d", first.Generation, second.Generation)
	}
}

func TestHeartbeatDryRunAndInvalidBudgetNeverCallClassifier(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	task := codex.Task{TaskID: "semantic", Revision: "2", Title: "Semantic", Source: "vscode"}
	committed := state.New()
	committed.LastUpdateCheck = timePointer(now)
	committed.Tasks[task.TaskID] = record(codex.Task{TaskID: task.TaskID, Revision: "1", Title: task.Title}, state.StatusUnknown, now)
	runner, deps := testRunner(t, now, []codex.Task{task}, committed)
	preview, err := runner.Run(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := preview.(output.PreviewResult); !ok || deps.factory.opens != 0 || deps.classifierSessions.opens != 0 || deps.classifier.calls != 0 {
		t.Fatalf("preview=%T opens=%d classifier_sessions=%d classifier=%d", preview, deps.factory.opens, deps.classifierSessions.opens, deps.classifier.calls)
	}
	deps.client.latest[task.TaskID] = completedEvidence(now, "done", "no footer")
	invalid := config.Default("control")
	invalid.ClassifierContextBudgetBytes = 0
	deps.store.configOverride = &invalid
	value, err := runner.Run(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	result := value.(output.HeartbeatResult)
	if deps.classifierSessions.opens != 0 || deps.classifier.calls != 0 || len(result.Retries) != 1 || result.Retries[0].ErrorCode != "invalid_context_budget" {
		t.Fatalf("result=%+v classifier_sessions=%d classifier=%d", result, deps.classifierSessions.opens, deps.classifier.calls)
	}
}

func TestClassifierSessionOpensOnceForMultipleBatches(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	tasks := []codex.Task{{TaskID: "semantic-a", Revision: "1", Title: "A", Source: "vscode"}, {TaskID: "semantic-b", Revision: "1", Title: "B", Source: "vscode"}, {TaskID: "semantic-c", Revision: "1", Title: "C", Source: "vscode"}}
	committed := state.New()
	committed.LastUpdateCheck = timePointer(now)
	runner, deps := testRunner(t, now, tasks, committed)
	for _, task := range tasks {
		deps.client.latest[task.TaskID] = completedEvidence(now, "continue", "ambiguous")
	}
	deps.classifier.batchSizeOne = true
	if _, err := runner.Run(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if deps.classifierSessions.opens != 1 || deps.classifierSessions.closes != 1 || deps.classifier.calls != 1 {
		t.Fatalf("sessions=%+v classifier_calls=%d", deps.classifierSessions, deps.classifier.calls)
	}
}

func TestDeterministicHeartbeatDoesNotOpenClassifierSession(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	task := codex.Task{TaskID: "deterministic", Revision: "1", Title: "Task", Source: "vscode"}
	committed := state.New()
	committed.LastUpdateCheck = timePointer(now)
	runner, deps := testRunner(t, now, []codex.Task{task}, committed)
	deps.client.latest[task.TaskID] = completedEvidence(now, "done", "🧵🐻 complete")
	if _, err := runner.Run(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if deps.classifierSessions.opens != 0 || deps.classifier.calls != 0 {
		t.Fatalf("sessions=%d classifier=%d", deps.classifierSessions.opens, deps.classifier.calls)
	}
}

func TestClassifierIsolationSetupFailureBlocksMutations(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	task := codex.Task{TaskID: "semantic", Revision: "1", Title: "Task", Source: "vscode"}
	committed := state.New()
	committed.LastUpdateCheck = timePointer(now)
	runner, deps := testRunner(t, now, []codex.Task{task}, committed)
	deps.client.latest[task.TaskID] = completedEvidence(now, "continue", "ambiguous")
	deps.classifierSessions.openErr = errors.New("synthetic isolation failure")
	value, err := runner.Run(context.Background(), false)
	if err == nil || value.(output.HeartbeatResult).ErrorCode != "classifier_isolation_failed" {
		t.Fatalf("result=%+v err=%v", value, err)
	}
	if deps.classifier.calls != 0 || len(deps.client.titles) != 0 || len(deps.client.archives) != 0 {
		t.Fatalf("classifier=%d titles=%v archives=%v", deps.classifier.calls, deps.client.titles, deps.client.archives)
	}
}

func TestClassifierCleanupFailureBlocksPendingNotice(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	task := codex.Task{TaskID: "semantic", Revision: "1", Title: "Task", Source: "vscode"}
	committed := state.New()
	runner, deps := testRunner(t, now, []codex.Task{task}, committed)
	cfg := config.Default("control")
	cfg.AutoUpdateEnabled = false
	deps.store.configOverride = &cfg
	deps.update.result = UpdateStatus{LatestVersion: "2.0.0", Newer: true}
	deps.client.latest[task.TaskID] = completedEvidence(now, "done", "ambiguous")
	deps.classifierSessions.closeErr = errors.New("synthetic cleanup failure")
	value, err := runner.Run(context.Background(), false)
	if err == nil || value.(output.HeartbeatResult).ErrorCode != "classifier_cleanup_failed" {
		t.Fatalf("result=%+v err=%v", value, err)
	}
	if len(deps.client.notices) != 0 || len(deps.client.titles) != 0 || len(deps.client.archives) != 0 {
		t.Fatalf("notices=%v titles=%v archives=%v", deps.client.notices, deps.client.titles, deps.client.archives)
	}
}

func TestClassifierCleanupFailureBlocksMutationAndRecovers(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	task := codex.Task{TaskID: "semantic", Revision: "1", Title: "Task", Source: "vscode"}
	committed := state.New()
	committed.LastUpdateCheck = timePointer(now)
	runner, deps := testRunner(t, now, []codex.Task{task}, committed)
	deps.client.latest[task.TaskID] = completedEvidence(now, "done", "ambiguous")
	deps.classifierSessions.closeErr = errors.New("synthetic cleanup failure")
	value, err := runner.Run(context.Background(), false)
	if err == nil || value.(output.HeartbeatResult).ErrorCode != "classifier_cleanup_failed" {
		t.Fatalf("result=%+v err=%v", value, err)
	}
	checkpoint, loadErr := deps.store.store.LoadCycle()
	if loadErr != nil || checkpoint.ClassifierCleanupToken == "" || len(deps.client.titles) != 0 {
		t.Fatalf("checkpoint=%+v load_err=%v titles=%v", checkpoint, loadErr, deps.client.titles)
	}
	deps.classifierSessions.closeErr = nil
	if _, err := runner.Run(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if deps.classifierSessions.cleanups != 1 || deps.classifier.calls != 1 {
		t.Fatalf("sessions=%+v classifier=%d", deps.classifierSessions, deps.classifier.calls)
	}
}

func TestClassifierTokenJournalFailureStillRecoversOrphan(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	task := codex.Task{TaskID: "semantic", Revision: "1", Title: "Task", Source: "vscode"}
	committed := state.New()
	committed.LastUpdateCheck = timePointer(now)
	runner, deps := testRunner(t, now, []codex.Task{task}, committed)
	deps.client.latest[task.TaskID] = completedEvidence(now, "continue", "ambiguous")
	deps.store.failClassifierToken = true
	deps.classifierSessions.closeErr = errors.New("synthetic cleanup failure")
	value, err := runner.Run(context.Background(), false)
	if err == nil || value.(output.HeartbeatResult).ErrorCode != "classifier_cleanup_failed" || deps.classifier.calls != 0 {
		t.Fatalf("result=%+v err=%v classifier=%d", value, err, deps.classifier.calls)
	}
	orphanScans := deps.classifierSessions.orphanCleanups
	deps.store.failClassifierToken = false
	deps.classifierSessions.closeErr = nil
	if _, err := runner.Run(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if deps.classifierSessions.orphanCleanups != orphanScans+1 || deps.classifier.calls != 1 {
		t.Fatalf("sessions=%+v classifier=%d", deps.classifierSessions, deps.classifier.calls)
	}
}

func TestClassifierOrphanCleanupFailureStopsBeforeInventory(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	committed := state.New()
	committed.LastUpdateCheck = timePointer(now)
	runner, deps := testRunner(t, now, nil, committed)
	deps.classifierSessions.cleanupErr = errors.New("synthetic orphan cleanup failure")
	value, err := runner.Run(context.Background(), false)
	if err == nil || value.(output.HeartbeatResult).ErrorCode != "classifier_cleanup_failed" {
		t.Fatalf("result=%+v err=%v", value, err)
	}
	if deps.index.calls != 0 || deps.managed.calls != 0 {
		t.Fatalf("inventory=%d managed=%d", deps.index.calls, deps.managed.calls)
	}
}

func TestClassifierCancellationCleansSessionBeforeMutation(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	task := codex.Task{TaskID: "semantic", Revision: "1", Title: "Task", Source: "vscode"}
	committed := state.New()
	committed.LastUpdateCheck = timePointer(now)
	runner, deps := testRunner(t, now, []codex.Task{task}, committed)
	deps.client.latest[task.TaskID] = completedEvidence(now, "continue", "ambiguous")
	deps.classifier.waitForCancel = true
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	value, err := runner.Run(ctx, false)
	if !errors.Is(err, context.Canceled) || value.(output.HeartbeatResult).ErrorCode != "classifier_interrupted" {
		t.Fatalf("result=%+v err=%v", value, err)
	}
	if deps.classifierSessions.opens != 1 || deps.classifierSessions.closes != 1 || len(deps.client.titles) != 0 {
		t.Fatalf("sessions=%+v titles=%v", deps.classifierSessions, deps.client.titles)
	}
}

func TestCycleStaleMutationIsSkippedWithoutRetry(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	task := codex.Task{TaskID: "active", Revision: "2", Title: "✅ Active", Source: "vscode"}
	committed := state.New()
	committed.LastUpdateCheck = timePointer(now)
	committed.Tasks[task.TaskID] = record(codex.Task{TaskID: task.TaskID, Revision: "1", Title: task.Title}, state.StatusComplete, now)
	runner, deps := testRunner(t, now, []codex.Task{task}, committed)
	activeAt := now.Unix()
	deps.client.latest[task.TaskID] = appserver.RecentEvidence{ThreadStatus: appserver.ThreadStatus{Type: "active"}, RecencyAt: &activeAt, Latest: &appserver.EvidenceTurn{ID: "turn", Status: "inProgress"}}
	deps.index.hook = func(call int, index *fakeIndex) {
		if call == 2 {
			changed, _ := index.task(task.TaskID)
			changed.Revision = "3"
			changed.Title = "User edited subject"
			index.replace(changed)
		}
	}
	value, err := runner.Run(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	result := value.(output.HeartbeatResult)
	if len(deps.client.titles) != 0 || len(result.Retries) != 0 {
		t.Fatalf("titles=%v result=%+v", deps.client.titles, result)
	}
	stored, _ := deps.store.store.LoadState()
	if stored.Tasks[task.TaskID].CapturedRevision != "2" {
		t.Fatalf("captured revision=%s", stored.Tasks[task.TaskID].CapturedRevision)
	}
}

func TestHeartbeatFailedUpdateCheckWaitsUntilNextDay(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	committed := state.New()
	committed.LastUpdateCheck = timePointer(now.Add(-25 * time.Hour))
	runner, deps := testRunner(t, now, nil, committed)
	deps.update.err = errors.New("release service unavailable")
	value, err := runner.Run(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if result := value.(output.HeartbeatResult); !result.Empty() {
		t.Fatalf("failed daily update check was noisy: %+v", result)
	}
	stored, err := deps.store.store.LoadState()
	if err != nil || stored.LastUpdateCheck == nil || !stored.LastUpdateCheck.Equal(now) || deps.update.calls != 1 {
		t.Fatalf("last_check=%v calls=%d err=%v", stored.LastUpdateCheck, deps.update.calls, err)
	}
	deps.update.calls = 0
	value, err = runner.Run(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if result := value.(output.HeartbeatResult); !result.Empty() || deps.update.calls != 0 {
		t.Fatalf("same-day heartbeat retried update: result=%+v calls=%d", result, deps.update.calls)
	}
}

func TestHeartbeatUpdateNoticeDeliveredOnce(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	committed := state.New()
	committed.LastUpdateCheck = timePointer(now.Add(-25 * time.Hour))
	runner, deps := testRunner(t, now, nil, committed)
	cfg := config.Default("control")
	cfg.AutoUpdateEnabled = false
	deps.store.configOverride = &cfg
	deps.update.result = UpdateStatus{LatestVersion: "1.2.0", Newer: true}
	value, err := runner.Run(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	first := value.(output.HeartbeatResult)
	if first.Empty() || first.ErrorCode != "" || len(first.Changed) != 1 || first.Changed[0].TaskID != "control" || len(deps.client.notices) != 1 || !strings.Contains(deps.client.notices[0], "ThreadBear 1.2.0 is ready") {
		t.Fatalf("result=%+v notices=%v", first, deps.client.notices)
	}
	deps.update.calls = 0
	deps.factory.opens = 0
	if _, err := runner.Run(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	stored, _ := deps.store.store.LoadState()
	if len(stored.DeliveredNoticeVersions) != 1 || stored.DeliveredNoticeVersions[0] != "1.2.0" || len(deps.client.notices) != 1 || deps.update.calls != 0 || deps.factory.opens != 0 || len(deps.client.latestReads) != 0 {
		t.Fatalf("delivered=%v notices=%v checks=%d opens=%d latestReads=%v", stored.DeliveredNoticeVersions, deps.client.notices, deps.update.calls, deps.factory.opens, deps.client.latestReads)
	}
}

func TestCrashAmbiguousMutationKeepsApplyingJournal(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	t.Run("notice", func(t *testing.T) {
		committed := state.New()
		committed.LastUpdateCheck = timePointer(now.Add(-25 * time.Hour))
		runner, deps := testRunner(t, now, nil, committed)
		cfg := config.Default("control")
		cfg.AutoUpdateEnabled = false
		deps.store.configOverride = &cfg
		deps.update.result = UpdateStatus{LatestVersion: "1.2.0", Newer: true}
		deps.client.failNotice = true
		if _, err := runner.Run(context.Background(), false); err == nil {
			t.Fatal("ambiguous notice did not stop the cycle")
		}
		cycle, err := deps.store.store.LoadCycle()
		if err != nil || cycle.Operations["notice:1.2.0"].Stage != state.StageApplying {
			t.Fatalf("cycle=%+v err=%v", cycle, err)
		}
		deps.client.failNotice = false
		if _, err := runner.Run(context.Background(), false); err != nil {
			t.Fatal(err)
		}
		stored, _ := deps.store.store.LoadState()
		if len(stored.DeliveredNoticeVersions) != 1 || stored.DeliveredNoticeVersions[0] != "1.2.0" || len(deps.client.notices) != 1 || len(deps.client.latestReads) != 0 {
			t.Fatalf("delivered=%v notices=%v latestReads=%v", stored.DeliveredNoticeVersions, deps.client.notices, deps.client.latestReads)
		}
		if _, err := runner.Run(context.Background(), false); err != nil {
			t.Fatal(err)
		}
		if len(deps.client.notices) != 1 {
			t.Fatalf("recovery duplicated notice: %v", deps.client.notices)
		}
	})
	t.Run("archive", func(t *testing.T) {
		task := codex.Task{TaskID: "complete", Revision: "1", Title: "✅ Complete", Source: "vscode"}
		committed := state.New()
		committed.LastUpdateCheck = timePointer(now)
		record := record(task, state.StatusComplete, now.Add(-15*24*time.Hour))
		record.StateStartedAt = now.Add(-15 * 24 * time.Hour)
		committed.Tasks[task.TaskID] = record
		runner, deps := testRunner(t, now, []codex.Task{task}, committed)
		oldActivity := now.Add(-15 * 24 * time.Hour).Unix()
		deps.client.latest[task.TaskID] = appserver.RecentEvidence{ThreadStatus: appserver.ThreadStatus{Type: "idle"}, RecencyAt: &oldActivity, Latest: &appserver.EvidenceTurn{ID: "turn", Status: "completed"}}
		deps.client.archiveErrorAfterApply = true
		if _, err := runner.Run(context.Background(), false); err == nil {
			t.Fatal("ambiguous archive did not preserve the checkpoint")
		}
		deps.client.archiveErrorAfterApply = false
		if _, err := runner.Run(context.Background(), false); err != nil {
			t.Fatal(err)
		}
		stored, _ := deps.store.store.LoadState()
		if _, ok := stored.Archives[task.TaskID]; ok {
			t.Fatal("archive without completion evidence was claimed")
		}
	})
}

func TestHeartbeatUpdateDueCurrentOpensNoAppServer(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	committed := state.New()
	committed.LastUpdateCheck = timePointer(now.Add(-25 * time.Hour))
	runner, deps := testRunner(t, now, nil, committed)
	deps.update.result = UpdateStatus{LatestVersion: "1.0.0", Newer: false}
	value, err := runner.Run(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if !value.(output.HeartbeatResult).Empty() || deps.factory.opens != 0 || deps.update.calls != 1 {
		t.Fatalf("result=%+v opens=%d checks=%d", value, deps.factory.opens, deps.update.calls)
	}
}

func TestIdleHeartbeatWithoutDueWorkDoesNotWriteState(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	committed := state.New()
	committed.LastUpdateCheck = timePointer(now)
	runner, deps := testRunner(t, now, nil, committed)
	before, err := deps.store.store.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Run(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	after, err := deps.store.store.LoadState()
	if err != nil || !reflect.DeepEqual(after, before) || deps.update.calls != 0 || deps.managed.calls != 1 {
		t.Fatalf("before=%+v after=%+v checks=%d reconciles=%d err=%v", before, after, deps.update.calls, deps.managed.calls, err)
	}
}

func TestAutoUpdateAppliesCheckedVersionWithoutReadyNotice(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	committed := state.New()
	committed.LastUpdateCheck = timePointer(now.Add(-31 * time.Minute))
	runner, deps := testRunner(t, now, nil, committed)
	deps.update.result = UpdateStatus{LatestVersion: "1.2.0", Newer: true}
	deps.updater.result = updatepkg.Result{PreviousVersion: "1.0.0", InstalledVersion: "1.2.0", Changed: true}
	value, err := runner.Run(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if !value.(output.HeartbeatResult).Empty() || deps.updater.calls != 1 || !reflect.DeepEqual(deps.updater.targets, []string{"1.2.0"}) || len(deps.client.notices) != 0 || deps.factory.opens != 0 {
		t.Fatalf("result=%+v updater=%d targets=%v notices=%v opens=%d", value, deps.updater.calls, deps.updater.targets, deps.client.notices, deps.factory.opens)
	}
	stored, err := deps.store.store.LoadState()
	if err != nil || stored.LastUpdateCheck == nil || !stored.LastUpdateCheck.Equal(now) || stored.LastUpdateFailure != nil {
		t.Fatalf("state=%+v err=%v", stored, err)
	}
}

func TestUpdateFailuresPersistAndClearWithoutNotices(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		code string
		set  func(*Runner, testDeps)
	}{
		{name: "check", code: "update_check_failed", set: func(_ *Runner, deps testDeps) { deps.update.err = errors.New("offline") }},
		{name: "apply", code: "update_apply_failed", set: func(_ *Runner, deps testDeps) {
			deps.update.result = UpdateStatus{LatestVersion: "1.2.0", Newer: true}
			deps.updater.err = errors.New("checksum")
		}},
		{name: "unavailable", code: "update_updater_unavailable", set: func(runner *Runner, deps testDeps) {
			deps.update.result = UpdateStatus{LatestVersion: "1.2.0", Newer: true}
			runner.deps.Updater = nil
		}},
		{name: "timeout", code: "update_apply_timeout", set: func(runner *Runner, deps testDeps) {
			deps.update.result = UpdateStatus{LatestVersion: "1.2.0", Newer: true}
			deps.updater.block = true
			runner.deps.UpdateApplyTimeout = 10 * time.Millisecond
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			committed := state.New()
			committed.LastUpdateCheck = timePointer(now.Add(-31 * time.Minute))
			runner, deps := testRunner(t, now, nil, committed)
			test.set(runner, deps)
			value, err := runner.Run(context.Background(), false)
			if err != nil || !value.(output.HeartbeatResult).Empty() || len(deps.client.notices) != 0 {
				t.Fatalf("result=%+v err=%v notices=%v", value, err, deps.client.notices)
			}
			stored, err := deps.store.store.LoadState()
			if err != nil || stored.LastUpdateFailure == nil || stored.LastUpdateFailure.Code != test.code || stored.LastUpdateCheck == nil || !stored.LastUpdateCheck.Equal(now) {
				t.Fatalf("state=%+v err=%v", stored, err)
			}
			deps.clock.now = now.Add(31 * time.Minute)
			deps.update.err = nil
			deps.update.result = UpdateStatus{LatestVersion: "1.0.0"}
			deps.updater.block = false
			runner.deps.Updater = deps.updater
			if _, err := runner.Run(context.Background(), false); err != nil {
				t.Fatal(err)
			}
			stored, _ = deps.store.store.LoadState()
			if stored.LastUpdateFailure != nil {
				t.Fatalf("failure not cleared: %+v", stored.LastUpdateFailure)
			}
		})
	}
}

func TestVersionAdoptionReconcileAndAnnouncement(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	t.Run("fresh adoption", func(t *testing.T) {
		runner, deps := testRunner(t, now, nil, state.New())
		stored, _ := deps.store.store.LoadState()
		stored.LastAnnouncedVersion = ""
		stored.LastReconciledVersion = ""
		stored.LastUpdateCheck = timePointer(now)
		if err := deps.store.store.SaveState(stored); err != nil {
			t.Fatal(err)
		}
		if _, err := runner.Run(context.Background(), false); err != nil {
			t.Fatal(err)
		}
		stored, _ = deps.store.store.LoadState()
		if stored.LastAnnouncedVersion != "1.0.0" || stored.LastReconciledVersion != "1.0.0" || len(deps.client.notices) != 0 || deps.managed.calls != 1 {
			t.Fatalf("state=%+v notices=%v reconciles=%d", stored, deps.client.notices, deps.managed.calls)
		}
	})

	t.Run("drift announcement", func(t *testing.T) {
		runner, deps := testRunner(t, now, nil, state.New())
		stored, _ := deps.store.store.LoadState()
		stored.LastAnnouncedVersion = "1.1.0"
		stored.LastReconciledVersion = "1.1.0"
		stored.LastUpdateCheck = timePointer(now)
		if err := deps.store.store.SaveState(stored); err != nil {
			t.Fatal(err)
		}
		runner.deps.InstalledVersion = "1.2.0"
		runner.deps.ReleaseNotes = func() []string { return []string{"Safer updates", "Fresher skill text", "Quieter checks"} }
		if _, err := runner.Run(context.Background(), false); err != nil {
			t.Fatal(err)
		}
		want := "🧵🐻 I gave myself a quick brush-up: v1.1.0 → v1.2.0!\n- Safer updates\n- Fresher skill text\n- Quieter checks\nPrefer to update by hand? threadbear configure --auto-update=false"
		stored, _ = deps.store.store.LoadState()
		if stored.LastAnnouncedVersion != "1.2.0" || stored.LastReconciledVersion != "1.2.0" || !reflect.DeepEqual(deps.client.notices, []string{want}) || deps.managed.calls != 1 || strings.Contains(want, "is ready") {
			t.Fatalf("state=%+v notices=%q reconciles=%d", stored, deps.client.notices, deps.managed.calls)
		}
		if _, err := runner.Run(context.Background(), false); err != nil {
			t.Fatal(err)
		}
		if len(deps.client.notices) != 1 || deps.managed.calls != 2 {
			t.Fatalf("duplicate notices=%v reconciles=%d", deps.client.notices, deps.managed.calls)
		}
	})
}

func TestAnnouncementRecoveryAndReconcileRetry(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	t.Run("ambiguous delivery", func(t *testing.T) {
		runner, deps := testRunner(t, now, nil, state.New())
		stored, _ := deps.store.store.LoadState()
		stored.LastAnnouncedVersion = "1.1.0"
		stored.LastReconciledVersion = "1.1.0"
		stored.LastUpdateCheck = timePointer(now)
		if err := deps.store.store.SaveState(stored); err != nil {
			t.Fatal(err)
		}
		runner.deps.InstalledVersion = "1.2.0"
		runner.deps.ReleaseNotes = func() []string { return []string{"Bullet one"} }
		deps.client.failNotice = true
		if _, err := runner.Run(context.Background(), false); err == nil {
			t.Fatal("announcement insertion did not report ambiguity")
		}
		cycle, err := deps.store.store.LoadCycle()
		operation := cycle.Operations["announcement:1.2.0"]
		if err != nil || operation.Stage != state.StageApplying || operation.PreviousVersion != "1.1.0" {
			t.Fatalf("cycle=%+v operation=%+v err=%v", cycle, operation, err)
		}
		deps.client.failNotice = false
		if _, err := runner.Run(context.Background(), false); err != nil {
			t.Fatal(err)
		}
		stored, _ = deps.store.store.LoadState()
		if stored.LastAnnouncedVersion != "1.2.0" || len(deps.client.notices) != 1 {
			t.Fatalf("state=%+v notices=%v", stored, deps.client.notices)
		}
	})

	t.Run("reconcile failure does not block", func(t *testing.T) {
		runner, deps := testRunner(t, now, nil, state.New())
		stored, _ := deps.store.store.LoadState()
		stored.LastAnnouncedVersion = "1.1.0"
		stored.LastReconciledVersion = "1.1.0"
		stored.LastUpdateCheck = timePointer(now)
		if err := deps.store.store.SaveState(stored); err != nil {
			t.Fatal(err)
		}
		runner.deps.InstalledVersion = "1.2.0"
		deps.managed.err = errors.New("read only")
		if _, err := runner.Run(context.Background(), false); err != nil {
			t.Fatal(err)
		}
		stored, _ = deps.store.store.LoadState()
		if stored.LastAnnouncedVersion != "1.2.0" || stored.LastReconciledVersion != "1.1.0" || stored.LastReconcileFailure == nil || len(deps.client.notices) != 1 {
			t.Fatalf("state=%+v notices=%v", stored, deps.client.notices)
		}
		deps.managed.err = nil
		if _, err := runner.Run(context.Background(), false); err != nil {
			t.Fatal(err)
		}
		stored, _ = deps.store.store.LoadState()
		if stored.LastReconciledVersion != "1.2.0" || stored.LastReconcileFailure != nil || deps.managed.calls != 2 || len(deps.client.notices) != 1 {
			t.Fatalf("state=%+v reconciles=%d notices=%v", stored, deps.managed.calls, deps.client.notices)
		}
	})
}

func TestAnnouncementVerifiedBeforeStateCrashDoesNotDuplicate(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	runner, deps := testRunner(t, now, nil, state.New())
	stored, _ := deps.store.store.LoadState()
	stored.LastAnnouncedVersion = "1.1.0"
	stored.LastReconciledVersion = "1.2.0"
	stored.LastUpdateCheck = timePointer(now)
	if err := deps.store.store.SaveState(stored); err != nil {
		t.Fatal(err)
	}
	runner.deps.InstalledVersion = "1.2.0"
	runner.deps.ReleaseNotes = func() []string { return []string{"Crash-safe bullet"} }
	deps.store.failSaveState = true
	if _, err := runner.Run(context.Background(), false); err == nil {
		t.Fatal("final state write did not fail")
	}
	cycle, err := deps.store.store.LoadCycle()
	if err != nil || cycle.Operations["announcement:1.2.0"].Stage != state.StageVerified || len(deps.client.notices) != 1 {
		t.Fatalf("cycle=%+v notices=%v err=%v", cycle, deps.client.notices, err)
	}
	deps.store.failSaveState = false
	if _, err := runner.Run(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	stored, _ = deps.store.store.LoadState()
	if stored.LastAnnouncedVersion != "1.2.0" || len(deps.client.notices) != 1 {
		t.Fatalf("state=%+v notices=%v", stored, deps.client.notices)
	}
}

func TestReconcileRespectsDisabledAgents(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	runner, deps := testRunner(t, now, nil, state.New())
	stored, _ := deps.store.store.LoadState()
	stored.LastAnnouncedVersion = "1.0.0"
	stored.LastReconciledVersion = "0.9.0"
	stored.LastUpdateCheck = timePointer(now)
	if err := deps.store.store.SaveState(stored); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default("control")
	cfg.AgentsEnabled = false
	deps.store.configOverride = &cfg
	if _, err := runner.Run(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(deps.managed.agents, []bool{false}) {
		t.Fatalf("agents=%v", deps.managed.agents)
	}
}

type testDeps struct {
	store              *wrappedStore
	clock              *fakeClock
	index              *fakeIndex
	client             *fakeClient
	factory            *fakeFactory
	classifier         *fakeClassifier
	classifierSessions *fakeClassifierSessionFactory
	update             *fakeUpdateChecker
	updater            *fakeUpdater
	managed            *fakeManagedSurfaces
	tokens             *fakeTokenReader
}

func testRunner(t *testing.T, now time.Time, tasks []codex.Task, committed state.State) (*Runner, testDeps) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "threadbear")
	store := state.NewStore(dir)
	cfg := config.Default("control")
	if err := store.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	if committed.LastAnnouncedVersion == "" {
		committed.LastAnnouncedVersion = "1.0.0"
	}
	if committed.LastReconciledVersion == "" {
		committed.LastReconciledVersion = "1.0.0"
	}
	if err := store.SaveState(committed); err != nil {
		t.Fatal(err)
	}
	index := &fakeIndex{tasks: append([]codex.Task{}, tasks...)}
	client := &fakeClient{index: index, latest: make(map[string]appserver.RecentEvidence), previous: make(map[string]*appserver.EvidenceTurn), persisted: make(map[string][]string), failTitle: make(map[string]bool)}
	factory := &fakeFactory{client: client}
	classifier := &fakeClassifier{results: make(map[string]status.Classification)}
	classifierSessions := &fakeClassifierSessionFactory{}
	update := &fakeUpdateChecker{result: UpdateStatus{LatestVersion: "1.0.0"}}
	updater := &fakeUpdater{}
	managed := &fakeManagedSurfaces{}
	tokenReader := &fakeTokenReader{snapshots: make(map[string]tokens.Snapshot), errs: make(map[string]error)}
	wrapped := &wrappedStore{store: store}
	clock := &fakeClock{now: now}
	runner, err := New(Dependencies{
		Store: wrapped, Inventory: index, AppServer: factory, ClassifierSessions: classifierSessions, UpdateChecker: update, Updater: updater, ManagedSurfaces: managed, Clock: clock, InstalledVersion: "1.0.0",
		TokenReader: tokenReader,
		NewCycleID:  func() string { return "cycle-1" },
		NewClassifier: func(_ status.EphemeralRunner, cfg config.Config) (Classifier, error) {
			if cfg.ClassifierContextBudgetBytes != 250000 {
				t.Fatalf("context budget=%d", cfg.ClassifierContextBudgetBytes)
			}
			return classifier, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return runner, testDeps{store: wrapped, clock: clock, index: index, client: client, factory: factory, classifier: classifier, classifierSessions: classifierSessions, update: update, updater: updater, managed: managed, tokens: tokenReader}
}

func completedEvidence(now time.Time, user, agent string) appserver.RecentEvidence {
	seconds := now.Unix()
	return appserver.RecentEvidence{ThreadStatus: appserver.ThreadStatus{Type: "idle"}, RecencyAt: &seconds, Latest: &appserver.EvidenceTurn{ID: "turn", Status: "completed", UserMessage: user, AgentMessage: agent}}
}

func record(task codex.Task, taskStatus state.TaskStatus, activity time.Time) state.TaskRecord {
	subject := strings.TrimSpace(strings.TrimLeft(task.Title, "✅⏳🚨🙋🤖➡️❔ "))
	return state.TaskRecord{TaskID: task.TaskID, CapturedRevision: task.Revision, CapturedTitle: task.Title, Status: taskStatus, Provenance: state.ProvenanceFooter, StateStartedAt: activity, LastSubstantiveActivity: activity, DurableSubject: subject, LastAppliedTitle: task.Title, TokenDisplayPosition: tokens.PositionStart}
}

func timePointer(value time.Time) *time.Time { value = value.UTC(); return &value }
func taskID(index int) string                { return fmt.Sprintf("task-%03d", index) }

func TestArchiveRequiresCompleteAndStateAge(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	record := state.TaskRecord{Status: state.StatusComplete, StateStartedAt: now.Add(-13 * 24 * time.Hour), LastSubstantiveActivity: now.Add(-30 * 24 * time.Hour)}
	if archiveEligible(record, now, 14) {
		t.Fatal("newly complete task archived from old activity")
	}
	record.StateStartedAt = now.Add(-15 * 24 * time.Hour)
	if !archiveEligible(record, now, 14) {
		t.Fatal("old complete task was not eligible")
	}
	for _, taskStatus := range []state.TaskStatus{state.StatusRunning, state.StatusBlocked, state.StatusNeedsInput, state.StatusAutomation, state.StatusNextSteps, state.StatusUnknown} {
		record.Status = taskStatus
		if archiveEligible(record, now, 14) {
			t.Fatalf("%s became archive eligible", taskStatus)
		}
	}
}

func TestHeartbeatRepairsManagedSurfacesBeforeInventoryAndClassifier(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	task := codex.Task{TaskID: "semantic", Revision: "2", Title: "Semantic", Source: "vscode"}
	committed := state.New()
	committed.LastUpdateCheck = timePointer(now)
	committed.Tasks[task.TaskID] = record(codex.Task{TaskID: task.TaskID, Revision: "1", Title: task.Title}, state.StatusUnknown, now.Add(-time.Hour))
	runner, deps := testRunner(t, now, []codex.Task{task}, committed)
	managed := &fakeManagedSurfaces{}
	runner.deps.ManagedSurfaces = managed
	deps.index.hook = func(_ int, _ *fakeIndex) {
		if managed.calls != 1 {
			t.Fatalf("inventory ran before managed reconciliation: calls=%d", managed.calls)
		}
	}
	deps.client.latest[task.TaskID] = completedEvidence(now, "done", "no footer")
	deps.classifier.results[task.TaskID] = status.Classification{TaskID: task.TaskID, Revision: task.Revision, Status: state.StatusComplete, Provenance: state.ProvenanceLuna, DurableSubject: "Semantic"}
	if _, err := runner.Run(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if managed.calls != 1 || deps.classifier.calls != 1 {
		t.Fatalf("managed=%d classifier=%d", managed.calls, deps.classifier.calls)
	}
}

func TestHeartbeatCleanIdleManagedComparisonProducesNoOutput(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	committed := state.New()
	committed.LastUpdateCheck = timePointer(now)
	runner, deps := testRunner(t, now, nil, committed)
	managed := &fakeManagedSurfaces{}
	runner.deps.ManagedSurfaces = managed
	value, err := runner.Run(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if !value.(output.HeartbeatResult).Empty() || managed.calls != 1 || deps.index.calls != 1 || deps.factory.opens != 0 {
		t.Fatalf("result=%+v managed=%d inventories=%d opens=%d", value, managed.calls, deps.index.calls, deps.factory.opens)
	}
}

func TestHeartbeatIdleManagedRepairFailureIsVisible(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	committed := state.New()
	committed.LastUpdateCheck = timePointer(now)
	runner, deps := testRunner(t, now, nil, committed)
	runner.deps.ManagedSurfaces = &fakeManagedSurfaces{err: errors.New("synthetic managed failure")}
	value, err := runner.Run(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	result := value.(output.HeartbeatResult)
	if result.Empty() || result.CycleID != "managed-surfaces" || result.ErrorCode != "managed_surfaces_unavailable" || deps.factory.opens != 0 {
		t.Fatalf("result=%+v opens=%d", result, deps.factory.opens)
	}
}

func TestHeartbeatReportsManagedDriftRepair(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	committed := state.New()
	committed.LastUpdateCheck = timePointer(now)
	runner, deps := testRunner(t, now, nil, committed)
	managed := &fakeManagedSurfaces{resources: []string{"skill", "agents"}}
	runner.deps.ManagedSurfaces = managed
	value, err := runner.Run(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	result := value.(output.HeartbeatResult)
	if result.Empty() || strings.Join(result.ManagedResources, ",") != "skill,agents" || result.CycleID != "managed-surfaces" || deps.factory.opens != 0 {
		t.Fatalf("result=%+v opens=%d", result, deps.factory.opens)
	}
}

func TestHeartbeatManagedRepairFailureStillDeliversUpdateNotice(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	committed := state.New()
	committed.LastUpdateCheck = timePointer(now.Add(-25 * time.Hour))
	runner, deps := testRunner(t, now, nil, committed)
	cfg := config.Default("control")
	cfg.AutoUpdateEnabled = false
	deps.store.configOverride = &cfg
	runner.deps.ManagedSurfaces = &fakeManagedSurfaces{err: errors.New("synthetic managed failure")}
	deps.update.result = UpdateStatus{LatestVersion: "1.2.0", Newer: true}
	value, err := runner.Run(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	result := value.(output.HeartbeatResult)
	if result.ErrorCode != "managed_surfaces_unavailable" || len(deps.client.notices) != 1 {
		t.Fatalf("result=%+v notices=%v", result, deps.client.notices)
	}
	stored, err := deps.store.store.LoadState()
	if err != nil || stored.Generation != committed.Generation+1 || len(stored.DeliveredNoticeVersions) != 1 || stored.DeliveredNoticeVersions[0] != "1.2.0" || stored.LastUpdateCheck == nil || !stored.LastUpdateCheck.Equal(now) {
		t.Fatalf("stored=%+v err=%v", stored, err)
	}
}

func TestHeartbeatManagedRepairFailureDegradesUnresolvedTasksWithoutAborting(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	task := codex.Task{TaskID: "semantic", Revision: "2", Title: "Semantic", Source: "vscode"}
	committed := state.New()
	committed.LastUpdateCheck = timePointer(now.Add(-25 * time.Hour))
	committed.Tasks[task.TaskID] = record(codex.Task{TaskID: task.TaskID, Revision: "1", Title: task.Title}, state.StatusUnknown, now.Add(-time.Hour))
	runner, deps := testRunner(t, now, []codex.Task{task}, committed)
	cfg := config.Default("control")
	cfg.AutoUpdateEnabled = false
	deps.store.configOverride = &cfg
	runner.deps.ManagedSurfaces = &fakeManagedSurfaces{err: errors.New("synthetic managed failure")}
	deps.update.result = UpdateStatus{LatestVersion: "1.2.0", Newer: true}
	deps.client.latest[task.TaskID] = completedEvidence(now, "done", "no footer")
	value, err := runner.Run(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	result := value.(output.HeartbeatResult)
	if deps.classifier.calls != 0 || len(result.Retries) != 1 || result.Retries[0].ErrorCode != "managed_surfaces_unavailable" || len(deps.client.notices) != 1 {
		t.Fatalf("result=%+v classifier=%d notices=%v", result, deps.classifier.calls, deps.client.notices)
	}
	stored, err := deps.store.store.LoadState()
	if err != nil || stored.Generation != committed.Generation+1 || stored.Tasks[task.TaskID].Status != state.StatusUnknown || len(stored.DeliveredNoticeVersions) != 1 || stored.DeliveredNoticeVersions[0] != "1.2.0" || stored.LastUpdateCheck == nil || !stored.LastUpdateCheck.Equal(now) {
		t.Fatalf("stored=%+v err=%v", stored, err)
	}
}

func TestAppServerUnavailableDoesNotStageTitleOrConsumeBootstrap(t *testing.T) {
	now := time.Date(2026, 7, 29, 11, 0, 0, 0, time.UTC)
	task := codex.Task{TaskID: "bootstrap", Revision: "1", Title: "✅ Existing subject", Source: "vscode"}
	committed := state.New()
	committed.LastUpdateCheck = timePointer(now)
	runner, deps := testRunner(t, now, []codex.Task{task}, committed)
	runner.deps.AppServer = nil

	value, err := runner.Run(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	result := value.(output.HeartbeatResult)
	stored, err := deps.store.store.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if stored.BootstrapComplete || len(stored.PendingTitlePlans) != 0 {
		t.Fatalf("bootstrap=%t pending=%+v", stored.BootstrapComplete, stored.PendingTitlePlans)
	}
	record := stored.Tasks[task.TaskID]
	if record.Retry == nil || record.Retry.Operation != "evidence" || record.Retry.ErrorCode != "app_server_unavailable" {
		t.Fatalf("retry=%+v result=%+v", record.Retry, result)
	}
}

func TestFreshStateAdoptsStrictLeadingStatusWithoutClassifierOrOwnershipInference(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	task := codex.Task{TaskID: "bootstrap", Revision: "1", Title: "✅ 26k User subject → user action", Source: "vscode"}
	committed := state.New()
	committed.LastUpdateCheck = timePointer(now)
	runner, deps := testRunner(t, now, []codex.Task{task}, committed)
	deps.client.latest[task.TaskID] = completedEvidence(now, "done", "no footer")
	if _, err := runner.Run(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	stored, err := deps.store.store.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	record := stored.Tasks[task.TaskID]
	if deps.classifier.calls != 0 || record.Provenance != state.ProvenanceBootstrapTitle || record.DurableSubject != "26k User subject → user action" || record.ManagedAction != "" || record.ManagedTokenDisplay != "" {
		t.Fatalf("classifier=%d record=%+v", deps.classifier.calls, record)
	}
	if !stored.BootstrapComplete || len(stored.PendingTitlePlans) != 0 || len(deps.client.titles) != 0 {
		t.Fatalf("bootstrap=%t pending=%+v writes=%v", stored.BootstrapComplete, stored.PendingTitlePlans, deps.client.titles)
	}
}

func TestVersionOneMigrationFlagPreventsLeadingStatusBootstrap(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	task := codex.Task{TaskID: "legacy", Revision: "1", Title: "🚨 Legacy title", Source: "vscode"}
	committed := state.New()
	committed.BootstrapComplete = true
	committed.LastUpdateCheck = timePointer(now)
	runner, deps := testRunner(t, now, []codex.Task{task}, committed)
	deps.client.latest[task.TaskID] = completedEvidence(now, "done", "no footer")
	if _, err := runner.Run(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	stored, err := deps.store.store.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if deps.classifier.calls != 1 || stored.Tasks[task.TaskID].Provenance == state.ProvenanceBootstrapTitle {
		t.Fatalf("classifier=%d record=%+v", deps.classifier.calls, stored.Tasks[task.TaskID])
	}
}

func TestBootstrapUsesStrictLeadingTitleWhenEvidenceReadFails(t *testing.T) {
	now := time.Date(2026, 7, 31, 15, 0, 0, 0, time.UTC)
	task := codex.Task{TaskID: "bootstrap", Revision: "1", Title: "🚨 Deterministic subject", Source: "vscode"}
	committed := state.New()
	committed.LastUpdateCheck = timePointer(now)
	runner, deps := testRunner(t, now, []codex.Task{task}, committed)
	if _, err := runner.Run(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	after, _ := deps.store.store.LoadState()
	record := after.Tasks[task.TaskID]
	if !after.BootstrapComplete || record.Status != state.StatusBlocked || record.Provenance != state.ProvenanceBootstrapTitle || record.DurableSubject != "Deterministic subject" || record.Retry != nil || deps.classifier.calls != 0 {
		t.Fatalf("state=%+v classifier=%d", after, deps.classifier.calls)
	}
}

func TestBootstrapEvidenceFailureWithoutStrictTitleRemainsIncomplete(t *testing.T) {
	now := time.Date(2026, 7, 31, 15, 30, 0, 0, time.UTC)
	task := codex.Task{TaskID: "bootstrap", Revision: "1", Title: "Plain subject", Source: "vscode"}
	committed := state.New()
	committed.LastUpdateCheck = timePointer(now)
	runner, deps := testRunner(t, now, []codex.Task{task}, committed)
	if _, err := runner.Run(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	after, _ := deps.store.store.LoadState()
	record := after.Tasks[task.TaskID]
	if after.BootstrapComplete || record.Status != state.StatusUnknown || record.Retry == nil || len(after.PendingTitlePlans) != 0 || len(deps.client.titles) != 0 {
		t.Fatalf("state=%+v titles=%v", after, deps.client.titles)
	}
}

func TestDisabledRenameCancelsMigratedRefreshAndAllowsArchive(t *testing.T) {
	now := time.Date(2026, 7, 31, 16, 0, 0, 0, time.UTC)
	task := codex.Task{TaskID: "archive", Revision: "1", Title: "✅ Subject", Source: "vscode"}
	committed := state.New()
	committed.BootstrapComplete = true
	committed.LastUpdateCheck = timePointer(now)
	archiveRecord := record(task, state.StatusComplete, now.Add(-15*24*time.Hour))
	archiveRecord.StateStartedAt = now.Add(-15 * 24 * time.Hour)
	committed.Tasks[task.TaskID] = archiveRecord
	operationID := state.TitleOperationID(task.TaskID, task.Revision, task.Title, task.Title)
	committed.PendingTitlePlans[task.TaskID] = state.PendingTitlePlan{OperationID: operationID, TaskID: task.TaskID, ExpectedRevision: task.Revision, ExpectedTitle: task.Title, DesiredTitle: task.Title, NativeOutcome: state.NativeTitlePending}
	runner, deps := testRunner(t, now, []codex.Task{task}, committed)
	cfg := config.Default("control")
	cfg.RenameEnabled = false
	deps.store.configOverride = &cfg
	oldActivity := now.Add(-15 * 24 * time.Hour).Unix()
	deps.client.latest[task.TaskID] = appserver.RecentEvidence{ThreadStatus: appserver.ThreadStatus{Type: "idle"}, RecencyAt: &oldActivity, Latest: &appserver.EvidenceTurn{ID: "turn", Status: "completed"}}
	result, err := runner.Run(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := deps.store.store.LoadState()
	if !reflect.DeepEqual(result.(output.HeartbeatResult).ArchivedIDs, []string{task.TaskID}) || !reflect.DeepEqual(deps.client.archives, []string{task.TaskID}) || len(after.PendingTitlePlans) != 0 || len(deps.client.titles) != 0 {
		t.Fatalf("result=%+v state=%+v archives=%v titles=%v", result, after, deps.client.archives, deps.client.titles)
	}
}

func migratedPlan(task codex.Task, desired string) state.PendingTitlePlan {
	return state.PendingTitlePlan{
		OperationID: state.TitleOperationID(task.TaskID, task.Revision, task.Title, desired),
		TaskID:      task.TaskID, ExpectedRevision: task.Revision, ExpectedTitle: task.Title, DesiredTitle: desired,
		DurableSubject: "Subject", NativeOutcome: state.NativeTitlePending,
	}
}

func TestPendingTitleMigrationSettlesAlreadyAppliedPlanWithoutReads(t *testing.T) {
	now := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	task := codex.Task{TaskID: "task-a", Revision: "1", Title: "✅ Subject", Source: "vscode"}
	committed := state.New()
	committed.BootstrapComplete = true
	committed.LastUpdateCheck = timePointer(now)
	committed.Tasks[task.TaskID] = record(task, state.StatusComplete, now)
	plan := migratedPlan(task, task.Title)
	plan.NativeOutcome = state.NativeTitleSucceeded
	reported := now.Add(-time.Second)
	plan.NativeReportedAt = &reported
	plan.CanonicalCheckedAt = &reported
	committed.PendingTitlePlans[task.TaskID] = plan
	runner, deps := testRunner(t, now, []codex.Task{task}, committed)
	if _, err := runner.Run(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	stored, _ := deps.store.store.LoadState()
	if len(stored.PendingTitlePlans) != 0 || stored.Tasks[task.TaskID].LastAppliedTitle != task.Title || len(deps.client.titles) != 0 {
		t.Fatalf("state=%+v", stored)
	}
}

func TestPendingTitleMigrationSettlesAlreadyAppliedAdvancedRevisionWithoutClassifier(t *testing.T) {
	now := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	expected := codex.Task{TaskID: "task-a", Revision: "1", Title: "Subject", Source: "vscode"}
	current := codex.Task{TaskID: "task-a", Revision: "2", Title: "✅ Subject", Source: "vscode"}
	committed := state.New()
	committed.BootstrapComplete = true
	committed.LastUpdateCheck = timePointer(now)
	committed.Tasks[expected.TaskID] = record(expected, state.StatusComplete, now)
	plan := migratedPlan(expected, current.Title)
	plan.NativeOutcome = state.NativeTitleSucceeded
	reported := now.Add(-time.Second)
	plan.NativeReportedAt = &reported
	plan.CanonicalCheckedAt = &reported
	committed.PendingTitlePlans[expected.TaskID] = plan
	runner, deps := testRunner(t, now, []codex.Task{current}, committed)
	if _, err := runner.Run(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	stored, _ := deps.store.store.LoadState()
	if len(stored.PendingTitlePlans) != 0 || stored.Tasks[current.TaskID].CapturedRevision != current.Revision || deps.classifier.calls != 0 {
		t.Fatalf("state=%+v", stored)
	}
}

func TestPendingTitleMigrationPromotesValidPlanWithoutClassificationReads(t *testing.T) {
	now := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	task := codex.Task{TaskID: "task-a", Revision: "1", Title: "Subject", Source: "vscode"}
	committed := state.New()
	committed.BootstrapComplete = true
	committed.LastUpdateCheck = timePointer(now)
	committed.Tasks[task.TaskID] = record(task, state.StatusComplete, now)
	plan := migratedPlan(task, "✅ Subject")
	committed.PendingTitlePlans[task.TaskID] = plan
	runner, deps := testRunner(t, now, []codex.Task{task}, committed)
	if _, err := runner.Run(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	stored, _ := deps.store.store.LoadState()
	if stored.PendingTitlePlans[task.TaskID].OperationID != plan.OperationID || len(deps.client.titles) != 0 || deps.classifier.calls != 0 {
		t.Fatalf("state=%+v", stored)
	}
}

func TestPendingTitleMigrationSameTitleForcesOneSetterCall(t *testing.T) {
	now := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	task := codex.Task{TaskID: "task-a", Revision: "1", Title: "✅ Subject", Source: "vscode"}
	committed := state.New()
	committed.BootstrapComplete = true
	committed.LastUpdateCheck = timePointer(now)
	committed.Tasks[task.TaskID] = record(task, state.StatusComplete, now)
	plan := migratedPlan(task, task.Title)
	committed.PendingTitlePlans[task.TaskID] = plan
	runner, deps := testRunner(t, now, []codex.Task{task}, committed)
	if _, err := runner.Run(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	stored, _ := deps.store.store.LoadState()
	if stored.PendingTitlePlans[task.TaskID].OperationID != plan.OperationID || len(deps.client.titles) != 0 {
		t.Fatalf("state=%+v writes=%v", stored, deps.client.titles)
	}
}

func TestPendingTitleMigrationDropsMissingAndDriftedPlans(t *testing.T) {
	now := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	expected := codex.Task{TaskID: "task-a", Revision: "1", Title: "Subject", Source: "vscode"}
	committed := state.New()
	committed.BootstrapComplete = true
	committed.LastUpdateCheck = timePointer(now)
	committed.Tasks[expected.TaskID] = record(expected, state.StatusComplete, now)
	committed.PendingTitlePlans[expected.TaskID] = migratedPlan(expected, "✅ Subject")
	runner, deps := testRunner(t, now, nil, committed)
	if _, err := runner.Run(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	stored, _ := deps.store.store.LoadState()
	if len(stored.PendingTitlePlans) != 0 || len(deps.client.titles) != 0 {
		t.Fatalf("state=%+v", stored)
	}
}

func TestPendingTitleMigrationRetainsPlanOnFailedDirectWrite(t *testing.T) {
	now := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	task := codex.Task{TaskID: "task-a", Revision: "1", Title: "Subject", Source: "vscode"}
	committed := state.New()
	committed.BootstrapComplete = true
	committed.LastUpdateCheck = timePointer(now)
	committed.Tasks[task.TaskID] = record(task, state.StatusComplete, now)
	plan := migratedPlan(task, "✅ Subject")
	committed.PendingTitlePlans[task.TaskID] = plan
	runner, deps := testRunner(t, now, []codex.Task{task}, committed)
	value, err := runner.Run(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	stored, _ := deps.store.store.LoadState()
	if stored.PendingTitlePlans[task.TaskID].OperationID != plan.OperationID || len(deps.client.titles) != 0 || len(value.(output.HeartbeatResult).Retries) != 0 {
		t.Fatalf("state=%+v result=%+v", stored, value)
	}
}

func TestPendingTitleMigrationRevalidatesBeforeSetter(t *testing.T) {
	now := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	task := codex.Task{TaskID: "task-a", Revision: "1", Title: "Subject", Source: "vscode"}
	committed := state.New()
	committed.BootstrapComplete = true
	committed.LastUpdateCheck = timePointer(now)
	committed.Tasks[task.TaskID] = record(task, state.StatusComplete, now)
	committed.PendingTitlePlans[task.TaskID] = migratedPlan(task, "✅ Subject")
	runner, deps := testRunner(t, now, []codex.Task{task}, committed)
	deps.index.hook = func(call int, index *fakeIndex) {
		if call == 2 {
			changed, _ := index.task(task.TaskID)
			changed.Revision = "2"
			changed.Title = "User edit"
			index.replace(changed)
		}
	}

	if _, err := runner.Run(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	stored, _ := deps.store.store.LoadState()
	if len(deps.client.titles) != 0 || stored.PendingTitlePlans[task.TaskID].OperationID == "" {
		t.Fatalf("state=%+v titles=%v", stored, deps.client.titles)
	}
}

func TestTitleVerificationIsSavedBeforeSameTaskArchive(t *testing.T) {
	now := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	task := codex.Task{TaskID: "task-a", Revision: "2", Title: "Subject", Source: "vscode"}
	committed := state.New()
	committed.BootstrapComplete = true
	committed.LastUpdateCheck = timePointer(now)
	previous := record(codex.Task{TaskID: task.TaskID, Revision: "1", Title: task.Title}, state.StatusComplete, now.Add(-15*24*time.Hour))
	previous.StateStartedAt = now.Add(-15 * 24 * time.Hour)
	committed.Tasks[task.TaskID] = previous
	runner, deps := testRunner(t, now, []codex.Task{task}, committed)
	old := now.Add(-15 * 24 * time.Hour).Unix()
	deps.client.latest[task.TaskID] = appserver.RecentEvidence{ThreadStatus: appserver.ThreadStatus{Type: "idle"}, RecencyAt: &old, Latest: &appserver.EvidenceTurn{ID: "turn", Status: "completed", AgentMessage: "🧵🐻 complete"}}
	if _, err := runner.Run(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	stored, _ := deps.store.store.LoadState()
	if stored.PendingTitlePlans[task.TaskID].DesiredTitle != "✅ Subject" || len(deps.client.archives) != 0 {
		t.Fatalf("state=%+v archives=%v", stored, deps.client.archives)
	}
}

func TestCrashApplyingSameTitleRefreshRetriesSetter(t *testing.T) {
	now := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	task := codex.Task{TaskID: "task-a", Revision: "1", Title: "✅ Subject", Source: "vscode"}
	committed := state.New()
	committed.BootstrapComplete = true
	committed.LastUpdateCheck = timePointer(now)
	committed.Tasks[task.TaskID] = record(task, state.StatusComplete, now)
	runner, deps := testRunner(t, now, []codex.Task{task}, committed)
	cycle := state.NewCycle("cycle", committed.Generation, now)
	cycle.Inventory[task.TaskID] = state.CapturedTask{TaskID: task.TaskID, Revision: task.Revision, Title: task.Title, LastSubstantiveActivity: now}
	cycle.Results[task.TaskID] = state.ClassificationResult{TaskID: task.TaskID, Revision: task.Revision, Status: state.StatusComplete, Provenance: state.ProvenanceFooter, DurableSubject: "Subject"}
	cycle.Operations["title:"+task.TaskID] = state.CycleOperation{Kind: state.OperationTitle, Stage: state.StageApplying, TaskID: task.TaskID, ExpectedRevision: task.Revision, ExpectedTitle: task.Title, DesiredTitle: task.Title, DurableSubject: "Subject", ForceWrite: true}
	if err := deps.store.store.SaveCycle(cycle); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Run(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	stored, _ := deps.store.store.LoadState()
	if stored.PendingTitlePlans[task.TaskID].DesiredTitle != task.Title || len(deps.client.titles) != 0 {
		t.Fatalf("state=%+v", stored)
	}
}

func TestCrashAppliedTitleRecoversWithoutRepeatingSetter(t *testing.T) {
	now := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	current := codex.Task{TaskID: "task-a", Revision: "2", Title: "✅ Subject", Source: "vscode"}
	expected := codex.Task{TaskID: current.TaskID, Revision: "1", Title: "Subject", Source: "vscode"}
	committed := state.New()
	committed.BootstrapComplete = true
	committed.LastUpdateCheck = timePointer(now)
	committed.Tasks[current.TaskID] = record(expected, state.StatusComplete, now)
	runner, deps := testRunner(t, now, []codex.Task{current}, committed)
	cycle := state.NewCycle("cycle-applied", committed.Generation, now)
	cycle.Inventory[current.TaskID] = state.CapturedTask{TaskID: current.TaskID, Revision: expected.Revision, Title: expected.Title, LastSubstantiveActivity: now}
	cycle.Results[current.TaskID] = state.ClassificationResult{TaskID: current.TaskID, Revision: expected.Revision, Status: state.StatusComplete, Provenance: state.ProvenanceFooter, DurableSubject: "Subject"}
	cycle.Operations["title:"+current.TaskID] = state.CycleOperation{Kind: state.OperationTitle, Stage: state.StageApplied, TaskID: current.TaskID, ExpectedRevision: expected.Revision, ExpectedTitle: expected.Title, DesiredTitle: current.Title, DurableSubject: "Subject"}
	if err := deps.store.store.SaveCycle(cycle); err != nil {
		t.Fatal(err)
	}

	if _, err := runner.Run(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	stored, _ := deps.store.store.LoadState()
	if len(deps.client.titles) != 0 || stored.Tasks[current.TaskID].CapturedRevision != current.Revision || stored.Tasks[current.TaskID].CapturedTitle != current.Title {
		t.Fatalf("titles=%v state=%+v", deps.client.titles, stored.Tasks[current.TaskID])
	}
}

func TestSourceNativePlanSettlesOnlyWhenPersistedFooterMatches(t *testing.T) {
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	expected := codex.Task{TaskID: "control", Revision: "1", Title: "✅ Control task", Source: "vscode"}
	current := codex.Task{TaskID: "control", Revision: "2", Title: "🙋 Control task → choose the release region", Source: "vscode"}
	committed := state.New()
	committed.BootstrapComplete = true
	committed.LastUpdateCheck = timePointer(now)
	committed.Tasks[expected.TaskID] = record(expected, state.StatusComplete, now)
	plan := state.PendingTitlePlan{
		OperationID: state.TitleOperationID(expected.TaskID, expected.Revision, expected.Title, current.Title),
		TaskID:      expected.TaskID, ExpectedRevision: expected.Revision, ExpectedTitle: expected.Title, DesiredTitle: current.Title,
		DurableSubject: "Control task", ManagedAction: "choose the release region", NativeOutcome: state.NativeTitleSucceeded,
		ExpectedFooter: "🧵🐻 needs input (you): choose the release region",
	}
	reported := now.Add(-time.Second)
	plan.NativeReportedAt = &reported
	plan.CanonicalCheckedAt = &reported
	committed.PendingTitlePlans[expected.TaskID] = plan
	runner, deps := testRunner(t, now, []codex.Task{current}, committed)
	deps.store.configOverride = func() *config.Config { cfg := config.Default(expected.TaskID); return &cfg }()
	deps.client.latest[current.TaskID] = completedEvidence(now, "choose", "🧵🐻 needs input (you): choose the release region")
	if _, err := runner.Run(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	stored, _ := deps.store.store.LoadState()
	if len(stored.PendingTitlePlans) != 0 || stored.Tasks[current.TaskID].LastAppliedTitle != current.Title || stored.Tasks[current.TaskID].Status != state.StatusNeedsInput {
		t.Fatalf("state=%+v", stored)
	}
}

func TestSourceNativePlanRecomputesWhenPersistedFooterDiffers(t *testing.T) {
	now := time.Date(2026, 8, 3, 12, 30, 0, 0, time.UTC)
	expected := codex.Task{TaskID: "control", Revision: "1", Title: "✅ Control task", Source: "vscode"}
	current := codex.Task{TaskID: "control", Revision: "2", Title: "🙋 Control task → choose the release region", Source: "vscode"}
	committed := state.New()
	committed.BootstrapComplete = true
	committed.LastUpdateCheck = timePointer(now)
	committed.Tasks[expected.TaskID] = record(expected, state.StatusComplete, now)
	plan := state.PendingTitlePlan{OperationID: state.TitleOperationID(expected.TaskID, expected.Revision, expected.Title, current.Title), TaskID: expected.TaskID, ExpectedRevision: expected.Revision, ExpectedTitle: expected.Title, DesiredTitle: current.Title, DurableSubject: "Control task", ManagedAction: "choose the release region", NativeOutcome: state.NativeTitleSucceeded, ExpectedFooter: "🧵🐻 needs input (you): choose the release region"}
	reported := now.Add(-time.Second)
	plan.NativeReportedAt = &reported
	plan.CanonicalCheckedAt = &reported
	committed.PendingTitlePlans[expected.TaskID] = plan
	runner, deps := testRunner(t, now, []codex.Task{current}, committed)
	deps.store.configOverride = func() *config.Config { cfg := config.Default(expected.TaskID); return &cfg }()
	deps.client.latest[current.TaskID] = completedEvidence(now, "done", "🧵🐻 complete")
	if _, err := runner.Run(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	stored, _ := deps.store.store.LoadState()
	replacement := stored.PendingTitlePlans[current.TaskID]
	if replacement.ExpectedFooter != "" || replacement.DesiredTitle != "✅ Control task" || replacement.NativeOutcome != state.NativeTitlePending {
		t.Fatalf("replacement=%+v", replacement)
	}
}
