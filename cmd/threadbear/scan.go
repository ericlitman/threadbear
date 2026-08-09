package main

import (
	"context"
	"errors"
	"fmt"
)

const (
	cleanupNeedsUpdate = "needs_cleanup"
	cleanupPrepared    = "prepared"
	cleanupUnchanged   = "unchanged"
	cleanupSkipped     = "skipped"
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

type cleanupItem struct {
	TaskID       string `json:"task_id"`
	Title        string `json:"title,omitempty"`
	DesiredTitle string `json:"desired_title,omitempty"`
	Outcome      string `json:"outcome"`
	Reason       string `json:"reason,omitempty"`
}

type cleanupResult struct {
	Ready        bool          `json:"ready"`
	PlanComplete bool          `json:"plan_complete"`
	ReadOnly     bool          `json:"read_only"`
	Total        int           `json:"total"`
	NeedsCleanup int           `json:"needs_cleanup"`
	Prepared     int           `json:"prepared"`
	Unchanged    int           `json:"unchanged"`
	Skipped      int           `json:"skipped"`
	Items        []cleanupItem `json:"items"`
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

func runTitleCleanup(ctx context.Context, prepare bool, activeTaskID string) (cleanupResult, error) {
	if prepare && !taskIDPattern.MatchString(activeTaskID) {
		return cleanupResult{}, errors.New("CODEX_THREAD_ID is unavailable or invalid")
	}
	budget := appServerListBudget
	if prepare {
		budget = appServerCleanupBudget
	}
	client, err := startAppServer(ctx, budget)
	if err != nil {
		return cleanupResult{}, err
	}
	defer client.abort()
	nextRequestID := 2
	tasks, err := client.inventory(&nextRequestID)
	if err != nil {
		return cleanupResult{}, err
	}
	items := prepareTitleCleanupItems(tasks)
	if prepare {
		moveActiveTaskLast(items, activeTaskID)
	}
	result := summarizeTitleCleanup(items, !prepare)
	result.Ready, result.PlanComplete = true, true
	if !prepare {
		client.close()
		return result, nil
	}
	for index := range items {
		item := &items[index]
		if item.Outcome != cleanupNeedsUpdate {
			continue
		}
		item.Outcome = cleanupPrepared
		item.Reason = "app-native title removal required"
	}
	client.close()
	result = summarizeTitleCleanup(items, false)
	result.Ready, result.PlanComplete = true, true
	return result, nil
}

func moveActiveTaskLast(items []cleanupItem, activeTaskID string) {
	if activeTaskID == "" {
		return
	}
	for index := range items {
		if items[index].TaskID == activeTaskID {
			active := items[index]
			copy(items[index:], items[index+1:])
			items[len(items)-1] = active
			return
		}
	}
}

func prepareTitleCleanupItems(tasks []indexedTask) []cleanupItem {
	items := make([]cleanupItem, 0, len(tasks))
	for _, task := range tasks {
		item := cleanupItem{TaskID: task.ID, Outcome: cleanupSkipped}
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
			item.Title, item.DesiredTitle = task.Title, subject
			item.Outcome = cleanupNeedsUpdate
			items = append(items, item)
			continue
		}
		item.Title, item.Outcome, item.Reason = task.Title, cleanupUnchanged, "no ThreadBear prefix"
		items = append(items, item)
	}
	return items
}

func summarizeTitleCleanup(items []cleanupItem, readOnly bool) cleanupResult {
	result := cleanupResult{ReadOnly: readOnly, Total: len(items), Items: items}
	for _, item := range items {
		if item.Outcome == cleanupNeedsUpdate || item.Outcome == cleanupPrepared {
			result.NeedsCleanup++
		}
		switch item.Outcome {
		case cleanupPrepared:
			result.Prepared++
		case cleanupUnchanged:
			result.Unchanged++
		case cleanupSkipped:
			result.Skipped++
		}
	}
	return result
}
