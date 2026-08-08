package main

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf16"

	"golang.org/x/sys/unix"
)

const maxTitleUnits = 60

var taskIDPattern = regexp.MustCompile(`^[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}$`)

var statusIcons = map[string]string{
	"complete":    "✅",
	"next_steps":  "➡️",
	"needs_input": "🙋",
	"blocked":     "🚨",
	"automation":  "🤖",
}

// These exact visible prefixes are reserved for current ThreadBear titles.
// Other leading emoji remain user text unless they match an ambiguous old
// ThreadBear rendering below, which cannot be distinguished safely.
var ownedTitlePrefixes = []string{"✅ ", "➡️ ", "🙋 ", "🚨 ", "🤖 ", "🐻 "}

// Older operation-shaped titles are ambiguous. Leave the complete title
// untouched instead of guessing whether its leading emoji is user-authored.
var blockedTitlePrefixes = []string{"➡ ", "⏳ ", "❔ ", "🧵🐻"}

var internalEnvelopeMarkers = []string{
	"<codex_delegation>", "<codex_internal_context", "<environment_context>",
	"<app-context", "<collaboration_mode", "<multi_agent_mode",
	"<permissions_instructions", "<permissions instructions",
	"<apps_instructions", "<plugins_instructions", "<skills_instructions",
	"<recommended_plugins",
}

func unlock(lock *os.File) {
	_ = unix.Flock(int(lock.Fd()), unix.LOCK_UN)
	_ = lock.Close()
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

func subjectFromTitle(title string) (subject string, decorated bool, err error) {
	for _, prefix := range blockedTitlePrefixes {
		if strings.HasPrefix(title, prefix) {
			return "", false, errors.New("title has an ambiguous old ThreadBear prefix")
		}
	}
	for _, prefix := range ownedTitlePrefixes {
		if strings.HasPrefix(title, prefix) {
			subject = strings.TrimPrefix(title, prefix)
			if err := validateSubject(subject); err != nil {
				return "", false, err
			}
			return subject, true, nil
		}
	}
	if err := validateSubject(title); err != nil {
		return "", false, err
	}
	return title, false, nil
}

func validateSubject(subject string) error {
	if strings.TrimSpace(subject) == "" {
		return errors.New("subject is blank")
	}
	if strings.ContainsFunc(subject, func(char rune) bool {
		return unicode.IsControl(char) || unicode.Is(unicode.Zl, char) || unicode.Is(unicode.Zp, char)
	}) {
		return errors.New("subject contains multiline or control text")
	}
	lower := strings.ToLower(subject)
	for _, marker := range internalEnvelopeMarkers {
		if strings.Contains(lower, marker) {
			return errors.New("subject is a raw internal envelope")
		}
	}
	if len(utf16.Encode([]rune("🐻 "+subject))) > maxTitleUnits {
		return errors.New("subject does not fit without truncation")
	}
	return nil
}
