package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"golang.org/x/sys/unix"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf16"
)

const maxTitleUnits = 60

type taskState struct {
	Subject string `json:"subject"`
}

type store struct{ dir string }

var taskIDPattern = regexp.MustCompile(`^[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}$`)

var statusIcons = map[string]string{
	"complete":    "✅",
	"next_steps":  "➡️",
	"needs_input": "🙋",
	"blocked":     "🚨",
	"automation":  "🤖",
}

// Every value here is a title rendering ThreadBear may own. Recognition is
// deliberately finite and byte-exact; ThreadBear never guesses by stripping.
var ownedIcons = []string{"✅", "➡️", "🙋", "🚨", "🤖", "🐻"}

// legacyPrefixes are ambiguous without a subject record. They include the
// current renderings and old ThreadBear decorations that may remain after the
// 2.2.1 reset.
var legacyPrefixes = []string{
	"✅", "➡️", "➡", "🙋", "🚨", "🤖", "🐻",
	"⏳", "❔", "🧵🐻",
}

var internalEnvelopeMarkers = []string{
	"<codex_delegation>", "<codex_internal_context", "<environment_context>",
	"<app-context", "<collaboration_mode", "<multi_agent_mode",
	"<permissions_instructions", "<permissions instructions",
	"<apps_instructions", "<plugins_instructions", "<skills_instructions",
	"<recommended_plugins",
}

func newStore(dir string) store    { return store{dir: dir} }
func (s store) subjectDir() string { return filepath.Join(s.dir, "subjects") }

func (s store) paths(id string) (string, string, error) {
	if !taskIDPattern.MatchString(id) {
		return "", "", errors.New("task ID cannot name a subject record")
	}
	return filepath.Join(s.subjectDir(), id+".json"), filepath.Join(s.subjectDir(), id+".lock"), nil
}

func (s store) openSubjectFile(path string, flags int, chmodDir bool) (*os.File, error) {
	dir, err := unix.Open(s.subjectDir(), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer unix.Close(dir)
	if chmodDir {
		if err := unix.Fchmod(dir, 0o700); err != nil {
			return nil, err
		}
	}
	fd, err := unix.Openat(dir, filepath.Base(path), flags|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}

func (s store) lock(id string) (*os.File, error) {
	_, lockPath, err := s.paths(id)
	if err != nil {
		return nil, err
	}
	file, err := s.openSubjectFile(lockPath, unix.O_CREAT|unix.O_RDWR, true)
	if err != nil {
		return nil, err
	}
	info, statErr := file.Stat()
	if statErr != nil || !info.Mode().IsRegular() {
		return nil, errors.Join(errors.New("ThreadBear state lock is not a regular file"), statErr, file.Close())
	}
	fd := int(file.Fd())
	if err = errors.Join(unix.Fchmod(fd, 0o600), unix.Flock(fd, unix.LOCK_EX)); err != nil {
		return nil, errors.Join(err, file.Close())
	}
	return file, nil
}

func (s store) lifecycleFence() (*os.File, error) {
	dir, err := unix.Open(s.dir, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, errors.Join(errors.New("ThreadBear lifecycle fence is unavailable"), err)
	}
	defer unix.Close(dir)
	var dirStat unix.Stat_t
	if err := unix.Fstat(dir, &dirStat); err != nil || dirStat.Mode&unix.S_IFMT != unix.S_IFDIR || dirStat.Mode&0o777 != 0o700 {
		return nil, errors.Join(errors.New("ThreadBear state directory is not private"), err)
	}
	fd, err := unix.Openat(dir, "lifecycle.lock", unix.O_RDONLY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, errors.Join(errors.New("ThreadBear lifecycle fence is unavailable"), err)
	}
	file := os.NewFile(uintptr(fd), filepath.Join(s.dir, "lifecycle.lock"))
	info, statErr := file.Stat()
	if statErr != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		return nil, errors.Join(errors.New("ThreadBear lifecycle fence is not a private regular file"), statErr, file.Close())
	}
	if err := unix.Flock(fd, unix.LOCK_SH|unix.LOCK_NB); err != nil {
		return nil, errors.Join(errors.New("ThreadBear lifecycle is busy"), err, file.Close())
	}
	current, pathErr := os.Lstat(file.Name())
	if pathErr != nil || !current.Mode().IsRegular() || current.Mode().Perm() != 0o600 || !os.SameFile(info, current) {
		unlock(file)
		return nil, errors.Join(errors.New("ThreadBear lifecycle changed during writer admission"), pathErr)
	}
	return file, nil
}

func unlock(lock *os.File) {
	_ = unix.Flock(int(lock.Fd()), unix.LOCK_UN)
	_ = lock.Close()
}

func (s store) readTask(id string) (taskState, error) {
	path, _, err := s.paths(id)
	if err != nil {
		return taskState{}, err
	}
	file, err := s.openSubjectFile(path, unix.O_RDONLY, false)
	if err != nil {
		return taskState{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return taskState{}, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		return taskState{}, errors.New("ThreadBear subject record is not a private regular file")
	}
	var value taskState
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil || validateSubject(value.Subject) != nil {
		return taskState{}, errors.New("unsupported or corrupt ThreadBear subject record")
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return taskState{}, errors.New("unsupported or corrupt ThreadBear subject record")
	}
	return value, nil
}

func (s store) withFence(change func() error) error {
	fence, err := s.lifecycleFence()
	if err != nil {
		return err
	}
	defer unlock(fence)
	return change()
}

func (s store) updateTask(id string, change func(*taskState) (bool, error)) error {
	return s.withFence(func() error { return s.updateTaskUnderFence(id, change) })
}

// updateTaskUnderFence is only for callers already holding lifecycleFence.
// Keeping native reads and subject preparation under that shared fence
// prevents uninstall from completing between those steps.
func (s store) updateTaskUnderFence(id string, change func(*taskState) (bool, error)) (err error) {
	lock, err := s.lock(id)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, unix.Flock(int(lock.Fd()), unix.LOCK_UN), lock.Close()) }()

	value, err := s.readTask(id)
	if errors.Is(err, os.ErrNotExist) {
		value = taskState{}
	} else if err != nil {
		return err
	}
	changed, err := change(&value)
	if err != nil || !changed {
		return err
	}
	if err := validateSubject(value.Subject); err != nil {
		return err
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	path, _, err := s.paths(id)
	if err != nil {
		return err
	}
	return writeAtomic(path, append(data, '\n'), 0o600)
}

func renderTitle(status, subject string) (string, error) {
	icon, ok := statusIcons[status]
	if !ok {
		return "", fmt.Errorf("unsupported ThreadBear status %q", status)
	}
	if err := validateSubject(subject); err != nil {
		return "", err
	}
	return icon + " " + subject, nil
}

func resolveSubject(current string, previous taskState) (string, error) {
	if isOperationTitle(current) {
		if previous.Subject != "" {
			return previous.Subject, nil
		}
		return "", errors.New("current title is a raw ThreadBear operation")
	}
	if previous.Subject != "" && isOwnedRendering(current, previous.Subject) {
		return previous.Subject, nil
	}
	// With no ownership, a legacy prefix is ambiguous. Otherwise any safe
	// byte string is an intentional rename, including leading user emoji.
	if previous.Subject == "" && hasLegacyPrefix(current) {
		return "", errors.New("current title has an unowned ThreadBear prefix")
	}
	if err := validateSubject(current); err != nil {
		return "", err
	}
	return current, nil
}

// A skipped old hook could leave one of these exact operation shapes visible.
// Keep the grammar finite so an ordinary user rename beginning with 🧵🐻 is
// still treated as the user's subject.
func isOperationTitle(title string) bool {
	if title == "🧵🐻 complete" || title == "🧵🐻 automation" || title == "⏳ ThreadBear is working" {
		return true
	}
	for _, prefix := range []string{
		"🧵🐻 next steps (", "🧵🐻 needs input (", "🧵🐻 blocked (",
		"⏳ ThreadBear is working:",
	} {
		if strings.HasPrefix(title, prefix) {
			return true
		}
	}
	return false
}

func validateSubject(subject string) error {
	if strings.TrimSpace(subject) == "" {
		return errors.New("subject is blank")
	}
	if hasUnsafeText(subject) {
		return errors.New("subject contains multiline or control text")
	}
	lower := strings.ToLower(subject)
	for _, marker := range internalEnvelopeMarkers {
		if strings.Contains(lower, marker) {
			return errors.New("subject is a raw internal envelope")
		}
	}
	// Stored subjects must fit every production prefix. Otherwise a title can
	// get stuck on a stale success icon when a later status uses a wider emoji.
	if utf16Units("🐻 "+subject) > maxTitleUnits {
		return errors.New("subject does not fit without truncation")
	}
	return nil
}

func hasUnsafeText(value string) bool {
	return strings.ContainsFunc(value, func(char rune) bool {
		return unicode.IsControl(char) || unicode.Is(unicode.Zl, char) || unicode.Is(unicode.Zp, char)
	})
}

func isOwnedRendering(current, subject string) bool {
	for _, icon := range ownedIcons {
		if current == icon+" "+subject {
			return true
		}
	}
	return false
}

func hasLegacyPrefix(title string) bool {
	return hasPrefix(title, legacyPrefixes)
}

func hasPrefix(title string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(title, prefix) {
			return true
		}
	}
	return false
}

func utf16Units(value string) int { return len(utf16.Encode([]rune(value))) }
