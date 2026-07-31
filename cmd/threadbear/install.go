package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"text/template"
	"time"

	"github.com/ericlitman/threadbear/assets"
)

const (
	launchLabel = "org.litman.threadbear"
	blockStart  = "<!-- BEGIN THREADBEAR MANAGED BLOCK -->"
	blockEnd    = "<!-- END THREADBEAR MANAGED BLOCK -->"
)

func codexHome() string {
	if value := os.Getenv("CODEX_HOME"); value != "" {
		return value
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".codex")
}

func stateDir() string {
	if value := os.Getenv("THREADBEAR_STATE_DIR"); value != "" {
		return value
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "threadbear")
}

func installPaths() (binary, agents, skill, plist string) {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "bin", "threadbear"),
		filepath.Join(codexHome(), "AGENTS.md"),
		filepath.Join(codexHome(), "skills", "threadbear", "SKILL.md"),
		filepath.Join(home, "Library", "LaunchAgents", launchLabel+".plist")
}

func install(ctx context.Context, control string, dry, confirmed bool) (any, error) {
	if control == "" {
		return nil, errors.New("install requires --control-task-id")
	}
	task, found, err := oneTask(ctx, control)
	if err != nil || !found {
		return nil, errors.New("control task must be an existing active Codex task")
	}
	binary, agents, skill, plist := installPaths()
	preview := []string{"adopt control task " + task.ID, "write " + binary, "manage " + agents, "manage " + skill, "schedule five-minute heartbeat"}
	if dry {
		return map[string]any{"ready": true, "dry_run": true, "effects": preview}, nil
	}
	if !confirmed {
		return nil, errors.New("install requires --noninteractive --confirm after its preview")
	}
	domain := fmt.Sprintf("gui/%d", os.Getuid())
	if err := bootout(ctx, domain); err != nil {
		return nil, err
	}
	codexPath, err := exec.LookPath("codex")
	if err != nil {
		return nil, errors.New("Codex executable is unavailable")
	}
	codexPath, _ = filepath.Abs(codexPath)
	source, err := os.Executable()
	if err != nil {
		return nil, err
	}
	if err := copyAtomic(source, binary, 0o755); err != nil {
		return nil, err
	}
	if err := writeManaged(agents, assets.AgentsManagedContent); err != nil {
		return nil, err
	}
	if err := writeManaged(skill, assets.SkillManagedContent); err != nil {
		return nil, err
	}
	disk := newStore(stateDir())
	lock, err := disk.lock()
	if err != nil {
		return nil, err
	}
	value := freshState(control)
	value.CodexPath = codexPath
	err = disk.save(value)
	unlock(lock)
	if err != nil {
		return nil, err
	}
	initial, err := heartbeat(ctx, false)
	if err != nil {
		return nil, fmt.Errorf("initial deterministic scan: %w", err)
	}
	home, _ := os.UserHomeDir()
	logDir := filepath.Join(stateDir(), "logs")
	_ = os.MkdirAll(logDir, 0o700)
	spec := map[string]any{
		"Label": launchLabel, "BinaryPath": binary, "StartInterval": 300, "Home": home, "CodexHome": codexHome(),
		"Path": filepath.Dir(codexPath) + ":/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin", "LCAll": "C",
		"StdoutPath": filepath.Join(logDir, "heartbeat.stdout.log"), "StderrPath": filepath.Join(logDir, "heartbeat.stderr.log"),
	}
	tmpl, err := template.New("plist").Funcs(template.FuncMap{"xml": html.EscapeString}).Parse(assets.LaunchAgentPlistTemplate)
	var rendered bytes.Buffer
	if err == nil {
		err = tmpl.Execute(&rendered, spec)
	}
	if err != nil {
		return nil, err
	}
	if err := writeAtomic(plist, rendered.Bytes(), 0o600); err != nil {
		return nil, err
	}
	if output, err := exec.CommandContext(ctx, "launchctl", "bootstrap", domain, plist).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("load LaunchAgent: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return map[string]any{"ready": true, "installed": true, "control_task_id": control, "initial_scan": initial.Stats, "effects": preview}, nil
}

func uninstall(ctx context.Context, confirmed bool) (any, error) {
	if !confirmed {
		return nil, errors.New("uninstall requires --noninteractive --confirm")
	}
	if !safeRemovalRoot(stateDir()) {
		return nil, errors.New("refusing unsafe ThreadBear state removal path")
	}
	binary, agents, skill, plist := installPaths()
	domain := fmt.Sprintf("gui/%d", os.Getuid())
	if err := bootout(ctx, domain); err != nil {
		return nil, err
	}
	disk := newStore(stateDir())
	lock, err := disk.lock()
	if err != nil {
		return nil, err
	}
	lockHeld := true
	defer func() {
		if lockHeld {
			_ = unlock(lock)
		}
	}()
	_, err = disk.load()
	if err != nil {
		return nil, errors.New("ThreadBear is not installed")
	}
	if err := removeManaged(agents); err != nil {
		return nil, err
	}
	if err := removeManaged(skill); err != nil {
		return nil, err
	}
	for _, path := range []string{plist, binary} {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	if err := unlock(lock); err != nil {
		return nil, err
	}
	lockHeld = false
	if err := os.RemoveAll(stateDir()); err != nil {
		return nil, err
	}
	return map[string]any{"ready": true, "uninstalled": true}, nil
}

func safeRemovalRoot(path string) bool {
	clean := filepath.Clean(path)
	home, _ := os.UserHomeDir()
	return filepath.IsAbs(clean) && clean != "/" && clean != filepath.Clean(home) &&
		clean != filepath.Clean(codexHome()) && len(strings.Split(strings.Trim(clean, string(filepath.Separator)), string(filepath.Separator))) >= 3
}

func writeManaged(path, content string) error {
	old, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	text := string(old)
	block := blockStart + "\n" + strings.TrimSpace(content) + "\n" + blockEnd
	if start := strings.Index(text, blockStart); start >= 0 {
		end := strings.Index(text[start:], blockEnd)
		if end < 0 {
			return errors.New("unterminated ThreadBear managed block")
		}
		text = text[:start] + block + text[start+end+len(blockEnd):]
	} else {
		if text != "" && !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		text += block + "\n"
	}
	return writeAtomic(path, []byte(text), 0o600)
}

func removeManaged(path string) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	text := string(data)
	start, end := strings.Index(text, blockStart), strings.Index(text, blockEnd)
	if start < 0 && end < 0 {
		return nil
	}
	if start < 0 || end < start {
		return errors.New("invalid ThreadBear managed block")
	}
	text = strings.TrimSpace(text[:start] + text[end+len(blockEnd):])
	if text == "" {
		return os.Remove(path)
	}
	return writeAtomic(path, []byte(text+"\n"), 0o600)
}

func copyAtomic(source, destination string, mode os.FileMode) error {
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	return writeAtomic(destination, data, mode)
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
	if err = f.Chmod(mode); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	err = errors.Join(err, f.Close())
	if err != nil {
		return err
	}
	return os.Rename(name, path)
}

func selfTest() (any, error) {
	if runtime.GOOS != "darwin" || assets.AgentsManagedContent == "" || assets.SkillManagedContent == "" || version == "" {
		return nil, errors.New("candidate is incomplete or unsupported")
	}
	return map[string]any{"ready": true, "version": version}, nil
}

func status() (any, error) {
	value, err := newStore(stateDir()).load()
	if err != nil {
		return nil, err
	}
	binary, _, _, plist := installPaths()
	for _, path := range []string{binary, plist} {
		info, statErr := os.Lstat(path)
		if statErr != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return nil, fmt.Errorf("installed runtime is incomplete: %s", path)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	domain := fmt.Sprintf("gui/%d", os.Getuid())
	if output, printErr := exec.CommandContext(ctx, "launchctl", "print", domain+"/"+launchLabel).CombinedOutput(); printErr != nil {
		return nil, fmt.Errorf("LaunchAgent is not healthy: %s", strings.TrimSpace(string(output)))
	}
	return map[string]any{"ready": true, "version": version, "control_task_id": value.ControlTaskID, "pending_titles": len(value.Plans), "last_scan": value.LastScan}, nil
}

func bootout(ctx context.Context, domain string) error {
	output, err := exec.CommandContext(ctx, "launchctl", "bootout", domain+"/"+launchLabel).CombinedOutput()
	if err == nil {
		return nil
	}
	message := strings.ToLower(string(output))
	if strings.Contains(message, "no such process") || strings.Contains(message, "could not find service") ||
		strings.Contains(message, "service not found") {
		return nil
	}
	return fmt.Errorf("stop LaunchAgent: %w: %s", err, strings.TrimSpace(string(output)))
}
