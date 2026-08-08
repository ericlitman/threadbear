package main

import (
	"context"
	"errors"
	"fmt"
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
	Ready           bool     `json:"ready"`
	TaskID          string   `json:"task_id"`
	Status          string   `json:"status"`
	Icon            string   `json:"icon"`
	OwnedPrefixes   []string `json:"owned_prefixes"`
	BlockedPrefixes []string `json:"blocked_prefixes"`
	InternalMarkers []string `json:"internal_markers"`
	MaxTitleUnits   int      `json:"max_title_units"`
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

func runCurrentTitle(_ context.Context, taskID, status string) (currentTitleResult, error) {
	result := currentTitleResult{TaskID: taskID, Status: status}
	icon, ok := statusIcons[status]
	if !ok {
		return result, fmt.Errorf("unsupported ThreadBear status %q", status)
	}
	if !taskIDPattern.MatchString(taskID) {
		return result, errors.New("CODEX_THREAD_ID is unavailable or invalid")
	}
	result.Ready = true
	result.Icon = icon
	result.OwnedPrefixes = append([]string(nil), ownedTitlePrefixes...)
	result.BlockedPrefixes = append([]string(nil), blockedTitlePrefixes...)
	result.InternalMarkers = append([]string(nil), internalEnvelopeMarkers...)
	result.MaxTitleUnits = maxTitleUnits
	return result, nil
}

func runOnboarding(ctx context.Context, apply bool, activeTaskID string) (onboardingResult, error) {
	if apply && !taskIDPattern.MatchString(activeTaskID) {
		return onboardingResult{}, errors.New("CODEX_THREAD_ID is unavailable or invalid")
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
	for index := range items {
		item := &items[index]
		if !item.Safe || item.TaskID == activeTaskID || item.Outcome != onboardingNeedsUpdate {
			continue
		}
		item.Outcome = onboardingPrepared
		item.Reason = "app-native title write required"
	}
	client.close()
	result = summarizeOnboarding(items, false)
	result.Ready, result.PlanComplete = true, true
	return result, nil
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
	for _, task := range tasks {
		item := onboardingItem{TaskID: task.ID, Outcome: onboardingSkipped}
		if task.RawFallback {
			item.Reason = "native task name is blank; task is raw or unowned"
			items = append(items, item)
			continue
		}
		subject, decorated, subjectErr := subjectFromTitle(task.Title)
		if subjectErr != nil {
			item.Reason = subjectErr.Error()
			items = append(items, item)
			continue
		}
		if decorated {
			item.Title, item.Subject, item.DesiredTitle = task.Title, subject, task.Title
			item.Safe, item.Outcome, item.Reason = true, onboardingUnchanged, "already decorated"
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
