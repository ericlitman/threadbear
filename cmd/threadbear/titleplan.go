package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"sort"
)

type titleReport struct {
	Reports []struct {
		OperationID string `json:"operation_id"`
		Outcome     string `json:"outcome"`
		ErrorCode   string `json:"error_code,omitempty"`
		TaskID      string `json:"task_id"`
		Title       string `json:"title"`
	} `json:"reports"`
}

func titlePlan(ctx context.Context, mode, operation string, input io.Reader) (any, error) {
	disk := newStore(stateDir())
	switch mode {
	case "stage":
		data, _ := io.ReadAll(io.LimitReader(input, 16*1024))
		result := parseFooter(string(data))
		if !result.Resolved {
			return nil, errors.New("stage input has no exact ThreadBear footer")
		}
		lock, err := disk.lock()
		if err != nil {
			return nil, err
		}
		defer unlock(lock)
		value, err := disk.load()
		if err != nil {
			return nil, err
		}
		if os.Getenv("CODEX_THREAD_ID") != value.ControlTaskID {
			return nil, errors.New("title plan must run in the retained control task")
		}
		task, found, err := oneTask(ctx, value.ControlTaskID)
		if err != nil || !found {
			return nil, errors.New("retained control task is not readable and active")
		}
		_, boundary, err := readEvidence(task.RolloutPath)
		if err != nil {
			return nil, err
		}
		turnID, err := latestTurnIDFile(task.RolloutPath)
		if err != nil {
			return nil, err
		}
		result.Evidence, result.Size = evidenceID("retained-footer", int64(len(data)), data), boundary
		old := value.Tasks[task.ID]
		record, pending := decision(task, old, result)
		pending.TurnID = turnID
		pending.ID = planID(pending)
		value.Tasks[task.ID] = record
		for id, existing := range value.Plans {
			if existing.TaskID == task.ID {
				delete(value.Plans, id)
			}
		}
		value.Plans[pending.ID] = pending
		if err := disk.save(value); err != nil {
			return nil, err
		}
		return map[string]any{"ready": true}, nil
	case "batch":
		value, err := disk.load()
		if err != nil {
			return nil, err
		}
		ids := make([]string, 0, len(value.Plans))
		for id := range value.Plans {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		if len(ids) > 8 {
			ids = ids[:8]
		}
		return map[string]any{"ready": true, "operation_ids": ids, "continuation_due": len(value.Plans) > len(ids)}, nil
	case "operation":
		value, err := disk.load()
		if err != nil {
			return nil, err
		}
		pending, ok := value.Plans[operation]
		if !ok {
			return nil, errors.New("unknown title operation")
		}
		task, found, err := oneTask(ctx, pending.TaskID)
		if err != nil || !found {
			return map[string]any{"ready": true, "disposition": "drifted"}, nil
		}
		record := value.Tasks[pending.TaskID]
		guarded := task.Revision == pending.Revision && sameEvidence(task, record)
		if pending.TaskID == value.ControlTaskID {
			turnID, readErr := latestTurnIDFile(task.RolloutPath)
			guarded = readErr == nil && pending.TurnID != "" && turnID == pending.TurnID
		}
		if task.Title != pending.ExpectedTitle && task.Title != pending.DesiredTitle || !guarded {
			return map[string]any{"ready": true, "disposition": "drifted"}, nil
		}
		return map[string]any{"ready": true, "disposition": "ready", "action": "set", "task_id": pending.TaskID, "desired_title": pending.DesiredTitle}, nil
	case "report":
		var report titleReport
		if err := json.NewDecoder(io.LimitReader(input, 64*1024)).Decode(&report); err != nil {
			return nil, err
		}
		accepted := 0
		lock, err := disk.lock()
		if err != nil {
			return nil, err
		}
		defer unlock(lock)
		value, err := disk.load()
		if err != nil {
			return nil, err
		}
		for _, item := range report.Reports {
			pending, ok := value.Plans[item.OperationID]
			if !ok || item.Outcome != "succeeded" || item.TaskID != pending.TaskID || item.Title != pending.DesiredTitle {
				continue
			}
			task, found, readErr := oneTask(ctx, pending.TaskID)
			if readErr != nil || !found || task.Title != pending.DesiredTitle {
				continue
			}
			record := value.Tasks[pending.TaskID]
			record.Title, record.LastApplied = pending.DesiredTitle, pending.DesiredTitle
			record.Status, record.Subject, record.Action = pending.Status, pending.Subject, pending.Action
			value.Tasks[pending.TaskID] = record
			delete(value.Plans, item.OperationID)
			if pending.TaskID == value.ControlTaskID && pending.Status == "complete" && len(value.Plans) == 0 {
				value.NativeBootstrap = false
			}
			accepted++
		}
		if err := disk.save(value); err != nil {
			return nil, err
		}
		return map[string]any{"ready": true, "accepted": accepted}, nil
	default:
		return nil, errors.New("title-plan mode is required")
	}
}
