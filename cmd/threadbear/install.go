package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ericlitman/threadbear/assets"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
)

const blockStart, blockEnd = "<!-- BEGIN THREADBEAR MANAGED BLOCK -->", "<!-- END THREADBEAR MANAGED BLOCK -->"

type lifecyclePaths struct{ binary, agents, skill, hooks string }
type rawObject map[string]json.RawMessage

func codexHome() string {
	if value := os.Getenv("CODEX_HOME"); value != "" {
		return value
	}
	return filepath.Join(homeDir(), ".codex")
}
func homeDir() string  { home, _ := os.UserHomeDir(); return home }
func stateDir() string { return filepath.Join(homeDir(), ".local", "share", "threadbear") }
func installPaths() lifecyclePaths {
	return lifecyclePaths{filepath.Join(homeDir(), ".local/bin/threadbear"), filepath.Join(codexHome(), "AGENTS.md"), filepath.Join(codexHome(), "skills/threadbear/SKILL.md"), filepath.Join(codexHome(), "hooks.json")}
}
func install(controlTaskID string, dry, confirmed, debugCanaries bool) (any, error) {
	controlTaskID = strings.TrimSpace(controlTaskID)
	value, err := currentStateOrEmpty()
	if err != nil {
		return nil, err
	}
	mainTaskID := value.MainTaskID
	if mainTaskID != "" && controlTaskID != "" && controlTaskID != mainTaskID {
		return nil, fmt.Errorf("install would replace persisted ThreadBear task %q with %q", mainTaskID, controlTaskID)
	}
	if mainTaskID == "" {
		if controlTaskID == "" {
			return nil, errors.New("first install requires --control-task-id for the active ThreadBear task")
		}
		mainTaskID = controlTaskID
	}
	p := installPaths()
	hooks, write, err := editHooks(p.hooks, p.binary, true)
	if err != nil {
		return nil, err
	}
	if dry {
		phase := value.Phase
		if phase == "" {
			phase = phaseMigrationPending
		}
		result := map[string]any{"ready": true, "dry_run": true, "main_task_id": mainTaskID, "phase": phase, "controller_task_id": value.ControllerTaskID, "controller_required": phase == phaseMigrationPending}
		if debugCanaries {
			result["debug_canaries"] = true
		}
		return result, nil
	}
	if !confirmed {
		return nil, errors.New("install requires --noninteractive --confirm after its preview")
	}
	err = newStore(stateDir()).update(func(saved *state) (bool, error) {
		changed := saved.MainTaskID != mainTaskID
		if saved.MainTaskID != "" && saved.MainTaskID != mainTaskID {
			return false, errors.New("persisted ThreadBear task changed during install")
		}
		saved.MainTaskID = mainTaskID
		if saved.Phase == "" {
			saved.Phase, changed = phaseMigrationPending, true
		}
		value = *saved
		return changed, nil
	})
	if err != nil {
		return nil, err
	}
	source, err := os.Executable()
	if err != nil {
		return nil, err
	}
	binary, err := os.ReadFile(source)
	if err != nil {
		return nil, err
	}
	if err = writeAtomic(p.binary, binary, 0o755); err == nil {
		err = manageBlock(p.agents, assets.AgentsManagedContent)
	}
	if err == nil {
		err = writeAtomic(p.skill, []byte(assets.SkillManagedContent), 0o600)
	}
	if err == nil && write {
		err = writeAtomic(p.hooks, hooks, 0o600)
	}
	result := map[string]any{"ready": err == nil && value.Phase == phaseMigrationComplete, "installed": err == nil, "main_task_id": value.MainTaskID, "controller_task_id": value.ControllerTaskID, "phase": value.Phase, "controller_required": err == nil && value.Phase == phaseMigrationPending}
	if debugCanaries {
		result["debug_canaries"] = true
	}
	return result, err
}
func prepareUninstall(ctx context.Context, initiatorTaskID string) (any, error) {
	if initiatorTaskID = strings.TrimSpace(initiatorTaskID); initiatorTaskID == "" {
		return nil, errors.New("uninstall prepare requires the active initiating task ID")
	}
	return withUninstallLocks(func() (any, error) {
		value, err := newStore(stateDir()).read()
		if err != nil {
			return nil, err
		}
		if value.Phase != phaseMigrationComplete || value.ArchivePending != nil {
			return nil, errors.New("uninstall requires a completed installation with no pending archive")
		}
		initiator, found, err := archiveTaskByID(ctx, initiatorTaskID)
		if err != nil || !found || !initiator.User || !initiator.Visible || initiator.Archived {
			return nil, errors.Join(err, errors.New("uninstall initiator is not an active user task in Codex"))
		}
		if pending := value.UninstallPending; pending != nil {
			if pending.InitiatorTaskID != initiatorTaskID || pending.MainTaskID != value.MainTaskID || pending.ControllerTaskID != value.ControllerTaskID {
				return nil, errors.New("uninstall is already owned by another task or installation identity")
			}
			reconciled, drifted, err := reconcileUninstallTitles(ctx)
			if err != nil {
				return nil, err
			}
			return uninstallPreparationResult(pending, true, reconciled, drifted), nil
		}
		for _, record := range value.Tasks {
			if record.Pending != nil {
				return nil, errors.New("cannot prepare uninstall while a native title operation is pending; reconcile it first")
			}
		}
		main, found, err := archiveTaskByID(ctx, value.MainTaskID)
		if err != nil || !found || !main.User {
			return nil, errors.Join(err, errors.New("persisted ThreadBear control task is not available in Codex"))
		}
		pending := &uninstallOperation{InitiatorTaskID: initiatorTaskID, MainTaskID: value.MainTaskID, MainArchived: main.Archived, ControllerTaskID: value.ControllerTaskID}
		err = newStore(stateDir()).update(func(saved *state) (bool, error) {
			saved.UninstallPending = pending
			return true, nil
		})
		if err != nil {
			return nil, err
		}
		return uninstallPreparationResult(pending, false, 0, 0), nil
	})
}
func uninstallPreparationResult(pending *uninstallOperation, resumed bool, reconciled, drifted int) any {
	return map[string]any{"ready": true, "prepared": true, "resumed": resumed, "reconciled_titles": reconciled, "drifted_titles": drifted, "initiator_task_id": pending.InitiatorTaskID, "main_task_id": pending.MainTaskID, "main_archived": pending.MainArchived, "controller_task_id": pending.ControllerTaskID}
}
func reconcileUninstallTitles(ctx context.Context) (count, drifted int, err error) {
	err = newStore(stateDir()).update(func(value *state) (bool, error) {
		for id, record := range value.Tasks {
			pending := record.Pending
			if pending == nil {
				continue
			}
			task, found, readErr := archiveTaskByID(ctx, id)
			if readErr != nil {
				return false, readErr
			}
			if !found || !task.User || !task.Visible || task.Title != pending.Proposed && task.Title != pending.Prior {
				drifted++
			} else {
				if task.Title == pending.Proposed {
					record.Subject, record.Last, record.Status, record.Action = pending.BaseSubject, pending.Proposed, pending.Status, pending.Action
				}
				count++
			}
			record.Pending = nil
			value.Tasks[id] = record
		}
		return count+drifted > 0, nil
	})
	return count, drifted, err
}
func completeUninstall(ctx context.Context, initiatorTaskID string, confirmed, abort bool) (any, error) {
	if !confirmed && !abort {
		return nil, errors.New("uninstall requires --noninteractive --confirm")
	}
	if initiatorTaskID = strings.TrimSpace(initiatorTaskID); initiatorTaskID == "" {
		return nil, errors.New("uninstall requires the initiating task ID")
	}
	if !abort {
		if committed, err := finishCommittedUninstall(); err != nil || committed {
			return map[string]any{"ready": err == nil, "uninstalled": err == nil}, err
		}
	}
	return withUninstallLocks(func() (any, error) {
		value, err := newStore(stateDir()).read()
		if err != nil {
			return nil, err
		}
		pending := value.UninstallPending
		if pending == nil || pending.InitiatorTaskID != initiatorTaskID || pending.MainTaskID != value.MainTaskID || pending.ControllerTaskID != value.ControllerTaskID {
			return nil, errors.New("uninstall commit requires the exact prepared initiating task")
		}
		main, found, err := archiveTaskByID(ctx, pending.MainTaskID)
		if err != nil || !found || !main.User {
			return nil, errors.Join(err, errors.New("persisted ThreadBear control task is not available in Codex"))
		}
		if main.Archived != pending.MainArchived {
			return nil, errors.New("control task archive state must be restored before uninstall completion")
		}
		if abort {
			err = newStore(stateDir()).update(func(saved *state) (bool, error) {
				for id, record := range saved.Tasks {
					record.Pending = nil
					saved.Tasks[id] = record
				}
				saved.UninstallPending = nil
				return true, nil
			})
			return map[string]any{"ready": err == nil, "aborted": err == nil, "main_archived": pending.MainArchived}, err
		}
		if stripStatusIcons(main.Title) != main.Title {
			return nil, errors.New("uninstall requires title cleanup from the ThreadBear control task")
		}
		return uninstallLocked(ctx, value)
	})
}
func withUninstallLocks(action func() (any, error)) (any, error) {
	store := newStore(stateDir())
	operationLock, err := store.operationLock()
	if err != nil {
		return nil, err
	}
	defer unlock(operationLock)
	titleLock, err := store.titleLock()
	if err != nil {
		return nil, err
	}
	defer unlock(titleLock)
	return action()
}
func finishCommittedUninstall() (bool, error) {
	p := installPaths()
	if _, err := os.Stat(newStore(stateDir()).path()); !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	_, skillErr := os.Stat(p.skill)
	agents, agentsErr := os.ReadFile(p.agents)
	_, hooksChanged, hooksErr := editHooks(p.hooks, p.binary, false)
	if !errors.Is(skillErr, os.ErrNotExist) || agentsErr == nil && (strings.Contains(string(agents), blockStart) || strings.Contains(string(agents), blockEnd) || strings.Contains(string(agents), strings.TrimSpace(assets.AgentsManagedContent))) || agentsErr != nil && !errors.Is(agentsErr, os.ErrNotExist) || hooksErr != nil || hooksChanged {
		return false, errors.Join(agentsErr, hooksErr, errors.New("uninstall state is missing before local artifacts were settled"))
	}
	if err := os.RemoveAll(stateDir()); err != nil {
		return false, err
	}
	return true, removeFiles(p.binary)
}
func uninstallLocked(ctx context.Context, value state) (any, error) {
	for _, record := range value.Tasks {
		if record.Pending != nil {
			return nil, errors.New("cannot uninstall while a native title operation is pending; reconcile it first")
		}
	}
	if value.MainTaskID != "" {
		tasks, scanErr := inventory(ctx)
		if scanErr != nil {
			return nil, scanErr
		}
		if slices.ContainsFunc(tasks, func(task indexedTask) bool { return stripStatusIcons(task.Title) != task.Title }) {
			return nil, errors.New("uninstall requires title cleanup from the ThreadBear control task")
		}
	}
	p := installPaths()
	hooks, write, err := editHooks(p.hooks, p.binary, false)
	if err != nil {
		return nil, err
	}
	err = validateFile(p.skill, assets.SkillManagedContent)
	if err == nil {
		err = manageBlock(p.agents, "")
	}
	if err == nil && write {
		if len(hooks) == 0 {
			err = os.Remove(p.hooks)
		} else {
			err = writeAtomic(p.hooks, hooks, 0o600)
		}
	}
	if err == nil {
		err = removeFiles(p.skill)
	}
	if err == nil {
		err = os.RemoveAll(stateDir())
	}
	if err == nil {
		err = removeFiles(p.binary)
	}
	return map[string]any{"ready": err == nil, "uninstalled": err == nil}, err
}
func status(ctx context.Context) (any, error) {
	p := installPaths()
	_, changed, err := editHooks(p.hooks, p.binary, true)
	if err == nil && changed {
		err = errors.New("native title hooks are incomplete")
	}
	for _, path := range []string{p.binary, p.agents, p.skill} {
		if err == nil {
			_, err = os.Stat(path)
		}
	}
	value, stateErr := reconcileMigration(ctx)
	err = errors.Join(err, validateFile(p.skill, assets.SkillManagedContent), validateFile(p.agents, managedBlock()), stateErr)
	result := map[string]any{"ready": err == nil && value.Phase == phaseMigrationComplete && value.MainTaskID != "", "installed": err == nil, "version": version, "phase": value.Phase, "main_task_id": value.MainTaskID, "controller_task_id": value.ControllerTaskID, "maintenance_automation_id": maintenanceAutomationID, "archive_pending": value.ArchivePending != nil, "uninstall_pending": value.UninstallPending != nil, "owned_archives": len(value.Archives)}
	if value.MigrationFailure != "" {
		result["migration_failure"] = value.MigrationFailure
		result["next_action"] = "resume migration from the ThreadBear task"
	} else if value.Phase == phaseMigrationPending {
		result["next_action"] = "start migration from the ThreadBear task"
	}
	return result, err
}
func selfTest() (any, error) {
	if runtime.GOOS != "darwin" || assets.AgentsManagedContent == "" || assets.SkillManagedContent == "" || version == "" {
		return nil, errors.New("candidate is incomplete or unsupported")
	}
	return map[string]any{"ready": true, "version": version}, nil
}
func editHooks(path, binary string, add bool) ([]byte, bool, error) {
	data, err := os.ReadFile(path)
	missing := errors.Is(err, os.ErrNotExist)
	if missing && !add {
		return nil, false, nil
	}
	if err != nil && !missing {
		return nil, false, err
	}
	root, events := rawObject{}, rawObject{}
	if !missing && (json.Unmarshal(data, &root) != nil || root == nil) {
		return nil, false, errors.New("hooks.json must contain an object")
	}
	if raw, ok := root["hooks"]; ok && (json.Unmarshal(raw, &events) != nil || events == nil) {
		return nil, false, errors.New("hooks.json hooks must be an object")
	}
	before := encodedJSON(decodedJSON(data))
	owner, removed := ownedHookJSON(binary), false
	for _, event := range []string{"PreToolUse", "PostToolUse"} {
		var groups []json.RawMessage
		if raw, ok := events[event]; ok && (json.Unmarshal(raw, &groups) != nil || groups == nil) {
			return nil, false, fmt.Errorf("hooks.json %s must be an array", event)
		}
		kept := slices.DeleteFunc(groups, func(group json.RawMessage) bool {
			owned := ownedHookGroup(group, binary)
			removed = removed || owned
			return owned
		})
		if add {
			kept = append(kept, owner)
		}
		if raw, _ := json.Marshal(kept); len(kept) == 0 {
			delete(events, event)
		} else {
			events[event] = raw
		}
	}
	if !add && !removed {
		return data, false, nil
	}
	if !add && len(events) == 0 {
		delete(root, "hooks")
	} else {
		root["hooks"], _ = json.Marshal(events)
	}
	if !add && len(root) == 0 {
		return nil, true, nil
	}
	updated, err := json.MarshalIndent(root, "", "  ")
	return append(updated, '\n'), string(before) != string(encodedJSON(decodedJSON(updated))), err
}
func encodedJSON(value any) json.RawMessage { data, _ := json.Marshal(value); return data }
func ownedHookJSON(binary string) json.RawMessage {
	return encodedJSON(map[string]any{"matcher": "codex_appset_thread_title", "hooks": []any{map[string]any{"type": "command", "command": quoteCommand(binary), "timeout": 1}}})
}
func ownedHookGroup(group json.RawMessage, binary string) bool {
	value, hooks := rawObject{}, []rawObject{}
	return json.Unmarshal(group, &value) == nil && json.Unmarshal(value["hooks"], &hooks) == nil && len(hooks) == 1 && string(value["matcher"]) == `"codex_appset_thread_title"` && string(hooks[0]["type"]) == `"command"` && string(hooks[0]["command"]) == string(encodedJSON(quoteCommand(binary)))
}
func decodedJSON(data []byte) any  { var value any; _ = json.Unmarshal(data, &value); return value }
func quoteCommand(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "' hook" }
func validateFile(path, content string) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	text := string(data)
	valid := text == content
	if strings.HasPrefix(content, blockStart) {
		valid = strings.Count(text, blockStart) == 1 && strings.Count(text, blockEnd) == 1 && strings.Contains(text, content)
	}
	if err == nil && !valid {
		return errors.New("managed file was modified: " + path)
	}
	return err
}
func managedBlock() string {
	return blockStart + "\n" + strings.TrimSpace(assets.AgentsManagedContent) + "\n" + blockEnd
}
func manageBlock(path, content string) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) && content == "" {
		return nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	text := string(data)
	start, end := strings.Index(text, blockStart), strings.Index(text, blockEnd)
	if strings.Count(text, blockStart) > 1 || strings.Count(text, blockEnd) > 1 || start < 0 != (end < 0) || end >= 0 && end < start {
		return errors.New("invalid ThreadBear managed block")
	}
	if content == "" && (start < 0 && strings.Contains(text, strings.TrimSpace(assets.AgentsManagedContent)) || start >= 0 && !strings.Contains(text, managedBlock())) {
		return errors.New("managed file was modified: " + path)
	}
	if content == "" && start < 0 {
		return nil
	}
	if content != "" {
		block := managedBlock()
		if start >= 0 {
			text = text[:start] + block + text[end+len(blockEnd):]
		} else {
			if text != "" && !strings.HasSuffix(text, "\n") {
				text += "\n"
			}
			text += block + "\n"
		}
	} else if start >= 0 {
		after := text[end+len(blockEnd):]
		if strings.HasSuffix(text[:start], "\n") && strings.HasPrefix(after, "\n") {
			after = after[1:]
		}
		text = text[:start] + after
	}
	if strings.TrimSpace(text) == "" {
		return os.Remove(path)
	}
	return writeAtomic(path, []byte(text), 0o600)
}
func writeAtomic(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".threadbear-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	err = f.Chmod(mode)
	if err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	if err = errors.Join(err, f.Close()); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
func removeFiles(paths ...string) error {
	for _, path := range paths {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}
