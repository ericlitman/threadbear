package main

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestMaintenancePlansOnlyInactiveOwnedCompleteUserTasks(t *testing.T) {
	root, db := testIndex(t)
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	old := now.AddDate(0, 0, -14)
	maintenanceNow = func() time.Time { return now }
	t.Cleanup(func() { maintenanceNow = time.Now })
	for _, id := range []string{"eligible", "fresh", "blocked", "drift", "main", "controller", "worker", "automation", "archived"} {
		path := addTask(t, db, root, id, "✅ "+id, nil, "vscode", 0)
		writeMigrationRollout(t, path, "🧵🐻 complete")
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatal(err)
		}
	}
	nullPath := addTask(t, db, root, "nulls", "✅ nulls", nil, "vscode", 0)
	writeMigrationRollout(t, nullPath, "🧵🐻 complete")
	if _, err := db.Exec(`UPDATE threads SET archived=NULL, preview=NULL WHERE id='nulls'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE threads SET archived=1 WHERE id='archived'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE threads SET thread_source='subagent' WHERE id='worker'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE threads SET thread_source='automation' WHERE id='automation'`); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filepathForTask(root, "fresh"), now, now); err != nil {
		t.Fatal(err)
	}
	if err := newStore(stateDir()).update(func(value *state) (bool, error) {
		value.MainTaskID, value.ControllerTaskID, value.Phase = "main", "controller", phaseMigrationComplete
		for _, id := range []string{"eligible", "fresh", "drift", "main", "controller", "worker", "automation", "archived"} {
			value.Tasks[id] = taskState{Subject: id, Last: "✅ " + id, Status: "complete"}
		}
		value.Tasks["blocked"] = taskState{Subject: "blocked", Last: "✅ blocked", Status: "blocked"}
		value.Tasks["drift"] = taskState{Subject: "drift", Last: "✅ another title", Status: "complete"}
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	result, err := maintenance(context.Background(), "", "", "", 14)
	if err != nil {
		t.Fatal(err)
	}
	candidates := result.(map[string]any)["candidates"].([]map[string]string)
	if len(candidates) != 1 || candidates[0]["task_id"] != "eligible" || candidates[0]["inactive_since"] != old.Format(time.RFC3339Nano) {
		t.Fatalf("candidates = %#v", candidates)
	}
}

func TestMaintenanceArchiveRestoreAndInterruptionReconcile(t *testing.T) {
	root, db := testIndex(t)
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	old := now.AddDate(0, 0, -30)
	maintenanceNow = func() time.Time { return now }
	t.Cleanup(func() { maintenanceNow = time.Now })
	path := addTask(t, db, root, "target", "✅ target", nil, "vscode", 0)
	writeMigrationRollout(t, path, "🧵🐻 complete")
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	if err := newStore(stateDir()).update(func(value *state) (bool, error) {
		value.MainTaskID, value.Phase = "main", phaseMigrationComplete
		value.Tasks["target"] = taskState{Subject: "target", Last: "✅ target", Status: "complete"}
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	staged, err := maintenance(context.Background(), "target", "", "", 14)
	if err != nil || staged.(map[string]any)["action"] != "archive" {
		t.Fatalf("stage archive = %#v, %v", staged, err)
	}
	if _, err := maintenance(context.Background(), "", "other", "", 14); err != nil {
		t.Fatalf("pending plan should report, not fail: %v", err)
	}
	if _, err := db.Exec(`UPDATE threads SET archived=1 WHERE id='target'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE threads SET preview='' WHERE id='target'`); err != nil {
		t.Fatal(err)
	}
	reconciled, err := maintenance(context.Background(), "target", "", "", 14)
	if err != nil || reconciled.(map[string]any)["reconciled"] != true {
		t.Fatalf("reconcile archive = %#v, %v", reconciled, err)
	}
	if _, err := maintenance(context.Background(), "", "target", "", 14); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE threads SET archived=0 WHERE id='target'`); err != nil {
		t.Fatal(err)
	}
	restored, err := maintenance(context.Background(), "", "target", "", 14)
	if err != nil || restored.(map[string]any)["reconciled"] != true {
		t.Fatalf("reconcile restore = %#v, %v", restored, err)
	}
	plan, err := maintenance(context.Background(), "", "", "", 14)
	if err != nil || len(plan.(map[string]any)["candidates"].([]map[string]string)) != 0 {
		t.Fatalf("restored task was immediately eligible: %#v, %v", plan, err)
	}
	value, _ := newStore(stateDir()).read()
	if value.Archives["target"] || value.ArchivePending != nil || value.Tasks["target"].ArchiveActivity != now.Format(time.RFC3339Nano) {
		t.Fatalf("archive state = %#v", value)
	}
}

func TestMaintenanceRejectsDriftAndUninstallWithPendingArchive(t *testing.T) {
	root, db := testIndex(t)
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	maintenanceNow = func() time.Time { return now }
	t.Cleanup(func() { maintenanceNow = time.Now })
	path := addTask(t, db, root, "target", "✅ target", nil, "vscode", 0)
	writeMigrationRollout(t, path, "🧵🐻 complete")
	old := now.AddDate(0, 0, -15)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	if err := newStore(stateDir()).update(func(value *state) (bool, error) {
		value.MainTaskID, value.Phase = "main", phaseMigrationComplete
		value.Tasks["target"] = taskState{Subject: "target", Last: "✅ target", Status: "complete"}
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := maintenance(context.Background(), "target", "", "", 14); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE threads SET name='User rename' WHERE id='target'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE threads SET thread_source='subagent' WHERE id='target'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE threads SET archived=1 WHERE id='target'`); err != nil {
		t.Fatal(err)
	}
	if _, err := maintenance(context.Background(), "target", "", "", 14); err == nil {
		t.Fatal("applied archive accepted title drift")
	}
	if _, err := uninstall(context.Background(), true); err == nil {
		t.Fatal("uninstall accepted pending archive")
	}
	if _, err := maintenance(context.Background(), "", "", "target", 14); err == nil {
		t.Fatal("cancel accepted a still-applied drifted archive")
	}
	if _, err := db.Exec(`UPDATE threads SET archived=0 WHERE id='target'`); err != nil {
		t.Fatal(err)
	}
	cancelled, err := maintenance(context.Background(), "", "", "target", 14)
	if err != nil || cancelled.(map[string]any)["cancelled"] != true {
		t.Fatalf("cancel pending archive = %#v, %v", cancelled, err)
	}
	value, _ := newStore(stateDir()).read()
	if value.ArchivePending != nil {
		t.Fatal("cancel left a pending archive")
	}
}

func TestMaintenanceDetectsManualNativeRestore(t *testing.T) {
	root, db := testIndex(t)
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	maintenanceNow = func() time.Time { return now }
	t.Cleanup(func() { maintenanceNow = time.Now })
	addTask(t, db, root, "target", "✅ target", nil, "vscode", 1)
	if err := newStore(stateDir()).update(func(value *state) (bool, error) {
		value.MainTaskID, value.Phase = "main", phaseMigrationComplete
		value.Tasks["target"] = taskState{Subject: "target", Last: "✅ target", Status: "complete"}
		value.Archives = map[string]bool{"target": true}
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE threads SET archived=0 WHERE id='target'`); err != nil {
		t.Fatal(err)
	}
	if _, err := maintenance(context.Background(), "", "", "", 14); err != nil {
		t.Fatal(err)
	}
	value, _ := newStore(stateDir()).read()
	if value.Archives["target"] || value.Tasks["target"].ArchiveActivity != now.Format(time.RFC3339Nano) {
		t.Fatalf("manual restore state = %#v", value)
	}
}

func filepathForTask(root, id string) string { return root + "/" + id + ".jsonl" }
