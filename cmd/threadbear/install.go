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
func install(controlTaskID string, dry, confirmed bool) (any, error) {
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
		return map[string]any{"ready": true, "dry_run": true, "main_task_id": mainTaskID, "phase": value.Phase, "controller_task_id": value.ControllerTaskID, "controller_required": value.Phase != phaseMigrationComplete && value.ControllerTaskID == ""}, nil
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
			saved.Phase, changed = phaseMigrationRunning, true
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
	return map[string]any{"ready": err == nil && value.Phase == phaseMigrationComplete, "installed": err == nil, "main_task_id": value.MainTaskID, "controller_task_id": value.ControllerTaskID, "phase": value.Phase, "controller_required": err == nil && value.Phase == phaseMigrationRunning && value.ControllerTaskID == ""}, err
}
func uninstall(ctx context.Context, confirmed bool) (any, error) {
	if !confirmed {
		return nil, errors.New("uninstall requires --noninteractive --confirm")
	}
	value, err := currentStateOrEmpty()
	if err != nil {
		return nil, err
	}
	if value.Phase == phaseMigrationRunning {
		return nil, errors.New("cannot uninstall while installation migration is running; stop the controller first")
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
		err = removeFiles(p.binary, p.skill)
	}
	if err == nil {
		err = os.RemoveAll(stateDir())
	}
	return map[string]any{"ready": err == nil, "uninstalled": err == nil}, err
}
func status() (any, error) {
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
	value, stateErr := newStore(stateDir()).read()
	err = errors.Join(err, validateFile(p.skill, assets.SkillManagedContent), validateFile(p.agents, managedBlock()), stateErr)
	return map[string]any{"ready": err == nil && value.Phase == phaseMigrationComplete && value.MainTaskID != "", "installed": err == nil, "version": version, "phase": value.Phase, "main_task_id": value.MainTaskID, "controller_task_id": value.ControllerTaskID}, err
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
	return strings.Join([]string{blockStart, strings.TrimSpace(assets.AgentsManagedContent), blockEnd}, "\n")
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
