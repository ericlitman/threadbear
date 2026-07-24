package status

import (
	"strings"
	"unicode"

	"github.com/ericlitman/threadbear/internal/state"
)

const footerPrefix = "🐻 "
const footerMiddle = " · next ("

var footerStatuses = map[string]state.TaskStatus{
	"complete":    state.StatusComplete,
	"next steps":  state.StatusNextSteps,
	"needs input": state.StatusNeedsInput,
	"blocked":     state.StatusBlocked,
	"automation":  state.StatusAutomation,
}

func ParseFooter(input FooterInput) FooterResult {
	result := FooterResult{
		ClassifierMessage: input.Message,
		TitleMessage:      input.Message,
		Rejection:         FooterAbsent,
	}
	lines := strings.Split(input.Message, "\n")
	index := finalNonEmptyLine(lines)
	if index < 0 {
		return result
	}
	line := strings.TrimSuffix(lines[index], "\r")
	if hasEmbeddedFooter(lines[:index]) {
		result.Rejection = FooterEmbedded
		return result
	}
	if quotedFooter(line) {
		result.Rejection = FooterQuoted
		return result
	}
	footer, ok := parseFooterLine(line)
	if !ok {
		if strings.HasPrefix(line, footerPrefix) || strings.Contains(line, "🐻 STATUS") {
			result.Rejection = FooterMalformed
		}
		return result
	}
	result.TitleMessage = substantiveLines(lines[:index])
	if !input.LatestTurnCompleted {
		result.Rejection = FooterTurnIncomplete
		return result
	}
	if input.NewerUserMessage {
		result.Rejection = FooterNewerUserMessage
		return result
	}
	if input.Stale {
		result.Rejection = FooterStale
		return result
	}
	if input.StructuredStatus.Valid() && input.StructuredStatus != state.StatusUnknown && input.StructuredStatus != footer.Status {
		result.Rejection = FooterStructuredContradiction
		return result
	}
	valid, weak := validBinding(footer)
	if !valid {
		if weak {
			result.Rejection = FooterWeakAction
		} else {
			result.Rejection = FooterInvalidOwnerAction
		}
		return result
	}
	result.Footer = footer
	result.Accepted = true
	result.Rejection = FooterAccepted
	return result
}

func parseFooterLine(line string) (Footer, bool) {
	if strings.TrimSpace(line) != line || !strings.HasPrefix(line, footerPrefix) {
		return Footer{}, false
	}
	remainder := strings.TrimPrefix(line, footerPrefix)
	statusText, ownerAction, ok := strings.Cut(remainder, footerMiddle)
	if !ok || statusText == "" {
		return Footer{}, false
	}
	ownerText, action, ok := strings.Cut(ownerAction, "): ")
	if !ok || ownerText == "" || action == "" || strings.Contains(action, "\n") {
		return Footer{}, false
	}
	taskStatus, ok := footerStatuses[statusText]
	if !ok {
		return Footer{}, false
	}
	return Footer{Status: taskStatus, Owner: Owner(ownerText), Action: action}, true
}

func validBinding(footer Footer) (bool, bool) {
	switch footer.Status {
	case state.StatusComplete, state.StatusAutomation:
		return footer.Owner == OwnerNone && footer.Action == "none", false
	case state.StatusNeedsInput:
		if footer.Owner != OwnerYou || footer.Action == "none" {
			return false, false
		}
	case state.StatusBlocked:
		if footer.Owner != OwnerExternal || footer.Action == "none" {
			return false, false
		}
	case state.StatusNextSteps:
		if footer.Owner != OwnerYou && footer.Owner != OwnerAgent && footer.Owner != OwnerExternal || footer.Action == "none" {
			return false, false
		}
	default:
		return false, false
	}
	if !concreteAction(footer.Action) {
		return false, true
	}
	return true, false
}

func concreteAction(action string) bool {
	if strings.TrimSpace(action) != action || action == "" {
		return false
	}
	normalized := strings.ToLower(strings.Trim(action, " .!?"))
	words := strings.FieldsFunc(normalized, func(r rune) bool {
		return unicode.IsSpace(r) || strings.ContainsRune(",;:()", r)
	})
	if len(words) < 2 {
		return false
	}
	for _, prefix := range []string{
		"if ", "maybe ", "perhaps ", "possibly ", "potentially ",
		"could ", "might ", "may ", "would ", "should ",
		"i can ", "i could ", "we can ", "we could ", "you can ", "you could ",
		"let me know", "happy to ", "feel free ",
	} {
		if strings.HasPrefix(normalized, prefix) {
			return false
		}
	}
	for _, phrase := range []string{" if you want", " if needed", " if necessary", " when convenient"} {
		if strings.Contains(normalized, phrase) {
			return false
		}
	}
	for _, phrase := range []string{
		"help with that", "help with it", "do that", "do it", "handle that", "handle it",
		"follow up", "follow-up", "next step", "next steps", "more work", "something else",
		"anything else", "figure it out", "tbd", "to be determined",
	} {
		if normalized == phrase || strings.HasPrefix(normalized, phrase+" ") {
			return false
		}
	}
	if recordOnly(words) {
		return false
	}
	return true
}

func recordOnly(words []string) bool {
	recordWords := map[string]bool{
		"recorded": true, "filed": true, "created": true, "logged": true,
		"captured": true, "tracked": true, "noted": true, "documented": true,
	}
	for index, word := range words {
		if !recordWords[word] {
			continue
		}
		if subordinateClause(words[:index]) {
			continue
		}
		if index == 0 || passiveAuxiliary(words[max(0, index-3):index]) || recordSubject(words[index-1]) {
			return true
		}
	}
	return false
}

func subordinateClause(words []string) bool {
	for _, word := range words {
		switch word {
		case "how", "why", "whether":
			return true
		}
	}
	return false
}

func passiveAuxiliary(words []string) bool {
	for _, word := range words {
		switch word {
		case "is", "are", "was", "were", "be", "been", "being", "has", "have", "had":
			return true
		}
	}
	return false
}

func recordSubject(word string) bool {
	switch word {
	case "issue", "ticket", "task", "follow-up", "followup", "work", "request", "recommendation":
		return true
	}
	for _, char := range word {
		if char >= '0' && char <= '9' && strings.ContainsRune(word, '-') {
			return true
		}
	}
	return false
}

func finalNonEmptyLine(lines []string) int {
	for index := len(lines) - 1; index >= 0; index-- {
		if strings.TrimSpace(strings.TrimSuffix(lines[index], "\r")) != "" {
			return index
		}
	}
	return -1
}

func hasEmbeddedFooter(lines []string) bool {
	for _, raw := range lines {
		line := strings.TrimSuffix(raw, "\r")
		if _, ok := parseFooterLine(line); ok || quotedFooter(line) {
			return true
		}
	}
	return false
}

func quotedFooter(line string) bool {
	trimmed := strings.TrimSpace(line)
	for _, prefix := range []string{">", "\"", string(rune(39)), "`"} {
		if strings.HasPrefix(trimmed, prefix) && strings.Contains(trimmed, footerPrefix) {
			return true
		}
	}
	return false
}

func substantiveLines(lines []string) string {
	return strings.TrimRightFunc(strings.Join(lines, "\n"), unicode.IsSpace)
}
