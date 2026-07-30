package titlebatch

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ericlitman/threadbear/internal/codex"
	"github.com/ericlitman/threadbear/internal/config"
	"github.com/ericlitman/threadbear/internal/output"
	"github.com/ericlitman/threadbear/internal/state"
)

const controlTaskID = "11111111-1111-4111-8111-111111111111"

type fakeInventory struct{ tasks []codex.Task }

func (f *fakeInventory) Inventory(_ context.Context, controlTaskID string) (codex.Inventory, error) {
	tasks := make([]codex.Task, 0, len(f.tasks))
	for _, task := range f.tasks {
		if task.TaskID != controlTaskID {
			tasks = append(tasks, task)
		}
	}
	return codex.Inventory{Tasks: tasks}, nil
}

func (f *fakeInventory) Task(_ context.Context, taskID string) (codex.Task, error) {
	for _, task := range f.tasks {
		if task.TaskID == taskID {
			return task, nil
		}
	}
	return codex.Task{}, errors.New("missing task")
}

func titleBatchFixture(t *testing.T, task codex.Task) (*state.Store, *fakeInventory, time.Time) {
	t.Helper()
	store := state.NewStore(filepath.Join(t.TempDir(), "state"))
	cfg := config.Default(controlTaskID)
	cfg.AgentsEnabled = true
	if err := store.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	committed := state.New()
	committed.BootstrapComplete = true
	committed.Tasks[task.TaskID] = state.TaskRecord{TaskID: task.TaskID, CapturedRevision: task.Revision, CapturedTitle: task.Title, LastAppliedTitle: task.Title, DurableSubject: "Control task", Status: state.StatusComplete, Provenance: state.ProvenanceFooter, StateStartedAt: time.Unix(1, 0), LastSubstantiveActivity: time.Unix(1, 0)}
	if err := store.SaveState(committed); err != nil {
		t.Fatal(err)
	}
	return store, &fakeInventory{tasks: []codex.Task{task}}, time.Unix(10, 0).UTC()
}

func TestStageRequiresExactPersistedSourceIdentity(t *testing.T) {
	task := codex.Task{TaskID: controlTaskID, Revision: "1", Title: "✅ Control task"}
	store, inventory, now := titleBatchFixture(t, task)
	service := Service{Store: store, Inventory: inventory, SourceIdentity: func() string { return "other" }, Input: bytes.NewBufferString(`{"footer":"🧵🐻 complete"}`), Now: func() time.Time { return now }}
	result, err := service.Stage(context.Background())
	if err == nil || result.(output.ErrorResult).ErrorCode != "source_identity_mismatch" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestStageListReportAndCanonicalSettlement(t *testing.T) {
	task := codex.Task{TaskID: controlTaskID, Revision: "2", Title: "✅ Control task"}
	store, inventory, now := titleBatchFixture(t, task)
	service := Service{Store: store, Inventory: inventory, SourceIdentity: func() string { return controlTaskID }, Input: bytes.NewBufferString(`{"footer":"🧵🐻 needs input (you): choose the release region"}`), Now: func() time.Time { return now }}
	staged, err := service.Stage(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := staged.(output.TitleBatchResult); len(got.Plans) != 0 || len(got.Dispositions) != 1 || got.Dispositions[0].Outcome != "staged" {
		t.Fatalf("staged=%+v", got)
	}
	stagedState, _ := store.LoadState()
	storedPlan := stagedState.PendingTitlePlans[task.TaskID]
	plan := output.TitleBatchItem{OperationID: storedPlan.OperationID, TaskID: storedPlan.TaskID, ExpectedRevision: storedPlan.ExpectedRevision, ExpectedTitle: storedPlan.ExpectedTitle, DesiredTitle: storedPlan.DesiredTitle}
	if plan.DesiredTitle != "🙋 Control task → choose the release region" {
		t.Fatalf("plan=%+v", plan)
	}
	listed, err := service.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.(output.TitleBatchResult).Plans) != 1 {
		t.Fatalf("listed=%+v", listed)
	}
	service.Input = bytes.NewBufferString(`{"reports":[{"operation_id":"` + plan.OperationID + `","outcome":"accepted"}]}`)
	reported, err := service.Report()
	if err != nil {
		t.Fatal(err)
	}
	if len(reported.(output.TitleBatchReportResult).AcceptedIDs) != 1 {
		t.Fatalf("reported=%+v", reported)
	}
	inventory.tasks[0].Title = plan.DesiredTitle
	settled, err := service.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := settled.(output.TitleBatchResult); len(got.Plans) != 0 || len(got.Dispositions) != 1 || got.Dispositions[0].Outcome != "canonical_verified_awaiting_footer" {
		t.Fatalf("settled=%+v", got)
	}
	committed, _ := store.LoadState()
	if committed.PendingTitlePlans[task.TaskID].CanonicalCheckedAt == nil || committed.Tasks[task.TaskID].LastAppliedTitle == plan.DesiredTitle {
		t.Fatalf("state=%+v", committed)
	}
}

func TestOperationRevalidationRejectsDrift(t *testing.T) {
	task := codex.Task{TaskID: "task-a", Revision: "1", Title: "Control task"}
	store, inventory, now := titleBatchFixture(t, task)
	committed, _ := store.LoadState()
	plan := state.PendingTitlePlan{OperationID: state.TitleOperationID(task.TaskID, task.Revision, task.Title, "✅ Control task"), TaskID: task.TaskID, ExpectedRevision: task.Revision, ExpectedTitle: task.Title, DesiredTitle: "✅ Control task", DurableSubject: "Control task", NativeOutcome: state.NativeTitlePending}
	committed.PendingTitlePlans[task.TaskID] = plan
	if err := store.SaveState(committed); err != nil {
		t.Fatal(err)
	}
	inventory.tasks[0].Revision = "2"
	inventory.tasks[0].Title = "User edit"
	service := Service{Store: store, Inventory: inventory, SourceIdentity: func() string { return controlTaskID }, Now: func() time.Time { return now }}
	result, err := service.Operation(context.Background(), plan.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	got := result.(output.TitleBatchResult)
	if len(got.Plans) != 0 || len(got.Dispositions) != 1 || got.Dispositions[0].Outcome != "drifted" {
		t.Fatalf("result=%+v", got)
	}
}

func TestNativeSuccessRetriesAfterBoundedCanonicalWait(t *testing.T) {
	task := codex.Task{TaskID: "task-a", Revision: "1", Title: "Control task"}
	store, inventory, now := titleBatchFixture(t, task)
	committed, _ := store.LoadState()
	plan := state.PendingTitlePlan{OperationID: state.TitleOperationID(task.TaskID, task.Revision, task.Title, "✅ Control task"), TaskID: task.TaskID, ExpectedRevision: task.Revision, ExpectedTitle: task.Title, DesiredTitle: "✅ Control task", DurableSubject: "Control task", NativeOutcome: state.NativeTitlePending}
	committed.PendingTitlePlans[task.TaskID] = plan
	if err := store.SaveState(committed); err != nil {
		t.Fatal(err)
	}
	service := Service{Store: store, Inventory: inventory, SourceIdentity: func() string { return controlTaskID }, Input: bytes.NewBufferString(`{"reports":[{"operation_id":"` + plan.OperationID + `","outcome":"accepted"}]}`), Now: func() time.Time { return now }}
	if _, err := service.Report(); err != nil {
		t.Fatal(err)
	}
	pending, err := service.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if pending.(output.TitleBatchResult).Dispositions[0].Outcome != "native_succeeded_pending_canonical" {
		t.Fatalf("pending=%+v", pending)
	}
	now = now.Add(state.NativeTitleCanonicalTimeout)
	retry, err := service.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(retry.(output.TitleBatchResult).Plans) != 1 {
		t.Fatalf("retry=%+v", retry)
	}
}

func TestReportRejectsDuplicateOperationWithoutMutation(t *testing.T) {
	task := codex.Task{TaskID: "task-a", Revision: "1", Title: "Control task"}
	store, inventory, now := titleBatchFixture(t, task)
	committed, _ := store.LoadState()
	plan := state.PendingTitlePlan{OperationID: state.TitleOperationID(task.TaskID, task.Revision, task.Title, "✅ Control task"), TaskID: task.TaskID, ExpectedRevision: task.Revision, ExpectedTitle: task.Title, DesiredTitle: "✅ Control task", DurableSubject: "Control task", NativeOutcome: state.NativeTitlePending}
	committed.PendingTitlePlans[task.TaskID] = plan
	if err := store.SaveState(committed); err != nil {
		t.Fatal(err)
	}
	body := `{"reports":[{"operation_id":"` + plan.OperationID + `","outcome":"accepted"},{"operation_id":"` + plan.OperationID + `","outcome":"accepted"}]}`
	service := Service{Store: store, Inventory: inventory, SourceIdentity: func() string { return controlTaskID }, Input: bytes.NewBufferString(body), Now: func() time.Time { return now }}
	result, err := service.Report()
	if err != nil {
		t.Fatal(err)
	}
	if len(result.(output.TitleBatchReportResult).RejectedIDs) != 1 {
		t.Fatalf("result=%+v", result)
	}
	after, _ := store.LoadState()
	if after.PendingTitlePlans[task.TaskID].NativeOutcome != state.NativeTitlePending {
		t.Fatalf("state=%+v", after)
	}
}

func TestStageAdoptsRetainedSourceWithoutInferringActionOrTokenOwnership(t *testing.T) {
	store := state.NewStore(filepath.Join(t.TempDir(), "state"))
	cfg := config.Default(controlTaskID)
	cfg.AgentsEnabled = true
	if err := store.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveState(state.New()); err != nil {
		t.Fatal(err)
	}
	task := codex.Task{TaskID: controlTaskID, Revision: "1", Title: "✅ 26k User-owned control subject → user-owned suffix"}
	inventory := &fakeInventory{tasks: []codex.Task{task}}
	now := time.Unix(20, 0).UTC()
	service := Service{Store: store, Inventory: inventory, SourceIdentity: func() string { return controlTaskID }, Input: bytes.NewBufferString(`{"footer":"🧵🐻 complete"}`), Now: func() time.Time { return now }}
	result, err := service.Stage(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := result.(output.TitleBatchResult); len(got.Plans) != 0 {
		t.Fatalf("stage leaked manifest: %+v", got)
	}
	committed, _ := store.LoadState()
	plan := committed.PendingTitlePlans[controlTaskID]
	if plan.DesiredTitle != task.Title {
		t.Fatalf("plan=%+v", plan)
	}
	record := committed.Tasks[controlTaskID]
	if record.DurableSubject != "26k User-owned control subject → user-owned suffix" || record.ManagedAction != "" || record.ManagedTokenDisplay != "" {
		t.Fatalf("record=%+v", record)
	}
}
