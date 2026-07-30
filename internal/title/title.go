package title

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

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
	suggested := stripStatusPrefixes(suggestedSubject)
	if managedCapturedTitle(record) {
		subject = strings.TrimSpace(record.DurableSubject)
		if subject == "" {
			subject = ownedSubject(record)
		}
		if suggested != "" && canReachSuggestedSubject(subject, suggested, record.ManagedTokenPosition, display.Position) {
			subject = suggested
		}
		oppositePosition := tokens.PositionOff
		switch record.ManagedTokenPosition {
		case tokens.PositionStart:
			oppositePosition = tokens.PositionEnd
		case tokens.PositionEnd:
			oppositePosition = tokens.PositionStart
		}
		if ownedTokenCopies(subject, record.ManagedTokenDisplay, oppositePosition) > 1 {
			subject = stripOwnedTokenCopies(subject, record.ManagedTokenDisplay, oppositePosition)
		}
	} else {
		subject = stripStatusPrefixes(current)
		subject = stripOwnedToken(subject, record.ManagedTokenDisplay, record.ManagedTokenPosition)
		if suggested != "" && canReachSuggestedSubject(subject, suggested, display.Position) {
			subject = suggested
		}
	}
	if subject == "" {
		subject = suggested
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
	return Result{
		Title:                renderTitle(nextStatus, subject, action, display),
		DurableSubject:       subject,
		ManagedAction:        action,
		ManagedTokenDisplay:  display.Value,
		ManagedTokenPosition: display.Position,
	}, nil
}

func managedCapturedTitle(record state.TaskRecord) bool {
	return managedCurrentTitle(record, record.CapturedTitle)
}

func managedCurrentTitle(record state.TaskRecord, current string) bool {
	lastApplied := record.LastAppliedTitle
	return lastApplied != "" && (current == lastApplied || current == PersistedTitle(lastApplied))
}

func SubjectNeedsClassification(record state.TaskRecord, current string, displayPosition tokens.Position) bool {
	if !hasCanonicalStatusPrefix(current) {
		return false
	}
	if managedCurrentTitle(record, current) {
		subject := strings.TrimSpace(record.DurableSubject)
		if subject == "" {
			subject = ownedSubject(record)
		}
		if hasManagedBoundaryToken(subject, record.ManagedTokenPosition) {
			return true
		}
		return displayPosition != record.ManagedTokenPosition && hasManagedBoundaryToken(subject, displayPosition)
	}
	if record.LastAppliedTitle != "" {
		return false
	}
	return hasManagedBoundaryToken(stripStatusPrefixes(current), displayPosition)
}

func PersistedTitle(value string) string {
	if utf16Units(value) <= 60 {
		return value
	}
	return truncateUTF16(value, 59) + "…"
}

func Cleanup(record state.TaskRecord, current string) (string, bool) {
	if record.LastAppliedTitle == "" {
		return current, false
	}
	target := cleanupRetainedTitle(record)
	if current == target || current == PersistedTitle(target) {
		return current, false
	}
	if current == record.LastAppliedTitle || current == PersistedTitle(record.LastAppliedTitle) {
		return target, target != current
	}
	cleaned := stripCleanupStatus(current, record.LastAppliedTitle)
	cleaned = stripCleanupToken(cleaned, record.ManagedTokenDisplay, record.ManagedTokenPosition)
	return cleaned, cleaned != current
}

func cleanupRetainedTitle(record state.TaskRecord) string {
	subject := record.DurableSubject
	if subject == "" {
		subject = ownedSubject(record)
	}
	if record.ManagedAction != "" && subject != "" {
		return subject + " → " + record.ManagedAction
	}
	return subject
}

func stripCleanupStatus(value, owned string) string {
	for _, emoji := range []string{"⏳", "🚨", "🙋", "🤖", "➡️", "✅", "❔"} {
		prefix := emoji + " "
		if owned == emoji && value == emoji {
			return ""
		}
		if strings.HasPrefix(owned, prefix) && strings.HasPrefix(value, prefix) {
			return strings.TrimPrefix(value, prefix)
		}
	}
	return value
}

func stripCleanupToken(value, managed string, position tokens.Position) string {
	if managed == "" {
		return value
	}
	switch position {
	case tokens.PositionStart:
		prefix := managed + " "
		if strings.HasPrefix(value, prefix) {
			return strings.TrimPrefix(value, prefix)
		}
	case tokens.PositionEnd:
		for _, suffix := range []string{" · out " + managed, " · " + managed} {
			if strings.HasSuffix(value, suffix) {
				return strings.TrimSuffix(value, suffix)
			}
		}
	}
	return value
}

func AdoptSingleLeadingStatus(value string) (state.TaskStatus, string, bool) {
	value = strings.TrimSpace(value)
	for _, candidate := range []struct {
		emoji  string
		status state.TaskStatus
	}{
		{"⏳", state.StatusRunning}, {"🚨", state.StatusBlocked}, {"🙋", state.StatusNeedsInput},
		{"🤖", state.StatusAutomation}, {"➡️", state.StatusNextSteps}, {"✅", state.StatusComplete}, {"❔", state.StatusUnknown},
	} {
		if value != candidate.emoji && !strings.HasPrefix(value, candidate.emoji+" ") {
			continue
		}
		remainder := strings.TrimSpace(strings.TrimPrefix(value, candidate.emoji))
		if remainder != "" && hasCanonicalStatusPrefix(remainder) {
			return "", "", false
		}
		return candidate.status, remainder, true
	}
	return "", "", false
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
		return boundTitle(title, " · "+display.Value)
	}
	return boundTitle(title, "")
}

func boundTitle(title, suffix string) string {
	if utf16Units(title+suffix) <= 60 {
		return title + suffix
	}
	if suffix == "" {
		return truncateUTF16(title, 59) + "…"
	}
	prefixUnits := 59 - utf16Units(suffix)
	if prefixUnits < 0 {
		prefixUnits = 0
	}
	return truncateUTF16(title, prefixUnits) + "…" + suffix
}

func truncateUTF16(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	units := 0
	end := 0
	for offset, valueRune := range value {
		runeUnits := utf16.RuneLen(valueRune)
		if runeUnits < 0 || units+runeUnits > limit {
			break
		}
		units += runeUnits
		end = offset + utf8.RuneLen(valueRune)
	}
	return value[:end]
}

func utf16Units(value string) int {
	units := 0
	for _, valueRune := range value {
		runeUnits := utf16.RuneLen(valueRune)
		if runeUnits > 0 {
			units += runeUnits
		}
	}
	return units
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
	value = strings.TrimSpace(value)
	if managed == "" {
		return value
	}
	switch position {
	case tokens.PositionStart:
		if strings.HasPrefix(value, managed) {
			remainder := strings.TrimPrefix(value, managed)
			if remainder == "" {
				value = ""
			} else if first, _ := utf8.DecodeRuneInString(remainder); unicode.IsSpace(first) {
				value = strings.TrimLeftFunc(remainder, unicode.IsSpace)
			}
		}
	case tokens.PositionEnd:
		const persistedEllipsis = " ·…"
		if strings.HasSuffix(value, persistedEllipsis) {
			withoutEllipsis := strings.TrimSuffix(value, persistedEllipsis)
			stripped := stripOwnedEndToken(withoutEllipsis, managed)
			if stripped != withoutEllipsis {
				value = stripped + persistedEllipsis
				break
			}
		}
		value = stripOwnedEndToken(value, managed)
	}
	return strings.TrimSpace(value)
}

func stripOwnedEndToken(value, managed string) string {
	for _, suffix := range []string{" · out " + managed, " · " + managed} {
		if strings.HasSuffix(value, suffix) {
			return strings.TrimSuffix(value, suffix)
		}
	}
	return value
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

func stripManagedBoundaryToken(value string, position tokens.Position) string {
	value = strings.TrimSpace(value)
	switch position {
	case tokens.PositionStart:
		token, remainder, found := strings.Cut(value, " ")
		if found && tokens.IsDisplayValue(token) {
			return strings.TrimSpace(remainder)
		}
	case tokens.PositionEnd:
		for _, delimiter := range []string{" · out ", " · "} {
			index := strings.LastIndex(value, delimiter)
			if index >= 0 && tokens.IsDisplayValue(value[index+len(delimiter):]) {
				return strings.TrimSpace(value[:index])
			}
		}
	}
	return value
}

func canReachSuggestedSubject(value, suggested string, positions ...tokens.Position) bool {
	value = strings.TrimSpace(value)
	seen := map[string]bool{value: true}
	pending := []string{value}
	for next := 0; next < len(pending); next++ {
		candidate := pending[next]
		if candidate == suggested {
			return true
		}
		for _, position := range positions {
			stripped := stripManagedBoundaryToken(candidate, position)
			if stripped != candidate && !seen[stripped] {
				seen[stripped] = true
				pending = append(pending, stripped)
			}
		}
	}
	return false
}

func hasManagedBoundaryToken(value string, position tokens.Position) bool {
	value = strings.TrimSpace(value)
	return stripManagedBoundaryToken(value, position) != value
}

func ownedTokenCopies(value, managed string, position tokens.Position) int {
	count := 0
	value = strings.TrimSpace(value)
	for {
		stripped := stripOwnedToken(value, managed, position)
		if stripped == value {
			return count
		}
		value = stripped
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
