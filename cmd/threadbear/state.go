package main

import (
	"cmp"
	"encoding/json"
	"errors"
	"golang.org/x/sys/unix"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf16"
)

const stateFormat, phaseMigrationPending, phaseMigrationRunning, phaseMigrationComplete, phaseMigrationFailed = 4, "migration_pending", "migration_running", "migration_complete", "migration_failed"

type pendingProposal struct {
	CallerTaskID string `json:"caller_task_id"`
	ToolUseID    string `json:"tool_use_id"`
	BaseSubject  string `json:"base_subject"`
	Prior        string `json:"prior"`
	Proposed     string `json:"proposed"`
	Status       string `json:"status"`
	Action       string `json:"action,omitempty"`
	Attempt      string `json:"attempt,omitempty"`
}
type taskState struct {
	Subject         string           `json:"subject"`
	Original        string           `json:"original,omitempty"`
	Last            string           `json:"last,omitempty"`
	Status          string           `json:"status,omitempty"`
	Action          string           `json:"action,omitempty"`
	ArchiveActivity string           `json:"archive_activity,omitempty"`
	Pending         *pendingProposal `json:"pending,omitempty"`
}
type archiveOperation struct {
	TaskID   string `json:"task_id"`
	Action   string `json:"action"`
	Title    string `json:"title"`
	Activity string `json:"activity,omitempty"`
}
type uninstallOperation struct {
	InitiatorTaskID  string `json:"initiator_task_id"`
	MainTaskID       string `json:"main_task_id"`
	MainArchived     bool   `json:"main_archived"`
	ControllerTaskID string `json:"controller_task_id,omitempty"`
}
type state struct {
	Format           int                  `json:"format"`
	MainTaskID       string               `json:"main_task_id,omitempty"`
	ControllerTaskID string               `json:"controller_task_id,omitempty"`
	Phase            string               `json:"phase,omitempty"`
	MigrationStarted string               `json:"migration_started_at,omitempty"`
	MigrationFailure string               `json:"migration_failure,omitempty"`
	Tasks            map[string]taskState `json:"tasks"`
	Archives         map[string]bool      `json:"archives,omitempty"`
	ArchivePending   *archiveOperation    `json:"archive_pending,omitempty"`
	UninstallPending *uninstallOperation  `json:"uninstall_pending,omitempty"`
}
type footer struct{ Status, Action string }
type store struct{ dir string }

func newStore(dir string) store { return store{dir: dir} }
func (s store) path() string    { return filepath.Join(s.dir, "native.json") }
func (s store) openLock(name string, mode int, createDir bool) (*os.File, error) {
	if createDir {
		if err := os.MkdirAll(s.dir, 0o700); err != nil {
			return nil, err
		}
	}
	dir, err := unix.Open(s.dir, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer unix.Close(dir)
	if err = unix.Fchmod(dir, 0o700); err != nil {
		return nil, err
	}
	fd, err := unix.Openat(dir, name, unix.O_CREAT|unix.O_RDWR|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), name)
	info, statErr := f.Stat()
	if statErr != nil || !info.Mode().IsRegular() {
		return nil, errors.Join(errors.New("ThreadBear lock is not a regular file"), statErr, f.Close())
	}
	if err = errors.Join(unix.Fchmod(fd, 0o600), unix.Flock(fd, mode)); err != nil {
		return nil, errors.Join(err, f.Close())
	}
	return f, nil
}
func (s store) lock() (*os.File, error) { return s.openLock("native.lock", unix.LOCK_EX, true) }
func (s store) operationLock() (*os.File, error) {
	return s.openLock("operation.lock", unix.LOCK_EX, false)
}
func (s store) titleLock() (*os.File, error)   { return s.openLock("title.lock", unix.LOCK_EX, false) }
func (s store) installLock() (*os.File, error) { return s.openLock("title.lock", unix.LOCK_EX, true) }
func unlock(lock *os.File)                     { _ = unix.Flock(int(lock.Fd()), unix.LOCK_UN); _ = lock.Close() }
func (s store) read() (state, error) {
	fd, err := unix.Open(s.path(), unix.O_RDONLY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return state{}, err
	}
	f := os.NewFile(uintptr(fd), s.path())
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return state{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		return state{}, errors.New("ThreadBear state is not a private regular file")
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return state{}, err
	}
	var value state
	if err := json.Unmarshal(data, &value); err != nil || value.Format != stateFormat && value.Format != 3 || value.Tasks == nil || value.Phase != "" && value.Phase != phaseMigrationPending && value.Phase != phaseMigrationRunning && value.Phase != phaseMigrationComplete && value.Phase != phaseMigrationFailed {
		return state{}, errors.New("unsupported or corrupt ThreadBear state format")
	}
	return value, nil
}
func (s store) update(change func(*state) (bool, error)) (err error) {
	lock, err := s.lock()
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, unix.Flock(int(lock.Fd()), unix.LOCK_UN), lock.Close()) }()
	value, err := s.read()
	created := errors.Is(err, os.ErrNotExist)
	if created {
		value = state{Format: stateFormat, Tasks: map[string]taskState{}}
	} else if err != nil {
		return err
	}
	changed, err := change(&value)
	if err != nil || !created && !changed {
		return err
	}
	data, err := json.Marshal(value)
	if err == nil {
		err = writeAtomic(s.path(), append(data, '\n'), 0o600)
	}
	return err
}
func canonicalSubject(current string, previous taskState) string {
	if current == previous.Last && previous.Subject != "" {
		return previous.Subject
	}
	if pending := previous.Pending; pending != nil && pending.BaseSubject != "" && (current == pending.Prior || current == pending.Proposed) {
		return pending.BaseSubject
	}
	return strings.Join(strings.Fields(current), " ")
}
func parseFooter(message string) (footer, bool) {
	lines := strings.Split(strings.TrimRight(message, "\r\n"), "\n")
	line := lines[len(lines)-1]
	if strings.Contains(strings.Join(lines[:len(lines)-1], "\n"), "🧵🐻 ") {
		return footer{}, false
	}
	if line == "🧵🐻 complete" || line == "🧵🐻 automation" {
		return footer{Status: strings.TrimPrefix(line, "🧵🐻 ")}, true
	}
	remainder := strings.TrimPrefix(line, "🧵🐻 ")
	statusText, ownerAction, ok := strings.Cut(remainder, " (")
	owner, action, ownerOK := strings.Cut(ownerAction, "): ")
	status, statusOK := map[string]string{"next steps": "next_steps", "needs input": "needs_input", "blocked": "blocked"}[statusText]
	if remainder == line || !ok || !ownerOK || !statusOK || strings.TrimSpace(action) != action || len(strings.Fields(action)) < 1 {
		return footer{}, false
	}
	if status == "needs_input" && owner != "you" || status == "blocked" && owner != "external" || status == "next_steps" && owner != "you" && owner != "agent" && owner != "external" {
		return footer{}, false
	}
	return footer{Status: status, Action: action}, true
}

var statusIcons, statusPrefix = map[string]string{"": " ", "running": "⏳", "blocked": "🚨", "needs_input": "🙋", "automation": "🤖", "next_steps": "➡️", "complete": "✅", "unknown": "❔", "cleanup": " "}, regexp.MustCompile(`^(?:(?:⏳|🚨|🙋|🤖|➡️?|✅|❔) *)+`)

func stripStatusIcons(title string) string { return statusPrefix.ReplaceAllString(title, "") }
func renderTitle(status, subject, action string) string {
	icon := cmp.Or(statusIcons[status], statusIcons["unknown"])
	subject, action = strings.TrimSpace(subject), strings.TrimSpace(action)
	prefix := truncateUTF16(strings.TrimSpace(icon+" "+subject), 60)
	if budget := 60 - len(utf16.Encode([]rune(prefix+" → "))); action != "" && status != "complete" && status != "automation" && budget > 0 {
		return prefix + " → " + truncateUTF16(action, budget)
	}
	return prefix
}
func truncateUTF16(value string, limit int) string {
	units := utf16.Encode([]rune(value))
	if len(units) <= limit {
		return value
	}
	if limit < 1 {
		return ""
	}
	units = units[:limit-1]
	if len(units) > 0 && units[len(units)-1] >= 0xd800 && units[len(units)-1] <= 0xdbff {
		units = units[:len(units)-1]
	}
	return strings.TrimSpace(string(utf16.Decode(units))) + "…"
}
