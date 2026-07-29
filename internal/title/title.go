package title

import (
	"errors"
	"strings"

	"github.com/ericlitman/threadbear/internal/state"
	"github.com/ericlitman/threadbear/internal/tokens"
)

var ErrEmptySubject = errors.New("title subject is empty")
var ErrInvalidStatus = errors.New("title status is invalid")
var ErrInvalidTokenDisplay = errors.New("title token display is invalid")

type Result struct {
	Title                string
	DurableSubject       string
	ManagedAction        string
	ManagedTokenDisplay  string
	ManagedTokenPosition tokens.Position
}

func Reconcile(record state.TaskRecord, nextStatus state.TaskStatus, suggestedSubject, nextAction string, display tokens.Display) (Result, error) {
	if !nextStatus.Valid() {
		return Result{}, ErrInvalidStatus
	}
	display, err := normalizeDisplay(display)
	if err != nil {
		return Result{}, err
	}
	current := record.CapturedTitle
	subject := ""
	if record.LastAppliedTitle != "" && current == record.LastAppliedTitle {
		hasDurableSubject := strings.TrimSpace(record.DurableSubject) != ""
		subject = strings.TrimSpace(record.DurableSubject)
		if subject == "" {
			subject = ownedSubject(record)
		}
		if record.ManagedTokenDisplay == "" {
			copies := ownedTokenCopies(subject, display.Value, display.Position)
			authoritative := subject != "" && stripStatusPrefixes(renderTitle(nextStatus, subject, strings.TrimSpace(record.ManagedAction), display)) == stripStatusPrefixes(record.LastAppliedTitle)
			if copies > 1 || !hasDurableSubject && !authoritative {
				subject = stripOwnedTokenCopies(subject, display.Value, display.Position)
			}
		} else {
			if ownedTokenCopies(subject, record.ManagedTokenDisplay, record.ManagedTokenPosition) > 1 {
				subject = stripOwnedTokenCopies(subject, record.ManagedTokenDisplay, record.ManagedTokenPosition)
			}
			if display.Value != record.ManagedTokenDisplay || display.Position != record.ManagedTokenPosition {
				observed := stripStatusPrefixes(current)
				if ownedTokenCopies(observed, display.Value, record.ManagedTokenPosition) > 1 {
					subject = stripOwnedTokenCopies(subject, display.Value, record.ManagedTokenPosition)
				}
				if ownedTokenCopies(observed, display.Value, display.Position) > 1 {
					subject = stripOwnedTokenCopies(subject, display.Value, display.Position)
				}
			}
		}
	} else {
		subject = stripOwnedToken(stripStatusPrefixes(current), record.ManagedTokenDisplay, record.ManagedTokenPosition)
		if record.LastAppliedTitle == "" && record.ManagedTokenDisplay == "" && hasCanonicalStatusPrefix(current) {
			subject = stripOwnedTokenCopies(subject, display.Value, display.Position)
		}
	}
	if subject == "" {
		subject = stripStatusPrefixes(suggestedSubject)
	}
	statusOnly := subject == "" && isStatusOnly(current)
	if subject == "" && !statusOnly {
		return Result{}, ErrEmptySubject
	}
	if statusOnly {
		display = tokens.Display{}
	}
	action := strings.TrimSpace(nextAction)
	if subject == "" {
		action = ""
	}
	title := renderTitle(nextStatus, subject, action, display)
	return Result{
		Title:                title,
		DurableSubject:       subject,
		ManagedAction:        action,
		ManagedTokenDisplay:  display.Value,
		ManagedTokenPosition: display.Position,
	}, nil
}

func renderTitle(status state.TaskStatus, subject, action string, display tokens.Display) string {
	title := status.Emoji()
	if display.Position == tokens.PositionStart {
		title += " " + display.Value
	}
	if subject != "" {
		title += " " + subject
		if action != "" {
			title += " → " + action
		}
	}
	if display.Position == tokens.PositionEnd {
		title += " · out " + display.Value
	}
	return title
}

func ownedSubject(record state.TaskRecord) string {
	subject := stripStatusPrefixes(record.LastAppliedTitle)
	subject = stripOwnedToken(subject, record.ManagedTokenDisplay, record.ManagedTokenPosition)
	action := strings.TrimSpace(record.ManagedAction)
	if action != "" {
		subject = strings.TrimSuffix(subject, " → "+action)
	}
	return strings.TrimSpace(subject)
}

func normalizeDisplay(display tokens.Display) (tokens.Display, error) {
	display.Value = strings.TrimSpace(display.Value)
	if display.Position == tokens.PositionOff || display.Value == "" {
		return tokens.Display{}, nil
	}
	if display.Position != tokens.PositionStart && display.Position != tokens.PositionEnd || strings.ContainsAny(display.Value, " \t\r\n") {
		return tokens.Display{}, ErrInvalidTokenDisplay
	}
	return display, nil
}

func stripOwnedToken(value, managed string, position tokens.Position) string {
	managed = strings.TrimSpace(managed)
	if managed == "" {
		return strings.TrimSpace(value)
	}
	switch position {
	case tokens.PositionStart:
		value = strings.TrimPrefix(value, managed+" ")
	case tokens.PositionEnd:
		value = strings.TrimSuffix(value, " · out "+managed)
	}
	return strings.TrimSpace(value)
}

func stripOwnedTokenCopies(value, managed string, position tokens.Position) string {
	for {
		stripped := stripOwnedToken(value, managed, position)
		if stripped == strings.TrimSpace(value) {
			return stripped
		}
		value = stripped
	}
}

func ownedTokenCopies(value, managed string, position tokens.Position) int {
	managed = strings.TrimSpace(managed)
	if managed == "" {
		return 0
	}
	count := 0
	value = strings.TrimSpace(value)
	for {
		previous := value
		switch position {
		case tokens.PositionStart:
			value = strings.TrimPrefix(value, managed+" ")
		case tokens.PositionEnd:
			value = strings.TrimSuffix(value, " · out "+managed)
		}
		value = strings.TrimSpace(value)
		if value == previous {
			return count
		}
		count++
	}
}

func hasCanonicalStatusPrefix(value string) bool {
	value = strings.TrimSpace(value)
	for _, emoji := range []string{"⏳", "🚨", "🙋", "🤖", "➡️", "✅", "❔"} {
		if value == emoji || strings.HasPrefix(value, emoji+" ") {
			return true
		}
	}
	return false
}

func isStatusOnly(value string) bool {
	switch strings.TrimSpace(value) {
	case "⏳", "🚨", "🙋", "🤖", "➡️", "✅", "❔":
		return true
	default:
		return false
	}
}

func stripStatusPrefixes(value string) string {
	value = strings.TrimSpace(value)
	for {
		stripped := false
		for _, emoji := range []string{"⏳", "🚨", "🙋", "🤖", "➡️", "✅", "❔"} {
			if strings.HasPrefix(value, emoji) {
				value = strings.TrimSpace(strings.TrimPrefix(value, emoji))
				stripped = true
				break
			}
		}
		if !stripped {
			return value
		}
	}
}
