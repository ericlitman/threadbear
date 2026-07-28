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
		subject = strings.TrimSpace(record.DurableSubject)
		if subject == "" {
			subject = ownedSubject(record)
		}
	} else {
		subject = stripOwnedToken(stripStatusPrefixes(current), record.ManagedTokenDisplay, record.ManagedTokenPosition)
	}
	if subject == "" {
		subject = stripStatusPrefixes(suggestedSubject)
	}
	statusOnly := subject == "" && isStatusOnly(current)
	if subject == "" && !statusOnly {
		return Result{}, ErrEmptySubject
	}
	action := strings.TrimSpace(nextAction)
	title := nextStatus.Emoji()
	if display.Position == tokens.PositionStart {
		title += " " + display.Value
	}
	if subject != "" {
		title += " " + subject
		if action != "" {
			title += " → " + action
		}
	} else {
		action = ""
	}
	if display.Position == tokens.PositionEnd {
		title += " · out " + display.Value
	}
	return Result{
		Title:                title,
		DurableSubject:       subject,
		ManagedAction:        action,
		ManagedTokenDisplay:  display.Value,
		ManagedTokenPosition: display.Position,
	}, nil
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
