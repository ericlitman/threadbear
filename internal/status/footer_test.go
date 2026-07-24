package status

import (
	"testing"

	"github.com/ericlitman/threadbear/internal/state"
)

func TestParseFooterValidStatusMatrix(t *testing.T) {
	tests := []struct {
		name   string
		line   string
		status state.TaskStatus
		owner  Owner
		action string
	}{
		{"complete", "🐻 complete · next (none): none", state.StatusComplete, OwnerNone, "none"},
		{"AE17 next steps agent", "🐻 next steps · next (agent): create the implementation plan", state.StatusNextSteps, OwnerAgent, "create the implementation plan"},
		{"next steps you", "🐻 next steps · next (you): choose the release region", state.StatusNextSteps, OwnerYou, "choose the release region"},
		{"next steps external", "🐻 next steps · next (external): approve the production release", state.StatusNextSteps, OwnerExternal, "approve the production release"},
		{"AE7 needs input", "🐻 needs input · next (you): choose the release region", state.StatusNeedsInput, OwnerYou, "choose the release region"},
		{"blocked", "🐻 blocked · next (external): restore the signing service", state.StatusBlocked, OwnerExternal, "restore the signing service"},
		{"automation", "🐻 automation · next (none): none", state.StatusAutomation, OwnerNone, "none"},
		{"AE14 pressure test", "🐻 next steps · next (agent): pressure-test the requirements before implementation", state.StatusNextSteps, OwnerAgent, "pressure-test the requirements before implementation"},
		{"concrete noun phrase", "🐻 needs input · next (you): release region choice", state.StatusNeedsInput, OwnerYou, "release region choice"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := ParseFooter(FooterInput{Message: "Substantive response.\n\n" + test.line + "\n", LatestTurnCompleted: true})
			if !got.Accepted || got.Rejection != FooterAccepted {
				t.Fatalf("rejected valid footer: %+v", got)
			}
			if got.Footer.Status != test.status || got.Footer.Owner != test.owner || got.Footer.Action != test.action {
				t.Fatalf("footer = %+v", got.Footer)
			}
			if got.TitleMessage != "Substantive response." {
				t.Fatalf("substantive = %q", got.TitleMessage)
			}
		})
	}
}

func TestParseFooterRejectsNonCurrentOrMalformedSignals(t *testing.T) {
	valid := "Response.\n🐻 complete · next (none): none"
	tests := []struct {
		name  string
		input FooterInput
		want  FooterRejection
	}{
		{"absent", FooterInput{Message: "Response.", LatestTurnCompleted: true}, FooterAbsent},
		{"malformed status", FooterInput{Message: "Response.\n🐻 done · next (none): none", LatestTurnCompleted: true}, FooterMalformed},
		{"running is not a footer status", FooterInput{Message: "Response.\n🐻 running · next (none): none", LatestTurnCompleted: true}, FooterMalformed},
		{"unknown is not a footer status", FooterInput{Message: "Response.\n🐻 unknown · next (none): none", LatestTurnCompleted: true}, FooterMalformed},
		{"malformed spacing", FooterInput{Message: "Response.\n🐻 complete - next (none): none", LatestTurnCompleted: true}, FooterMalformed},
		{"embedded", FooterInput{Message: "🐻 complete · next (none): none\nMore response.", LatestTurnCompleted: true}, FooterEmbedded},
		{"quoted block", FooterInput{Message: "Response.\n> 🐻 complete · next (none): none", LatestTurnCompleted: true}, FooterQuoted},
		{"quoted string", FooterInput{Message: "Response.\n\"🐻 complete · next (none): none\"", LatestTurnCompleted: true}, FooterQuoted},
		{"stale", FooterInput{Message: valid, LatestTurnCompleted: true, Stale: true}, FooterStale},
		{"turn incomplete", FooterInput{Message: valid}, FooterTurnIncomplete},
		{"newer user", FooterInput{Message: valid, LatestTurnCompleted: true, NewerUserMessage: true}, FooterNewerUserMessage},
		{"contradiction", FooterInput{Message: valid, LatestTurnCompleted: true, StructuredStatus: state.StatusRunning}, FooterStructuredContradiction},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := ParseFooter(test.input)
			if got.Accepted || got.Rejection != test.want {
				t.Fatalf("ParseFooter() = %+v, want %q", got, test.want)
			}
		})
	}
}

func TestParseFooterRejectsInvalidOwnerActionMatrix(t *testing.T) {
	lines := []string{
		"🐻 complete · next (agent): create the plan",
		"🐻 complete · next (none): create the plan",
		"🐻 automation · next (external): restore the service",
		"🐻 needs input · next (agent): choose the region",
		"🐻 blocked · next (you): restore the service",
		"🐻 next steps · next (none): none",
		"🐻 next steps · next (team): create the plan",
	}
	for _, line := range lines {
		t.Run(line, func(t *testing.T) {
			got := ParseFooter(FooterInput{Message: "Response.\n" + line, LatestTurnCompleted: true})
			if got.Accepted || got.Rejection != FooterInvalidOwnerAction {
				t.Fatalf("ParseFooter() = %+v", got)
			}
		})
	}
}

func TestParseFooterRejectsWeakActions(t *testing.T) {
	actions := []string{
		"help with that",
		"I can help with that",
		"let me know if you need anything",
		"if you want choose the release region",
		"maybe create the implementation plan",
		"do that",
		"follow up later",
		"issue recorded in BEAR-19",
		"ticket filed for deployment",
		"BEAR-19 was filed for deployment",
		"deployment follow-up was captured in BEAR-19",
		"why deployment failed was documented in BEAR-19",
	}
	for _, action := range actions {
		t.Run(action, func(t *testing.T) {
			got := ParseFooter(FooterInput{Message: "Response.\n🐻 next steps · next (agent): " + action, LatestTurnCompleted: true})
			if got.Accepted || got.Rejection != FooterWeakAction {
				t.Fatalf("ParseFooter() = %+v", got)
			}
		})
	}
}

func TestParseFooterAE18CompletionDoesNotInventAction(t *testing.T) {
	got := ParseFooter(FooterInput{Message: "Finished without a warranted follow-up.\n🐻 complete · next (none): none", LatestTurnCompleted: true})
	if !got.Accepted || got.Footer.Status != state.StatusComplete || got.Footer.Action != "none" {
		t.Fatalf("ParseFooter() = %+v", got)
	}
}

func TestParseFooterKeepsRejectedFooterForClassifier(t *testing.T) {
	message := "Finished.\n🐻 next steps · next (agent): I can help with that"
	got := ParseFooter(FooterInput{Message: message, LatestTurnCompleted: true})
	if got.Accepted || got.Rejection != FooterWeakAction {
		t.Fatalf("ParseFooter() = %+v", got)
	}
	if got.ClassifierMessage != message || got.TitleMessage != "Finished." {
		t.Fatalf("classifier = %q, title = %q", got.ClassifierMessage, got.TitleMessage)
	}
}

func TestParseFooterAcceptsConcreteImperativeWithEmbeddedModal(t *testing.T) {
	got := ParseFooter(FooterInput{Message: "Analysis complete.\n🐻 next steps · next (agent): investigate whether the service could lose data", LatestTurnCompleted: true})
	if !got.Accepted || got.Footer.Status != state.StatusNextSteps {
		t.Fatalf("ParseFooter() = %+v", got)
	}
}

func TestParseFooterRecordWordsFallThroughPerRuling(t *testing.T) {
	// BEAR-16 ruling: any record-word presence leaves the deterministic path;
	// the action is weak, and classification decides. This intentionally covers
	// genuine concrete actions that mention recorded artifacts — the end state
	// is still correct via Luna, and a wrong deterministic accept never happens.
	actions := []string{
		"investigate why BEAR-19 was filed for deployment",
		"analyze captured logs for the failure cause",
		"compare recorded outcomes before release",
		"review ticket filed for deployment",
		"why deployment failed was documented in BEAR-19",
	}
	for _, action := range actions {
		t.Run(action, func(t *testing.T) {
			got := ParseFooter(FooterInput{Message: "Analysis complete.\n🐻 next steps · next (agent): " + action, LatestTurnCompleted: true})
			if got.Accepted {
				t.Fatalf("record-word action must fall through, got %+v", got)
			}
			if got.Rejection != FooterWeakAction {
				t.Fatalf("Rejection = %v, want FooterWeakAction", got.Rejection)
			}
		})
	}
}
