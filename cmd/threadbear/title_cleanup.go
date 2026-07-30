package main

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/ericlitman/threadbear/internal/codex"
	"github.com/ericlitman/threadbear/internal/state"
	"github.com/ericlitman/threadbear/internal/title"
)

type titleCleanupInventory interface {
	Inventory(context.Context, string) (codex.Inventory, error)
}

type titleCleanupClient interface {
	SetTitle(context.Context, string, string) error
	Close() error
}

const (
	titleCleanupSettleTimeout  = 5 * time.Second
	titleCleanupSettleInterval = 50 * time.Millisecond
)

type activeTitleCleaner struct {
	inventory titleCleanupInventory
	open      func(context.Context) (titleCleanupClient, error)
	now       func() time.Time
	wait      func(context.Context, time.Duration) error
}

type activeTitleOperation struct {
	taskID           string
	expectedRevision string
	expectedTitle    string
	desiredTitle     string
}

func (c activeTitleCleaner) CleanActiveTitles(ctx context.Context, controlTaskID string, committed state.State) (cleaned int, err error) {
	if c.inventory == nil || c.open == nil {
		return 0, errors.New("title cleanup dependencies are required")
	}
	observed, err := c.inventory.Inventory(ctx, controlTaskID)
	if err != nil {
		return 0, fmt.Errorf("inventory active managed tasks: %w", err)
	}
	operations := make([]activeTitleOperation, 0)
	for _, task := range observed.Tasks {
		record, managed := committed.Tasks[task.TaskID]
		if !managed || task.Archived || task.TaskID == controlTaskID {
			continue
		}
		record = titleCleanupRecord(record, committed.PendingTitlePlans[task.TaskID], task)
		desired, changed := title.Cleanup(record, task.Title)
		if !changed {
			continue
		}
		operations = append(operations, activeTitleOperation{
			taskID: task.TaskID, expectedRevision: task.Revision,
			expectedTitle: task.Title, desiredTitle: desired,
		})
	}
	sort.Slice(operations, func(i, j int) bool { return operations[i].taskID < operations[j].taskID })
	if len(operations) == 0 {
		return 0, nil
	}
	client, err := c.open(ctx)
	if err != nil {
		return 0, fmt.Errorf("open App Server for title cleanup: %w", err)
	}
	defer func() {
		if closeErr := client.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close App Server after cleaning %d title(s): %w", cleaned, closeErr))
		}
	}()
	for _, operation := range operations {
		current, readErr := c.inventory.Inventory(ctx, controlTaskID)
		if readErr != nil {
			return cleaned, fmt.Errorf("after cleaning %d title(s), revalidate task %s: %w", cleaned, operation.taskID, readErr)
		}
		row, found := cleanupTask(current, operation.taskID)
		if !found || row.Archived || row.Revision != operation.expectedRevision || row.Title != operation.expectedTitle {
			return cleaned, fmt.Errorf("after cleaning %d title(s), task %s revision or title drifted; rerun uninstall", cleaned, operation.taskID)
		}
		if writeErr := client.SetTitle(ctx, operation.taskID, operation.desiredTitle); writeErr != nil {
			return cleaned, fmt.Errorf("after cleaning %d title(s), write task %s title: %w", cleaned, operation.taskID, writeErr)
		}
		if settleErr := c.waitForTitle(ctx, controlTaskID, operation); settleErr != nil {
			return cleaned, fmt.Errorf("after cleaning %d title(s), %w", cleaned, settleErr)
		}
		cleaned++
	}
	return cleaned, nil
}

func (c activeTitleCleaner) waitForTitle(ctx context.Context, controlTaskID string, operation activeTitleOperation) error {
	now := c.now
	if now == nil {
		now = time.Now
	}
	wait := c.wait
	if wait == nil {
		wait = waitForTitleCleanup
	}
	settleCtx, cancel := context.WithTimeout(ctx, titleCleanupSettleTimeout)
	defer cancel()
	deadline := now().Add(titleCleanupSettleTimeout)
	for {
		if err := settleCtx.Err(); err != nil {
			return fmt.Errorf("verify task %s title: %w", operation.taskID, err)
		}
		verified, err := c.inventory.Inventory(settleCtx, controlTaskID)
		if err != nil {
			return fmt.Errorf("verify task %s title: %w", operation.taskID, err)
		}
		if err := settleCtx.Err(); err != nil {
			return fmt.Errorf("verify task %s title: %w", operation.taskID, err)
		}
		row, found := cleanupTask(verified, operation.taskID)
		if found && !row.Archived && (row.Title == operation.desiredTitle || row.Title == title.PersistedTitle(operation.desiredTitle)) {
			return nil
		}
		if !found || row.Archived || row.Title != operation.expectedTitle {
			return fmt.Errorf("task %s title was not visible after application; rerun uninstall", operation.taskID)
		}
		remaining := deadline.Sub(now())
		if remaining <= 0 {
			return fmt.Errorf("task %s title was not visible after application within %s; rerun uninstall", operation.taskID, titleCleanupSettleTimeout)
		}
		if err := wait(settleCtx, min(titleCleanupSettleInterval, remaining)); err != nil {
			return fmt.Errorf("verify task %s title: %w", operation.taskID, err)
		}
	}
}

func waitForTitleCleanup(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func titleCleanupRecord(record state.TaskRecord, plan state.PendingTitlePlan, task codex.Task) state.TaskRecord {
	if plan.TaskID == "" || task.Title != title.PersistedTitle(plan.DesiredTitle) {
		return record
	}
	if plan.ExpectedTitle == plan.DesiredTitle && plan.NativeOutcome != state.NativeTitleSucceeded {
		return record
	}
	record.LastAppliedTitle = plan.DesiredTitle
	record.DurableSubject = plan.DurableSubject
	record.ManagedAction = plan.ManagedAction
	record.ManagedTokenDisplay = plan.ManagedTokenDisplay
	record.ManagedTokenPosition = plan.ManagedTokenPosition
	return record
}

func cleanupTask(inventory codex.Inventory, taskID string) (codex.Task, bool) {
	for _, task := range inventory.Tasks {
		if task.TaskID == taskID {
			return task, true
		}
	}
	return codex.Task{}, false
}
