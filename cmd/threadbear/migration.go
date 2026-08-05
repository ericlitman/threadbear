package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"time"
)

func currentStateOrEmpty() (value state, err error) {
	value, err = newStore(stateDir()).read()
	if errors.Is(err, os.ErrNotExist) {
		return state{Format: stateFormat, Tasks: map[string]taskState{}}, nil
	}
	return
}
func transitionMigration(ctx context.Context, phase, controllerID string, settled bool) (any, error) {
	if phase != phaseMigrationRunning && phase != phaseMigrationComplete && phase != phaseMigrationFailed || controllerID == "" || settled && (phase != phaseMigrationFailed || os.Getenv("CODEX_THREAD_ID") != controllerID) {
		return nil, errors.New("migration requires a valid phase and controller task ID")
	}
	store := newStore(stateDir())
	titleLock, err := store.titleLock()
	if err != nil {
		return nil, err
	}
	defer unlock(titleLock)
	if phase == phaseMigrationRunning {
		if _, err = reconcileTitles(ctx, controllerID); err != nil {
			return nil, err
		}
	}
	remaining := -1
	var known state
	if phase == phaseMigrationComplete {
		_, remaining, known, err = migrationInventory(ctx)
		if err != nil {
			return nil, err
		}
		if remaining != 0 || hasPendingTitle(known) {
			return nil, errors.New("migration has unresolved tasks")
		}
	}
	err = store.update(func(value *state) (bool, error) {
		if value.MainTaskID == "" || controllerID == value.MainTaskID || value.ControllerTaskID != "" && value.ControllerTaskID != controllerID || phase != phaseMigrationRunning && value.ControllerTaskID != controllerID || value.Phase == phaseMigrationComplete && phase != phaseMigrationComplete {
			return false, errors.New("migration controller or main task changed")
		}
		value.ControllerTaskID, value.Phase = controllerID, phase
		value.MigrationStarted, value.MigrationFailure = "", map[string]string{phaseMigrationFailed: map[bool]string{true: "controller reported a settled migration failure", false: "controller reported a migration failure"}[settled]}[phase]
		if phase == phaseMigrationRunning {
			value.MigrationStarted = time.Now().UTC().Format(time.RFC3339Nano)
		}
		return true, nil
	})
	if err != nil {
		return nil, err
	}
	result := map[string]any{"ready": phase == phaseMigrationComplete, "recorded": true, "phase": phase}
	return result, nil
}
func reconcileMigration(ctx context.Context) (state, error) {
	value, err := newStore(stateDir()).read()
	if err != nil || value.Phase != phaseMigrationRunning {
		return value, err
	}
	if value.ControllerTaskID == "" {
		err = newStore(stateDir()).update(func(current *state) (bool, error) {
			if current.Phase != phaseMigrationRunning || current.ControllerTaskID != "" {
				return false, nil
			}
			current.Phase = phaseMigrationPending
			current.MigrationStarted = ""
			current.MigrationFailure = ""
			return true, nil
		})
		if err != nil {
			return state{}, err
		}
		return newStore(stateDir()).read()
	}
	controller, found, lookupErr := oneTask(ctx, value.ControllerTaskID)
	if lookupErr != nil {
		return value, lookupErr
	}
	inactive := !found
	if found {
		lifecycle, eventTime, known, lifecycleErr := latestTaskLifecycle(controller.RolloutPath)
		if lifecycleErr != nil {
			return value, lifecycleErr
		}
		inactive = known && (lifecycle == "task_complete" || lifecycle == "turn_aborted")
		if inactive && value.MigrationStarted != "" {
			started, startErr := time.Parse(time.RFC3339Nano, value.MigrationStarted)
			ended, endErr := time.Parse(time.RFC3339Nano, eventTime)
			inactive = startErr == nil && endErr == nil && !ended.Before(started)
		}
	}
	if !inactive {
		return value, nil
	}
	err = newStore(stateDir()).update(func(current *state) (bool, error) {
		if current.Phase != phaseMigrationRunning || current.ControllerTaskID != value.ControllerTaskID {
			return false, nil
		}
		current.Phase = phaseMigrationFailed
		current.MigrationStarted = ""
		current.MigrationFailure = "controller stopped before migration completed"
		return true, nil
	})
	if err != nil {
		return state{}, err
	}
	return newStore(stateDir()).read()
}
func latestTaskLifecycle(path string) (string, string, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", "", false, err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64<<10), 8<<20)
	latest, timestamp := "", ""
	for scanner.Scan() {
		var item struct {
			Timestamp string `json:"timestamp"`
			Type      string `json:"type"`
			Payload   struct {
				Type string `json:"type"`
			} `json:"payload"`
		}
		if json.Unmarshal(scanner.Bytes(), &item) == nil && item.Type == "event_msg" &&
			(item.Payload.Type == "task_started" || item.Payload.Type == "task_complete" || item.Payload.Type == "turn_aborted") {
			latest, timestamp = item.Payload.Type, item.Timestamp
		}
	}
	if err := scanner.Err(); err != nil {
		return "", "", false, err
	}
	return latest, timestamp, latest != "", nil
}
