package titleplan

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ericlitman/threadbear/internal/codex"
	"github.com/ericlitman/threadbear/internal/config"
	"github.com/ericlitman/threadbear/internal/output"
	"github.com/ericlitman/threadbear/internal/state"
)

type fakeInventory struct{ tasks []codex.Task }

func (f *fakeInventory) Inventory(context.Context, string) (codex.Inventory, error) {
	return codex.Inventory{Tasks: append([]codex.Task(nil), f.tasks...)}, nil
}

func TestBatchReportAndCanonicalSettlementRemainDistinct(t *testing.T) {
	store := state.NewStore(filepath.Join(t.TempDir(), "state"))
	if err := store.SaveConfig(config.Default("control")); err != nil {
		t.Fatal(err)
	}
	committed := state.New()
	committed.Tasks["task"] = state.TaskRecord{TaskID: "task", CapturedRevision: "1", CapturedTitle: "Subject", Status: state.StatusComplete, Provenance: state.ProvenanceFooter, StateStartedAt: time.Unix(1, 0), LastSubstantiveActivity: time.Unix(1, 0)}
	operationID := state.TitleOperationID("task", "1", "Subject", "✅ Subject")
	committed.PendingTitlePlans["task"] = state.PendingTitlePlan{OperationID: operationID, TaskID: "task", ExpectedRevision: "1", ExpectedTitle: "Subject", DesiredTitle: "✅ Subject", DurableSubject: "Subject", NativeOutcome: state.NativeTitlePending}
	if err := store.SaveState(committed); err != nil {
		t.Fatal(err)
	}
	inventory := &fakeInventory{tasks: []codex.Task{{TaskID: "task", Revision: "1", Title: "Subject"}}}
	now := time.Unix(2, 0)
	heartbeat := &fakeHeartbeat{}
	service := Service{Store: store, Inventory: inventory, Heartbeat: heartbeat, Now: func() time.Time { return now }}
	planned, err := service.Plan(context.Background(), "", "", true, false)
	if err != nil {
		t.Fatal(err)
	}
	manifest := planned.(output.TitlePlanResult)
	if len(manifest.Plans) != 1 || manifest.Plans[0].DesiredTitle != "✅ Subject" {
		t.Fatalf("manifest = %+v", manifest)
	}
	service.Reports = bytes.NewBufferString(`{"reports":[{"operation_id":"` + operationID + `","task_id":"task","native_success":true}]}`)
	reported, err := service.Plan(context.Background(), "", "", false, true)
	if err != nil {
		t.Fatal(err)
	}
	if got := reported.(output.TitleReportResult); len(got.AcceptedIDs) != 1 {
		t.Fatalf("report = %+v", got)
	}
	afterReport, _ := store.LoadState()
	if afterReport.Generation != committed.Generation+1 || afterReport.PendingTitlePlans["task"].NativeOutcome != state.NativeTitleSucceeded || afterReport.Tasks["task"].LastAppliedTitle != "" {
		t.Fatalf("native report claimed canonical persistence: %+v", afterReport)
	}
	pendingCanonical, err := service.Plan(context.Background(), "", "", true, false)
	if err != nil {
		t.Fatal(err)
	}
	pendingResult := pendingCanonical.(output.TitlePlanResult)
	if len(pendingResult.Plans) != 0 || len(pendingResult.Dispositions) != 1 || pendingResult.Dispositions[0].Outcome != "native_succeeded_pending_canonical" {
		t.Fatalf("pending canonical = %+v", pendingResult)
	}
	inventory.tasks[0].Revision, inventory.tasks[0].Title = "2", "✅ Subject"
	settled, err := service.Plan(context.Background(), "", "", true, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(settled.(output.TitlePlanResult).Dispositions) != 1 {
		t.Fatalf("settled = %+v", settled)
	}
	final, _ := store.LoadState()
	if final.Generation != afterReport.Generation+1 || len(final.PendingTitlePlans) != 0 || final.Tasks["task"].CapturedRevision != "1" || final.Tasks["task"].LastAppliedTitle != "✅ Subject" {
		t.Fatalf("final = %+v", final)
	}
}

type fakeWaiter struct{ taskID string }

func (w *fakeWaiter) Wait(_ context.Context, taskID string) error { w.taskID = taskID; return nil }

type fakeHeartbeat struct{ calls int }

func (h *fakeHeartbeat) Run(context.Context, bool) (output.Result, error) {
	h.calls++
	return output.HeartbeatResult{}, nil
}

type fakePlanner struct{ taskIDs []string }

func (p *fakePlanner) PlanTitle(_ context.Context, taskID string) error {
	p.taskIDs = append(p.taskIDs, taskID)
	return nil
}

func TestWaitModeWaitsThenPlansAndReturnsNoOp(t *testing.T) {
	store := state.NewStore(filepath.Join(t.TempDir(), "state"))
	if err := store.SaveConfig(config.Default("control")); err != nil {
		t.Fatal(err)
	}
	committed := state.New()
	if err := store.SaveState(committed); err != nil {
		t.Fatal(err)
	}
	waiter, heartbeat, planner := &fakeWaiter{}, &fakeHeartbeat{}, &fakePlanner{}
	service := Service{Store: store, Inventory: &fakeInventory{}, Heartbeat: heartbeat, Planner: planner, Waiter: waiter, Now: time.Now}
	result, err := service.Plan(context.Background(), "source", "", false, false)
	if err != nil {
		t.Fatal(err)
	}
	planned := result.(output.TitlePlanResult)
	if waiter.taskID != "source" || heartbeat.calls != 0 || len(planner.taskIDs) != 1 || planner.taskIDs[0] != "source" || planned.Mode != "wait" || len(planned.Dispositions) != 1 || planned.Dispositions[0].Outcome != "no_op" {
		t.Fatalf("waiter=%q heartbeat=%d planner=%v result=%+v", waiter.taskID, heartbeat.calls, planner.taskIDs, planned)
	}
}

func TestSameTitleRefreshRequiresNativeSuccessBeforeSettlement(t *testing.T) {
	store := state.NewStore(filepath.Join(t.TempDir(), "state"))
	if err := store.SaveConfig(config.Default("control")); err != nil {
		t.Fatal(err)
	}
	committed := state.New()
	committed.Tasks["task"] = state.TaskRecord{TaskID: "task", CapturedRevision: "1", CapturedTitle: "✅ Subject", LastAppliedTitle: "✅ Subject", Status: state.StatusComplete, Provenance: state.ProvenanceFooter, StateStartedAt: time.Unix(1, 0), LastSubstantiveActivity: time.Unix(1, 0)}
	operationID := state.TitleOperationID("task", "1", "✅ Subject", "✅ Subject")
	committed.PendingTitlePlans["task"] = state.PendingTitlePlan{OperationID: operationID, TaskID: "task", ExpectedRevision: "1", ExpectedTitle: "✅ Subject", DesiredTitle: "✅ Subject", DurableSubject: "Subject", NativeOutcome: state.NativeTitlePending}
	if err := store.SaveState(committed); err != nil {
		t.Fatal(err)
	}
	inventory := &fakeInventory{tasks: []codex.Task{{TaskID: "task", Revision: "1", Title: "✅ Subject"}}}
	service := Service{Store: store, Inventory: inventory, Heartbeat: &fakeHeartbeat{}, Now: time.Now}
	planned, err := service.Plan(context.Background(), "", "", true, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := planned.(output.TitlePlanResult); len(got.Plans) != 1 || len(got.Dispositions) != 0 {
		t.Fatalf("same-title plan settled before native report: %+v", got)
	}
	service.Reports = bytes.NewBufferString(`{"reports":[{"operation_id":"` + operationID + `","task_id":"task","native_success":true}]}`)
	if _, err := service.Plan(context.Background(), "", "", false, true); err != nil {
		t.Fatal(err)
	}
	settled, err := service.Plan(context.Background(), "", "", true, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := settled.(output.TitlePlanResult); len(got.Plans) != 0 || len(got.Dispositions) != 1 || got.Dispositions[0].Outcome != "canonical_persisted" {
		t.Fatalf("same-title plan did not settle after native report: %+v", got)
	}
}

func TestReportRejectsDuplicateTaskWithoutMutatingPlan(t *testing.T) {
	store := state.NewStore(filepath.Join(t.TempDir(), "state"))
	if err := store.SaveConfig(config.Default("control")); err != nil {
		t.Fatal(err)
	}
	committed := state.New()
	committed.Tasks["task"] = state.TaskRecord{TaskID: "task", CapturedRevision: "1", CapturedTitle: "Subject", Status: state.StatusComplete, Provenance: state.ProvenanceFooter, StateStartedAt: time.Unix(1, 0), LastSubstantiveActivity: time.Unix(1, 0)}
	operationID := state.TitleOperationID("task", "1", "Subject", "✅ Subject")
	committed.PendingTitlePlans["task"] = state.PendingTitlePlan{OperationID: operationID, TaskID: "task", ExpectedRevision: "1", ExpectedTitle: "Subject", DesiredTitle: "✅ Subject", NativeOutcome: state.NativeTitlePending}
	if err := store.SaveState(committed); err != nil {
		t.Fatal(err)
	}
	report := `{"reports":[{"operation_id":"` + operationID + `","task_id":"task","native_success":true},{"operation_id":"` + operationID + `","task_id":"task","native_success":true}]}`
	service := Service{Store: store, Reports: bytes.NewBufferString(report), Now: time.Now}
	result, err := service.Plan(context.Background(), "", "", false, true)
	if err != nil {
		t.Fatal(err)
	}
	got := result.(output.TitleReportResult)
	if len(got.AcceptedIDs) != 0 || len(got.RejectedIDs) != 1 || got.RejectedIDs[0] != "task" {
		t.Fatalf("report = %+v", got)
	}
	after, _ := store.LoadState()
	if after.Generation != committed.Generation || after.PendingTitlePlans["task"].NativeOutcome != state.NativeTitlePending {
		t.Fatalf("duplicate report mutated state: %+v", after)
	}
}

func TestDisabledWaitAndBatchDoNotStageOrExposeTitles(t *testing.T) {
	for _, mode := range []string{"wait", "batch"} {
		t.Run(mode, func(t *testing.T) {
			store := state.NewStore(filepath.Join(t.TempDir(), "state"))
			cfg := config.Default("control")
			cfg.RenameEnabled = false
			if err := store.SaveConfig(cfg); err != nil {
				t.Fatal(err)
			}
			committed := state.New()
			committed.Tasks["task"] = state.TaskRecord{TaskID: "task", CapturedRevision: "1", CapturedTitle: "✅ Subject", LastAppliedTitle: "✅ Subject", Status: state.StatusComplete, Provenance: state.ProvenanceFooter, StateStartedAt: time.Unix(1, 0), LastSubstantiveActivity: time.Unix(1, 0)}
			operationID := state.TitleOperationID("task", "1", "✅ Subject", "✅ Subject")
			committed.PendingTitlePlans["task"] = state.PendingTitlePlan{OperationID: operationID, TaskID: "task", ExpectedRevision: "1", ExpectedTitle: "✅ Subject", DesiredTitle: "✅ Subject", NativeOutcome: state.NativeTitlePending}
			if err := store.SaveState(committed); err != nil {
				t.Fatal(err)
			}
			waiter, heartbeat, planner := &fakeWaiter{}, &fakeHeartbeat{}, &fakePlanner{}
			service := Service{Store: store, Inventory: &fakeInventory{}, Heartbeat: heartbeat, Planner: planner, Waiter: waiter, Now: time.Now}
			var result output.Result
			var err error
			if mode == "wait" {
				result, err = service.Plan(context.Background(), "task", "", false, false)
			} else {
				result, err = service.Plan(context.Background(), "", "", true, false)
			}
			if err != nil {
				t.Fatal(err)
			}
			planned := result.(output.TitlePlanResult)
			if len(planned.Plans) != 0 || waiter.taskID != "" || heartbeat.calls != 0 || len(planner.taskIDs) != 0 {
				t.Fatalf("result=%+v waiter=%q heartbeat=%d planner=%v", planned, waiter.taskID, heartbeat.calls, planner.taskIDs)
			}
			after, err := store.LoadState()
			if err != nil {
				t.Fatal(err)
			}
			if len(after.PendingTitlePlans) != 0 || after.Generation != committed.Generation+1 {
				t.Fatalf("disabled title state = %+v", after)
			}
		})
	}
}

func TestTitleSettlementAndReportRefuseUnrelatedAppliedArchiveCycle(t *testing.T) {
	for _, mode := range []string{"settlement", "report"} {
		t.Run(mode, func(t *testing.T) {
			store := state.NewStore(filepath.Join(t.TempDir(), "state"))
			if err := store.SaveConfig(config.Default("control")); err != nil {
				t.Fatal(err)
			}
			committed := state.New()
			committed.Generation = 7
			committed.Tasks["title"] = state.TaskRecord{TaskID: "title", CapturedRevision: "1", CapturedTitle: "Subject", Status: state.StatusComplete, Provenance: state.ProvenanceFooter, StateStartedAt: time.Unix(1, 0), LastSubstantiveActivity: time.Unix(1, 0)}
			operationID := state.TitleOperationID("title", "1", "Subject", "✅ Subject")
			committed.PendingTitlePlans["title"] = state.PendingTitlePlan{OperationID: operationID, TaskID: "title", ExpectedRevision: "1", ExpectedTitle: "Subject", DesiredTitle: "✅ Subject", NativeOutcome: state.NativeTitlePending}
			if err := store.SaveState(committed); err != nil {
				t.Fatal(err)
			}
			cycle := state.NewCycle("archive-crash", committed.Generation, time.Unix(2, 0))
			cycle.Inventory["archive"] = state.CapturedTask{TaskID: "archive", Revision: "1", Title: "Archive", LastSubstantiveActivity: time.Unix(1, 0)}
			cycle.Operations["archive:archive"] = state.CycleOperation{Kind: state.OperationArchive, Stage: state.StageApplied, TaskID: "archive", ExpectedRevision: "1", ExpectedTitle: "Archive"}
			if err := store.SaveCycle(cycle); err != nil {
				t.Fatal(err)
			}
			service := Service{Store: store, Inventory: &fakeInventory{tasks: []codex.Task{{TaskID: "title", Revision: "2", Title: "✅ Subject"}}}, Heartbeat: &fakeHeartbeat{}, Now: time.Now}
			if mode == "report" {
				service.Reports = bytes.NewBufferString(`{"reports":[{"operation_id":"` + operationID + `","task_id":"title","native_success":true}]}`)
			}
			var err error
			if mode == "report" {
				_, err = service.Plan(context.Background(), "", "", false, true)
			} else {
				_, err = service.Plan(context.Background(), "", "", true, false)
			}
			if err == nil {
				t.Fatal("title operation did not refuse an in-flight cycle")
			}
			after, loadErr := store.LoadState()
			if loadErr != nil {
				t.Fatal(loadErr)
			}
			if after.Generation != committed.Generation || after.PendingTitlePlans["title"].NativeOutcome != state.NativeTitlePending {
				t.Fatalf("title operation mutated state: %+v", after)
			}
			if recovered, loadErr := store.LoadCycle(); loadErr != nil || recovered.Operations["archive:archive"].Stage != state.StageApplied {
				t.Fatalf("archive checkpoint was lost: %+v err=%v", recovered, loadErr)
			}
		})
	}
}

func TestNativeReportOutcomesAreMonotonicAndIdempotent(t *testing.T) {
	store := state.NewStore(filepath.Join(t.TempDir(), "state"))
	if err := store.SaveConfig(config.Default("control")); err != nil {
		t.Fatal(err)
	}
	committed := state.New()
	committed.Tasks["task"] = state.TaskRecord{TaskID: "task", CapturedRevision: "1", CapturedTitle: "Subject", Status: state.StatusComplete, Provenance: state.ProvenanceFooter, StateStartedAt: time.Unix(1, 0), LastSubstantiveActivity: time.Unix(1, 0)}
	operationID := state.TitleOperationID("task", "1", "Subject", "✅ Subject")
	committed.PendingTitlePlans["task"] = state.PendingTitlePlan{OperationID: operationID, TaskID: "task", ExpectedRevision: "1", ExpectedTitle: "Subject", DesiredTitle: "✅ Subject", NativeOutcome: state.NativeTitlePending}
	if err := store.SaveState(committed); err != nil {
		t.Fatal(err)
	}
	now := time.Unix(10, 0)
	report := func(success bool, code string) output.TitleReportResult {
		body := `{"reports":[{"operation_id":"` + operationID + `","task_id":"task","native_success":`
		if success {
			body += `true}]}`
		} else {
			body += `false,"error_code":"` + code + `"}]}`
		}
		service := Service{Store: store, Reports: bytes.NewBufferString(body), Now: func() time.Time { return now }}
		result, err := service.Plan(context.Background(), "", "", false, true)
		if err != nil {
			t.Fatal(err)
		}
		return result.(output.TitleReportResult)
	}
	if got := report(false, "native_failed"); len(got.AcceptedIDs) != 1 {
		t.Fatalf("first failure = %+v", got)
	}
	afterFailure, _ := store.LoadState()
	now = time.Unix(20, 0)
	if got := report(false, "native_failed"); len(got.AcceptedIDs) != 1 {
		t.Fatalf("failure replay = %+v", got)
	}
	afterReplay, _ := store.LoadState()
	if afterReplay.Generation != afterFailure.Generation || !afterReplay.PendingTitlePlans["task"].NativeReportedAt.Equal(*afterFailure.PendingTitlePlans["task"].NativeReportedAt) {
		t.Fatalf("failure replay mutated state: before=%+v after=%+v", afterFailure, afterReplay)
	}
	if got := report(true, ""); len(got.AcceptedIDs) != 1 {
		t.Fatalf("failure-to-success = %+v", got)
	}
	afterSuccess, _ := store.LoadState()
	now = time.Unix(30, 0)
	if got := report(true, ""); len(got.AcceptedIDs) != 1 {
		t.Fatalf("success replay = %+v", got)
	}
	afterSuccessReplay, _ := store.LoadState()
	if afterSuccessReplay.Generation != afterSuccess.Generation || !afterSuccessReplay.PendingTitlePlans["task"].NativeReportedAt.Equal(*afterSuccess.PendingTitlePlans["task"].NativeReportedAt) {
		t.Fatalf("success replay mutated state: before=%+v after=%+v", afterSuccess, afterSuccessReplay)
	}
	if got := report(false, "late_failure"); len(got.AcceptedIDs) != 0 || len(got.RejectedIDs) != 1 {
		t.Fatalf("failure after success = %+v", got)
	}
	final, _ := store.LoadState()
	if final.Generation != afterSuccess.Generation || final.PendingTitlePlans["task"].NativeOutcome != state.NativeTitleSucceeded {
		t.Fatalf("failure after success mutated state: %+v", final)
	}
}

func TestOperationRevalidationRejectsStaleRevision(t *testing.T) {
	store := state.NewStore(filepath.Join(t.TempDir(), "state"))
	if err := store.SaveConfig(config.Default("control")); err != nil {
		t.Fatal(err)
	}
	committed := state.New()
	committed.Tasks["task"] = state.TaskRecord{TaskID: "task", CapturedRevision: "1000", CapturedTitle: "Subject", Status: state.StatusComplete, Provenance: state.ProvenanceFooter, StateStartedAt: time.Unix(1, 0), LastSubstantiveActivity: time.Unix(1, 0)}
	operationID := state.TitleOperationID("task", "1000", "Subject", "✅ Subject")
	committed.PendingTitlePlans["task"] = state.PendingTitlePlan{OperationID: operationID, TaskID: "task", ExpectedRevision: "1000", ExpectedTitle: "Subject", DesiredTitle: "✅ Subject", NativeOutcome: state.NativeTitlePending}
	if err := store.SaveState(committed); err != nil {
		t.Fatal(err)
	}
	inventory := &fakeInventory{tasks: []codex.Task{{TaskID: "task", Revision: "1000", Title: "Subject"}}}
	service := Service{Store: store, Inventory: inventory, Now: time.Now}
	result, err := service.Plan(context.Background(), "", operationID, false, false)
	if err != nil {
		t.Fatal(err)
	}
	planned := result.(output.TitlePlanResult)
	if planned.Mode != "operation" || len(planned.Plans) != 1 || planned.Plans[0].OperationID != operationID {
		t.Fatalf("revalidated plan = %+v", planned)
	}
	inventory.tasks[0].Revision = "1001"
	result, err = service.Plan(context.Background(), "", operationID, false, false)
	if err != nil {
		t.Fatal(err)
	}
	drifted := result.(output.TitlePlanResult)
	if len(drifted.Plans) != 0 || len(drifted.Dispositions) != 1 || drifted.Dispositions[0].Outcome != "drifted" {
		t.Fatalf("stale revalidation = %+v", drifted)
	}
	after, _ := store.LoadState()
	if len(after.PendingTitlePlans) != 0 {
		t.Fatalf("stale plan retained: %+v", after.PendingTitlePlans)
	}
}

func TestNativeSuccessRetriesAfterCanonicalDeadline(t *testing.T) {
	store := state.NewStore(filepath.Join(t.TempDir(), "state"))
	if err := store.SaveConfig(config.Default("control")); err != nil {
		t.Fatal(err)
	}
	reportedAt := time.Unix(100, 0).UTC()
	committed := state.New()
	committed.Tasks["task"] = state.TaskRecord{TaskID: "task", CapturedRevision: "1", CapturedTitle: "Subject", Status: state.StatusComplete, Provenance: state.ProvenanceFooter, StateStartedAt: time.Unix(1, 0), LastSubstantiveActivity: time.Unix(1, 0)}
	operationID := state.TitleOperationID("task", "1", "Subject", "✅ Subject")
	committed.PendingTitlePlans["task"] = state.PendingTitlePlan{OperationID: operationID, TaskID: "task", ExpectedRevision: "1", ExpectedTitle: "Subject", DesiredTitle: "✅ Subject", NativeOutcome: state.NativeTitleSucceeded, NativeReportedAt: &reportedAt}
	if err := store.SaveState(committed); err != nil {
		t.Fatal(err)
	}
	inventory := &fakeInventory{tasks: []codex.Task{{TaskID: "task", Revision: "1", Title: "Subject"}}}
	now := reportedAt.Add(state.NativeTitleCanonicalTimeout - time.Second)
	service := Service{Store: store, Inventory: inventory, Heartbeat: &fakeHeartbeat{}, Now: func() time.Time { return now }}
	result, err := service.Plan(context.Background(), "", "", true, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := result.(output.TitlePlanResult); len(got.Plans) != 0 || len(got.Dispositions) != 1 || got.Dispositions[0].Outcome != "native_succeeded_pending_canonical" {
		t.Fatalf("pre-deadline result = %+v", got)
	}
	now = reportedAt.Add(state.NativeTitleCanonicalTimeout)
	result, err = service.Plan(context.Background(), "", "", true, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := result.(output.TitlePlanResult); len(got.Plans) != 1 || got.Plans[0].OperationID != operationID || len(got.Dispositions) != 0 {
		t.Fatalf("retry result = %+v", got)
	}
	after, _ := store.LoadState()
	plan := after.PendingTitlePlans["task"]
	if plan.NativeOutcome != state.NativeTitleFailed || plan.NativeErrorCode != "canonical_not_persisted" || plan.NativeReportedAt == nil || !plan.NativeReportedAt.Equal(now) {
		t.Fatalf("retryable state = %+v", plan)
	}
}

func TestReportRejectsMissingNativeSuccess(t *testing.T) {
	store := state.NewStore(filepath.Join(t.TempDir(), "state"))
	if err := store.SaveConfig(config.Default("control")); err != nil {
		t.Fatal(err)
	}
	committed := state.New()
	committed.Generation = 7
	committed.Tasks["task"] = state.TaskRecord{TaskID: "task", CapturedRevision: "1", CapturedTitle: "Subject", Status: state.StatusComplete, Provenance: state.ProvenanceFooter, StateStartedAt: time.Unix(1, 0), LastSubstantiveActivity: time.Unix(1, 0)}
	operationID := state.TitleOperationID("task", "1", "Subject", "✅ Subject")
	committed.PendingTitlePlans["task"] = state.PendingTitlePlan{OperationID: operationID, TaskID: "task", ExpectedRevision: "1", ExpectedTitle: "Subject", DesiredTitle: "✅ Subject", NativeOutcome: state.NativeTitlePending}
	if err := store.SaveState(committed); err != nil {
		t.Fatal(err)
	}
	body := `{"reports":[{"operation_id":"` + operationID + `","task_id":"task","error_code":"native_failed"}]}`
	service := Service{Store: store, Reports: bytes.NewBufferString(body), Now: time.Now}
	result, err := service.Plan(context.Background(), "", "", false, true)
	if err == nil {
		t.Fatal("missing native_success was accepted")
	}
	if got := result.(output.ErrorResult); got.ErrorCode != "invalid_report" {
		t.Fatalf("result = %+v", got)
	}
	after, _ := store.LoadState()
	if after.Generation != committed.Generation || after.PendingTitlePlans["task"].NativeOutcome != state.NativeTitlePending {
		t.Fatalf("invalid report mutated state: %+v", after)
	}
}

func TestControlTaskWaitIsNoOp(t *testing.T) {
	store := state.NewStore(filepath.Join(t.TempDir(), "state"))
	if err := store.SaveConfig(config.Default("control")); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveState(state.New()); err != nil {
		t.Fatal(err)
	}
	waiter, planner := &fakeWaiter{}, &fakePlanner{}
	service := Service{Store: store, Inventory: &fakeInventory{}, Planner: planner, Waiter: waiter, Now: time.Now}
	result, err := service.Plan(context.Background(), "control", "", false, false)
	if err != nil {
		t.Fatal(err)
	}
	got := result.(output.TitlePlanResult)
	if waiter.taskID != "" || len(planner.taskIDs) != 0 || len(got.Plans) != 0 || len(got.Dispositions) != 1 || got.Dispositions[0].Outcome != "no_op" {
		t.Fatalf("waiter=%q planner=%v result=%+v", waiter.taskID, planner.taskIDs, got)
	}
}

func TestOperationRevalidationRejectsSameSecondNativeMismatch(t *testing.T) {
	store := state.NewStore(filepath.Join(t.TempDir(), "state"))
	if err := store.SaveConfig(config.Default("control")); err != nil {
		t.Fatal(err)
	}
	committed := state.New()
	committed.Tasks["task"] = state.TaskRecord{TaskID: "task", CapturedRevision: "1700000000123", CapturedTitle: "Subject", Status: state.StatusComplete, Provenance: state.ProvenanceFooter, StateStartedAt: time.Unix(1, 0), LastSubstantiveActivity: time.Unix(1, 0)}
	operationID := state.TitleOperationID("task", "1700000000123", "Subject", "✅ Subject")
	committed.PendingTitlePlans["task"] = state.PendingTitlePlan{OperationID: operationID, TaskID: "task", ExpectedRevision: "1700000000123", ExpectedTitle: "Subject", DesiredTitle: "✅ Subject", NativeOutcome: state.NativeTitlePending}
	if err := store.SaveState(committed); err != nil {
		t.Fatal(err)
	}
	inventory := &fakeInventory{tasks: []codex.Task{{TaskID: "task", Revision: "1700000000789", Title: "Subject"}}}
	service := Service{Store: store, Inventory: inventory, Now: time.Now}
	result, err := service.Plan(context.Background(), "", operationID, false, false)
	if err != nil {
		t.Fatal(err)
	}
	got := result.(output.TitlePlanResult)
	if len(got.Plans) != 0 || len(got.Dispositions) != 1 || got.Dispositions[0].Outcome != "drifted" {
		t.Fatalf("same-second millisecond drift = %+v", got)
	}
}
