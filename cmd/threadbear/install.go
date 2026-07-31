package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ericlitman/threadbear/assets"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
)

const (
	launchLabel = "org.litman.threadbear"
	blockStart  = "<!-- BEGIN THREADBEAR MANAGED BLOCK -->"
	blockEnd    = "<!-- END THREADBEAR MANAGED BLOCK -->"
)

type lifecyclePaths struct{ binary, agents, skill, hooks, plist string }
type rawObject map[string]json.RawMessage

func codexHome() string {
	if value := os.Getenv("CODEX_HOME"); value != "" {
		return value
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".codex")
}
func stateDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "threadbear")
}
func installPaths() lifecyclePaths {
	home, _ := os.UserHomeDir()
	return lifecyclePaths{filepath.Join(home, ".local/bin/threadbear"), filepath.Join(codexHome(), "AGENTS.md"), filepath.Join(codexHome(), "skills/threadbear/SKILL.md"), filepath.Join(codexHome(), "hooks.json"), filepath.Join(home, "Library/LaunchAgents", launchLabel+".plist")}
}
func install(ctx context.Context, dry, confirmed bool) (any, error) {
	p := installPaths()
	hooks, write, err := editHooks(p.hooks, p.binary, true)
	if err != nil {
		return nil, err
	}
	if dry {
		return map[string]any{"ready": true, "dry_run": true}, nil
	}
	if !confirmed {
		return nil, errors.New("install requires --noninteractive --confirm after its preview")
	}
	legacy, err := readLegacyState(filepath.Join(stateDir(), "core.json"))
	if err != nil {
		return nil, err
	}
	if err = stopLegacyService(ctx); err != nil {
		return nil, err
	}
	err = newStore(stateDir()).update(func(value *state) (bool, error) {
		changed := false
		for id, task := range legacy {
			if old, ok := value.Tasks[id]; !ok || old.Subject == "" {
				value.Tasks[id] = task
				changed = true
			}
		}
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
	if err == nil {
		err = removeFiles(p.plist, filepath.Join(stateDir(), "core.json"), filepath.Join(stateDir(), "core.lock"))
	}
	return map[string]any{"ready": err == nil, "installed": err == nil}, err
}
func uninstall(ctx context.Context, confirmed bool) (any, error) {
	if !confirmed {
		return nil, errors.New("uninstall requires --noninteractive --confirm")
	}
	p := installPaths()
	hooks, write, err := editHooks(p.hooks, p.binary, false)
	if err != nil {
		return nil, err
	}
	if err = validateFile(p.skill, assets.SkillManagedContent); err == nil {
		err = stopLegacyService(ctx)
	}
	if err == nil {
		err = manageBlock(p.agents, "")
	}
	if err == nil && write && len(hooks) == 0 {
		err = os.Remove(p.hooks)
	} else if err == nil && write {
		err = writeAtomic(p.hooks, hooks, 0o600)
	}
	if err == nil {
		err = removeFiles(p.plist, p.binary, p.skill)
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
	if err == nil {
		err = validateFile(p.skill, assets.SkillManagedContent)
	}
	if err == nil {
		err = validateManagedBlock(p.agents)
	}
	if err == nil {
		_, err = newStore(stateDir()).read()
	}
	return map[string]any{"ready": err == nil, "version": version}, err
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
	owner := ownedHookJSON(binary)
	changed := missing
	for _, event := range []string{"PreToolUse", "PostToolUse"} {
		var groups []json.RawMessage
		if raw, ok := events[event]; ok && (json.Unmarshal(raw, &groups) != nil || groups == nil) {
			return nil, false, fmt.Errorf("hooks.json %s must be an array", event)
		}
		kept := slices.DeleteFunc(groups, func(group json.RawMessage) bool { return sameJSON(group, owner) })
		owners := len(groups) - len(kept)
		if add {
			kept = append(kept, owner)
		}
		changed = changed || owners != 1 && add || owners != 0 && !add
		if raw, _ := json.Marshal(kept); len(kept) == 0 {
			delete(events, event)
		} else {
			events[event] = raw
		}
	}
	if !changed {
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
	data, err = json.MarshalIndent(root, "", "  ")
	return append(data, '\n'), true, err
}
func ownedHookJSON(binary string) json.RawMessage {
	data, _ := json.Marshal(map[string]any{"matcher": "codex_appset_thread_title", "hooks": []any{map[string]any{"type": "command", "command": quoteCommand(binary), "timeout": 5}}})
	return data
}
func sameJSON(a, b []byte) bool {
	var left, right any
	if json.Unmarshal(a, &left) != nil || json.Unmarshal(b, &right) != nil {
		return false
	}
	return reflect.DeepEqual(left, right)
}
func quoteCommand(binary string) string {
	return "'" + strings.ReplaceAll(binary, "'", "'\"'\"'") + "' hook"
}
func readLegacyState(path string) (map[string]taskState, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var legacy struct {
		Tasks map[string]struct {
			Subject string `json:"subject"`
			Last    string `json:"last_applied"`
		} `json:"tasks"`
	}
	if err = json.Unmarshal(data, &legacy); err != nil {
		return nil, fmt.Errorf("read legacy state: %w", err)
	}
	result := map[string]taskState{}
	for id, task := range legacy.Tasks {
		if strings.TrimSpace(task.Subject) != "" {
			result[id] = taskState{Subject: task.Subject, Last: task.Last}
		}
	}
	return result, nil
}

var stopLegacyService = func(ctx context.Context) error {
	output, err := exec.CommandContext(ctx, "launchctl", "bootout", fmt.Sprintf("gui/%d/%s", os.Getuid(), launchLabel)).CombinedOutput()
	message := strings.ToLower(string(output))
	if err != nil && !strings.Contains(message, "no such process") && !strings.Contains(message, "could not find service") && !strings.Contains(message, "service not found") {
		return fmt.Errorf("stop legacy LaunchAgent: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func validateFile(path, content string) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	valid := string(data) == content
	if err == nil && !valid {
		return errors.New("managed file was modified: " + path)
	}
	return err
}
func validateManagedBlock(path string) error {
	data, err := os.ReadFile(path)
	text := string(data)
	valid := strings.Count(text, blockStart) == 1 && strings.Count(text, blockEnd) == 1 && strings.Contains(text, managedBlock())
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
	name := f.Name()
	defer os.Remove(name)
	err = f.Chmod(mode)
	if err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	err = errors.Join(err, f.Close())
	if err == nil {
		err = os.Rename(name, path)
	}
	return err
}
func removeFiles(paths ...string) error {
	for _, path := range paths {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}
