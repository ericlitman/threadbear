package assets

import (
	"strings"
	"testing"
)

func TestManagedGuidanceUsesConcreteFooterExamples(t *testing.T) {
	examples := []string{
		"🧵🐻 complete · next (none): none",
		"🧵🐻 next steps · next (you): approve the release plan",
		"🧵🐻 next steps · next (agent): implement the approved plan",
		"🧵🐻 next steps · next (external): review the security exception",
		"🧵🐻 needs input · next (you): choose the release region",
		"🧵🐻 blocked · next (external): restore the signing service",
		"🧵🐻 automation · next (none): none",
	}
	for _, example := range examples {
		if !strings.Contains(AgentsManagedContent, example) {
			t.Errorf("managed AGENTS content is missing %q", example)
		}
		if !strings.Contains(SkillManagedContent, example) {
			t.Errorf("managed skill content is missing %q", example)
		}
	}
	if strings.Contains(AgentsManagedContent, "🧵🐻 STATUS") {
		t.Fatal("managed AGENTS content contains a fill-in footer")
	}
}
