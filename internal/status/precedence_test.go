package status

import (
	"testing"

	"github.com/ericlitman/threadbear/internal/state"
)

func TestResolvePrecedence(t *testing.T) {
	validFooter := FooterInput{Message: "Finished.\n🧵🐻 complete", LatestTurnCompleted: true}
	tests := []struct {
		name       string
		facts      Facts
		status     state.TaskStatus
		provenance state.Provenance
		resolved   bool
	}{
		{name: "waiting outranks all", facts: Facts{WaitingForUser: true, RuntimeActive: true, StructuredFailure: true, HealthyIdleAutomation: true, Interrupted: true, Footer: validFooter}, status: state.StatusNeedsInput, provenance: state.ProvenanceRuntime, resolved: true},
		{name: "AE2 runtime", facts: Facts{RuntimeActive: true, StructuredFailure: true, Footer: validFooter}, status: state.StatusRunning, provenance: state.ProvenanceRuntime, resolved: true},
		{name: "AE3 structured failure", facts: Facts{StructuredFailure: true, Footer: validFooter}, status: state.StatusBlocked, provenance: state.ProvenanceStructuredError, resolved: true},
		{name: "automation", facts: Facts{HealthyIdleAutomation: true, Interrupted: true, Footer: validFooter}, status: state.StatusAutomation, provenance: state.ProvenanceAutomation, resolved: true},
		{name: "AE13 interruption", facts: Facts{Interrupted: true, Footer: validFooter}, status: state.StatusUnknown, provenance: state.ProvenanceInterruption, resolved: true},
		{name: "footer", facts: Facts{Footer: validFooter}, status: state.StatusComplete, provenance: state.ProvenanceFooter, resolved: true},
		{name: "AE4 prose unresolved", facts: Facts{Footer: FooterInput{Message: "Another task remains blocked, but this request is finished.", LatestTurnCompleted: true}}, resolved: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := Resolve(test.facts)
			if got.Resolved != test.resolved || got.Status != test.status || got.Provenance != test.provenance {
				t.Fatalf("Resolve() = %+v", got)
			}
		})
	}
}

func TestResolveStructuredEvidenceStillRemovesFooterFromMessage(t *testing.T) {
	got := Resolve(Facts{
		StructuredFailure: true,
		Footer: FooterInput{
			Message:             "Quoted success is not authoritative.\n🧵🐻 complete",
			LatestTurnCompleted: true,
		},
	})
	if got.Status != state.StatusBlocked || got.TitleMessage != "Quoted success is not authoritative." || got.ClassifierMessage == got.TitleMessage {
		t.Fatalf("Resolve() = %+v", got)
	}
}

func TestResolveUnresolvedIsNotPersistedUnknown(t *testing.T) {
	got := Resolve(Facts{})
	if got.Resolved || got.Status == state.StatusUnknown || got.Provenance == state.ProvenanceUnknown {
		t.Fatalf("unresolved result = %+v", got)
	}
}

func TestResolveAE16WaitingOutranksRecommendation(t *testing.T) {
	got := Resolve(Facts{WaitingForUser: true, Footer: FooterInput{Message: "Choose before I can finish.\n🧵🐻 next steps (agent): create the implementation plan", LatestTurnCompleted: true}})
	if got.Status != state.StatusNeedsInput || got.Provenance != state.ProvenanceRuntime {
		t.Fatalf("Resolve() = %+v", got)
	}
}
