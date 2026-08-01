package main

import (
	"context"
	"errors"
	"os"
)

func currentStateOrEmpty() (state, error) {
	value, err := newStore(stateDir()).read()
	if errors.Is(err, os.ErrNotExist) {
		return state{Format: stateFormat, Tasks: map[string]taskState{}}, nil
	}
	return value, err
}
func transitionMigration(ctx context.Context, phase, controllerID string) (any, error) {
	if phase != phaseMigrationRunning && phase != phaseMigrationComplete && phase != phaseMigrationFailed || controllerID == "" {
		return nil, errors.New("migration requires a valid phase and controller task ID")
	}
	var err error
	remaining := -1
	if phase == phaseMigrationComplete {
		_, remaining, _, err = migrationInventory(ctx)
		if err != nil {
			return nil, err
		}
		if remaining != 0 {
			return nil, errors.New("migration has unresolved tasks")
		}
	}
	err = newStore(stateDir()).update(func(value *state) (bool, error) {
		if value.MainTaskID == "" || controllerID == value.MainTaskID || value.ControllerTaskID != "" && value.ControllerTaskID != controllerID || phase != phaseMigrationRunning && value.ControllerTaskID != controllerID || value.Phase == phaseMigrationComplete && phase != phaseMigrationComplete {
			return false, errors.New("migration controller or main task changed")
		}
		value.ControllerTaskID, value.Phase = controllerID, phase
		return true, nil
	})
	if err != nil {
		return nil, err
	}
	result := map[string]any{"ready": true, "phase": phase}
	if remaining >= 0 {
		result["remaining"] = remaining
	}
	return result, nil
}
