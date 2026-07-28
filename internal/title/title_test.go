package title

import (
	"errors"
	"testing"

	"github.com/ericlitman/threadbear/internal/state"
	"github.com/ericlitman/threadbear/internal/tokens"
)

func TestReconcileRendersEveryStatusEmoji(t *testing.T) {
	tests := []struct {
		status state.TaskStatus
		emoji  string
	}{
		{state.StatusRunning, "⏳"}, {state.StatusBlocked, "🚨"}, {state.StatusNeedsInput, "🙋"},
		{state.StatusAutomation, "🤖"}, {state.StatusNextSteps, "➡️"}, {state.StatusComplete, "✅"}, {state.StatusUnknown, "❔"},
	}
	for _, test := range tests {
		t.Run(string(test.status), func(t *testing.T) {
			got, err := Reconcile(state.TaskRecord{CapturedTitle: "Release service"}, test.status, "", "", tokens.Display{})
			if err != nil {
				t.Fatal(err)
			}
			if got.Title != test.emoji+" Release service" || got.DurableSubject != "Release service" || got.ManagedAction != "" {
				t.Fatalf("Reconcile() = %+v", got)
			}
		})
	}
}

func TestReconcileAE2CanonicalizesRepeatedKnownPrefixes(t *testing.T) {
	got, err := Reconcile(state.TaskRecord{CapturedTitle: "✅ ❔ ✅ Release service"}, state.StatusRunning, "", "", tokens.Display{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "⏳ Release service" || got.DurableSubject != "Release service" {
		t.Fatalf("Reconcile() = %+v", got)
	}
}

func TestReconcileExactOwnershipReplacesAndRemovesManagedAction(t *testing.T) {
	record := state.TaskRecord{CapturedTitle: "➡️ Release service → review the rollout", DurableSubject: "Release service", ManagedAction: "review the rollout", LastAppliedTitle: "➡️ Release service → review the rollout"}
	replaced, err := Reconcile(record, state.StatusNeedsInput, "", "choose the release region", tokens.Display{})
	if err != nil {
		t.Fatal(err)
	}
	if replaced.Title != "🙋 Release service → choose the release region" || replaced.DurableSubject != "Release service" {
		t.Fatalf("replacement = %+v", replaced)
	}
	removed, err := Reconcile(record, state.StatusComplete, "", "", tokens.Display{})
	if err != nil {
		t.Fatal(err)
	}
	if removed.Title != "✅ Release service" || removed.ManagedAction != "" {
		t.Fatalf("removal = %+v", removed)
	}
}

func TestReconcileUserEditAdoptsEntireNonStatusRemainder(t *testing.T) {
	record := state.TaskRecord{CapturedTitle: "✅ User subject → this arrow text is mine", DurableSubject: "Old subject", ManagedAction: "old managed action", LastAppliedTitle: "➡️ Old subject → old managed action"}
	got, err := Reconcile(record, state.StatusNextSteps, "", "create the implementation plan", tokens.Display{})
	if err != nil {
		t.Fatal(err)
	}
	wantSubject := "User subject → this arrow text is mine"
	if got.DurableSubject != wantSubject || got.Title != "➡️ "+wantSubject+" → create the implementation plan" {
		t.Fatalf("Reconcile() = %+v", got)
	}
}

func TestReconcileDoesNotInferOwnershipFromMatchingShape(t *testing.T) {
	record := state.TaskRecord{CapturedTitle: "➡️ New subject → user-authored suffix", DurableSubject: "New subject", ManagedAction: "user-authored suffix", LastAppliedTitle: "➡️ Different subject → user-authored suffix"}
	got, err := Reconcile(record, state.StatusComplete, "", "", tokens.Display{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "✅ New subject → user-authored suffix" || got.DurableSubject != "New subject → user-authored suffix" {
		t.Fatalf("Reconcile() = %+v", got)
	}
}

func TestReconcileDoesNotAccumulateActions(t *testing.T) {
	record := state.TaskRecord{CapturedTitle: "➡️ Requirements review → pressure-test the requirements before implementation", DurableSubject: "Requirements review", ManagedAction: "pressure-test the requirements before implementation", LastAppliedTitle: "➡️ Requirements review → pressure-test the requirements before implementation"}
	got, err := Reconcile(record, state.StatusNextSteps, "", "create the implementation plan", tokens.Display{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "➡️ Requirements review → create the implementation plan" {
		t.Fatalf("Reconcile() = %+v", got)
	}
}

func TestReconcileRendersSingleStatusEmojiWithoutSubject(t *testing.T) {
	for _, captured := range []string{"⏳", "🚨", "🙋", "🤖", "➡️", "✅", "❔"} {
		t.Run(captured, func(t *testing.T) {
			got, err := Reconcile(
				state.TaskRecord{CapturedTitle: captured},
				state.StatusComplete,
				"",
				"ignored without a subject",
				tokens.Display{Position: tokens.PositionStart, Value: "1.6m"},
			)
			if err != nil {
				t.Fatal(err)
			}
			if got.Title != "✅ 1.6m" || got.DurableSubject != "" || got.ManagedAction != "" {
				t.Fatalf("Reconcile() = %+v", got)
			}
		})
	}

	end, err := Reconcile(
		state.TaskRecord{CapturedTitle: "❔"},
		state.StatusUnknown,
		"",
		"",
		tokens.Display{Position: tokens.PositionEnd, Value: "1.6m"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if end.Title != "❔ · out 1.6m" || end.ManagedTokenPosition != tokens.PositionEnd {
		t.Fatalf("end = %+v", end)
	}
}

func TestReconcileRejectsInvalidInputs(t *testing.T) {
	if _, err := Reconcile(state.TaskRecord{CapturedTitle: "Subject"}, state.TaskStatus("future"), "", "", tokens.Display{}); !errors.Is(err, ErrInvalidStatus) {
		t.Fatalf("invalid status error = %v", err)
	}
	for _, captured := range []string{"", "✅ ❔"} {
		if _, err := Reconcile(state.TaskRecord{CapturedTitle: captured}, state.StatusComplete, "", "", tokens.Display{}); !errors.Is(err, ErrEmptySubject) {
			t.Fatalf("empty subject for %q error = %v", captured, err)
		}
	}
}

func TestReconcileRequiresExactLastAppliedTitleMatch(t *testing.T) {
	record := state.TaskRecord{
		CapturedTitle:    "  ➡️ User subject → user suffix  ",
		DurableSubject:   "Managed subject",
		ManagedAction:    "user suffix",
		LastAppliedTitle: "➡️ User subject → user suffix",
	}
	got, err := Reconcile(record, state.StatusComplete, "", "", tokens.Display{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "✅ User subject → user suffix" || got.DurableSubject != "User subject → user suffix" {
		t.Fatalf("Reconcile() = %+v", got)
	}
}

func TestReconcileUsesSuggestedSubjectOnlyWhenNoUserSubjectExists(t *testing.T) {
	got, err := Reconcile(state.TaskRecord{}, state.StatusNextSteps, "Release readiness", "review the rollout", tokens.Display{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "➡️ Release readiness → review the rollout" || got.DurableSubject != "Release readiness" {
		t.Fatalf("Reconcile() = %+v", got)
	}

	preserved, err := Reconcile(state.TaskRecord{CapturedTitle: "User release title"}, state.StatusNextSteps, "Generated title", "review the rollout", tokens.Display{})
	if err != nil {
		t.Fatal(err)
	}
	if preserved.DurableSubject != "User release title" {
		t.Fatalf("Reconcile() = %+v", preserved)
	}
}

func TestReconcileRecoversOwnedSubjectFromExactManagedTitle(t *testing.T) {
	record := state.TaskRecord{
		CapturedTitle:    "➡️ Release service → review the rollout",
		ManagedAction:    "review the rollout",
		LastAppliedTitle: "➡️ Release service → review the rollout",
	}
	got, err := Reconcile(record, state.StatusComplete, "", "", tokens.Display{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "✅ Release service" || got.DurableSubject != "Release service" {
		t.Fatalf("Reconcile() = %+v", got)
	}
}

func TestReconcileNormalizesSuggestedSubjectPrefix(t *testing.T) {
	got, err := Reconcile(state.TaskRecord{}, state.StatusRunning, "✅ Release service", "", tokens.Display{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "⏳ Release service" || got.DurableSubject != "Release service" {
		t.Fatalf("Reconcile() = %+v", got)
	}
}

func TestReconcilePlacesOutputTokensInManagedZones(t *testing.T) {
	start, err := Reconcile(
		state.TaskRecord{CapturedTitle: "Release service"},
		state.StatusBlocked,
		"",
		"",
		tokens.Display{Position: tokens.PositionStart, Value: "1.6m"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if start.Title != "🚨 1.6m Release service" || start.ManagedTokenDisplay != "1.6m" || start.ManagedTokenPosition != tokens.PositionStart {
		t.Fatalf("start = %+v", start)
	}

	end, err := Reconcile(
		state.TaskRecord{CapturedTitle: "Release service"},
		state.StatusNextSteps,
		"",
		"review the rollout",
		tokens.Display{Position: tokens.PositionEnd, Value: "1.6m"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if end.Title != "➡️ Release service → review the rollout · out 1.6m" || end.ManagedTokenDisplay != "1.6m" || end.ManagedTokenPosition != tokens.PositionEnd {
		t.Fatalf("end = %+v", end)
	}
}

func TestReconcilePreservesUserEditedSubjectAcrossTokenUpdate(t *testing.T) {
	record := state.TaskRecord{
		CapturedTitle:        "🚨 1.2m User-edited subject",
		DurableSubject:       "Old subject",
		LastAppliedTitle:     "🚨 1.2m Old subject",
		ManagedTokenDisplay:  "1.2m",
		ManagedTokenPosition: tokens.PositionStart,
	}
	got, err := Reconcile(
		record,
		state.StatusBlocked,
		"",
		"",
		tokens.Display{Position: tokens.PositionStart, Value: "1.6m"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "🚨 1.6m User-edited subject" || got.DurableSubject != "User-edited subject" {
		t.Fatalf("Reconcile() = %+v", got)
	}
}

func TestReconcileDisablingTokensRemovesOnlyManagedFigure(t *testing.T) {
	record := state.TaskRecord{
		CapturedTitle:        "➡️ Release service → review the rollout · out 1.6m",
		DurableSubject:       "Release service",
		ManagedAction:        "review the rollout",
		LastAppliedTitle:     "➡️ Release service → review the rollout · out 1.6m",
		ManagedTokenDisplay:  "1.6m",
		ManagedTokenPosition: tokens.PositionEnd,
	}
	got, err := Reconcile(record, state.StatusNextSteps, "", "review the rollout", tokens.Display{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "➡️ Release service → review the rollout" || got.DurableSubject != "Release service" || got.ManagedTokenDisplay != "" {
		t.Fatalf("Reconcile() = %+v", got)
	}
}
