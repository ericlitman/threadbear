package watch

import (
	"context"

	"github.com/ericlitman/threadbear/internal/codex"
	"github.com/ericlitman/threadbear/internal/state"
)

func findTask(inventory codex.Inventory, taskID string) (codex.Task, bool) {
	for _, task := range inventory.Tasks {
		if task.TaskID == taskID {
			return task, true
		}
	}
	return codex.Task{}, false
}

func revalidate(ctx context.Context, inventory InventoryReader, controlTaskID string, operation state.CycleOperation) (codex.Task, bool, error) {
	var task codex.Task
	var ok bool
	if operation.TaskID == controlTaskID {
		reader, available := inventory.(interface {
			Task(context.Context, string) (codex.Task, error)
		})
		if !available {
			return codex.Task{}, false, nil
		}
		current, err := reader.Task(ctx, controlTaskID)
		if err != nil {
			return codex.Task{}, false, err
		}
		task, ok = current, true
	} else {
		current, err := inventory.Inventory(ctx, controlTaskID)
		if err != nil {
			return codex.Task{}, false, err
		}
		task, ok = findTask(current, operation.TaskID)
	}
	if !ok || task.Archived || task.Revision != operation.ExpectedRevision || task.Title != operation.ExpectedTitle {
		return codex.Task{}, false, nil
	}
	return task, true, nil
}
