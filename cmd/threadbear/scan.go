package main

import (
	"context"
	"errors"
	"fmt"
	"os"
)

const (
	onboardingNeedsUpdate = "needs_update"
	onboardingPrepared    = "prepared"
	onboardingUnchanged   = "unchanged"
	onboardingSkipped     = "skipped"
)

type indexedTask struct {
	ID          string `json:"task_id"`
	Title       string `json:"title"`
	RawFallback bool   `json:"-"`
}

type currentTitleResult struct {
	Ready         bool   `json:"ready"`
	TaskID        string `json:"task_id"`
	Status        string `json:"status"`
	PreviousTitle string `json:"previous_title"`
	DesiredTitle  string `json:"desired_title"`
	WriteRequired bool   `json:"write_required"`
	Unchanged     bool   `json:"unchanged"`
	Reason        string `json:"reason"`
}

type onboardingItem struct {
	TaskID       string `json:"task_id"`
	Title        string `json:"title,omitempty"`
	Subject      string `json:"subject,omitempty"`
	DesiredTitle string `json:"desired_title,omitempty"`
	Safe         bool   `json:"safe"`
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
	Prepared           int              `json:"prepared"`
	Unchanged          int              `json:"unchanged"`
	Skipped            int              `json:"skipped"`
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
			result.Reason = "native task name is blank; task is raw or unowned"
			return errors.New(result.Reason)
		}

		subject, err := persistSubjectUnderFence(disk, task.ID, task.Title)
		if err != nil {
			result.Reason = "subject could not be resolved or saved"
			return err
		}
		desired, err := renderTitle(status, subject)
		if err != nil {
			result.Reason = "desired title could not be rendered"
			return err
		}
		result.PreviousTitle, result.DesiredTitle = task.Title, desired
		result.WriteRequired = desired != task.Title
		result.Unchanged = !result.WriteRequired
		if result.WriteRequired {
			result.Reason = "app-native title write required"
		} else {
			result.Reason = "native title already matches desired title"
		}
		result.Ready = true
		return nil
	}()
	if err != nil {
		return result, err
	}

	// The exact native name was already read and the plan is complete. Process
	// exit cannot change that proof, so close and reap without promoting it into
	// another title observation.
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
	// The completed thread/list snapshot is the preparation authority. Subject
	// state is safe to pre-persist: the later app-native writer owns the immediate
	// title observation and a future planner adopts any safe intervening rename.
	for index := range items {
		item := &items[index]
		if !item.Safe || item.TaskID == activeTaskID || item.Outcome != onboardingNeedsUpdate {
			continue
		}
		targetErr := func() error {
			if _, err := persistSubjectUnderFence(disk, item.TaskID, item.Title); err != nil {
				item.Outcome = onboardingSkipped
				item.Reason = "subject state could not be saved"
				return fmt.Errorf("save subject state for %s: %w", item.TaskID, err)
			}
			item.Outcome = onboardingPrepared
			item.Reason = "app-native title write required"
			return nil
		}()
		if targetErr != nil {
			if item.Outcome != onboardingSkipped {
				item.Outcome = onboardingSkipped
				item.Reason = "the ThreadBear lifecycle changed before this task could be prepared"
			}
			operationErr = errors.Join(operationErr, targetErr)
		}
	}
	// Reap the one long-lived process after every admitted task has been
	// prepared. Exit is proof-neutral once the plan is complete.
	client.close()
	result = summarizeOnboarding(items, false)
	result.Ready, result.PlanComplete = operationErr == nil, true
	result.OnboardingComplete = result.OnboardingComplete && operationErr == nil
	return result, operationErr
}

func excludeActiveOnboardingTask(items []onboardingItem, activeTaskID string) {
	if activeTaskID == "" {
		return
	}
	for index := range items {
		if items[index].TaskID == activeTaskID {
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
			item.Safe, item.Outcome, item.Reason = true, onboardingUnchanged, "already decorated"
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
	for _, item := range items {
		if item.Safe {
			result.Safe++
			if item.Outcome == onboardingNeedsUpdate || item.Outcome == onboardingPrepared {
				result.NeedsUpdate++
			}
		}
		switch item.Outcome {
		case onboardingPrepared:
			result.Prepared++
		case onboardingUnchanged:
			result.Unchanged++
		case onboardingSkipped:
			result.Skipped++
		}
	}
	if readOnly {
		result.OnboardingComplete = result.NeedsUpdate == 0
	} else {
		result.OnboardingComplete = result.Prepared == 0
	}
	return result
}
