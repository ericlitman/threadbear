package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestMigrationInventoryAtZeroOneAndTwoHundredTasks(t *testing.T) {
	t.Run("zero", func(t *testing.T) {
		_, _ = testIndex(t)
		items, _, _, err := migrationInventory(context.Background())
		if err != nil || len(items) != 0 {
			t.Fatalf("migrationInventory() = %#v, %v", items, err)
		}
	})

	t.Run("one", func(t *testing.T) {
		root, db := testIndex(t)
		path := addTask(t, db, root, "only", "Ship the release", nil, "vscode", 0)
		writeMigrationRollout(t, path, "🧵🐻 next steps (you): approve the release")
		items, _, _, err := migrationInventory(context.Background())
		if err != nil || len(items) != 1 {
			t.Fatalf("migrationInventory() = %#v, %v", items, err)
		}
		got := items[0]
		if got.TaskID != "only" || got.Subject != "Ship the release" || got.Status != "next_steps" ||
			got.Action != "approve the release" || !got.Deterministic || got.Applied {
			t.Fatalf("one-task inventory = %#v", got)
		}
	})

	t.Run("two hundred", func(t *testing.T) {
		root, db := testIndex(t)
		paths := make(map[string]string, 201)
		for i := 0; i < 200; i++ {
			id := fmt.Sprintf("task-%03d", i)
			title := "Subject " + id
			switch i {
			case 2:
				title = "➡️ Similar-looking user title → literal suffix"
			case 3:
				title = "➡️ Classified subject → approve the release"
			case 4:
				title = "✅ Owned subject"
			}
			paths[id] = addTask(t, db, root, id, title, nil, "vscode", 0)
			if i == 0 || i == 4 || i >= 5 && i%2 == 0 {
				writeMigrationRollout(t, paths[id], "🧵🐻 complete")
			}
		}
		archived := addTask(t, db, root, "archived", "Archived subject", nil, "vscode", 1)
		writeMigrationRollout(t, archived, "🧵🐻 complete")
		writeMigrationState(t, map[string]any{
			"task-003": map[string]any{
				"subject": "Classified subject", "last": "➡️ Classified subject → approve the release",
				"status": "next_steps", "action": "approve the release",
			},
			"task-004": map[string]any{
				"subject": "Owned subject", "last": "✅ Owned subject", "status": "complete",
			},
		})

		before, err := os.ReadFile(newStore(stateDir()).path())
		if err != nil {
			t.Fatal(err)
		}
		first, _, _, err := migrationInventory(context.Background())
		if err != nil || len(first) != 200 {
			t.Fatalf("migrationInventory() count = %d, %v", len(first), err)
		}
		second, _, _, err := migrationInventory(context.Background())
		if err != nil || !reflect.DeepEqual(second, first) {
			t.Fatalf("idempotent rerun differs: %v\nfirst: %#v\nsecond: %#v", err, first, second)
		}
		after, err := os.ReadFile(newStore(stateDir()).path())
		if err != nil || !reflect.DeepEqual(after, before) {
			t.Fatalf("inventory mutated state: %v\nbefore: %s\nafter: %s", err, before, after)
		}

		byID := make(map[string]inventoryItem, len(first))
		deterministic, applied := 0, 0
		for _, item := range first {
			byID[item.TaskID] = item
			if item.Deterministic {
				deterministic++
			}
			if item.Applied {
				applied++
			}
		}
		if deterministic != 100 || applied != 2 {
			t.Fatalf("inventory counts: deterministic=%d applied=%d", deterministic, applied)
		}
		if _, ok := byID["archived"]; ok {
			t.Fatal("archived task entered migration inventory")
		}
		if got := byID["task-001"]; got.Status != "unknown" || got.Deterministic || got.Applied {
			t.Fatalf("ambiguous task = %#v", got)
		}
		if got := byID["task-002"]; got.Subject != "➡️ Similar-looking user title → literal suffix" || got.Deterministic || got.Applied {
			t.Fatalf("similar-looking user-owned title = %#v", got)
		}
		if got := byID["task-003"]; got.Subject != "Classified subject" || got.Status != "next_steps" ||
			got.Action != "approve the release" || !got.Deterministic || !got.Applied {
			t.Fatalf("persisted ambiguous classification = %#v", got)
		}
		if got := byID["task-004"]; got.Subject != "Owned subject" || got.Status != "complete" ||
			!got.Deterministic || !got.Applied {
			t.Fatalf("deterministic owned task = %#v", got)
		}
	})
}

func TestRolloutFooterStopsAtNewerUnsettledTurn(t *testing.T) {
	old := rolloutLine("response_item", map[string]any{
		"type": "message", "role": "assistant", "phase": "final_answer",
		"content": []map[string]string{{"text": "Done.\n\n🧵🐻 complete"}},
	})
	for name, newer := range map[string]string{
		"active user turn": rolloutLine("response_item", map[string]any{
			"type": "message", "role": "user", "content": []map[string]string{{"text": "One more change"}},
		}),
		"aborted turn": rolloutLine("event_msg", map[string]any{"type": "turn_aborted"}),
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "rollout.jsonl")
			if err := os.WriteFile(path, []byte(old+newer), 0o600); err != nil {
				t.Fatal(err)
			}
			if got, ok := rolloutFooter(path); ok {
				t.Fatalf("older footer crossed an unsettled turn boundary: %#v", got)
			}
		})
	}
}

func TestReconcileMigrationFailsStoppedController(t *testing.T) {
	root, db := testIndex(t)
	addTask(t, db, root, "main", "ThreadBear", nil, "vscode", 0)
	controller := addTask(t, db, root, "controller", "Migration controller", nil, "vscode", 0)
	if err := newStore(stateDir()).update(func(value *state) (bool, error) {
		value.MainTaskID, value.ControllerTaskID, value.Phase = "main", "controller", phaseMigrationRunning
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	data := []byte(
		rolloutLine("event_msg", map[string]any{"type": "task_started"}) +
			rolloutLine("event_msg", map[string]any{"type": "task_complete"}))
	if err := os.WriteFile(controller, data, 0o600); err != nil {
		t.Fatal(err)
	}

	value, err := reconcileMigration(context.Background())
	if err != nil || value.Phase != phaseMigrationFailed || value.MigrationFailure != "controller stopped before migration completed" {
		t.Fatalf("reconciled state = %#v, %v", value, err)
	}
}

func TestReconcileMigrationRestoresPendingWhenControllerWasNeverRecorded(t *testing.T) {
	testIndex(t)
	if err := newStore(stateDir()).update(func(value *state) (bool, error) {
		value.MainTaskID, value.Phase = "main", phaseMigrationRunning
		value.MigrationStarted = "2026-08-03T12:00:00Z"
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}

	value, err := reconcileMigration(context.Background())
	if err != nil || value.Phase != phaseMigrationPending || value.MigrationStarted != "" || value.ControllerTaskID != "" {
		t.Fatalf("reconciled state = %#v, %v", value, err)
	}
}

func TestReconcileMigrationPreservesActiveOrUnknownController(t *testing.T) {
	for name, rollout := range map[string]string{
		"active":  lifecycleLine("task_started", "2100-01-01T00:00:00Z"),
		"unknown": "not-json\n",
	} {
		t.Run(name, func(t *testing.T) {
			root, db := testIndex(t)
			addTask(t, db, root, "main", "ThreadBear", nil, "vscode", 0)
			controller := addTask(t, db, root, "controller", "Migration controller", nil, "vscode", 0)
			if err := os.WriteFile(controller, []byte(rollout), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := newStore(stateDir()).update(func(value *state) (bool, error) {
				value.MainTaskID, value.ControllerTaskID, value.Phase = "main", "controller", phaseMigrationRunning
				return true, nil
			}); err != nil {
				t.Fatal(err)
			}
			value, err := reconcileMigration(context.Background())
			if err != nil || value.Phase != phaseMigrationRunning || value.MigrationFailure != "" {
				t.Fatalf("reconciled state = %#v, %v", value, err)
			}
		})
	}
}

func TestReconcileMigrationIgnoresTerminalEventFromPriorAttempt(t *testing.T) {
	root, db := testIndex(t)
	addTask(t, db, root, "main", "ThreadBear", nil, "vscode", 0)
	controller := addTask(t, db, root, "controller", "Migration controller", nil, "vscode", 0)
	if err := newStore(stateDir()).update(func(value *state) (bool, error) {
		value.MainTaskID = "main"
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := transitionMigration(context.Background(), phaseMigrationRunning, "controller", false); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(controller, []byte(lifecycleLine("task_complete", "2000-01-01T00:00:00Z")), 0o600); err != nil {
		t.Fatal(err)
	}
	value, err := reconcileMigration(context.Background())
	if err != nil || value.Phase != phaseMigrationRunning {
		t.Fatalf("reconciled state = %#v, %v", value, err)
	}
}

func TestReconcileMigrationReportsControllerLookupFailure(t *testing.T) {
	root, db := testIndex(t)
	addTask(t, db, root, "main", "ThreadBear", nil, "vscode", 0)
	addTask(t, db, root, "controller", "Migration controller", nil, "vscode", 0)
	if err := newStore(stateDir()).update(func(value *state) (bool, error) {
		value.MainTaskID, value.ControllerTaskID, value.Phase = "main", "controller", phaseMigrationRunning
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_HOME", filepath.Join(root, "missing-codex-home"))

	value, err := reconcileMigration(context.Background())
	if err == nil || value.Phase != phaseMigrationRunning {
		t.Fatalf("reconciled state = %#v, %v; want running state plus lookup error", value, err)
	}
}

func TestReconcileMigrationReportsRolloutReadFailure(t *testing.T) {
	root, db := testIndex(t)
	addTask(t, db, root, "main", "ThreadBear", nil, "vscode", 0)
	controller := addTask(t, db, root, "controller", "Migration controller", nil, "vscode", 0)
	if err := newStore(stateDir()).update(func(value *state) (bool, error) {
		value.MainTaskID, value.ControllerTaskID, value.Phase = "main", "controller", phaseMigrationRunning
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(controller); err != nil {
		t.Fatal(err)
	}

	value, err := reconcileMigration(context.Background())
	if err == nil || value.Phase != phaseMigrationRunning {
		t.Fatalf("reconciled state = %#v, %v; want running state plus rollout error", value, err)
	}
}

func writeMigrationRollout(t *testing.T, path, marker string) {
	t.Helper()
	line := rolloutLine("response_item", map[string]any{
		"type": "message", "role": "assistant", "phase": "final_answer",
		"content": []map[string]string{{"text": "Result.\n\n" + marker}},
	})
	if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeMigrationState(t *testing.T, tasks map[string]any) {
	t.Helper()
	data, err := json.Marshal(map[string]any{"format": stateFormat, "tasks": tasks})
	if err != nil {
		t.Fatal(err)
	}
	path := newStore(stateDir()).path()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

func lifecycleLine(kind, timestamp string) string {
	data, _ := json.Marshal(map[string]any{"timestamp": timestamp, "type": "event_msg", "payload": map[string]any{"type": kind}})
	return string(data) + "\n"
}
