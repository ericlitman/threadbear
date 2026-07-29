package titleplan

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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
	planned, err := service.Plan(context.Background(), "", "", true, false, false)
	if err != nil {
		t.Fatal(err)
	}
	manifest := planned.(output.TitlePlanResult)
	if len(manifest.Plans) != 1 || manifest.Plans[0].DesiredTitle != "✅ Subject" {
		t.Fatalf("manifest = %+v", manifest)
	}
	service.Reports = bytes.NewBufferString(`{"reports":[{"operation_id":"` + operationID + `","task_id":"task","native_success":true}]}`)
	reported, err := service.Plan(context.Background(), "", "", false, true, false)
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
	pendingCanonical, err := service.Plan(context.Background(), "", "", true, false, false)
	if err != nil {
		t.Fatal(err)
	}
	pendingResult := pendingCanonical.(output.TitlePlanResult)
	if len(pendingResult.Plans) != 0 || len(pendingResult.Dispositions) != 1 || pendingResult.Dispositions[0].Outcome != "native_succeeded_pending_canonical" {
		t.Fatalf("pending canonical = %+v", pendingResult)
	}
	inventory.tasks[0].Revision, inventory.tasks[0].Title = "2", "✅ Subject"
	settled, err := service.Plan(context.Background(), "", "", true, false, false)
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
	result, err := service.Plan(context.Background(), "source", "", false, false, false)
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
	planned, err := service.Plan(context.Background(), "", "", true, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := planned.(output.TitlePlanResult); len(got.Plans) != 1 || len(got.Dispositions) != 0 {
		t.Fatalf("same-title plan settled before native report: %+v", got)
	}
	service.Reports = bytes.NewBufferString(`{"reports":[{"operation_id":"` + operationID + `","task_id":"task","native_success":true}]}`)
	if _, err := service.Plan(context.Background(), "", "", false, true, false); err != nil {
		t.Fatal(err)
	}
	settled, err := service.Plan(context.Background(), "", "", true, false, false)
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
	result, err := service.Plan(context.Background(), "", "", false, true, false)
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
				result, err = service.Plan(context.Background(), "task", "", false, false, false)
			} else {
				result, err = service.Plan(context.Background(), "", "", true, false, false)
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
				_, err = service.Plan(context.Background(), "", "", false, true, false)
			} else {
				_, err = service.Plan(context.Background(), "", "", true, false, false)
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
		result, err := service.Plan(context.Background(), "", "", false, true, false)
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
	result, err := service.Plan(context.Background(), "", operationID, false, false, false)
	if err != nil {
		t.Fatal(err)
	}
	planned := result.(output.TitlePlanResult)
	if planned.Mode != "operation" || len(planned.Plans) != 1 || planned.Plans[0].OperationID != operationID {
		t.Fatalf("revalidated plan = %+v", planned)
	}
	inventory.tasks[0].Revision = "1001"
	result, err = service.Plan(context.Background(), "", operationID, false, false, false)
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
	result, err := service.Plan(context.Background(), "", "", true, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := result.(output.TitlePlanResult); len(got.Plans) != 0 || len(got.Dispositions) != 1 || got.Dispositions[0].Outcome != "native_succeeded_pending_canonical" {
		t.Fatalf("pre-deadline result = %+v", got)
	}
	now = reportedAt.Add(state.NativeTitleCanonicalTimeout)
	result, err = service.Plan(context.Background(), "", "", true, false, false)
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
	result, err := service.Plan(context.Background(), "", "", false, true, false)
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
	result, err := service.Plan(context.Background(), "control", "", false, false, false)
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
	result, err := service.Plan(context.Background(), "", operationID, false, false, false)
	if err != nil {
		t.Fatal(err)
	}
	got := result.(output.TitlePlanResult)
	if len(got.Plans) != 0 || len(got.Dispositions) != 1 || got.Dispositions[0].Outcome != "drifted" {
		t.Fatalf("same-second millisecond drift = %+v", got)
	}
}

func isASCIIString(value string) bool {
	for _, char := range value {
		if char > 127 {
			return false
		}
	}
	return true
}

type dispatchStoreOverride struct {
	*state.Store
	config *config.Config
	state  *state.State
}

func (s dispatchStoreOverride) LoadConfig() (config.Config, error) {
	if s.config != nil {
		return *s.config, nil
	}
	return s.Store.LoadConfig()
}

func (s dispatchStoreOverride) LoadState() (state.State, error) {
	if s.state != nil {
		return *s.state, nil
	}
	return s.Store.LoadState()
}

func TestDispatchEligibilityEnvelopeAndPrivacy(t *testing.T) {
	const sourceID = "11111111-1111-4111-8111-111111111111"
	const controlID = "22222222-2222-4222-8222-222222222222"
	newStore := func(t *testing.T, configure func(*config.Config)) *state.Store {
		t.Helper()
		store := state.NewStore(filepath.Join(t.TempDir(), "state"))
		cfg := config.Default(controlID)
		if configure != nil {
			configure(&cfg)
		}
		if err := store.SaveConfig(cfg); err != nil {
			t.Fatal(err)
		}
		committed := state.New()
		if err := store.SaveState(committed); err != nil {
			t.Fatal(err)
		}
		return store
	}
	t.Run("allowed", func(t *testing.T) {
		t.Setenv("CODEX_THREAD_ID", sourceID)
		result, err := (Service{Store: newStore(t, nil)}).Plan(context.Background(), "", "", false, false, true)
		if err != nil {
			t.Fatal(err)
		}
		got := result.(output.TitleDispatchResult)
		if !got.Allow || got.Disposition != "dispatch" || got.Child == nil || got.Child.Model != "gpt-5.6-luna" || got.Child.Thinking != "medium" || got.Child.Target.Type != "projectless" || got.Child.Target.DirectoryName != "threadbear-title-actuator" {
			t.Fatalf("dispatch = %+v", got)
		}
		if got.Child.Prompt != ChildPrompt {
			t.Fatal("dispatch envelope did not use the deterministic helper-owned child prompt")
		}
		const expectedLoader = `const r=await tools.exec_command({cmd:"~/.local/bin/threadbear title-plan --json --actuator __THREADBEAR_SOURCE_UUID__"});await(0,eval)("(async function(){"+JSON.parse(r.output).program+"\n})()")`
		const expectedPrompt = `THREADBEAR_TITLE_ACTUATOR_V1
one model pass;replace sole placeholder with lowercase canonical codex_delegation.source_thread_id;submit otherwise byte-for-byte to one functions.exec;no other tool or prose;const r=await tools.exec_command({cmd:"~/.local/bin/threadbear title-plan --json --actuator __THREADBEAR_SOURCE_UUID__"});await(0,eval)("(async function(){"+JSON.parse(r.output).program+"\n})()")`
		if ChildActuatorLoader != expectedLoader || len([]byte(ChildActuatorLoader)) != 195 || !isASCIIString(ChildActuatorLoader) {
			t.Fatalf("loader bytes=%d", len([]byte(ChildActuatorLoader)))
		}
		if got.Child.Prompt != expectedPrompt || len([]byte(got.Child.Prompt)) != 399 || len([]byte(got.Child.Prompt)) > 950 || !isASCIIString(got.Child.Prompt) {
			t.Fatalf("prompt bytes=%d prefix=%q", len([]byte(got.Child.Prompt)), got.Child.Prompt[:min(len(got.Child.Prompt), 80)])
		}
		if strings.Count(ChildActuatorProgram, ChildSourcePlaceholder) != 1 || strings.Count(ChildActuatorLoader, ChildSourcePlaceholder) != 1 || strings.Count(got.Child.Prompt, ChildSourcePlaceholder) != 1 || strings.Count(got.Child.Prompt, "functions.exec") != 1 || strings.Contains(got.Child.Prompt, ChildActuatorProgram) || strings.Count(got.Child.Prompt, ChildActuatorLoader) != 1 {
			t.Fatal("child prompt does not contain exactly one helper loader, placeholder, and exec contract")
		}
		if strings.ContainsAny(got.Child.Prompt, "<>&") {
			t.Fatal("child prompt contains a transport-sensitive character")
		}
		if strings.Count(ChildActuatorProgram, "tools.codex_app__set_thread_title") != 1 || strings.Count(ChildActuatorProgram, "tools.codex_app__set_thread_archived") != 1 || strings.Count(ChildActuatorProgram, "title-plan --json --report") != 1 {
			t.Fatal("child actuator does not preserve exact native/report call sites")
		}
		for _, forbidden := range []string{"import(", "process", "require(", "node:", "ALL_TOOLS", "fetch(", "XMLHttpRequest", "Deno.", "Bun.", "list_tools", "get_tool_schema", "tools.codex_app__list", "tools.codex_app__read"} {
			if strings.Contains(ChildActuatorProgram, forbidden) {
				t.Fatalf("child actuator contains forbidden primitive %q", forbidden)
			}
		}
		encoded, err := json.Marshal(got)
		if err != nil {
			t.Fatal(err)
		}
		for _, private := range []string{sourceID, controlID, `"source_thread_id":`, `"transcript":`, `"task_metadata":`, `"desired_title":`, `"manifest":`} {
			if bytes.Contains(encoded, []byte(private)) {
				t.Fatalf("dispatch envelope exposed %q: %s", private, encoded)
			}
		}
	})
	for _, test := range []struct {
		name        string
		source      string
		configure   func(*config.Config)
		disposition string
	}{
		{name: "missing source", disposition: "source_missing"},
		{name: "noncanonical source", source: "11111111-1111-4111-8111-11111111111A", disposition: "source_invalid"},
		{name: "control task", source: controlID, disposition: "control_task"},
		{name: "rename disabled", source: sourceID, configure: func(cfg *config.Config) { cfg.RenameEnabled = false }, disposition: "rename_disabled"},
		{name: "agents disabled", source: sourceID, configure: func(cfg *config.Config) { cfg.AgentsEnabled = false }, disposition: "agents_disabled"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("CODEX_THREAD_ID", test.source)
			result, err := (Service{Store: newStore(t, test.configure)}).Plan(context.Background(), "", "", false, false, true)
			if err != nil {
				t.Fatal(err)
			}
			got := result.(output.TitleDispatchResult)
			if got.Allow || got.Disposition != test.disposition || got.Child != nil {
				t.Fatalf("dispatch = %+v", got)
			}
		})
	}
}

func TestActuatorProgramUsesExactSourceSubstitution(t *testing.T) {
	const sourceID = "11111111-1111-4111-8111-111111111111"
	result, err := (Service{}).Actuator(sourceID)
	if err != nil {
		t.Fatal(err)
	}
	got := result.(output.TitleActuatorResult)
	want := strings.Replace(ChildActuatorProgram, ChildSourcePlaceholder, sourceID, 1)
	if got.Program != want || strings.Contains(got.Program, ChildSourcePlaceholder) || strings.Count(got.Program, sourceID) != 1 {
		t.Fatal("actuator helper did not emit the exact source-bound production program")
	}
	for _, invalid := range []string{"", "11111111-1111-4111-8111-11111111111A", " 11111111-1111-4111-8111-111111111111"} {
		if _, err := (Service{}).Actuator(invalid); err == nil {
			t.Fatalf("Actuator(%q) succeeded", invalid)
		}
	}
}

func TestDispatchFailsClosedForUnavailableOrInvalidInstalledState(t *testing.T) {
	const sourceID = "11111111-1111-4111-8111-111111111111"
	const controlID = "22222222-2222-4222-8222-222222222222"
	t.Setenv("CODEX_THREAD_ID", sourceID)
	missing := state.NewStore(filepath.Join(t.TempDir(), "missing"))
	result, err := (Service{Store: missing}).Plan(context.Background(), "", "", false, false, true)
	if err != nil || result.(output.TitleDispatchResult).Disposition != "config_unavailable" {
		t.Fatalf("missing config result=%+v err=%v", result, err)
	}
	store := state.NewStore(filepath.Join(t.TempDir(), "state"))
	if err := store.SaveConfig(config.Default(controlID)); err != nil {
		t.Fatal(err)
	}
	result, err = (Service{Store: store}).Plan(context.Background(), "", "", false, false, true)
	if err != nil || result.(output.TitleDispatchResult).Disposition != "state_unavailable" {
		t.Fatalf("missing state result=%+v err=%v", result, err)
	}
	if err := store.SaveState(state.New()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store.Directory(), "state.json"), []byte(`{"schema_version":99}`), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err = (Service{Store: store}).Plan(context.Background(), "", "", false, false, true)
	if err != nil || result.(output.TitleDispatchResult).Disposition != "state_unavailable" {
		t.Fatalf("invalid state result=%+v err=%v", result, err)
	}
	invalidConfig := state.NewStore(filepath.Join(t.TempDir(), "invalid-config"))
	if err := invalidConfig.SaveConfig(config.Default(controlID)); err != nil {
		t.Fatal(err)
	}
	if err := invalidConfig.SaveState(state.New()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(invalidConfig.Directory(), "config.json"), []byte(`{"schema_version":99}`), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err = (Service{Store: invalidConfig}).Plan(context.Background(), "", "", false, false, true)
	if err != nil || result.(output.TitleDispatchResult).Disposition != "config_unavailable" {
		t.Fatalf("invalid config result=%+v err=%v", result, err)
	}
	base := state.NewStore(filepath.Join(t.TempDir(), "overrides"))
	validConfig := config.Default(controlID)
	validState := state.New()
	invalidConfigValue := validConfig
	invalidConfigValue.SchemaVersion = 99
	result, err = (Service{Store: dispatchStoreOverride{Store: base, config: &invalidConfigValue, state: &validState}}).Plan(context.Background(), "", "", false, false, true)
	if err != nil || result.(output.TitleDispatchResult).Disposition != "config_invalid" {
		t.Fatalf("strict config result=%+v err=%v", result, err)
	}
	invalidStateValue := validState
	invalidStateValue.SchemaVersion = 99
	result, err = (Service{Store: dispatchStoreOverride{Store: base, config: &validConfig, state: &invalidStateValue}}).Plan(context.Background(), "", "", false, false, true)
	if err != nil || result.(output.TitleDispatchResult).Disposition != "state_invalid" {
		t.Fatalf("strict state result=%+v err=%v", result, err)
	}
}
