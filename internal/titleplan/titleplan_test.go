package titleplan

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"testing"
	"time"

	"github.com/ericlitman/threadbear/internal/codex"
	"github.com/ericlitman/threadbear/internal/config"
	"github.com/ericlitman/threadbear/internal/output"
	"github.com/ericlitman/threadbear/internal/state"
)

type planStore struct {
	cfg       config.Config
	committed state.State
	cycle     *state.CycleCheckpoint
	lockErr   error
	saveErr   error
}

func (s *planStore) LoadConfig() (config.Config, error) { return s.cfg, nil }
func (s *planStore) LoadState() (state.State, error)    { return s.committed, nil }
func (s *planStore) SaveState(v state.State) error {
	if s.saveErr == nil {
		s.committed = v
	}
	return s.saveErr
}
func (s *planStore) LoadCycle() (state.CycleCheckpoint, error) {
	if s.cycle == nil {
		return state.CycleCheckpoint{}, fs.ErrNotExist
	}
	return *s.cycle, nil
}
func (s *planStore) RemoveCycle() error { s.cycle = nil; return nil }
func (s *planStore) AcquireLock() (*state.Lock, error) {
	if s.lockErr != nil {
		return nil, s.lockErr
	}
	return &state.Lock{}, nil
}

type planInventory map[string]codex.Task

func (i planInventory) Task(_ context.Context, control, task string) (codex.Task, bool, error) {
	if control != "control" {
		return codex.Task{}, false, errors.New("wrong control")
	}
	v, ok := i[task]
	return v, ok, nil
}
func plan(id, rev, before, after string) state.PendingTitlePlan {
	return state.PendingTitlePlan{OperationID: state.TitleOperationID(id, rev, before, after), TaskID: id, ExpectedRevision: rev, ExpectedTitle: before, DesiredTitle: after, NativeOutcome: state.NativeTitlePending}
}
func pending(plans ...state.PendingTitlePlan) state.State {
	s := state.New()
	for _, p := range plans {
		s.Tasks[p.TaskID] = state.TaskRecord{TaskID: p.TaskID, CapturedRevision: p.ExpectedRevision, CapturedTitle: p.ExpectedTitle}
		s.PendingTitlePlans[p.TaskID] = p
	}
	return s
}
func service(store Store, inventory planInventory, input string) Service {
	return Service{Store: store, Inventory: inventory, Input: bytes.NewBufferString(input), ThreadID: func() string { return "control" }, Now: func() time.Time { return time.Unix(2, 0).UTC() }}
}
func call(t *testing.T, s Service, request Request) output.Result {
	t.Helper()
	result, err := s.Dispatch(context.Background(), request)
	if err != nil {
		t.Fatalf("%+v: %v", result, err)
	}
	return result
}

func TestRetiredDispatchFailsClosedWithoutDependencies(t *testing.T) {
	result, err := (Service{}).Dispatch(context.Background(), Request{Retired: true})
	got := result.(output.TitleDispatchResult)
	if err != nil || got.Allow || got.Disposition != "retired" {
		t.Fatalf("result=%+v err=%v", got, err)
	}
}

func TestTitlePlanStageOperationsAndReports(t *testing.T) {
	cfg := config.Default("control")
	control := codex.Task{TaskID: "control", Revision: "1", Title: "ThreadBear", Source: "vscode"}
	stageStore := &planStore{cfg: cfg, committed: state.New()}
	stage := call(t, service(stageStore, planInventory{"control": control}, "🧵🐻 complete\n"), Request{Stage: true}).(output.TitlePlanResult)
	if !stage.Ready || stageStore.committed.PendingTitlePlans["control"].DesiredTitle != "✅ ThreadBear" {
		t.Fatalf("stage=%+v state=%+v", stage, stageStore.committed)
	}
	if result, err := service(&planStore{cfg: cfg, committed: state.New()}, planInventory{"control": control}, "done\n🧵🐻 complete\n").Dispatch(context.Background(), Request{Stage: true}); err == nil || result.(output.ErrorResult).ErrorCode != "invalid_footer" {
		t.Fatalf("result=%+v err=%v", result, err)
	}

	plans := []state.PendingTitlePlan{plan("set", "1", "Old", "New"), plan("recover", "1", "Before", "After"), plan("drift", "1", "Before", "After")}
	store := &planStore{cfg: cfg, committed: pending(plans...)}
	inventory := planInventory{
		"set":     {TaskID: "set", Revision: "1", Title: "Old", Source: "vscode"},
		"recover": {TaskID: "recover", Revision: "2", Title: "After", Source: "vscode"},
		"drift":   {TaskID: "drift", Revision: "2", Title: "Edited", Source: "vscode"},
	}
	for _, test := range []struct{ id, disposition, action, task string }{
		{plans[0].OperationID, "ready", "set", "set"},
		{plans[1].OperationID, "ready", "report_success", "recover"},
		{plans[2].OperationID, "drifted", "", ""},
		{"unknown", "missing", "", ""},
	} {
		got := call(t, service(store, inventory, ""), Request{OperationID: test.id}).(output.TitlePlanResult)
		if got.Disposition != test.disposition || got.Action != test.action || got.TaskID != test.task {
			t.Fatalf("operation=%+v", got)
		}
	}

	reportPlan := plans[0]
	reportStore := &planStore{cfg: cfg, committed: pending(reportPlan)}
	reportInventory := planInventory{"set": inventory["set"]}
	failed := `{"reports":[{"operation_id":"` + reportPlan.OperationID + `","outcome":"failed","error_code":"native_setter_failed"}]}`
	success := `{"reports":[{"operation_id":"` + reportPlan.OperationID + `","outcome":"succeeded"}]}`
	for index, test := range []struct {
		payload             string
		accepted, unchanged int
	}{{failed, 1, 0}, {failed, 0, 1}, {success, 1, 0}, {success, 0, 1}} {
		if index == 2 {
			reportInventory["set"] = codex.Task{TaskID: "set", Revision: "2", Title: "New", Source: "vscode"}
		}
		got := call(t, service(reportStore, reportInventory, test.payload), Request{Report: true}).(output.TitlePlanResult)
		if got.Accepted != test.accepted || got.Unchanged != test.unchanged {
			t.Fatalf("report %d=%+v", index, got)
		}
	}
	if result, err := service(reportStore, reportInventory, failed).Dispatch(context.Background(), Request{Report: true}); err == nil || result.(output.ErrorResult).ErrorCode != "report_conflict" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	recovery := &planStore{cfg: cfg, committed: pending(reportPlan), saveErr: errors.New("disk")}
	if result, err := service(recovery, reportInventory, success).Dispatch(context.Background(), Request{Report: true}); err == nil || result.(output.ErrorResult).ErrorCode != "state_write_failed" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if got := call(t, service(recovery, reportInventory, ""), Request{OperationID: reportPlan.OperationID}).(output.TitlePlanResult); got.Action != "report_success" {
		t.Fatal(got)
	}
}

func TestStageRetriesThroughLockAndStaleCycle(t *testing.T) {
	store := state.NewStore(t.TempDir())
	cfg, committed := config.Default("control"), state.New()
	if err := store.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveState(committed); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveCycle(state.NewCycle("cycle", committed.Generation, time.Unix(1, 0))); err != nil {
		t.Fatal(err)
	}
	stage := func() output.TitlePlanResult {
		return call(t, service(store, planInventory{"control": {TaskID: "control", Revision: "1", Title: "ThreadBear", Source: "vscode"}}, "🧵🐻 complete\n"), Request{Stage: true}).(output.TitlePlanResult)
	}
	lock, err := store.AcquireLock()
	if err != nil {
		t.Fatal(err)
	}
	if got := stage(); got.ErrorCode != "heartbeat_active" {
		t.Fatal(got)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	if got := stage(); got.ErrorCode != "heartbeat_cycle_active" {
		t.Fatal(got)
	}
	committed.Generation++
	if err := store.SaveState(committed); err != nil {
		t.Fatal(err)
	}
	if got := stage(); !got.Ready || got.Retryable {
		t.Fatal(got)
	}
}
