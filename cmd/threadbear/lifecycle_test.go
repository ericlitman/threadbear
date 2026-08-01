package main

import (
	"context"
	"os"
	"testing"
)

func TestMigrationInventoryExcludesMainAndController(t *testing.T) {
	root, db := testIndex(t)
	addTask(t, db, root, "main", "ThreadBear", nil, "vscode", 0)
	addTask(t, db, root, "controller", "Migration controller", nil, "vscode", 0)
	targetRollout := addTask(t, db, root, "target", "Target", nil, "exec", 0)
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
	targetRollout := addTask(t, db, root, "target", "Target", nil, "exec", 0)
	writeMigrationRollout(t, targetRollout, "🧵🐻 complete")
	if err := newStore(stateDir()).update(func(value *state) (bool, error) {
		value.MainTaskID, value.ControllerTaskID, value.Phase = "main", "controller", phaseMigrationRunning
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := transitionMigration(context.Background(), phaseMigrationRunning, "controller"); err != nil {
		t.Fatal(err)
	}
	if _, err := transitionMigration(context.Background(), phaseMigrationRunning, "other"); err == nil {
		t.Fatal("migration accepted a second controller")
	}
	if _, err := transitionMigration(context.Background(), phaseMigrationComplete, "controller"); err == nil {
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
	result, err := transitionMigration(context.Background(), phaseMigrationComplete, "controller")
	if err != nil {
		t.Fatal(err)
	}
	if result.(map[string]any)["remaining"] != 0 {
		t.Fatalf("completion result = %#v", result)
	}
	value, err := newStore(stateDir()).read()
	if err != nil || value.Phase != phaseMigrationComplete {
		t.Fatalf("completed state = %#v, %v", value, err)
	}
}

func TestInstallMainTaskIdentityIsSticky(t *testing.T) {
	isolatedLifecycle(t)
	if _, err := install("main", false, true); err != nil {
		t.Fatal(err)
	}
	value, err := newStore(stateDir()).read()
	if err != nil || value.MainTaskID != "main" || value.Phase != phaseMigrationRunning {
		t.Fatalf("initial identity = %#v, %v", value, err)
	}
	if _, err := install("", false, true); err != nil {
		t.Fatal(err)
	}
	if _, err := install("other", false, true); err == nil {
		t.Fatal("reinstall replaced the persisted main task")
	}
}

func TestUninstallRefusesActiveMigration(t *testing.T) {
	p := isolatedLifecycle(t)
	if _, err := install("main", false, true); err != nil {
		t.Fatal(err)
	}
	if _, err := uninstall(true); err == nil {
		t.Fatal("uninstall accepted an active migration")
	}
	if _, err := os.Stat(p.binary); err != nil {
		t.Fatalf("blocked uninstall removed the binary: %v", err)
	}
}
