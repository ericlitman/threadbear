package app

import (
	"bytes"
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ericlitman/threadbear/internal/codex"
	"github.com/ericlitman/threadbear/internal/config"
	"github.com/ericlitman/threadbear/internal/output"
	"github.com/ericlitman/threadbear/internal/state"
	"github.com/ericlitman/threadbear/internal/tokens"
)

type commandClock struct{ now time.Time }

func (c commandClock) Now() time.Time { return c.now }

type commandInventory struct {
	tasks []codex.Task
	calls int
}

func (i *commandInventory) Inventory(context.Context, string) (codex.Inventory, error) {
	i.calls++
	return codex.Inventory{Tasks: append([]codex.Task(nil), i.tasks...)}, nil
}

type commandLaunchAgent struct {
	healthy       bool
	healthCalls   int
	applyCalls    int
	enableCalls   int
	disableCalls  int
	enableChange  bool
	disableChange bool
	applyErr      error
	applyErrs     []error
	applied       config.Config
	applyHistory  []config.Config
}

func (l *commandLaunchAgent) Healthy(context.Context) (bool, error) {
	l.healthCalls++
	return l.healthy, nil
}
func (l *commandLaunchAgent) Apply(_ context.Context, value config.Config) error {
	l.applyCalls++
	l.applied = value
	l.applyHistory = append(l.applyHistory, value)
	if len(l.applyErrs) >= l.applyCalls {
		return l.applyErrs[l.applyCalls-1]
	}
	return l.applyErr
}
func (l *commandLaunchAgent) Enable(context.Context) (bool, error) {
	l.enableCalls++
	return l.enableChange, nil
}
func (l *commandLaunchAgent) Disable(context.Context) (bool, error) {
	l.disableCalls++
	return l.disableChange, nil
}

type failingCommandStore struct {
	*state.Store
	saveConfigErr error
	saveStateErr  error
}

func (s *failingCommandStore) SaveConfig(value config.Config) error {
	if s.saveConfigErr != nil {
		return s.saveConfigErr
	}
	return s.Store.SaveConfig(value)
}

func (s *failingCommandStore) SaveState(value state.State) error {
	if s.saveStateErr != nil {
		return s.saveStateErr
	}
	return s.Store.SaveState(value)
}

type commandUnarchiver struct {
	inventory *commandInventory
	calls     int
	err       error
}

func (u *commandUnarchiver) Unarchive(context.Context, string) error {
	u.calls++
	if u.err == nil && len(u.inventory.tasks) == 0 {
		u.inventory.tasks = []codex.Task{{TaskID: "task-a", Revision: "rev-2", Title: "Restored"}}
	}
	return u.err
}

type commandHeartbeat struct {
	calls  int
	dryRun bool
}

func (h *commandHeartbeat) Run(_ context.Context, boolValue bool) (output.Result, error) {
	h.calls++
	h.dryRun = boolValue
	if !boolValue {
		return output.ErrorResult{Operation: "heartbeat", ErrorCode: "mutation_attempted"}, errors.New("not dry")
	}
	return output.PreviewResult{Command: "heartbeat", Effects: []string{"classify.task-a"}}, nil
}

func TestStatusHumanJSONParityAndPendingDiagnostics(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	store := commandStore(t, now)
	committed, err := store.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	committed.LastCompletedHeartbeat = &now
	committed.LastUpdateCheck = &now
	record := commandRecord("task-a", now)
	record.Retry = &state.Retry{Operation: "title", ErrorCode: "write_failed", Attempts: 1, LastAttemptAt: now, NextAttemptAt: now.Add(time.Minute)}
	committed.Tasks[record.TaskID] = record
	if err := store.SaveState(committed); err != nil {
		t.Fatal(err)
	}
	cycle := state.NewCycle("cycle-1", committed.Generation, now)
	cycle.Inventory["task-b"] = state.CapturedTask{TaskID: "task-b", Revision: "rev-b", Title: "Task B", LastSubstantiveActivity: now}
	cycle.Diagnostics["task-a"] = state.CycleDiagnostic{TaskID: "task-a", Operation: "title", ErrorCode: "write_failed"}
	cycle.Diagnostics["task-b"] = state.CycleDiagnostic{TaskID: "task-b", Operation: "classifier", ErrorCode: "unavailable"}
	if err := store.SaveCycle(cycle); err != nil {
		t.Fatal(err)
	}
	launch := &commandLaunchAgent{healthy: true}
	result, err := StatusHandler("1.2.3", store, launch)(context.Background(), Request{Command: CommandStatus})
	if err != nil {
		t.Fatal(err)
	}
	statusResult := result.(output.StatusResult)
	if statusResult.PendingRetries != 2 || statusResult.Preferences.ClassifierContextBudgetBytes != config.DefaultClassifierContextBudgetBytes || launch.healthCalls != 1 {
		t.Fatalf("status=%+v launch=%+v", statusResult, launch)
	}
	var human, machine bytes.Buffer
	if err := output.Write(&human, output.FormatHuman, statusResult); err != nil {
		t.Fatal(err)
	}
	if err := output.Write(&machine, output.FormatJSON, statusResult); err != nil {
		t.Fatal(err)
	}
	for _, fact := range []string{"1.2.3", "control-task", "2", "250000"} {
		if !strings.Contains(human.String(), fact) || !strings.Contains(machine.String(), fact) {
			t.Fatalf("missing %q human=%q json=%q", fact, human.String(), machine.String())
		}
	}
}

func TestInspectAE24ReadOnlyCycleFactsAndPrivacy(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	store := commandStore(t, now)
	committed, err := store.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	record := commandRecord("task-a", now)
	record.CapturedRevision = "rev-2"
	record.CapturedTitle = "unexpected secret message body"
	record.ManagedTokenDisplay = "1.6m"
	record.ManagedTokenPosition = tokens.PositionEnd
	record.TokenDisplayPosition = tokens.PositionEnd
	record.TokenRolloutPath = "/private/private-rollout.jsonl"
	record.OutputTokens = 1_600_000
	record.TotalTokens = 2_000_000
	record.TokenUsageFound = true
	committed.Tasks[record.TaskID] = record
	if err := store.SaveState(committed); err != nil {
		t.Fatal(err)
	}
	cycle := state.NewCycle("cycle-1", committed.Generation, now)
	cycle.Inventory["task-a"] = state.CapturedTask{TaskID: "task-a", Revision: "rev-2", Title: "unexpected secret message body", LastSubstantiveActivity: now}
	cycle.Results["task-a"] = state.ClassificationResult{TaskID: "task-a", Revision: "rev-2", Status: state.StatusNeedsInput, Provenance: state.ProvenanceFooter, ManagedAction: "choose_region"}
	cycle.Diagnostics["task-a"] = state.CycleDiagnostic{TaskID: "task-a", Operation: "title", ErrorCode: "stale_revision"}
	if err := store.SaveCycle(cycle); err != nil {
		t.Fatal(err)
	}
	inventory := &commandInventory{tasks: []codex.Task{{TaskID: "task-a", Revision: "rev-2", Title: "unexpected secret message body"}}}
	before := snapshotStateDirectory(t, store.Directory())
	result, err := InspectHandler(store, inventory, commandClock{now})(context.Background(), Request{Command: CommandInspect, TaskID: "task-a"})
	if err != nil {
		t.Fatal(err)
	}
	inspectResult := result.(output.InspectResult)
	if inspectResult.State != state.StatusNeedsInput ||
		inspectResult.Provenance != state.ProvenanceFooter ||
		inspectResult.Retry == nil ||
		inspectResult.TokenDisplayPosition != tokens.PositionEnd ||
		inspectResult.ManagedTokenPosition != tokens.PositionEnd ||
		inspectResult.ManagedTokenDisplay != "1.6m" ||
		!inspectResult.TokenUsageFound {
		t.Fatalf("inspect=%+v", inspectResult)
	}
	var human, machine bytes.Buffer
	if err := output.Write(&human, output.FormatHuman, inspectResult); err != nil {
		t.Fatal(err)
	}
	if err := output.Write(&machine, output.FormatJSON, inspectResult); err != nil {
		t.Fatal(err)
	}
	for _, fact := range []string{"token configured end", "token applied end/1.6m", "token usage found true"} {
		if !strings.Contains(human.String(), fact) {
			t.Fatalf("human output missing %q: %q", fact, human.String())
		}
	}
	for _, fact := range []string{
		`"token_display_position":"end"`,
		`"managed_token_position":"end"`,
		`"managed_token_display":"1.6m"`,
		`"token_usage_found":true`,
	} {
		if !strings.Contains(machine.String(), fact) {
			t.Fatalf("JSON output missing %q: %q", fact, machine.String())
		}
	}
	for _, forbidden := range []string{"secret message body", "private-rollout.jsonl", "hidden_reasoning", "classifier_payload"} {
		if strings.Contains(human.String(), forbidden) || strings.Contains(machine.String(), forbidden) {
			t.Fatalf("diagnostic leaked %q", forbidden)
		}
	}
	after := snapshotStateDirectory(t, store.Directory())
	if !reflect.DeepEqual(before, after) || inventory.calls != 1 {
		t.Fatalf("inspect mutated state or over-read inventory")
	}
}

func TestStatusMalformedStateUsesStableError(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	store := commandStore(t, now)
	if err := os.WriteFile(store.Directory()+"/state.json", []byte("{}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := StatusHandler("test", store, &commandLaunchAgent{healthy: true})(context.Background(), Request{Command: CommandStatus})
	if err == nil || result.(output.ErrorResult).ErrorCode != "state_read_failed" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestInspectUnknownTaskUsesStableError(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	store := commandStore(t, now)
	inventory := &commandInventory{}
	result, err := InspectHandler(store, inventory, commandClock{now})(context.Background(), Request{Command: CommandInspect, TaskID: "missing"})
	if err == nil || result.(output.ErrorResult).ErrorCode != "task_not_found" || inventory.calls != 1 {
		t.Fatalf("result=%+v err=%v inventory=%d", result, err, inventory.calls)
	}
}

func TestDryRunReadsRealStoreWithoutMutationOrHeartbeatRunner(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	store := commandStore(t, now)
	committed, err := store.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	lastUpdate := now.Add(-24 * time.Hour)
	committed.LastUpdateCheck = &lastUpdate
	taskA := commandRecord("task-a", now)
	taskA.Status = state.StatusRunning
	committed.Tasks["task-a"] = taskA
	futureRetry := commandRecord("task-b", now)
	futureRetry.Retry = &state.Retry{Operation: "title", ErrorCode: "write_failed", Attempts: 1, LastAttemptAt: now, NextAttemptAt: now.Add(time.Hour)}
	committed.Tasks["task-b"] = futureRetry
	committed.Tasks["task-c"] = commandRecord("task-c", now)
	taskD := commandRecord("task-d", now)
	taskD.Status = state.StatusRunning
	committed.Tasks["task-d"] = taskD
	committed.Tasks["task-removed"] = commandRecord("task-removed", now)
	if err := store.SaveState(committed); err != nil {
		t.Fatal(err)
	}
	cycle := state.NewCycle("cycle-1", committed.Generation, now)
	cycle.Inventory["task-d"] = state.CapturedTask{TaskID: "task-d", Revision: "rev-2", Title: "Task D", LastSubstantiveActivity: now}
	cycle.Results["task-d"] = state.ClassificationResult{TaskID: "task-d", Revision: "rev-2", Status: state.StatusRunning, Provenance: state.ProvenanceRuntime}
	if err := store.SaveCycle(cycle); err != nil {
		t.Fatal(err)
	}
	inventory := &commandInventory{tasks: []codex.Task{
		{TaskID: "task-a", Revision: "rev-2", Title: "Task A"},
		{TaskID: "task-b", Revision: "rev-1", Title: "Task"},
		{TaskID: "task-c", Revision: "rev-1", Title: "Task"},
		{TaskID: "task-d", Revision: "rev-2", Title: "Task D"},
	}}
	runner := &commandHeartbeat{}
	service := NewWithOperatorCommands("test", OperatorDependencies{Store: store, Inventory: inventory, Clock: commandClock{now}, Heartbeat: runner})
	before := snapshotStateDirectory(t, store.Directory())
	result, err := service.Dispatch(context.Background(), Request{Command: CommandHeartbeat, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	after := snapshotStateDirectory(t, store.Directory())
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("dry run mutated state directory\nbefore=%+v\nafter=%+v", before, after)
	}
	if runner.calls != 0 || inventory.calls != 1 {
		t.Fatalf("runner=%+v inventory=%d", runner, inventory.calls)
	}
	preview := result.(output.PreviewResult)
	want := []string{"archive.task-c", "classify.task-a", "remove.task-removed", "update_check"}
	if preview.Command != "heartbeat" || !reflect.DeepEqual(preview.Effects, want) {
		t.Fatalf("preview=%+v want=%v", preview, want)
	}
}

func TestNonDryHeartbeatDelegatesToRunner(t *testing.T) {
	runner := &commandHeartbeat{}
	service := NewWithOperatorCommands("test", OperatorDependencies{Heartbeat: runner})
	result, err := service.Dispatch(context.Background(), Request{Command: CommandHeartbeat})
	if err == nil || runner.calls != 1 || runner.dryRun || result.(output.ErrorResult).ErrorCode != "mutation_attempted" {
		t.Fatalf("result=%+v err=%v runner=%+v", result, err, runner)
	}
}

func TestEnableDelegatesIdempotentlyUnderLock(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	store := commandStore(t, now)
	launch := &commandLaunchAgent{enableChange: false}
	result, err := LifecycleHandler(store, launch, true)(context.Background(), Request{Command: CommandEnable})
	if err != nil {
		t.Fatal(err)
	}
	if result.(output.ActionResult).Changed || launch.enableCalls != 1 || launch.disableCalls != 0 {
		t.Fatalf("result=%+v launch=%+v", result, launch)
	}
	lock, err := store.AcquireLock()
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	result, err = LifecycleHandler(store, launch, true)(context.Background(), Request{Command: CommandEnable})
	if err == nil || result.(output.ErrorResult).ErrorCode != "enable_locked" || launch.enableCalls != 1 {
		t.Fatalf("result=%+v err=%v launch=%+v", result, err, launch)
	}
}

func TestDisableDelegatesIdempotentlyUnderLock(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	store := commandStore(t, now)
	launch := &commandLaunchAgent{disableChange: true}
	result, err := LifecycleHandler(store, launch, false)(context.Background(), Request{Command: CommandDisable})
	if err != nil {
		t.Fatal(err)
	}
	action := result.(output.ActionResult)
	if !action.Changed || launch.disableCalls != 1 || launch.enableCalls != 0 {
		t.Fatalf("result=%+v launch=%+v", action, launch)
	}
}

func TestStatusConfigurePartialPatchAndApplyFailure(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	store := commandStore(t, now)
	falseValue := false
	budget := 123456
	tokenDisplay := tokens.PositionEnd
	result, err := ConfigureHandler(store, nil, nil, nil)(context.Background(), Request{Command: CommandConfigure, Confirm: true, Configure: ConfigPatch{ArchiveEnabled: &falseValue, TokenDisplay: &tokenDisplay, ClassifierContextBudgetBytes: &budget}})
	if err != nil || !result.(output.ActionResult).Changed {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	stored, err := store.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if stored.ArchiveEnabled || stored.RenameEnabled != true || stored.TokenDisplay != tokens.PositionEnd || stored.ClassifierContextBudgetBytes != budget {
		t.Fatalf("config=%+v", stored)
	}
	seconds := 60
	launch := &commandLaunchAgent{applyErr: errors.New("synthetic apply failure")}
	result, err = ConfigureHandler(store, launch, nil, nil)(context.Background(), Request{Command: CommandConfigure, Confirm: true, Configure: ConfigPatch{HeartbeatSeconds: &seconds}})
	if err == nil || result.(output.ErrorResult).ErrorCode != "launch_agent_apply_failed" || launch.applyCalls != 1 {
		t.Fatalf("result=%+v err=%v launch=%+v", result, err, launch)
	}
	stored, err = store.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if stored.HeartbeatSeconds != config.DefaultHeartbeatSeconds {
		t.Fatalf("failed apply persisted config: %+v", stored)
	}
}

func TestConfigureRollsBackSchedulerWhenConfigSaveFails(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	realStore := commandStore(t, now)
	store := &failingCommandStore{Store: realStore, saveConfigErr: errors.New("synthetic config save failure")}
	launch := &commandLaunchAgent{}
	seconds := 60
	result, err := ConfigureHandler(store, launch, nil, nil)(context.Background(), Request{Command: CommandConfigure, Confirm: true, Configure: ConfigPatch{HeartbeatSeconds: &seconds}})
	if err == nil || result.(output.ErrorResult).ErrorCode != "config_write_failed" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if launch.applyCalls != 2 || len(launch.applyHistory) != 2 || launch.applyHistory[0].HeartbeatSeconds != seconds || launch.applyHistory[1].HeartbeatSeconds != config.DefaultHeartbeatSeconds {
		t.Fatalf("launch=%+v", launch)
	}
	stored, loadErr := realStore.LoadConfig()
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if stored.HeartbeatSeconds != config.DefaultHeartbeatSeconds {
		t.Fatalf("stored=%+v", stored)
	}
}

func TestConfigureReportsStableRollbackFailure(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	store := &failingCommandStore{Store: commandStore(t, now), saveConfigErr: errors.New("synthetic config save failure")}
	launch := &commandLaunchAgent{applyErrs: []error{nil, errors.New("synthetic rollback failure")}}
	seconds := 60
	result, err := ConfigureHandler(store, launch, nil, nil)(context.Background(), Request{Command: CommandConfigure, Confirm: true, Configure: ConfigPatch{HeartbeatSeconds: &seconds}})
	if err == nil || result.(output.ErrorResult).ErrorCode != "launch_agent_rollback_failed" || launch.applyCalls != 2 {
		t.Fatalf("result=%+v err=%v launch=%+v", result, err, launch)
	}
}

func TestRestoreAE22OwnedArchiveResetsInactivity(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	store := commandStore(t, now)
	committed, err := store.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	committed.Archives["task-a"] = state.ArchiveRecord{TaskID: "task-a", ArchivedAt: now.Add(-time.Hour), CapturedRevision: "rev-1", StateGeneration: committed.Generation}
	if err := store.SaveState(committed); err != nil {
		t.Fatal(err)
	}
	inventory := &commandInventory{}
	unarchiver := &commandUnarchiver{inventory: inventory}
	result, err := RestoreHandler(store, inventory, unarchiver, commandClock{now})(context.Background(), Request{Command: CommandRestore, TaskID: "task-a"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.(output.ActionResult).Changed || unarchiver.calls != 1 {
		t.Fatalf("result=%+v unarchiver=%+v", result, unarchiver)
	}
	stored, err := store.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	record := stored.Tasks["task-a"]
	if _, exists := stored.Archives["task-a"]; exists || !record.LastSubstantiveActivity.Equal(now) || record.Status != state.StatusUnknown {
		t.Fatalf("state=%+v", stored)
	}
}

func TestRestoreAppliedCheckpointAndAlreadyUnarchivedSafety(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	store := commandStore(t, now)
	cycle := state.NewCycle("cycle-1", 0, now.Add(-time.Hour))
	cycle.Inventory["task-a"] = state.CapturedTask{TaskID: "task-a", Revision: "rev-1", Title: "Archived", Archived: true, LastSubstantiveActivity: now.Add(-time.Hour)}
	cycle.Results["task-a"] = state.ClassificationResult{TaskID: "task-a", Revision: "rev-1", Status: state.StatusComplete, Provenance: state.ProvenanceRuntime}
	cycle.Operations["archive:task-a"] = state.CycleOperation{Kind: state.OperationArchive, Stage: state.StageApplied, TaskID: "task-a", ExpectedRevision: "rev-1", ExpectedTitle: "Archived"}
	if err := store.SaveCycle(cycle); err != nil {
		t.Fatal(err)
	}
	inventory := &commandInventory{tasks: []codex.Task{{TaskID: "task-a", Revision: "rev-2", Title: "Already restored"}}}
	unarchiver := &commandUnarchiver{inventory: inventory}
	_, err := RestoreHandler(store, inventory, unarchiver, commandClock{now})(context.Background(), Request{Command: CommandRestore, TaskID: "task-a"})
	if err != nil {
		t.Fatal(err)
	}
	if unarchiver.calls != 0 {
		t.Fatalf("already-unarchived task invoked adapter")
	}
	storedCycle, err := store.LoadCycle()
	if err != nil {
		t.Fatal(err)
	}
	if appliedArchiveOperation(storedCycle, "task-a") || storedCycle.Inventory["task-a"].Archived {
		t.Fatalf("cycle=%+v", storedCycle)
	}
}

func TestRestoreStateFailureRetainsAppliedOwnershipForRetry(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	realStore := commandStore(t, now)
	cycle := state.NewCycle("cycle-1", 0, now.Add(-time.Hour))
	cycle.Inventory["task-a"] = state.CapturedTask{TaskID: "task-a", Revision: "rev-1", Title: "Archived", Archived: true, LastSubstantiveActivity: now.Add(-time.Hour)}
	cycle.Results["task-a"] = state.ClassificationResult{TaskID: "task-a", Revision: "rev-1", Status: state.StatusComplete, Provenance: state.ProvenanceRuntime}
	cycle.Operations["archive:task-a"] = state.CycleOperation{Kind: state.OperationArchive, Stage: state.StageApplied, TaskID: "task-a", ExpectedRevision: "rev-1", ExpectedTitle: "Archived"}
	if err := realStore.SaveCycle(cycle); err != nil {
		t.Fatal(err)
	}
	store := &failingCommandStore{Store: realStore, saveStateErr: errors.New("synthetic state save failure")}
	inventory := &commandInventory{tasks: []codex.Task{{TaskID: "task-a", Revision: "rev-2", Title: "Already restored"}}}
	result, err := RestoreHandler(store, inventory, &commandUnarchiver{inventory: inventory}, commandClock{now})(context.Background(), Request{Command: CommandRestore, TaskID: "task-a"})
	if err == nil || result.(output.ErrorResult).ErrorCode != "state_write_failed" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	storedCycle, loadErr := realStore.LoadCycle()
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if !appliedArchiveOperation(storedCycle, "task-a") || !storedCycle.Inventory["task-a"].Archived {
		t.Fatalf("cycle lost retry ownership: %+v", storedCycle)
	}
}

func TestRestoreRejectsUnownedAndNonAppliedArchive(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	store := commandStore(t, now)
	cycle := state.NewCycle("cycle-1", 0, now)
	cycle.Inventory["task-a"] = state.CapturedTask{TaskID: "task-a", Revision: "rev-1", Title: "Task", LastSubstantiveActivity: now}
	cycle.Operations["archive:task-a"] = state.CycleOperation{Kind: state.OperationArchive, Stage: state.StagePrepared, TaskID: "task-a", ExpectedRevision: "rev-1", ExpectedTitle: "Task"}
	if err := store.SaveCycle(cycle); err != nil {
		t.Fatal(err)
	}
	inventory := &commandInventory{}
	unarchiver := &commandUnarchiver{inventory: inventory}
	result, err := RestoreHandler(store, inventory, unarchiver, commandClock{now})(context.Background(), Request{Command: CommandRestore, TaskID: "task-a"})
	if err == nil || result.(output.ErrorResult).ErrorCode != "archive_not_owned" || unarchiver.calls != 0 || inventory.calls != 0 {
		t.Fatalf("result=%+v err=%v inventory=%d unarchive=%d", result, err, inventory.calls, unarchiver.calls)
	}
}

type stateDirectorySnapshotEntry struct {
	Mode os.FileMode
	Data []byte
}

func snapshotStateDirectory(t *testing.T, directory string) map[string]stateDirectorySnapshotEntry {
	t.Helper()
	result := make(map[string]stateDirectorySnapshotEntry)
	info, err := os.Lstat(directory)
	if err != nil {
		t.Fatal(err)
	}
	result["."] = stateDirectorySnapshotEntry{Mode: info.Mode()}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		path := directory + "/" + entry.Name()
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		result[entry.Name()] = stateDirectorySnapshotEntry{Mode: info.Mode(), Data: data}
	}
	return result
}

func TestStatusIgnoresStaleCycleAndRejectsAheadCycle(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	store := commandStore(t, now)
	committed, err := store.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	committed.Generation = 1
	if err := store.SaveState(committed); err != nil {
		t.Fatal(err)
	}
	stale := state.NewCycle("cycle-stale", 0, now)
	stale.Inventory["task-a"] = state.CapturedTask{TaskID: "task-a", Revision: "rev-1", Title: "Task", LastSubstantiveActivity: now}
	stale.Diagnostics["task-a"] = state.CycleDiagnostic{TaskID: "task-a", Operation: "title", ErrorCode: "write_failed"}
	if err := store.SaveCycle(stale); err != nil {
		t.Fatal(err)
	}
	result, err := StatusHandler("test", store, &commandLaunchAgent{healthy: true})(context.Background(), Request{Command: CommandStatus})
	if err != nil || result.(output.StatusResult).PendingRetries != 0 {
		t.Fatalf("stale result=%+v err=%v", result, err)
	}
	ahead := state.NewCycle("cycle-ahead", 2, now)
	if err := store.SaveCycle(ahead); err != nil {
		t.Fatal(err)
	}
	result, err = StatusHandler("test", store, &commandLaunchAgent{healthy: true})(context.Background(), Request{Command: CommandStatus})
	if err == nil || result.(output.ErrorResult).ErrorCode != "cycle_read_failed" {
		t.Fatalf("ahead result=%+v err=%v", result, err)
	}
}

func TestInspectIgnoresStaleCycleClassification(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	store := commandStore(t, now)
	committed, err := store.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	committed.Generation = 1
	record := commandRecord("task-a", now)
	record.Status = state.StatusRunning
	committed.Tasks[record.TaskID] = record
	if err := store.SaveState(committed); err != nil {
		t.Fatal(err)
	}
	stale := state.NewCycle("cycle-stale", 0, now)
	stale.Inventory["task-a"] = state.CapturedTask{TaskID: "task-a", Revision: "rev-1", Title: "Task", LastSubstantiveActivity: now}
	stale.Results["task-a"] = state.ClassificationResult{TaskID: "task-a", Revision: "rev-1", Status: state.StatusComplete, Provenance: state.ProvenanceLuna}
	if err := store.SaveCycle(stale); err != nil {
		t.Fatal(err)
	}
	inventory := &commandInventory{tasks: []codex.Task{{TaskID: "task-a", Revision: "rev-1", Title: "Task"}}}
	result, err := InspectHandler(store, inventory, commandClock{now})(context.Background(), Request{Command: CommandInspect, TaskID: "task-a"})
	if err != nil || result.(output.InspectResult).State != state.StatusRunning {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestRestoreRefusesAppliedCycleWithUnrelatedWork(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	store := commandStore(t, now)
	cycle := state.NewCycle("cycle-1", 0, now)
	for _, taskID := range []string{"task-a", "task-b"} {
		cycle.Inventory[taskID] = state.CapturedTask{TaskID: taskID, Revision: "rev-1", Title: "Task", Archived: true, LastSubstantiveActivity: now}
		cycle.Results[taskID] = state.ClassificationResult{TaskID: taskID, Revision: "rev-1", Status: state.StatusComplete, Provenance: state.ProvenanceRuntime}
		cycle.Operations["archive:"+taskID] = state.CycleOperation{Kind: state.OperationArchive, Stage: state.StageApplied, TaskID: taskID, ExpectedRevision: "rev-1", ExpectedTitle: "Task"}
	}
	if err := store.SaveCycle(cycle); err != nil {
		t.Fatal(err)
	}
	inventory := &commandInventory{}
	result, err := RestoreHandler(store, inventory, &commandUnarchiver{inventory: inventory}, commandClock{now})(context.Background(), Request{Command: CommandRestore, TaskID: "task-a"})
	if err == nil || result.(output.ErrorResult).ErrorCode != "pending_cycle" || inventory.calls != 0 {
		t.Fatalf("result=%+v err=%v inventory=%d", result, err, inventory.calls)
	}
}

func TestStatusReportsUnavailableSchedulerAdapterHonestly(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	store := commandStore(t, now)
	launch := &unavailableCommandLaunchAgent{}
	result, err := StatusHandler("test", store, launch)(context.Background(), Request{Command: CommandStatus})
	if err != nil {
		t.Fatal(err)
	}
	statusResult := result.(output.StatusResult)
	if statusResult.LaunchAgentStatus != "unavailable" || statusResult.LaunchAgentHealthy {
		t.Fatalf("status=%+v", statusResult)
	}
	var human, machine bytes.Buffer
	if err := output.Write(&human, output.FormatHuman, statusResult); err != nil {
		t.Fatal(err)
	}
	if err := output.Write(&machine, output.FormatJSON, statusResult); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(human.String(), "scheduler adapter unavailable (pending install unit)") || !strings.Contains(machine.String(), `"launch_agent_status":"unavailable"`) {
		t.Fatalf("human=%q json=%q", human.String(), machine.String())
	}
	result, err = LifecycleHandler(store, launch, true)(context.Background(), Request{Command: CommandEnable})
	if err == nil || result.(output.ErrorResult).ErrorCode != "launch_agent_unavailable" {
		t.Fatalf("enable result=%+v err=%v", result, err)
	}
	seconds := 60
	result, err = ConfigureHandler(store, launch, nil, nil)(context.Background(), Request{Command: CommandConfigure, Confirm: true, Configure: ConfigPatch{HeartbeatSeconds: &seconds}})
	if err == nil || result.(output.ErrorResult).ErrorCode != "launch_agent_unavailable" {
		t.Fatalf("configure result=%+v err=%v", result, err)
	}
}

type unavailableCommandLaunchAgent struct{}

func (*unavailableCommandLaunchAgent) Healthy(context.Context) (bool, error) {
	return false, ErrLaunchAgentUnavailable
}
func (*unavailableCommandLaunchAgent) Apply(context.Context, config.Config) error {
	return ErrLaunchAgentUnavailable
}
func (*unavailableCommandLaunchAgent) Enable(context.Context) (bool, error) {
	return false, ErrLaunchAgentUnavailable
}
func (*unavailableCommandLaunchAgent) Disable(context.Context) (bool, error) {
	return false, ErrLaunchAgentUnavailable
}

func TestInspectCurrentRevisionInvalidatesPersistedClassification(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	store := commandStore(t, now)
	committed, err := store.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	record := commandRecord("task-a", now)
	record.CapturedRevision = "rev-1"
	record.CapturedTitle = "Old"
	record.ManagedTokenDisplay = "1.6m"
	record.ManagedTokenPosition = tokens.PositionStart
	record.TokenDisplayPosition = tokens.PositionStart
	record.TokenRolloutPath = "/private/private-rollout.jsonl"
	record.TokenUsageFound = true
	committed.Tasks[record.TaskID] = record
	if err := store.SaveState(committed); err != nil {
		t.Fatal(err)
	}
	cycle := state.NewCycle("cycle-1", committed.Generation, now)
	cycle.Inventory["task-a"] = state.CapturedTask{TaskID: "task-a", Revision: "rev-1", Title: "Old", LastSubstantiveActivity: now}
	cycle.Results["task-a"] = state.ClassificationResult{TaskID: "task-a", Revision: "rev-1", Status: state.StatusComplete, Provenance: state.ProvenanceLuna}
	if err := store.SaveCycle(cycle); err != nil {
		t.Fatal(err)
	}
	inventory := &commandInventory{tasks: []codex.Task{{TaskID: "task-a", Revision: "rev-2", Title: "New"}}}
	result, err := InspectHandler(store, inventory, commandClock{now})(context.Background(), Request{Command: CommandInspect, TaskID: "task-a"})
	if err != nil {
		t.Fatal(err)
	}
	inspect := result.(output.InspectResult)
	if inspect.CapturedRevision != "rev-2" ||
		inspect.State != state.StatusUnknown ||
		inspect.Provenance != state.ProvenanceUnknown ||
		inspect.ArchiveEligible ||
		inspect.TokenDisplayPosition != tokens.PositionOff ||
		inspect.ManagedTokenPosition != tokens.PositionOff ||
		inspect.ManagedTokenDisplay != "" ||
		inspect.TokenUsageFound {
		t.Fatalf("inspect=%+v", inspect)
	}
}

func TestInspectCurrentTitleInvalidatesPersistedTokenFacts(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	store := commandStore(t, now)
	committed, err := store.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	record := commandRecord("task-a", now)
	record.CapturedTitle = "Old title"
	record.ManagedTokenDisplay = "1.6m"
	record.ManagedTokenPosition = tokens.PositionEnd
	record.TokenDisplayPosition = tokens.PositionEnd
	record.TokenRolloutPath = "/private/private-rollout.jsonl"
	record.TokenUsageFound = true
	committed.Tasks[record.TaskID] = record
	if err := store.SaveState(committed); err != nil {
		t.Fatal(err)
	}
	inventory := &commandInventory{tasks: []codex.Task{{TaskID: record.TaskID, Revision: record.CapturedRevision, Title: "User-edited title"}}}

	result, err := InspectHandler(store, inventory, commandClock{now})(context.Background(), Request{Command: CommandInspect, TaskID: record.TaskID})
	if err != nil {
		t.Fatal(err)
	}

	inspect := result.(output.InspectResult)
	if inspect.TokenDisplayPosition != tokens.PositionOff ||
		inspect.ManagedTokenPosition != tokens.PositionOff ||
		inspect.ManagedTokenDisplay != "" ||
		inspect.TokenUsageFound {
		t.Fatalf("inspect exposed stale token facts: %+v", inspect)
	}
	var human, machine bytes.Buffer
	if err := output.Write(&human, output.FormatHuman, inspect); err != nil {
		t.Fatal(err)
	}
	if err := output.Write(&machine, output.FormatJSON, inspect); err != nil {
		t.Fatal(err)
	}
	for _, fact := range []string{"token configured off", "token applied off/none", "token usage found false"} {
		if !strings.Contains(human.String(), fact) {
			t.Fatalf("human output missing %q: %q", fact, human.String())
		}
	}
	for _, fact := range []string{
		`"token_display_position":"off"`,
		`"managed_token_position":"off"`,
		`"managed_token_display":""`,
		`"token_usage_found":false`,
	} {
		if !strings.Contains(machine.String(), fact) {
			t.Fatalf("JSON output missing safe default %q: %q", fact, machine.String())
		}
	}
	for _, forbidden := range []string{"Old title", "User-edited title", "private-rollout.jsonl"} {
		if strings.Contains(human.String(), forbidden) || strings.Contains(machine.String(), forbidden) {
			t.Fatalf("inspect leaked %q", forbidden)
		}
	}
}

func TestRestoreVerifiedCheckpointOwnership(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	store := commandStore(t, now)
	cycle := state.NewCycle("cycle-1", 0, now.Add(-time.Hour))
	cycle.Inventory["task-a"] = state.CapturedTask{TaskID: "task-a", Revision: "rev-1", Title: "Archived", Archived: true, LastSubstantiveActivity: now.Add(-time.Hour)}
	cycle.Results["task-a"] = state.ClassificationResult{TaskID: "task-a", Revision: "rev-1", Status: state.StatusComplete, Provenance: state.ProvenanceRuntime}
	cycle.Operations["archive:task-a"] = state.CycleOperation{Kind: state.OperationArchive, Stage: state.StageVerified, TaskID: "task-a", ExpectedRevision: "rev-1", ExpectedTitle: "Archived"}
	if err := store.SaveCycle(cycle); err != nil {
		t.Fatal(err)
	}
	inventory := &commandInventory{}
	unarchiver := &commandUnarchiver{inventory: inventory}
	result, err := RestoreHandler(store, inventory, unarchiver, commandClock{now})(context.Background(), Request{Command: CommandRestore, TaskID: "task-a"})
	if err != nil || !result.(output.ActionResult).Changed || unarchiver.calls != 1 {
		t.Fatalf("result=%+v err=%v unarchiver=%d", result, err, unarchiver.calls)
	}
	stored, err := store.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if !stored.Tasks["task-a"].LastSubstantiveActivity.Equal(now) {
		t.Fatalf("state=%+v", stored)
	}
}

func TestDryRunDoesNotPreviewArchiveForChangedUnresolvedTask(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	store := commandStore(t, now)
	committed, err := store.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	lastUpdate := now
	committed.LastUpdateCheck = &lastUpdate
	record := commandRecord("task-a", now)
	record.CapturedRevision = "rev-1"
	record.CapturedTitle = "Old complete"
	committed.Tasks[record.TaskID] = record
	if err := store.SaveState(committed); err != nil {
		t.Fatal(err)
	}
	inventory := &commandInventory{tasks: []codex.Task{{TaskID: "task-a", Revision: "rev-2", Title: "Now running"}}}
	result, err := heartbeatDryRun(context.Background(), store, inventory, commandClock{now})
	if err != nil {
		t.Fatal(err)
	}
	preview := result.(output.PreviewResult)
	if !reflect.DeepEqual(preview.Effects, []string{"classify.task-a"}) {
		t.Fatalf("preview=%+v", preview)
	}
}

func TestDryRunPreviewsTokenDisplayRepositionWithoutMutation(t *testing.T) {
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	store := commandStore(t, now)
	cfg, err := store.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	cfg.TokenDisplay = tokens.PositionEnd
	if err := store.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	committed, err := store.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	committed.LastUpdateCheck = &now
	record := commandRecord("task-a", now)
	record.Status = state.StatusNextSteps
	record.TokenDisplayPosition = tokens.PositionStart
	committed.Tasks[record.TaskID] = record
	if err := store.SaveState(committed); err != nil {
		t.Fatal(err)
	}
	inventory := &commandInventory{tasks: []codex.Task{{TaskID: record.TaskID, Revision: record.CapturedRevision, Title: record.CapturedTitle}}}
	before := snapshotStateDirectory(t, store.Directory())

	result, err := heartbeatDryRun(context.Background(), store, inventory, commandClock{now})
	if err != nil {
		t.Fatal(err)
	}

	after := snapshotStateDirectory(t, store.Directory())
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("dry run mutated state directory\nbefore=%+v\nafter=%+v", before, after)
	}
	preview := result.(output.PreviewResult)
	if !reflect.DeepEqual(preview.Effects, []string{"classify.task-a"}) {
		t.Fatalf("preview=%+v", preview)
	}
}

func commandStore(t *testing.T, now time.Time) *state.Store {
	t.Helper()
	store := state.NewStore(t.TempDir() + "/state")
	if err := store.SaveConfig(config.Default("control-task")); err != nil {
		t.Fatal(err)
	}
	committed := state.New()
	committed.Generation = 0
	if err := store.SaveState(committed); err != nil {
		t.Fatal(err)
	}
	return store
}

func commandRecord(taskID string, now time.Time) state.TaskRecord {
	return state.TaskRecord{TaskID: taskID, CapturedRevision: "rev-1", CapturedTitle: "Task", Status: state.StatusComplete, Provenance: state.ProvenanceRuntime, StateStartedAt: now.Add(-20 * 24 * time.Hour), LastSubstantiveActivity: now.Add(-20 * 24 * time.Hour), TokenDisplayPosition: tokens.PositionStart}
}
