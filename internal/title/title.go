package title

import (
	"errors"
	"strings"

	"github.com/ericlitman/threadbear/internal/state"
)

var ErrEmptySubject = errors.New("title subject is empty")
var ErrInvalidStatus = errors.New("title status is invalid")

type Result struct {
	Title          string
	DurableSubject string
	ManagedAction  string
}

func Reconcile(record state.TaskRecord, nextStatus state.TaskStatus, suggestedSubject, nextAction string) (Result, error) {
	if !nextStatus.Valid() {
		return Result{}, ErrInvalidStatus
	}
	current := record.CapturedTitle
	subject := ""
	if record.LastAppliedTitle != "" && current == record.LastAppliedTitle {
		subject = strings.TrimSpace(record.DurableSubject)
		if subject == "" {
			subject = ownedSubject(record)
		}
	} else {
		subject = stripStatusPrefixes(current)
	}
	if subject == "" {
		subject = stripStatusPrefixes(suggestedSubject)
	}
	if subject == "" {
		return Result{}, ErrEmptySubject
	}
	action := strings.TrimSpace(nextAction)
	title := nextStatus.Emoji() + " " + subject
	if action != "" {
		title += " → " + action
	}
	return Result{Title: title, DurableSubject: subject, ManagedAction: action}, nil
}

func ownedSubject(record state.TaskRecord) string {
	subject := stripStatusPrefixes(record.LastAppliedTitle)
	action := strings.TrimSpace(record.ManagedAction)
	if action != "" {
		subject = strings.TrimSuffix(subject, " → "+action)
	}
	return strings.TrimSpace(subject)
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
