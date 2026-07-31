package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"golang.org/x/sys/unix"
)

const stateFormat = 1

type state struct {
	Format          int                   `json:"format"`
	ControlTaskID   string                `json:"control_task_id"`
	CodexPath       string                `json:"codex_path"`
	NativeBootstrap bool                  `json:"native_bootstrap"`
	Tasks           map[string]taskRecord `json:"tasks"`
	Plans           map[string]plan       `json:"plans"`
	LastScan        scanStats             `json:"last_scan"`
}

type taskRecord struct {
	Revision        int64  `json:"revision"`
	Title           string `json:"title"`
	RolloutPath     string `json:"rollout_path"`
	RolloutSize     int64  `json:"rollout_size"`
	Evidence        string `json:"evidence"`
	Status          string `json:"status"`
	Subject         string `json:"subject"`
	Action          string `json:"action,omitempty"`
	LastApplied     string `json:"last_applied,omitempty"`
	AmbiguousPasses int    `json:"ambiguous_passes,omitempty"`
}

type plan struct {
	ID            string `json:"id"`
	TaskID        string `json:"task_id"`
	Revision      int64  `json:"revision"`
	TurnID        string `json:"turn_id,omitempty"`
	ExpectedTitle string `json:"expected_title"`
	DesiredTitle  string `json:"desired_title"`
	Evidence      string `json:"evidence"`
	Status        string `json:"status"`
	Subject       string `json:"subject"`
	Action        string `json:"action,omitempty"`
	Epoch         int64  `json:"epoch"`
}

type scanStats struct {
	Tasks         int   `json:"tasks"`
	Changed       int   `json:"changed"`
	Deterministic int   `json:"deterministic"`
	Ambiguous     int   `json:"ambiguous"`
	Luna          int   `json:"luna"`
	Staged        int   `json:"staged"`
	ScanMillis    int64 `json:"scan_millis"`
}

func freshState(control string) state {
	return state{Format: stateFormat, ControlTaskID: control, NativeBootstrap: true, Tasks: map[string]taskRecord{}, Plans: map[string]plan{}}
}

type store struct{ dir string }

func newStore(dir string) store { return store{dir: dir} }
func (s store) path() string    { return filepath.Join(s.dir, "core.json") }

func (s store) lock() (*os.File, error) {
	if info, err := os.Lstat(s.dir); err == nil && (info.Mode()&os.ModeSymlink != 0 || !info.IsDir()) {
		return nil, errors.New("ThreadBear state path is not a real directory")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return nil, err
	}
	if err := os.Chmod(s.dir, 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(s.dir, "core.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err == nil {
		err = unix.Flock(int(f.Fd()), unix.LOCK_EX)
	}
	if err != nil {
		if f != nil {
			f.Close()
		}
		return nil, err
	}
	return f, nil
}

func unlock(f *os.File) error {
	return errors.Join(unix.Flock(int(f.Fd()), unix.LOCK_UN), f.Close())
}

func (s store) load() (state, error) {
	if info, err := os.Lstat(s.path()); err == nil && (info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600) {
		return state{}, errors.New("ThreadBear state is not a regular file")
	} else if err != nil {
		return state{}, err
	}
	data, err := os.ReadFile(s.path())
	if err != nil {
		return state{}, err
	}
	var value state
	if err := json.Unmarshal(data, &value); err != nil {
		return state{}, err
	}
	if value.Format != stateFormat || strings.TrimSpace(value.ControlTaskID) == "" {
		return state{}, fmt.Errorf("unsupported ThreadBear state format %d", value.Format)
	}
	if value.Tasks == nil {
		value.Tasks = map[string]taskRecord{}
	}
	if value.Plans == nil {
		value.Plans = map[string]plan{}
	}
	return value, nil
}

func (s store) save(value state) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(s.dir, ".core-*")
	if err != nil {
		return err
	}
	name := f.Name()
	ok := false
	defer func() {
		f.Close()
		if !ok {
			os.Remove(name)
		}
	}()
	if err = f.Chmod(0o600); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(name, s.path())
	}
	if err != nil {
		return err
	}
	ok = true
	d, err := os.Open(s.dir)
	if err != nil {
		return err
	}
	return errors.Join(d.Sync(), d.Close())
}

var statusEmoji = map[string]string{
	"running": "⏳", "blocked": "🚨", "needs_input": "🙋", "automation": "🤖",
	"next_steps": "➡️", "complete": "✅", "unknown": "❔",
}

func title(status, subject, action string) string {
	value := statusEmoji[status]
	if subject = strings.TrimSpace(subject); subject != "" {
		value += " " + subject
	}
	if action != "" {
		value += " — " + strings.TrimSpace(action)
	}
	if utf16Len(value) <= 60 {
		return value
	}
	units := 0
	end := 0
	for i, r := range value {
		width := utf16.RuneLen(r)
		if units+width > 59 {
			break
		}
		units += width
		end = i + utf8.RuneLen(r)
	}
	return strings.TrimSpace(value[:end]) + "…"
}

func utf16Len(value string) int { return len(utf16.Encode([]rune(value))) }

func subject(value string) string {
	value = strings.TrimSpace(value)
	for {
		removed := false
		for _, mark := range statusEmoji {
			if strings.HasPrefix(value, mark+" ") {
				value = strings.TrimSpace(strings.TrimPrefix(value, mark))
				removed = true
				break
			}
		}
		if !removed {
			return value
		}
	}
}

func planID(value plan) string {
	copy := value
	copy.ID = ""
	data, _ := json.Marshal(copy)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:16])
}

func decide(task indexedTask, result analysis) (taskRecord, plan) {
	owned := subject(task.Title)
	record := taskRecord{Revision: task.Revision, Title: task.Title, RolloutPath: task.RolloutPath, RolloutSize: result.Size, Evidence: result.Evidence, Status: result.Status, Subject: owned, Action: result.Action}
	desired := title(result.Status, owned, result.Action)
	value := plan{TaskID: task.ID, Revision: task.Revision, ExpectedTitle: task.Title, DesiredTitle: desired, Evidence: result.Evidence, Status: result.Status, Subject: owned, Action: result.Action, Epoch: result.Size}
	value.ID = planID(value)
	return record, value
}

func lunaEligible(record taskRecord, evidence string) bool {
	return record.Evidence != "" && record.Evidence == evidence && record.AmbiguousPasses >= 2
}
