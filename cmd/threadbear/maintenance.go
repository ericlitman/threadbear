package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"sort"
	"time"
)

const maintenanceAutomationID = "threadbear-maintenance"

var maintenanceNow = time.Now

type archiveTask struct {
	ID, Title, RolloutPath  string
	Archived, Visible, User bool
}

func archiveTasks(ctx context.Context) ([]archiveTask, error) {
	db, err := openIndex()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `SELECT id, COALESCE(name,title,''), COALESCE(rollout_path,''), COALESCE(archived,0)<>0, COALESCE(preview,'')<>'', 1
		FROM threads WHERE source IN ('vscode','cli') AND COALESCE(thread_source,'') IN ('','user') ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tasks []archiveTask
	for rows.Next() {
		var task archiveTask
		if err := rows.Scan(&task.ID, &task.Title, &task.RolloutPath, &task.Archived, &task.Visible, &task.User); err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}
func archiveTaskByID(ctx context.Context, id string) (archiveTask, bool, error) {
	db, err := openIndex()
	if err != nil {
		return archiveTask{}, false, err
	}
	defer db.Close()
	var task archiveTask
	err = db.QueryRowContext(ctx, `SELECT id, COALESCE(name,title,''), COALESCE(rollout_path,''), COALESCE(archived,0)<>0,
		COALESCE(preview,'')<>'', COALESCE(source,'') IN ('vscode','cli') AND COALESCE(thread_source,'') IN ('','user')
		FROM threads WHERE id=?`, id).Scan(&task.ID, &task.Title, &task.RolloutPath, &task.Archived, &task.Visible, &task.User)
	if errors.Is(err, sql.ErrNoRows) {
		return archiveTask{}, false, nil
	}
	return task, err == nil, err
}
func archiveSnapshot(task archiveTask, value *state) (string, bool) {
	if !task.User {
		return "", false
	}
	record := value.Tasks[task.ID]
	footer, exact := rolloutFooter(task.RolloutPath)
	info, err := os.Stat(task.RolloutPath)
	if err != nil || !exact || footer.Status != "complete" || record.Pending != nil || record.Status != "complete" || record.Last != task.Title {
		return "", false
	}
	activity := info.ModTime().UTC()
	if restored, err := time.Parse(time.RFC3339Nano, record.ArchiveActivity); err == nil && restored.After(activity) {
		activity = restored
	}
	return activity.Format(time.RFC3339Nano), true
}
func archiveEligibility(task archiveTask, value *state, days int) (string, bool) {
	if task.Archived || !task.Visible || task.ID == value.MainTaskID || task.ID == value.ControllerTaskID {
		return "", false
	}
	activity, valid := archiveSnapshot(task, value)
	parsed, err := time.Parse(time.RFC3339Nano, activity)
	return activity, valid && err == nil && !parsed.After(maintenanceNow().UTC().AddDate(0, 0, -days))
}
func maintenance(ctx context.Context, archiveID, restoreID, cancelID string, days int) (any, error) {
	operationLock, err := newStore(stateDir()).operationLock()
	if err != nil {
		return nil, err
	}
	defer unlock(operationLock)
	actions := 0
	for _, id := range []string{archiveID, restoreID, cancelID} {
		if id != "" {
			actions++
		}
	}
	if days < 1 || actions > 1 {
		return nil, errors.New("maintenance requires positive archive days and at most one target action")
	}
	result := map[string]any{"ready": true, "automation_id": maintenanceAutomationID, "archive_after_days": days}
	err = newStore(stateDir()).update(func(value *state) (bool, error) {
		if value.MainTaskID == "" || value.Phase != phaseMigrationComplete || value.UninstallPending != nil {
			return false, errors.New("maintenance requires a completed installation with no prepared uninstall")
		}
		changed := false
		if value.Archives == nil {
			value.Archives, changed = map[string]bool{}, true
		}
		requestedAction, requestedID := "archive", archiveID
		if restoreID != "" {
			requestedAction, requestedID = "restore", restoreID
		}
		if pending := value.ArchivePending; pending != nil {
			task, found, err := archiveTaskByID(ctx, pending.TaskID)
			if err != nil {
				return changed, err
			}
			applied := found && pending.Action == "archive" && task.Archived || found && pending.Action == "restore" && !task.Archived
			if applied {
				if pending.Action == "archive" {
					activity, valid := archiveSnapshot(task, value)
					if !valid || task.Title != pending.Title || activity != pending.Activity {
						return changed, errors.New("applied native archive drifted; restore the task before cancelling the pending operation")
					}
					value.Archives[pending.TaskID] = true
				} else {
					delete(value.Archives, pending.TaskID)
					record := value.Tasks[pending.TaskID]
					record.ArchiveActivity = maintenanceNow().UTC().Format(time.RFC3339Nano)
					value.Tasks[pending.TaskID] = record
				}
				value.ArchivePending, changed = nil, true
				if requestedID == pending.TaskID && requestedAction == pending.Action {
					result["reconciled"], result["task_id"], result["action"] = true, pending.TaskID, pending.Action
					return changed, nil
				}
			} else {
				if cancelID != "" {
					unapplied := found && pending.Action == "archive" && !task.Archived || found && pending.Action == "restore" && task.Archived
					if cancelID != pending.TaskID || !unapplied {
						return changed, errors.New("cancel requires the exact known-unapplied pending task")
					}
					value.ArchivePending = nil
					result["cancelled"], result["task_id"], result["action"] = true, pending.TaskID, pending.Action
					return true, nil
				}
				if requestedID != pending.TaskID || requestedAction != pending.Action {
					result["pending"] = pending
					return changed, nil
				}
				if pending.Action == "archive" {
					activity, eligible := archiveEligibility(task, value, days)
					if !eligible || task.Title != pending.Title || activity != pending.Activity {
						return changed, errors.New("pending archive no longer matches an eligible task")
					}
				} else if !found || !task.Archived || !value.Archives[pending.TaskID] {
					return changed, errors.New("pending restore no longer matches an owned archive")
				}
				result["pending"], result["task_id"], result["action"] = true, pending.TaskID, pending.Action
				return changed, nil
			}
		}
		manuallyRestored := false
		for id := range value.Archives {
			task, found, err := archiveTaskByID(ctx, id)
			if err != nil {
				return changed, err
			}
			if found && !task.Archived {
				delete(value.Archives, id)
				record := value.Tasks[id]
				record.ArchiveActivity = maintenanceNow().UTC().Format(time.RFC3339Nano)
				value.Tasks[id] = record
				changed = true
				manuallyRestored = manuallyRestored || restoreID == id
			}
		}
		if manuallyRestored {
			result["reconciled"], result["task_id"], result["action"] = true, restoreID, "restore"
			return changed, nil
		}
		if cancelID != "" {
			return changed, errors.New("no pending native archive operation to cancel")
		}
		if restoreID != "" {
			task, found, err := archiveTaskByID(ctx, restoreID)
			if err != nil || !found || !task.Archived || !value.Archives[restoreID] {
				return changed, errors.Join(err, errors.New("restore requires a ThreadBear-owned archived task"))
			}
			value.ArchivePending = &archiveOperation{TaskID: restoreID, Action: "restore", Title: task.Title}
			result["task_id"], result["action"], result["pending"] = restoreID, "restore", true
			return true, nil
		}
		if archiveID != "" {
			task, found, err := archiveTaskByID(ctx, archiveID)
			activity, eligible := archiveEligibility(task, value, days)
			if err != nil || !found || !eligible {
				return changed, errors.Join(err, fmt.Errorf("task %q is not eligible for archive", archiveID))
			}
			value.ArchivePending = &archiveOperation{TaskID: archiveID, Action: "archive", Title: task.Title, Activity: activity}
			result["task_id"], result["action"], result["pending"] = archiveID, "archive", true
			return true, nil
		}
		tasks, err := archiveTasks(ctx)
		if err != nil {
			return changed, err
		}
		candidates := []map[string]string{}
		for _, task := range tasks {
			if activity, eligible := archiveEligibility(task, value, days); eligible {
				candidates = append(candidates, map[string]string{"task_id": task.ID, "inactive_since": activity})
			}
		}
		sort.Slice(candidates, func(i, j int) bool { return candidates[i]["task_id"] < candidates[j]["task_id"] })
		result["candidates"], result["owned_archives"] = candidates, len(value.Archives)
		return changed, nil
	})
	return result, err
}
