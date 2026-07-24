package app

import (
	"context"
	"errors"
	"time"

	"github.com/ericlitman/threadbear/internal/codex"
	"github.com/ericlitman/threadbear/internal/output"
	"github.com/ericlitman/threadbear/internal/state"
)

func RestoreHandler(store OperatorStore, inventory OperatorInventory, unarchiver Unarchiver, clock OperatorClock) Handler {
	return func(ctx context.Context, request Request) (output.Result, error) {
		if request.Command != CommandRestore || request.TaskID == "" {
			return commandError("restore", "invalid_request", ErrInvalidRequest)
		}
		if store == nil || inventory == nil || clock == nil {
			return commandError("restore", "dependency_unavailable", ErrUnavailable)
		}
		lock, err := store.AcquireLock()
		if err != nil {
			return commandError("restore", "restore_locked", err)
		}
		defer lock.Close()
		cfg, err := store.LoadConfig()
		if err != nil {
			return commandError("restore", "config_read_failed", err)
		}
		committed, err := store.LoadState()
		if err != nil {
			return commandError("restore", "state_read_failed", err)
		}
		cycle, cycleExists, err := loadOperatorCycleForGeneration(store, committed.Generation)
		if err != nil {
			return commandError("restore", "cycle_read_failed", err)
		}
		_, committedOwner := committed.Archives[request.TaskID]
		cycleOwner := false
		if cycleExists {
			cycleOwner = appliedArchiveOperation(cycle, request.TaskID)
			if committedOwner || cycleOwner && !exclusiveAppliedArchiveCycle(cycle, request.TaskID) {
				return commandError("restore", "pending_cycle", errors.New("heartbeat recovery must finish before restore"))
			}
		}
		if !committedOwner && !cycleOwner {
			return commandError("restore", "archive_not_owned", errors.New("task archive is not owned by ThreadBear"))
		}
		observed, err := inventory.Inventory(ctx, cfg.ControlTaskID)
		if err != nil {
			return commandError("restore", "inventory_failed", err)
		}
		current, unarchived := operatorTask(observed, request.TaskID)
		if !unarchived {
			if unarchiver == nil {
				return commandError("restore", "unarchive_unavailable", ErrUnavailable)
			}
			if err := unarchiver.Unarchive(ctx, request.TaskID); err != nil {
				observed, verifyErr := inventory.Inventory(ctx, cfg.ControlTaskID)
				if verifyErr != nil {
					return commandError("restore", "unarchive_failed", err)
				}
				current, unarchived = operatorTask(observed, request.TaskID)
				if !unarchived {
					return commandError("restore", "unarchive_failed", err)
				}
			} else {
				observed, err = inventory.Inventory(ctx, cfg.ControlTaskID)
				if err != nil {
					return commandError("restore", "restore_verify_failed", err)
				}
				current, unarchived = operatorTask(observed, request.TaskID)
				if !unarchived {
					return commandError("restore", "restore_verify_failed", errors.New("restored task is not visible"))
				}
			}
		}
		now := clock.Now().UTC()
		record, exists := committed.Tasks[request.TaskID]
		if !exists {
			record = restoredRecord(cycle, current, now)
		}
		record.TaskID = request.TaskID
		record.CapturedRevision = current.Revision
		record.CapturedTitle = current.Title
		record.LastSubstantiveActivity = now
		if record.StateStartedAt.IsZero() {
			record.StateStartedAt = now
		}
		record.Retry = nil
		committed.Tasks[request.TaskID] = record
		delete(committed.Archives, request.TaskID)
		committed.Generation++
		if err := store.SaveState(committed); err != nil {
			return commandError("restore", "state_write_failed", err)
		}
		if cycleExists && cycleOwner {
			for key, operation := range cycle.Operations {
				if operation.Kind == state.OperationArchive && (operation.Stage == state.StageApplied || operation.Stage == state.StageVerified) && operation.TaskID == request.TaskID {
					delete(cycle.Operations, key)
				}
			}
			captured := cycle.Inventory[request.TaskID]
			captured.TaskID = request.TaskID
			captured.Revision = current.Revision
			captured.Title = current.Title
			captured.Archived = false
			captured.LastSubstantiveActivity = now
			cycle.Inventory[request.TaskID] = captured
			if classification, ok := cycle.Results[request.TaskID]; ok && classification.Revision != current.Revision {
				delete(cycle.Results, request.TaskID)
			}
			delete(cycle.Diagnostics, request.TaskID)
			if err := store.SaveCycle(cycle); err != nil {
				return commandError("restore", "cycle_write_failed", err)
			}
		}
		return output.ActionResult{Command: "restore", Changed: true, ResourceIDs: []string{request.TaskID}}, nil
	}
}

func exclusiveAppliedArchiveCycle(cycle state.CycleCheckpoint, taskID string) bool {
	if len(cycle.Operations) != 1 || len(cycle.Inventory) != 1 {
		return false
	}
	if _, ok := cycle.Inventory[taskID]; !ok {
		return false
	}
	if len(cycle.Results) > 1 || len(cycle.Diagnostics) > 1 {
		return false
	}
	if _, ok := cycle.Results[taskID]; len(cycle.Results) == 1 && !ok {
		return false
	}
	if _, ok := cycle.Diagnostics[taskID]; len(cycle.Diagnostics) == 1 && !ok {
		return false
	}
	return appliedArchiveOperation(cycle, taskID)
}

func appliedArchiveOperation(cycle state.CycleCheckpoint, taskID string) bool {
	for _, operation := range cycle.Operations {
		if operation.Kind == state.OperationArchive && (operation.Stage == state.StageApplied || operation.Stage == state.StageVerified) && operation.TaskID == taskID {
			return true
		}
	}
	return false
}

func restoredRecord(cycle state.CycleCheckpoint, task codex.Task, now time.Time) state.TaskRecord {
	statusValue := state.StatusUnknown
	provenance := state.ProvenanceUnknown
	managedAction := ""
	durableSubject := ""
	if classification, ok := cycle.Results[task.TaskID]; ok && classification.Revision == task.Revision {
		statusValue = classification.Status
		provenance = classification.Provenance
		managedAction = classification.ManagedAction
		durableSubject = classification.DurableSubject
	}
	return state.TaskRecord{TaskID: task.TaskID, CapturedRevision: task.Revision, CapturedTitle: task.Title, Status: statusValue, Provenance: provenance, StateStartedAt: now, LastSubstantiveActivity: now, ManagedAction: managedAction, DurableSubject: durableSubject}
}
