package main

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
)

func TestMigrationInventoryExcludesMainAndController(t *testing.T) {
	root, db := testIndex(t)
	addTask(t, db, root, "main", "ThreadBear", nil, "vscode", 0)
	addTask(t, db, root, "controller", "Migration controller", nil, "vscode", 0)
	targetRollout := addTask(t, db, root, "target", "Target", nil, "vscode", 0)
	writeMigrationRollout(t, targetRollout, "🧵🐻 complete")
	if err := newStore(stateDir()).update(func(value *state) (bool, error) {
		value.MainTaskID, value.ControllerTaskID, value.Phase = "main", "controller", phaseMigrationRunning
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	items, _, _, err := migrationInventory(context.Background())
	if err != nil || len(items) != 1 || items[0].TaskID != "target" {
		t.Fatalf("migration scope = %#v, %v", items, err)
	}
}

func TestMigrationControllerRequiresAppliedFinalConvergence(t *testing.T) {
	root, db := testIndex(t)
	addTask(t, db, root, "main", "ThreadBear", nil, "vscode", 0)
	addTask(t, db, root, "controller", "Migration controller", nil, "vscode", 0)
	targetRollout := addTask(t, db, root, "target", "Target", nil, "vscode", 0)
	writeMigrationRollout(t, targetRollout, "🧵🐻 complete")
	if err := newStore(stateDir()).update(func(value *state) (bool, error) {
		value.MainTaskID, value.ControllerTaskID, value.Phase = "main", "controller", phaseMigrationRunning
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := transitionMigration(context.Background(), phaseMigrationRunning, "controller", false); err != nil {
		t.Fatal(err)
	}
	if _, err := transitionMigration(context.Background(), phaseMigrationRunning, "other", false); err == nil {
		t.Fatal("migration accepted a second controller")
	}
	if _, err := transitionMigration(context.Background(), phaseMigrationComplete, "controller", false); err == nil {
		t.Fatal("migration completed before applied inventory convergence")
	}
	if _, err := db.Exec(`UPDATE threads SET name=? WHERE id='target'`, "✅ Target"); err != nil {
		t.Fatal(err)
	}
	if err := newStore(stateDir()).update(func(value *state) (bool, error) {
		value.Tasks["target"] = taskState{Subject: "Target", Last: "✅ Target", Status: "complete"}
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	result, err := transitionMigration(context.Background(), phaseMigrationComplete, "controller", false)
	if err != nil {
		t.Fatal(err)
	}
	if result.(map[string]any)["ready"] != true {
		t.Fatalf("completion result = %#v", result)
	}
	value, err := newStore(stateDir()).read()
	if err != nil || value.Phase != phaseMigrationComplete {
		t.Fatalf("completed state = %#v, %v", value, err)
	}
}

func TestMigrationResumeReconcilesOnlyExactOwnedAppliedProposal(t *testing.T) {
	root, db := testIndex(t)
	addTask(t, db, root, "main", "ThreadBear", nil, "vscode", 0)
	addTask(t, db, root, "controller", "Migration controller", nil, "vscode", 0)
	addTask(t, db, root, "target", "✅ Target", nil, "vscode", 0)
	if err := newStore(stateDir()).update(func(value *state) (bool, error) {
		value.MainTaskID, value.ControllerTaskID, value.Phase = "main", "controller", phaseMigrationFailed
		value.Tasks["target"] = taskState{Pending: &pendingProposal{CallerTaskID: "controller", BaseSubject: "Target", Prior: "Target", Proposed: "✅ Target", Status: "complete"}}
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_THREAD_ID", "controller")
	result, err := transitionMigration(context.Background(), phaseMigrationRunning, "controller", false)
	if err != nil {
		t.Fatalf("resume = %#v, %v", result, err)
	}
	value, err := newStore(stateDir()).read()
	if got := value.Tasks["target"]; err != nil || got.Pending != nil || got.Subject != "Target" || got.Last != "✅ Target" {
		t.Fatalf("reconciled state = %#v, %v", got, err)
	}
	if err := newStore(stateDir()).update(func(value *state) (bool, error) {
		value.Phase = phaseMigrationFailed
		value.Tasks["target"] = taskState{Pending: &pendingProposal{BaseSubject: "Target", Prior: "Target", Proposed: "✅ Target", Status: "complete"}}
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := transitionMigration(context.Background(), phaseMigrationRunning, "controller", false); err == nil || !strings.Contains(err.Error(), "not owned") {
		t.Fatalf("legacy pending resume = %v", err)
	}
	value, _ = newStore(stateDir()).read()
	if value.Tasks["target"].Pending == nil {
		t.Fatal("rejected legacy proposal was cleared")
	}
	if _, err := db.Exec(`UPDATE threads SET archived=1 WHERE id='target'`); err != nil {
		t.Fatal(err)
	}
	if _, err := transitionMigration(context.Background(), phaseMigrationComplete, "controller", false); err == nil {
		t.Fatal("migration completed with an inactive pending title")
	}
}

func TestSettledMigrationFailureClearsOnlyKnownUnappliedProposal(t *testing.T) {
	root, db := testIndex(t)
	addTask(t, db, root, "main", "ThreadBear", nil, "vscode", 0)
	addTask(t, db, root, "controller", "Migration controller", nil, "vscode", 0)
	addTask(t, db, root, "target", "Target", nil, "vscode", 0)
	stage := func() {
		if err := newStore(stateDir()).update(func(value *state) (bool, error) {
			value.MainTaskID, value.ControllerTaskID, value.Phase = "main", "controller", phaseMigrationRunning
			value.Tasks["target"] = taskState{Pending: &pendingProposal{CallerTaskID: "controller", BaseSubject: "Target", Prior: "Target", Proposed: "✅ Target", Status: "complete"}}
			return true, nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	stage()
	t.Setenv("CODEX_THREAD_ID", "other")
	if _, err := transitionMigration(context.Background(), phaseMigrationFailed, "controller", true); err == nil {
		t.Fatal("a different task attested to the controller's settled wave")
	}
	t.Setenv("CODEX_THREAD_ID", "controller")
	if _, err := transitionMigration(context.Background(), phaseMigrationFailed, "controller", true); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	pre := hookPayload("PreToolUse", "controller", "after-attestation", map[string]any{"title": runningMarker + ": Migration controller"}, nil)
	if err := hook(context.Background(), strings.NewReader(pre), &output); err != nil || !strings.Contains(output.String(), `"permissionDecision":"deny"`) {
		t.Fatalf("post-attestation proposal = %q, %v", output.String(), err)
	}
	if _, err := transitionMigration(context.Background(), phaseMigrationRunning, "controller", false); err != nil {
		t.Fatal(err)
	}
	value, _ := newStore(stateDir()).read()
	if got := value.Tasks["target"]; got.Pending != nil || got.Last != "" {
		t.Fatalf("known-unapplied reconciliation = %#v", got)
	}
	stage()
	if _, err := transitionMigration(context.Background(), phaseMigrationFailed, "controller", false); err != nil {
		t.Fatal(err)
	}
	if _, err := transitionMigration(context.Background(), phaseMigrationRunning, "controller", false); err == nil {
		t.Fatal("unknown failure cleared an unchanged proposal")
	}
	if _, err := transitionMigration(context.Background(), phaseMigrationFailed, "controller", true); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE threads SET archived=1 WHERE id='target'`); err != nil {
		t.Fatal(err)
	}
	if _, err := transitionMigration(context.Background(), phaseMigrationRunning, "controller", false); err != nil {
		t.Fatal(err)
	}
	value, _ = newStore(stateDir()).read()
	if value.Tasks["target"].Pending != nil {
		t.Fatal("settled disappeared target remained pending")
	}
}

func TestMigrationReadinessRequiresCompletePhase(t *testing.T) {
	root, db := testIndex(t)
	addTask(t, db, root, "main", "ThreadBear", nil, "vscode", 0)
	addTask(t, db, root, "controller", "Migration controller", nil, "vscode", 0)
	if err := newStore(stateDir()).update(func(value *state) (bool, error) {
		value.MainTaskID = "main"
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, phase := range []string{phaseMigrationRunning, phaseMigrationFailed, phaseMigrationRunning} {
		result, err := transitionMigration(context.Background(), phase, "controller", false)
		if err != nil {
			t.Fatal(err)
		}
		if got := result.(map[string]any); got["ready"] != false || got["recorded"] != true {
			t.Fatalf("%s result = %#v", phase, got)
		}
	}
	value, err := newStore(stateDir()).read()
	if err != nil || value.MigrationStarted == "" || value.MigrationFailure != "" {
		t.Fatalf("resumed state = %#v, %v", value, err)
	}
}

func TestInstallMainTaskIdentityIsSticky(t *testing.T) {
	isolatedLifecycle(t)
	if _, err := install("main", false, true, false); err != nil {
		t.Fatal(err)
	}
	value, err := newStore(stateDir()).read()
	if err != nil || value.MainTaskID != "main" || value.Phase != phaseMigrationPending {
		t.Fatalf("initial identity = %#v, %v", value, err)
	}
	if _, err := install("", false, true, false); err != nil {
		t.Fatal(err)
	}
	if _, err := install("other", false, true, false); err == nil {
		t.Fatal("reinstall replaced the persisted main task")
	}
}

func TestFreshInstallIsPendingUntilControllerIsRecorded(t *testing.T) {
	isolatedLifecycle(t)
	result, err := install("main", false, true, false)
	if err != nil {
		t.Fatal(err)
	}
	got := result.(map[string]any)
	if got["phase"] != phaseMigrationPending || got["controller_required"] != true || got["ready"] != false {
		t.Fatalf("install result = %#v", got)
	}
	statusResult, err := status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	statusGot := statusResult.(map[string]any)
	if statusGot["phase"] != phaseMigrationPending || statusGot["next_action"] != "start migration from the ThreadBear task" {
		t.Fatalf("status result = %#v", statusGot)
	}
}

func TestUninstallRefusesActiveMigration(t *testing.T) {
	p := isolatedLifecycle(t)
	if _, err := install("main", false, true, false); err != nil {
		t.Fatal(err)
	}
	if _, err := transitionMigration(context.Background(), phaseMigrationRunning, "controller", false); err != nil {
		t.Fatal(err)
	}
	if _, err := uninstall(context.Background(), true); err == nil {
		t.Fatal("uninstall accepted an active migration")
	}
	if _, err := os.Stat(p.binary); err != nil {
		t.Fatalf("blocked uninstall removed the binary: %v", err)
	}
}

func TestStatusReconcilesStoppedMigration(t *testing.T) {
	root, db := testIndex(t)
	addTask(t, db, root, "main", "ThreadBear", nil, "vscode", 0)
	controller := addTask(t, db, root, "controller", "Migration controller", nil, "vscode", 0)
	if _, err := install("main", false, true, false); err != nil {
		t.Fatal(err)
	}
	if _, err := transitionMigration(context.Background(), phaseMigrationRunning, "controller", false); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(controller, []byte(
		lifecycleLine("task_started", "2099-12-31T23:59:59Z")+
			lifecycleLine("task_complete", "2100-01-01T00:00:00Z"),
	), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := result.(map[string]any)
	if got["phase"] != phaseMigrationFailed || got["migration_failure"] != "controller stopped before migration completed" || got["next_action"] != "resume migration from the ThreadBear task" {
		t.Fatalf("status result = %#v", got)
	}
}

func TestUninstallRefusesDecoratedActiveTitles(t *testing.T) {
	root, db := testIndex(t)
	p := installPaths()
	addTask(t, db, root, "target", "✅ ✅ Target", nil, "vscode", 0)
	if _, err := install("main", false, true, false); err != nil {
		t.Fatal(err)
	}
	if err := newStore(stateDir()).update(func(value *state) (bool, error) {
		value.Phase = phaseMigrationComplete
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := uninstall(context.Background(), true); err == nil || !strings.Contains(err.Error(), "requires title cleanup") {
		t.Fatalf("decorated uninstall = %v", err)
	}
	if _, err := os.Stat(p.binary); err != nil {
		t.Fatalf("blocked uninstall removed the binary: %v", err)
	}
	if _, err := db.Exec(`UPDATE threads SET title='Target' WHERE id='target'`); err != nil {
		t.Fatal(err)
	}
	if _, err := uninstall(context.Background(), true); err != nil {
		t.Fatal(err)
	}
}

func TestUninstallRefusesDecoratedArchivedMainAndIgnoresArchivedController(t *testing.T) {
	root, db := testIndex(t)
	p := installPaths()
	addTask(t, db, root, "main", "⏳ Control task", nil, "vscode", 1)
	addTask(t, db, root, "controller", "⏳ Completed controller", nil, "vscode", 1)
	if _, err := install("main", false, true, false); err != nil {
		t.Fatal(err)
	}
	if err := newStore(stateDir()).update(func(value *state) (bool, error) {
		value.ControllerTaskID, value.Phase = "controller", phaseMigrationComplete
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := uninstall(context.Background(), true); err == nil || !strings.Contains(err.Error(), "requires title cleanup") {
		t.Fatalf("decorated archived main uninstall = %v", err)
	}
	if _, err := os.Stat(p.binary); err != nil {
		t.Fatalf("blocked uninstall removed the binary: %v", err)
	}
	if _, err := db.Exec(`UPDATE threads SET title='Control task' WHERE id='main'`); err != nil {
		t.Fatal(err)
	}
	if _, err := uninstall(context.Background(), true); err != nil {
		t.Fatalf("clean archived main with distinct archived controller: %v", err)
	}
}
