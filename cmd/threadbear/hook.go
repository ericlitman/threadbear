package main

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

const titleTool, runningMarker, homeTitle, cleanupMarker = "codex_appset_thread_title", "⏳ ThreadBear is working", "🧵🐻 ThreadBear 🐻🧵", "🧵🐻 strip title icons"
const unknownMarker, maxHookBytes = "❔ ThreadBear could not classify", 1 << 20

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
	if len(data) > maxHookBytes {
		return errors.Join(err, errors.New("input exceeds 1 MiB"))
	}
	return errors.Join(err, json.Unmarshal(data, value))
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
	title, titleErr := stringField(event.ToolInput, "title", true)
	target, targetErr := stringField(event.ToolInput, "threadId", false)
	if target == "" {
		target = event.SessionID
	}
	return title, target, errors.Join(titleErr, targetErr)
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
	seed, seeded := strings.CutPrefix(title, runningMarker+": ")
	if seeded && (seed == "" || seed != strings.Join(strings.Fields(seed), " ") || seed != truncateUTF16(seed, 58)) {
		return errors.New("invalid running subject seed")
	}
	if seeded {
		result, terminal = footer{Status: "running"}, true
	} else if title == unknownMarker {
		result, terminal = footer{Status: "unknown"}, true
	} else if title == cleanupMarker {
		result, terminal = footer{Status: "cleanup"}, true
	}
	if !terminal {
		if title != homeTitle && (strings.HasPrefix(title, runningMarker) || strings.HasPrefix(title, "🧵🐻 ")) {
			return errors.New("invalid ThreadBear marker")
		}
		if value, stateErr := currentStateOrEmpty(); stateErr != nil || value.UninstallPending != nil {
			return errors.Join(stateErr, errors.New("title changes are paused for the prepared uninstall task"))
		}
		return nil
	}
	proposed, err := stageTitle(ctx, target, result.Status, result.Action, seed, event.SessionID, event.ToolUseID)
	if err != nil {
		return err
	}
	event.ToolInput["title"], _ = json.Marshal(proposed)
	return json.NewEncoder(out).Encode(map[string]any{"hookSpecificOutput": map[string]any{
		"hookEventName": "PreToolUse", "permissionDecision": "allow", "updatedInput": event.ToolInput,
	}})
}
func stageTitle(ctx context.Context, id, status, action, seed, caller, toolUseID string) (string, error) {
	store := newStore(stateDir())
	titleLock, err := store.titleLock()
	if err != nil {
		return "", err
	}
	defer unlock(titleLock)
	task, found, err := oneTask(ctx, id)
	if err != nil || !found {
		return "", errors.Join(err, errors.New("task is not active in Codex"))
	}
	var proposed string
	err = store.update(func(saved *state) (bool, error) {
		if pending := saved.UninstallPending; pending != nil && (pending.InitiatorTaskID != caller || status != "cleanup") {
			return false, errors.New("title changes are paused for the prepared uninstall task")
		}
		record := saved.Tasks[id]
		current, first := strings.Join(strings.Fields(task.Title), " "), strings.Join(strings.Fields(task.FirstMessage), " ")
		subject := canonicalSubject(task.Title, record)
		if record.Subject == "" && saved.Phase == phaseMigrationRunning && saved.ControllerTaskID == caller && caller != id {
			subject = stripStatusIcons(subject)
		}
		if status == "cleanup" {
			owner := saved.UninstallPending != nil && saved.UninstallPending.InitiatorTaskID == caller
			if saved.MainTaskID != caller && !owner {
				return false, errors.New("title cleanup requires the ThreadBear control task")
			}
			subject = cmp.Or(stripStatusIcons(task.Title), "Untitled task")
		} else if task.Name == "" && first != "" && (current == first || current == truncateUTF16(first, 60)) {
			subject = record.Subject
			if record.Pending != nil && record.Pending.BaseSubject != "" {
				subject = record.Pending.BaseSubject
			}
			if subject == "" && status == "running" {
				subject = seed
			}
			if subject == "" && saved.Phase == phaseMigrationRunning && saved.ControllerTaskID == caller && caller != id {
				subject = stripStatusIcons(current)
			}
			if subject == "" {
				return false, errors.New("fresh task has no subject owner")
			}
		}
		proposed = renderTitle(status, subject, action)
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
