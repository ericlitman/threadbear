package app

import (
	"context"
	"errors"
	"io/fs"
	"time"

	"github.com/ericlitman/threadbear/internal/codex"
	"github.com/ericlitman/threadbear/internal/output"
	"github.com/ericlitman/threadbear/internal/state"
)

func InspectHandler(store OperatorStore, inventory OperatorInventory, clock OperatorClock) Handler {
	return func(ctx context.Context, request Request) (output.Result, error) {
		if request.Command != CommandInspect || request.TaskID == "" {
			return commandError("inspect", "invalid_request", ErrInvalidRequest)
		}
		if store == nil || inventory == nil || clock == nil {
			return commandError("inspect", "dependency_unavailable", ErrUnavailable)
		}
		cfg, err := store.LoadConfig()
		if err != nil {
			return commandError("inspect", "config_read_failed", err)
		}
		committed, err := store.LoadState()
		if err != nil {
			return commandError("inspect", "state_read_failed", err)
		}
		cycle, cycleExists, err := loadOperatorCycleForGeneration(store, committed.Generation)
		if err != nil {
			return commandError("inspect", "cycle_read_failed", err)
		}
		observed, err := inventory.Inventory(ctx, cfg.ControlTaskID)
		if err != nil {
			return commandError("inspect", "inventory_failed", err)
		}
		current, currentExists := operatorTask(observed, request.TaskID)
		record, recordExists := committed.Tasks[request.TaskID]
		archive, archived := committed.Archives[request.TaskID]
		captured, capturedExists := cycle.Inventory[request.TaskID]
		classification, classified := cycle.Results[request.TaskID]
		if !currentExists && !recordExists && !archived && !capturedExists && !classified {
			return commandError("inspect", "task_not_found", fs.ErrNotExist)
		}

		revision := "unknown"
		statusValue := state.StatusUnknown
		provenance := state.ProvenanceUnknown
		managedAction := ""
		var retry *output.RetryResult
		recordMatchesCurrent := currentExists && recordExists && record.CapturedRevision == current.Revision && record.CapturedTitle == current.Title
		cycleMatchesCurrent := currentExists && cycleExists && capturedExists && classified && captured.Revision == current.Revision && captured.Title == current.Title && classification.Revision == current.Revision
		if currentExists {
			revision = current.Revision
		} else if archived {
			revision = archive.CapturedRevision
		} else if recordExists {
			revision = record.CapturedRevision
		}
		if recordExists && (!currentExists || recordMatchesCurrent) {
			statusValue = record.Status
			provenance = record.Provenance
			managedAction = record.ManagedAction
			if record.Retry != nil {
				retry = &output.RetryResult{TaskID: request.TaskID, Operation: record.Retry.Operation, ErrorCode: record.Retry.ErrorCode}
			}
		}
		if cycleMatchesCurrent {
			statusValue = classification.Status
			provenance = classification.Provenance
			managedAction = classification.ManagedAction
			if diagnostic, ok := cycle.Diagnostics[request.TaskID]; ok {
				retry = &output.RetryResult{TaskID: request.TaskID, Operation: diagnostic.Operation, ErrorCode: diagnostic.ErrorCode}
			}
		}
		eligible := recordMatchesCurrent && archiveEligibleForInspect(record, clock.Now().UTC(), cfg.ArchiveAfterDays) && cfg.ArchiveEnabled
		return output.InspectResult{
			TaskID:           request.TaskID,
			CapturedRevision: revision,
			State:            statusValue,
			Provenance:       provenance,
			ManagedAction:    managedAction,
			Retry:            retry,
			ArchiveEligible:  eligible,
		}, nil
	}
}

func loadOperatorCycle(store OperatorStore) (state.CycleCheckpoint, bool, error) {
	cycle, err := store.LoadCycle()
	if errors.Is(err, fs.ErrNotExist) {
		return state.CycleCheckpoint{}, false, nil
	}
	return cycle, err == nil, err
}

func loadOperatorCycleForGeneration(store OperatorStore, generation uint64) (state.CycleCheckpoint, bool, error) {
	cycle, exists, err := loadOperatorCycle(store)
	if err != nil || !exists {
		return cycle, exists, err
	}
	if generation > cycle.BaseGeneration {
		return state.CycleCheckpoint{}, false, nil
	}
	if generation < cycle.BaseGeneration {
		return state.CycleCheckpoint{}, false, errors.New("cycle generation is ahead of committed state")
	}
	return cycle, true, nil
}

func operatorTask(inventory codex.Inventory, taskID string) (codex.Task, bool) {
	for _, task := range inventory.Tasks {
		if task.TaskID == taskID {
			return task, true
		}
	}
	return codex.Task{}, false
}

func archiveEligibleForInspect(record state.TaskRecord, now time.Time, days int) bool {
	if record.Status != state.StatusComplete || days <= 0 {
		return false
	}
	start := record.LastSubstantiveActivity
	if record.StateStartedAt.After(start) {
		start = record.StateStartedAt
	}
	return !now.Before(start.Add(time.Duration(days) * 24 * time.Hour))
}
