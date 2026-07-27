package update

import (
	_ "embed"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	maximumReleaseNotes     = 3
	maximumReleaseNoteRunes = 200
)

//go:embed notes.txt
var embeddedReleaseNotes string

func ReleaseNotes() []string {
	return parseReleaseNotes(embeddedReleaseNotes)
}

func parseReleaseNotes(content string) []string {
	var notes []string
	for _, line := range strings.Split(content, "\n") {
		if !strings.HasPrefix(line, "- ") && !strings.HasPrefix(line, "* ") {
			continue
		}
		raw := line[2:]
		if containsControl(raw) {
			continue
		}
		note := strings.TrimSpace(raw)
		if !validReleaseNote(note) {
			continue
		}
		notes = append(notes, note)
		if len(notes) == maximumReleaseNotes {
			break
		}
	}
	return notes
}

func validReleaseNote(note string) bool {
	if note == "" || utf8.RuneCountInString(note) > maximumReleaseNoteRunes {
		return false
	}
	if strings.Contains(note, "](") || strings.Contains(note, "http://") || strings.Contains(note, "https://") || strings.ContainsAny(note, "<>`") || strings.Contains(note, "~~~") {
		return false
	}
	return !containsControl(note)
}

func containsControl(value string) bool {
	for _, char := range value {
		if unicode.IsControl(char) {
			return true
		}
	}
	return false
}
