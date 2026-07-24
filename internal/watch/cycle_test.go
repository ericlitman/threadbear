package watch

import (
	"bytes"
	"context"
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
)

type fakeClock struct{ now time.Time }

func (f fakeClock) Now() time.Time { return f.now }

type fakeIndex struct {
	tasks []codex.Task
	calls int
	hook  func(int, *fakeIndex)
}

func (f *fakeIndex) Inventory(context.Context, string) (codex.Inventory, error) {
	f.calls++
	if f.hook != nil {
		f.hook(f.calls, f)
	}
	return codex.Inventory{Tasks: append([]codex.Task{}, f.tasks...)}, nil
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
	titles                 []string
	archives               []string
	notices                []string
	failTitle              map[string]bool
	failNotice             bool
	archiveErrorAfterApply bool
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
func (f *fakeClient) SetTitle(_ context.Context, taskID, value string) error {
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
	f.archives = append(f.archives, taskID)
	f.index.remove(taskID)
	if f.archiveErrorAfterApply {
		return errors.New("archive response lost")
	}
	return nil
}
func (f *fakeClient) InsertNotice(_ context.Context, taskID string, text string) error {
	if !f.failNotice {
		f.notices = append(f.notices, text)
		f.latest[taskID] = appserver.RecentEvidence{Latest: &appserver.EvidenceTurn{ID: "notice", Status: "completed", AgentMessage: text}}
	}
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

type fakeClassifier struct {
	calls       int
	requestPrev []string
	results     map[string]status.Classification
}

func (f *fakeClassifier) ClassifyWithPrevious(ctx context.Context, tasks []status.TaskEvidence, load status.PreviousEvidenceLoader) []status.Classification {
	f.calls++
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
	return result
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

type wrappedStore struct {
	store          *state.Store
	configOverride *config.Config
	failSaveState  bool
	failRemove     bool
	failCycleAfter int
	cycleSaves     int
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
	if stdout.Len() != 0 || deps.factory.opens != 0 || deps.classifier.calls != 0 || deps.index.calls != 1 {
		t.Fatalf("stdout=%q opens=%d classifier=%d inventories=%d", stdout.String(), deps.factory.opens, deps.classifier.calls, deps.index.calls)
	}
}

func TestHeartbeatDeterministicRuntimeAndPartialSiblingFailure(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	tasks := []codex.Task{
		{TaskID: "task-a", Revision: "2", Title: "✅ Active work", Source: "vscode"},
		{TaskID: "task-b", Revision: "2", Title: "⏳ Finished work", Source: "vscode"},
	}
	committed := state.New()
	committed.LastUpdateCheck = timePointer(now)
	committed.Tasks["task-a"] = record(codex.Task{TaskID: "task-a", Revision: "1", Title: "✅ Active work"}, state.StatusComplete, now)
	committed.Tasks["task-b"] = record(codex.Task{TaskID: "task-b", Revision: "1", Title: "✅ Finished work"}, state.StatusComplete, now)
	runner, deps := testRunner(t, now, tasks, committed)
	activeAt := now.Unix()
	deps.client.latest["task-a"] = appserver.RecentEvidence{ThreadStatus: appserver.ThreadStatus{Type: "active"}, RecencyAt: &activeAt, Latest: &appserver.EvidenceTurn{ID: "turn-a", Status: "inProgress", UserMessage: "continue"}}
	deps.client.latest["task-b"] = completedEvidence(now, "done", "🐻 complete · next (none): none")
	deps.client.failTitle["task-b"] = true
	value, err := runner.Run(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	result := value.(output.HeartbeatResult)
	if deps.classifier.calls != 0 || len(result.Changed) != 1 || result.Changed[0].TaskID != "task-a" || result.Changed[0].State != state.StatusRunning || len(result.Retries) != 1 || result.Retries[0].TaskID != "task-b" {
		t.Fatalf("result=%+v classifier=%d", result, deps.classifier.calls)
	}
	if task, _ := deps.index.task("task-a"); !strings.HasPrefix(task.Title, "⏳ ") {
		t.Fatalf("title=%q", task.Title)
	}
	stored, loadErr := deps.store.store.LoadState()
	if loadErr != nil || stored.Tasks["task-b"].Retry == nil || stored.Tasks["task-a"].Status != state.StatusRunning {
		t.Fatalf("state=%+v err=%v", stored, loadErr)
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
	deps.client.latest["sibling"] = completedEvidence(now, "done", "🐻 complete · next (none): none")
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
	deps.client.latest["sibling"] = completedEvidence(now, "done", "🐻 complete · next (none): none")
	if _, err := runner.Run(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	stored, _ := deps.store.store.LoadState()
	if stored.Tasks["retry"].Retry == nil || contains(deps.client.latestReads, "retry") {
		t.Fatalf("retry=%+v reads=%v", stored.Tasks["retry"].Retry, deps.client.latestReads)
	}
	deps.clock.now = now.Add(time.Hour + time.Second)
	deps.client.latest["retry"] = completedEvidence(deps.clock.now, "done", "🐻 complete · next (none): none")
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
	record := record(codex.Task{TaskID: task.TaskID, Revision: "1", Title: task.Title}, state.StatusNextSteps, now)
	record.DurableSubject = "Ship release"
	record.ManagedAction = "deploy production"
	committed.Tasks[task.TaskID] = record
	runner, deps := testRunner(t, now, []codex.Task{task}, committed)
	activeAt := now.Unix()
	deps.client.latest[task.TaskID] = appserver.RecentEvidence{ThreadStatus: appserver.ThreadStatus{Type: "active"}, RecencyAt: &activeAt, Latest: &appserver.EvidenceTurn{ID: "turn", Status: "inProgress"}}
	if _, err := runner.Run(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	current, _ := deps.index.task(task.TaskID)
	if current.Title != "⏳ Ship release" {
		t.Fatalf("title=%q", current.Title)
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
	deps.client.latest["restored"] = completedEvidence(now, "restore", "🐻 complete · next (none): none")
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
	t.Run("title", func(t *testing.T) {
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
		if len(deps.client.titles) != 1 || deps.classifier.calls != 1 {
			t.Fatalf("titles=%v classifier=%d", deps.client.titles, deps.classifier.calls)
		}
		deps.store.failSaveState = false
		if _, err := runner.Run(context.Background(), false); err != nil {
			t.Fatal(err)
		}
		if len(deps.client.titles) != 1 || deps.classifier.calls != 1 {
			t.Fatalf("operation repeated: titles=%v classifier=%d", deps.client.titles, deps.classifier.calls)
		}
		stored, _ := deps.store.store.LoadState()
		current, _ := deps.index.task("semantic")
		if stored.Tasks["semantic"].CapturedRevision != current.Revision || stored.Tasks["semantic"].CapturedTitle != current.Title {
			t.Fatalf("stored=%+v current=%+v", stored.Tasks["semantic"], current)
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
		deps.store.failSaveState = true
		if _, err := runner.Run(context.Background(), false); err == nil {
			t.Fatal("first run did not crash")
		}
		if len(deps.client.archives) != 1 {
			t.Fatalf("archives=%v", deps.client.archives)
		}
		deps.store.failSaveState = false
		if _, err := runner.Run(context.Background(), false); err != nil {
			t.Fatal(err)
		}
		if len(deps.client.archives) != 1 {
			t.Fatalf("archive repeated: %v", deps.client.archives)
		}
		stored, _ := deps.store.store.LoadState()
		if _, ok := stored.Archives[task.TaskID]; !ok {
			t.Fatal("archive ownership missing after recovery")
		}
	})
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
	t.Run("title already applied", func(t *testing.T) {
		current := codex.Task{TaskID: "title", Revision: "2", Title: "✅ Title", Source: "vscode"}
		committed := state.New()
		committed.LastUpdateCheck = timePointer(now)
		committed.Tasks[current.TaskID] = record(codex.Task{TaskID: current.TaskID, Revision: "1", Title: "Title"}, state.StatusComplete, now)
		runner, deps := testRunner(t, now, []codex.Task{current}, committed)
		cycle := state.NewCycle("cycle-1", committed.Generation, now)
		cycle.Inventory[current.TaskID] = state.CapturedTask{TaskID: current.TaskID, Revision: "1", Title: "Title", LastSubstantiveActivity: now}
		cycle.Results[current.TaskID] = state.ClassificationResult{TaskID: current.TaskID, Revision: "1", Status: state.StatusComplete, Provenance: state.ProvenanceFooter, DurableSubject: "Title"}
		cycle.Operations["title:title"] = state.CycleOperation{Kind: state.OperationTitle, Stage: state.StageApplying, TaskID: current.TaskID, ExpectedRevision: "1", ExpectedTitle: "Title", DesiredTitle: current.Title}
		if err := deps.store.store.SaveCycle(cycle); err != nil {
			t.Fatal(err)
		}
		if _, err := runner.Run(context.Background(), false); err != nil {
			t.Fatal(err)
		}
		if len(deps.client.titles) != 0 {
			t.Fatalf("title repeated: %v", deps.client.titles)
		}
	})
	t.Run("title not applied", func(t *testing.T) {
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
		if len(deps.client.titles) != 1 {
			t.Fatalf("title was not retried: %v", deps.client.titles)
		}
	})
	t.Run("archive", func(t *testing.T) {
		committed := state.New()
		committed.LastUpdateCheck = timePointer(now)
		runner, deps := testRunner(t, now, nil, committed)
		cycle := state.NewCycle("cycle-1", committed.Generation, now)
		cycle.Inventory["complete"] = state.CapturedTask{TaskID: "complete", Revision: "1", Title: "✅ Complete", LastSubstantiveActivity: now.Add(-15 * 24 * time.Hour)}
		cycle.Results["complete"] = state.ClassificationResult{TaskID: "complete", Revision: "1", Status: state.StatusComplete, Provenance: state.ProvenanceFooter, DurableSubject: "Complete"}
		cycle.Operations["archive:complete"] = state.CycleOperation{Kind: state.OperationArchive, Stage: state.StageApplying, TaskID: "complete", ExpectedRevision: "1", ExpectedTitle: "✅ Complete"}
		if err := deps.store.store.SaveCycle(cycle); err != nil {
			t.Fatal(err)
		}
		if _, err := runner.Run(context.Background(), false); err != nil {
			t.Fatal(err)
		}
		stored, _ := deps.store.store.LoadState()
		if _, ok := stored.Archives["complete"]; ok || len(deps.client.archives) != 0 {
			t.Fatalf("external archive was claimed: archives=%v stored=%+v", deps.client.archives, stored.Archives)
		}
	})
	t.Run("applied archive", func(t *testing.T) {
		committed := state.New()
		committed.LastUpdateCheck = timePointer(now)
		runner, deps := testRunner(t, now, nil, committed)
		cycle := state.NewCycle("cycle-1", committed.Generation, now)
		cycle.Inventory["complete"] = state.CapturedTask{TaskID: "complete", Revision: "1", Title: "✅ Complete", LastSubstantiveActivity: now.Add(-15 * 24 * time.Hour)}
		cycle.Results["complete"] = state.ClassificationResult{TaskID: "complete", Revision: "1", Status: state.StatusComplete, Provenance: state.ProvenanceFooter, DurableSubject: "Complete"}
		cycle.Operations["archive:complete"] = state.CycleOperation{Kind: state.OperationArchive, Stage: state.StageApplied, TaskID: "complete", ExpectedRevision: "1", ExpectedTitle: "✅ Complete"}
		if err := deps.store.store.SaveCycle(cycle); err != nil {
			t.Fatal(err)
		}
		if _, err := runner.Run(context.Background(), false); err != nil {
			t.Fatal(err)
		}
		stored, _ := deps.store.store.LoadState()
		if _, ok := stored.Archives["complete"]; !ok || len(deps.client.archives) != 0 {
			t.Fatalf("applied archive was not recovered: archives=%v stored=%+v", deps.client.archives, stored.Archives)
		}
	})
	t.Run("notice", func(t *testing.T) {
		committed := state.New()
		committed.LastUpdateCheck = timePointer(now.Add(-25 * time.Hour))
		runner, deps := testRunner(t, now, nil, committed)
		cycle := state.NewCycle("cycle-1", committed.Generation, now)
		cycle.Operations["notice:1.2.0"] = state.CycleOperation{Kind: state.OperationNotice, Stage: state.StageApplying, NoticeVersion: "1.2.0"}
		if err := deps.store.store.SaveCycle(cycle); err != nil {
			t.Fatal(err)
		}
		deps.update.result = UpdateStatus{LatestVersion: "1.2.0", Newer: true}
		if _, err := runner.Run(context.Background(), false); err != nil {
			t.Fatal(err)
		}
		stored, _ := deps.store.store.LoadState()
		if !contains(stored.DeliveredNoticeVersions, "1.2.0") || len(deps.client.notices) != 1 {
			t.Fatalf("delivered=%v notices=%v", stored.DeliveredNoticeVersions, deps.client.notices)
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
	if _, ok := preview.(output.PreviewResult); !ok || deps.factory.opens != 0 || deps.classifier.calls != 0 {
		t.Fatalf("preview=%T opens=%d classifier=%d", preview, deps.factory.opens, deps.classifier.calls)
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
	if deps.classifier.calls != 0 || len(result.Retries) != 1 || result.Retries[0].ErrorCode != "invalid_context_budget" {
		t.Fatalf("result=%+v classifier=%d", result, deps.classifier.calls)
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

func TestHeartbeatUpdateNoticeDeliveredOnce(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	committed := state.New()
	committed.LastUpdateCheck = timePointer(now.Add(-25 * time.Hour))
	runner, deps := testRunner(t, now, nil, committed)
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
	if len(deps.client.notices) != 1 || deps.update.calls != 0 || deps.factory.opens != 0 {
		t.Fatalf("notices=%v checks=%d opens=%d", deps.client.notices, deps.update.calls, deps.factory.opens)
	}
}

func TestCrashAmbiguousMutationKeepsApplyingJournal(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	t.Run("notice", func(t *testing.T) {
		committed := state.New()
		committed.LastUpdateCheck = timePointer(now.Add(-25 * time.Hour))
		runner, deps := testRunner(t, now, nil, committed)
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
		if !contains(stored.DeliveredNoticeVersions, "1.2.0") || len(deps.client.notices) != 1 {
			t.Fatalf("delivered=%v notices=%v", stored.DeliveredNoticeVersions, deps.client.notices)
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

type testDeps struct {
	store      *wrappedStore
	clock      *fakeClock
	index      *fakeIndex
	client     *fakeClient
	factory    *fakeFactory
	classifier *fakeClassifier
	update     *fakeUpdateChecker
}

func testRunner(t *testing.T, now time.Time, tasks []codex.Task, committed state.State) (*Runner, testDeps) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "threadbear")
	store := state.NewStore(dir)
	cfg := config.Default("control")
	if err := store.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveState(committed); err != nil {
		t.Fatal(err)
	}
	index := &fakeIndex{tasks: append([]codex.Task{}, tasks...)}
	client := &fakeClient{index: index, latest: make(map[string]appserver.RecentEvidence), previous: make(map[string]*appserver.EvidenceTurn), failTitle: make(map[string]bool)}
	factory := &fakeFactory{client: client}
	classifier := &fakeClassifier{results: make(map[string]status.Classification)}
	update := &fakeUpdateChecker{result: UpdateStatus{LatestVersion: "1.0.0"}}
	wrapped := &wrappedStore{store: store}
	clock := &fakeClock{now: now}
	runner, err := New(Dependencies{
		Store: wrapped, Inventory: index, AppServer: factory, UpdateChecker: update, Clock: clock, InstalledVersion: "1.0.0",
		NewCycleID: func() string { return "cycle-1" },
		NewClassifier: func(_ AppServer, cfg config.Config) (Classifier, error) {
			if cfg.ClassifierContextBudgetBytes != 250000 {
				t.Fatalf("context budget=%d", cfg.ClassifierContextBudgetBytes)
			}
			return classifier, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return runner, testDeps{store: wrapped, clock: clock, index: index, client: client, factory: factory, classifier: classifier, update: update}
}

func completedEvidence(now time.Time, user, agent string) appserver.RecentEvidence {
	seconds := now.Unix()
	return appserver.RecentEvidence{ThreadStatus: appserver.ThreadStatus{Type: "idle"}, RecencyAt: &seconds, Latest: &appserver.EvidenceTurn{ID: "turn", Status: "completed", UserMessage: user, AgentMessage: agent}}
}

func record(task codex.Task, taskStatus state.TaskStatus, activity time.Time) state.TaskRecord {
	subject := strings.TrimSpace(strings.TrimLeft(task.Title, "✅⏳🚨🙋🤖➡️❔ "))
	return state.TaskRecord{TaskID: task.TaskID, CapturedRevision: task.Revision, CapturedTitle: task.Title, Status: taskStatus, Provenance: state.ProvenanceFooter, StateStartedAt: activity, LastSubstantiveActivity: activity, DurableSubject: subject, LastAppliedTitle: task.Title}
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
