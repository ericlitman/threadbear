package main

import (
	"context"
	"errors"
	"fmt"
	"os"
)

const (
	onboardingNeedsUpdate = "needs_update"
	onboardingUpdated     = "updated"
	onboardingUnchanged   = "unchanged"
	onboardingSkipped     = "skipped"
	onboardingUnconfirmed = "unconfirmed"
)

type indexedTask struct {
	ID          string `json:"task_id"`
	Title       string `json:"title"`
	RawFallback bool   `json:"-"`
}

type currentTitleResult struct {
	Ready         bool   `json:"ready"`
	TaskID        string `json:"task_id,omitempty"`
	Status        string `json:"status,omitempty"`
	PreviousTitle string `json:"previous_title,omitempty"`
	DesiredTitle  string `json:"desired_title,omitempty"`
	Title         string `json:"title,omitempty"`
	Updated       bool   `json:"updated"`
	Unchanged     bool   `json:"unchanged"`
	Unconfirmed   bool   `json:"unconfirmed"`
	Reason        string `json:"reason,omitempty"`
}

type onboardingItem struct {
	TaskID       string `json:"task_id"`
	Title        string `json:"title,omitempty"`
	Subject      string `json:"subject,omitempty"`
	DesiredTitle string `json:"desired_title,omitempty"`
	Safe         bool   `json:"safe"`
	Applied      bool   `json:"applied"`
	Outcome      string `json:"outcome"`
	Reason       string `json:"reason,omitempty"`
}

type onboardingResult struct {
	Ready              bool             `json:"ready"`
	PlanComplete       bool             `json:"plan_complete"`
	ReadOnly           bool             `json:"read_only"`
	OnboardingComplete bool             `json:"onboarding_complete"`
	Total              int              `json:"total"`
	Safe               int              `json:"safe"`
	NeedsUpdate        int              `json:"needs_update"`
	Updated            int              `json:"updated"`
	Unchanged          int              `json:"unchanged"`
	Skipped            int              `json:"skipped"`
	Unconfirmed        int              `json:"unconfirmed"`
	Items              []onboardingItem `json:"items"`
}

func runCurrentTitle(ctx context.Context, taskID, status string) (currentTitleResult, error) {
	result := currentTitleResult{TaskID: taskID, Status: status}
	if !taskIDPattern.MatchString(taskID) {
		return result, errors.New("CODEX_THREAD_ID is unavailable or invalid")
	}
	if _, ok := statusIcons[status]; !ok {
		return result, fmt.Errorf("unsupported ThreadBear status %q", status)
	}

	disk := newStore(stateDir())
	fence, err := disk.lifecycleFence()
	if err != nil {
		return result, err
	}
	defer unlock(fence)
	client, err := startAppServer(ctx, appServerCurrentBudget)
	if err != nil {
		return result, err
	}
	defer client.abort()
	err = func() error {
		task, err := client.currentTask(2, taskID)
		if err != nil {
			return err
		}
		if task.RawFallback {
			return errors.New("native task name is blank; task is raw or unowned")
		}

		subject, err := persistSubjectUnderFence(disk, task.ID, task.Title)
		if err != nil {
			return err
		}
		desired, err := renderTitle(status, subject)
		if err != nil {
			return err
		}
		result.PreviousTitle, result.DesiredTitle = task.Title, desired

		attempted := desired != task.Title
		nextRequestID := 3
		if attempted {
			if err := client.setName(nextRequestID, taskID, desired); err != nil {
				result.Unconfirmed = true
				result.Reason = "the single native title write did not return a usable result"
				return err
			}
			nextRequestID++
		}
		readback, err := client.currentTask(nextRequestID, taskID)
		if err != nil {
			result.Unconfirmed = attempted
			result.Reason = "the native title could not be read back"
			return err
		}
		if readback.RawFallback || readback.Title != desired {
			result.Unconfirmed = attempted
			if attempted {
				result.Reason = "the single native title write was not confirmed by exact readback"
			} else {
				result.Reason = "the native title changed during verification"
			}
			return errors.New(result.Reason)
		}
		result.Ready, result.Title = true, readback.Title
		result.Updated, result.Unchanged = attempted, !attempted
		return nil
	}()
	if err != nil {
		return result, err
	}

	// Exact native readback is the success gate. A process exit error after that
	// point cannot make an already-observed title uncertain.
	client.close()
	return result, nil
}

func persistSubjectUnderFence(disk store, taskID, currentTitle string) (string, error) {
	var subject string
	err := disk.updateTaskUnderFence(taskID, func(record *taskState) (bool, error) {
		resolved, err := resolveSubject(currentTitle, *record)
		if err != nil {
			return false, err
		}
		subject = resolved
		changed := record.Subject != resolved
		*record = taskState{Subject: resolved}
		return changed, nil
	})
	return subject, err
}

func runOnboarding(ctx context.Context, apply bool, activeTaskID string) (onboardingResult, error) {
	if apply && !taskIDPattern.MatchString(activeTaskID) {
		return onboardingResult{}, errors.New("CODEX_THREAD_ID is unavailable or invalid")
	}
	disk := newStore(stateDir())
	var fence *os.File
	var err error
	if apply {
		fence, err = disk.lifecycleFence()
		if err != nil {
			return onboardingResult{}, err
		}
		defer unlock(fence)
	}
	budget := appServerListBudget
	if apply {
		budget = appServerOnboardingBudget
	}
	client, err := startAppServer(ctx, budget)
	if err != nil {
		return onboardingResult{}, err
	}
	defer client.abort()
	nextRequestID := 2
	tasks, err := client.inventory(&nextRequestID)
	if err != nil {
		return onboardingResult{}, err
	}
	items := prepareOnboardingItems(tasks)
	excludeActiveOnboardingTask(items, activeTaskID)
	result := summarizeOnboarding(items, !apply)
	result.Ready, result.PlanComplete = true, true
	if !apply {
		client.close()
		return result, nil
	}

	var operationErr error
	// thread/read is only a drift check and exact readback for rows admitted by
	// the complete thread/list snapshot above. It never supplies a subject.
	for index := range items {
		item := &items[index]
		if !item.Safe {
			continue
		}
		if item.TaskID == activeTaskID {
			continue
		}
		targetErr := func() error {
			current, readErr := client.readTask(nextRequestID, item.TaskID)
			nextRequestID++
			if readErr != nil {
				item.Applied, item.Outcome = false, onboardingSkipped
				item.Reason = "native task name could not be revalidated"
				return nil
			}
			if current.RawFallback {
				item.Applied, item.Outcome = false, onboardingSkipped
				item.Reason = "native task name became blank during revalidation"
				return nil
			}
			if current.Title != item.Title {
				item.Applied, item.Outcome = false, onboardingSkipped
				item.Reason = "native task name changed after the onboarding snapshot"
				return nil
			}
			if item.DesiredTitle == current.Title {
				item.Applied, item.Outcome = true, onboardingUnchanged
				item.Reason = "already decorated"
				return nil
			}
			if _, err := persistSubjectUnderFence(disk, item.TaskID, current.Title); err != nil {
				item.Applied, item.Outcome = false, onboardingSkipped
				item.Reason = "subject state could not be saved"
				return fmt.Errorf("save subject state for %s: %w", item.TaskID, err)
			}
			if err := client.setName(nextRequestID, item.TaskID, item.DesiredTitle); err != nil {
				nextRequestID++
				item.Applied, item.Outcome = false, onboardingUnconfirmed
				item.Reason = "the single native title write did not return a usable result"
				return fmt.Errorf("set title for %s: %w", item.TaskID, err)
			}
			nextRequestID++
			readback, err := client.readTask(nextRequestID, item.TaskID)
			nextRequestID++
			if err != nil || readback.RawFallback || readback.Title != item.DesiredTitle {
				item.Applied, item.Outcome = false, onboardingUnconfirmed
				item.Reason = "the single native title write was not confirmed by exact readback"
				if err == nil {
					err = errors.New(item.Reason)
				}
				return fmt.Errorf("confirm title for %s: %w", item.TaskID, err)
			}
			item.Applied, item.Outcome, item.Reason = true, onboardingUpdated, ""
			return nil
		}()
		if targetErr != nil {
			if item.Outcome != onboardingUnconfirmed && item.Outcome != onboardingSkipped {
				item.Applied, item.Outcome = false, onboardingSkipped
				item.Reason = "the ThreadBear lifecycle changed before this task could be written"
			}
			operationErr = errors.Join(operationErr, targetErr)
		}
	}
	// As with the current-task path, per-task exact readback is authoritative.
	// Still reap the one long-lived process before returning the aggregate.
	client.close()
	result = summarizeOnboarding(items, false)
	result.Ready, result.PlanComplete = operationErr == nil, true
	return result, operationErr
}

func excludeActiveOnboardingTask(items []onboardingItem, activeTaskID string) {
	if activeTaskID == "" {
		return
	}
	for index := range items {
		if items[index].TaskID == activeTaskID {
			items[index].Applied = false
			items[index].Outcome = onboardingUnchanged
			items[index].Reason = "active task is handled by the terminal title writer"
			return
		}
	}
}

func prepareOnboardingItems(tasks []indexedTask) []onboardingItem {
	items := make([]onboardingItem, 0, len(tasks))
	disk := newStore(stateDir())
	for _, task := range tasks {
		item := onboardingItem{TaskID: task.ID, Outcome: onboardingSkipped}
		if task.RawFallback {
			item.Reason = "native task name is blank; task is raw or unowned"
			items = append(items, item)
			continue
		}
		record, readErr := disk.readTask(task.ID)
		if errors.Is(readErr, os.ErrNotExist) {
			record = taskState{}
		} else if readErr != nil {
			item.Reason = "subject state is unreadable"
			items = append(items, item)
			continue
		}
		if record.Subject != "" && isOwnedRendering(task.Title, record.Subject) {
			item.Title, item.Subject, item.DesiredTitle = task.Title, record.Subject, task.Title
			item.Safe, item.Applied, item.Outcome, item.Reason = true, true, onboardingUnchanged, "already decorated"
			items = append(items, item)
			continue
		}
		subject, subjectErr := resolveSubject(task.Title, record)
		if subjectErr != nil {
			item.Reason = subjectErr.Error()
			items = append(items, item)
			continue
		}
		item.Title, item.Subject, item.DesiredTitle = task.Title, subject, "🐻 "+subject
		item.Safe, item.Outcome = true, onboardingNeedsUpdate
		items = append(items, item)
	}
	return items
}

func summarizeOnboarding(items []onboardingItem, readOnly bool) onboardingResult {
	result := onboardingResult{ReadOnly: readOnly, Total: len(items), Items: items}
	allSafeConfirmed := true
	for _, item := range items {
		if item.Safe {
			result.Safe++
			if item.Outcome == onboardingNeedsUpdate {
				result.NeedsUpdate++
			}
			if item.Outcome != onboardingUpdated && item.Outcome != onboardingUnchanged {
				allSafeConfirmed = false
			}
		}
		switch item.Outcome {
		case onboardingUpdated:
			result.Updated++
		case onboardingUnchanged:
			result.Unchanged++
		case onboardingSkipped:
			result.Skipped++
		case onboardingUnconfirmed:
			result.Unconfirmed++
		}
	}
	if readOnly {
		result.OnboardingComplete = result.NeedsUpdate == 0
	} else {
		result.OnboardingComplete = allSafeConfirmed && result.Unconfirmed == 0
	}
	return result
}
