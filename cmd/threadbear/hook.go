package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

const (
	titleTool     = "codex_appset_thread_title"
	runningMarker = "⏳ ThreadBear is working"
	unknownMarker = "❔ ThreadBear could not classify"
	maxHookBytes  = 1 << 20
)

type hookInput struct {
	Event        string                     `json:"hook_event_name"`
	SessionID    string                     `json:"session_id"`
	ToolName     string                     `json:"tool_name"`
	ToolUseID    string                     `json:"tool_use_id"`
	ToolInput    map[string]json.RawMessage `json:"tool_input"`
	ToolResponse json.RawMessage            `json:"tool_response"`
}

func readBoundedJSON(r io.Reader, value any) error {
	data, err := io.ReadAll(io.LimitReader(r, maxHookBytes+1))
	if err != nil {
		return err
	}
	if len(data) > maxHookBytes {
		return errors.New("input exceeds 1 MiB")
	}
	return json.Unmarshal(data, value)
}
func stringField(values map[string]json.RawMessage, key string, required bool) (string, error) {
	raw, ok := values[key]
	if !ok && !required {
		return "", nil
	}
	var value string
	if !ok || strings.TrimSpace(string(raw)) == "null" || json.Unmarshal(raw, &value) != nil {
		return "", fmt.Errorf("tool input %s must be a string", key)
	}
	return value, nil
}
func titleTarget(event hookInput) (string, string, error) {
	title, err := stringField(event.ToolInput, "title", true)
	if err != nil {
		return "", "", err
	}
	target, err := stringField(event.ToolInput, "threadId", false)
	if target == "" {
		target = event.SessionID
	}
	return title, target, err
}
func hook(ctx context.Context, in io.Reader, out io.Writer) error {
	var event hookInput
	if err := readBoundedJSON(in, &event); err != nil {
		return err
	}
	if event.ToolName != titleTool {
		return nil
	}
	switch event.Event {
	case "PreToolUse":
		if err := preTitle(ctx, event, out); err != nil {
			return json.NewEncoder(out).Encode(map[string]any{"hookSpecificOutput": map[string]any{
				"hookEventName": "PreToolUse", "permissionDecision": "deny",
				"permissionDecisionReason": "ThreadBear could not safely prepare this title: " + err.Error(),
			}})
		}
		return nil
	case "PostToolUse":
		return postTitle(event)
	default:
		return fmt.Errorf("unsupported hook event %q", event.Event)
	}
}
func preTitle(ctx context.Context, event hookInput, out io.Writer) error {
	title, target, err := titleTarget(event)
	if err != nil {
		return err
	}
	result, terminal := parseFooter(title)
	if title == runningMarker {
		result, terminal = footer{Status: "running"}, true
	} else if title == unknownMarker {
		result, terminal = footer{Status: "unknown"}, true
	}
	if !terminal {
		if strings.HasPrefix(title, "🧵🐻 ") {
			return errors.New("invalid ThreadBear footer marker")
		}
		return nil
	}
	proposed, err := stageTitle(ctx, target, result.Status, result.Action, event.ToolUseID)
	if err != nil {
		return err
	}
	event.ToolInput["title"], _ = json.Marshal(proposed)
	return json.NewEncoder(out).Encode(map[string]any{"hookSpecificOutput": map[string]any{
		"hookEventName": "PreToolUse", "permissionDecision": "allow", "updatedInput": event.ToolInput,
	}})
}
func stageTitle(ctx context.Context, id, status, action, toolUseID string) (string, error) {
	task, found, err := oneTask(ctx, id)
	if err != nil {
		return "", err
	}
	if !found {
		return "", errors.New("task is not active in Codex")
	}
	var proposed string
	err = newStore(stateDir()).update(func(saved *state) (bool, error) {
		record := saved.Tasks[id]
		subject := canonicalSubject(task.Title, record)
		proposed = renderTitle(status, subject, action)
		record.Subject = subject
		record.Pending = &pendingProposal{ToolUseID: toolUseID, BaseSubject: subject, Prior: task.Title, Proposed: proposed, Status: status, Action: action}
		saved.Tasks[id] = record
		return true, nil
	})
	return proposed, err
}
func postTitle(event hookInput) error {
	title, target, err := titleTarget(event)
	if err != nil {
		return err
	}
	return newStore(stateDir()).update(func(saved *state) (bool, error) {
		record := saved.Tasks[target]
		if record.Pending == nil {
			return false, nil
		}
		pending := record.Pending
		if pending.ToolUseID != event.ToolUseID || pending.Proposed != title {
			return false, errors.New("native title call does not match its proposal")
		}
		var encoded string
		if json.Unmarshal(event.ToolResponse, &encoded) != nil {
			return false, errors.New("native title result is not JSON text")
		}
		var result map[string]string
		if json.Unmarshal([]byte(encoded), &result) != nil || len(result) != 2 || result["threadId"] != target || result["title"] != title {
			return false, errors.New("native title result mismatch")
		}
		record.Subject, record.Last, record.Status, record.Action, record.Pending = pending.BaseSubject, pending.Proposed, pending.Status, pending.Action, nil
		saved.Tasks[target] = record
		return true, nil
	})
}
