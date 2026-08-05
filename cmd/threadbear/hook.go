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

const titleTool, runningMarker, homeTitle, cleanupMarker, unknownMarker, controllerMarker, maxHookBytes = "codex_appset_thread_title", "⏳ ThreadBear is working", "🧵🐻 ThreadBear 🐻🧵", "🧵🐻 strip title icons", "❔ ThreadBear could not classify", "ThreadBear controller registration.", 1 << 20

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
	return title, cmp.Or(target, event.SessionID), errors.Join(titleErr, targetErr)
}
func hook(ctx context.Context, in io.Reader, out io.Writer) error {
	var event hookInput
	if err := readBoundedJSON(in, &event); err != nil {
		return err
	}
	if event.ToolName != titleTool {
		return nil
	}
	store := newStore(stateDir())
	titleLock, err := store.titleLock()
	if err != nil {
		return err
	}
	defer unlock(titleLock)
	if _, err = store.read(); err != nil && event.Event != "PreToolUse" {
		return err
	}
	switch event.Event {
	case "PreToolUse":
		if err == nil {
			err = preTitle(ctx, event, out)
		}
		if err != nil {
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
	raw, target, err := titleTarget(event)
	title, attempt, tagged := strings.Cut(raw, "⁣")
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
	} else if title == homeTitle {
		terminal, seed = true, homeTitle
	}
	if !terminal {
		if tagged {
			title, attempt = raw, ""
		}
		if title != homeTitle && (strings.HasPrefix(title, runningMarker) || strings.HasPrefix(title, "🧵🐻 ")) {
			return errors.New("invalid ThreadBear marker")
		}
		_, err = stageTitle(ctx, target, "", "", title, event.SessionID, event.ToolUseID, "")
		return err
	}
	proposed, err := stageTitle(ctx, target, result.Status, result.Action, seed, event.SessionID, event.ToolUseID, attempt)
	if err != nil {
		return err
	}
	event.ToolInput["title"], _ = json.Marshal(proposed)
	return json.NewEncoder(out).Encode(map[string]any{"hookSpecificOutput": map[string]any{
		"hookEventName": "PreToolUse", "permissionDecision": "allow", "updatedInput": event.ToolInput,
	}})
}
func stageTitle(ctx context.Context, id, status, action, seed, caller, toolUseID, attempt string) (string, error) {
	task, found, err := oneTask(ctx, id)
	if status == "" {
		known, ok, readErr := archiveTaskByID(ctx, id)
		task, found, err = indexedTask{Title: known.Title}, ok, readErr
	}
	if err != nil || !found {
		return "", errors.Join(err, errors.New("task is not active in Codex"))
	}
	first := strings.Join(strings.Fields(task.FirstMessage), " ")
	var proposed string
	err = newStore(stateDir()).update(func(saved *state) (bool, error) {
		if saved.Phase == phaseMigrationPending && saved.ControllerTaskID == "" && id == caller && status == "running" && strings.HasPrefix(first, "<codex_delegation> <source_thread_id>"+saved.MainTaskID+"</source_thread_id> <input>"+controllerMarker) {
			saved.ControllerTaskID, saved.Phase = caller, phaseMigrationRunning
		}
		if pending := saved.UninstallPending; saved.Phase == phaseMigrationFailed && pending == nil || pending != nil && (pending.InitiatorTaskID != caller || status != "cleanup") {
			return false, errors.New("title changes are paused for failed migration or prepared uninstall")
		}
		record := saved.Tasks[id]
		if record.Pending != nil {
			return false, errors.New("native title operation is already pending")
		}
		current := strings.Join(strings.Fields(task.Title), " ")
		subject := canonicalSubject(task.Title, record)
		if status == "" {
			subject = strings.Join(strings.Fields(seed), " ")
		}
		if record.Subject == "" && saved.Phase == phaseMigrationRunning && saved.ControllerTaskID == caller && caller != id {
			subject = stripStatusIcons(subject)
		}
		if status == "cleanup" {
			owner := saved.UninstallPending != nil && saved.UninstallPending.InitiatorTaskID == caller
			if saved.MainTaskID != caller && !owner {
				return false, errors.New("title cleanup requires the ThreadBear control task")
			}
			subject = cmp.Or(map[bool]string{true: record.Original}[task.Title == homeTitle], stripStatusIcons(task.Title), "Untitled task")
		} else if status != "" && task.Name == "" && first != "" && (current == first || current == truncateUTF16(first, 60)) {
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
		proposed = map[bool]string{true: seed, false: renderTitle(status, subject, action)}[status == ""]
		if status == "" && seed == homeTitle && attempt == "" {
			record.Original, record.Last = cmp.Or(record.Original, stripStatusIcons(current)), homeTitle
			saved.Tasks[id] = record
			return true, nil
		}
		record.Pending = &pendingProposal{CallerTaskID: caller, ToolUseID: toolUseID, BaseSubject: subject, Prior: task.Title, Proposed: proposed, Status: status, Action: action, Attempt: attempt}
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
		if pending.ToolUseID != event.ToolUseID || pending.CallerTaskID != "" && pending.CallerTaskID != event.SessionID || pending.Proposed != title {
			return false, errors.New("native title call does not match its proposal")
		}
		result, encoded := map[string]string{}, ""
		if json.Unmarshal(event.ToolResponse, &encoded) != nil || json.Unmarshal([]byte(encoded), &result) != nil || len(result) != 2 || result["threadId"] != target || result["title"] != title {
			return false, errors.New("native title result mismatch")
		}
		record.Subject, record.Last, record.Status, record.Action, record.Pending = pending.BaseSubject, pending.Proposed, pending.Status, pending.Action, nil
		saved.Tasks[target] = record
		return true, nil
	})
}
