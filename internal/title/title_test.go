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
			got, err := Reconcile(state.TaskRecord{CapturedTitle: captured}, state.StatusComplete, "", "ignored without a subject", tokens.Display{})
			if err != nil {
				t.Fatal(err)
			}
			if got.Title != "✅" || got.DurableSubject != "" || got.ManagedAction != "" {
				t.Fatalf("Reconcile() = %+v", got)
			}
		})
	}
}

func TestReconcileStatusOnlyTitleIsFixedPointAcrossTokenPositions(t *testing.T) {
	tests := []struct {
		name    string
		display tokens.Display
	}{
		{name: "off", display: tokens.Display{}},
		{name: "start", display: tokens.Display{Position: tokens.PositionStart, Value: "1.6m"}},
		{name: "end", display: tokens.Display{Position: tokens.PositionEnd, Value: "1.6m"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			first, err := Reconcile(state.TaskRecord{CapturedTitle: "❔"}, state.StatusUnknown, "", "", test.display)
			if err != nil {
				t.Fatal(err)
			}
			record := state.TaskRecord{
				CapturedTitle: first.Title, DurableSubject: first.DurableSubject, LastAppliedTitle: first.Title,
				ManagedTokenDisplay: first.ManagedTokenDisplay, ManagedTokenPosition: first.ManagedTokenPosition,
			}
			second, err := Reconcile(record, state.StatusUnknown, "", "", test.display)
			if err != nil {
				t.Fatal(err)
			}
			if first.Title != "❔" || second.Title != first.Title || second.DurableSubject != "" || second.ManagedTokenDisplay != "" {
				t.Fatalf("first=%+v second=%+v", first, second)
			}
		})
	}
}

func TestReconcilePreservesUnownedCanonicalLookingTokenText(t *testing.T) {
	for _, test := range []struct {
		name     string
		captured string
		display  tokens.Display
		want     string
	}{
		{name: "single start", captured: "✅ 26k Execute BEAR-59", display: tokens.Display{Position: tokens.PositionStart, Value: "26k"}, want: "✅ 26k 26k Execute BEAR-59"},
		{name: "repeated start", captured: "✅ 26k 26k 26k Execute BEAR-59", display: tokens.Display{Position: tokens.PositionStart, Value: "26k"}, want: "✅ 26k 26k 26k 26k Execute BEAR-59"},
		{name: "repeated end", captured: "✅ Execute BEAR-59 · out 26k · out 26k", display: tokens.Display{Position: tokens.PositionEnd, Value: "26k"}, want: "✅ Execute BEAR-59 · out 26k · out 26k · out 26k"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := Reconcile(state.TaskRecord{CapturedTitle: test.captured}, state.StatusComplete, "", "", test.display)
			if err != nil {
				t.Fatal(err)
			}
			if got.Title != test.want {
				t.Fatalf("Reconcile() = %+v", got)
			}
		})
	}
}

func TestReconcileCleansContaminatedOwnedSubjectAcrossPositions(t *testing.T) {
	record := state.TaskRecord{
		CapturedTitle:        "✅ 26k 26k 26k Execute BEAR-59",
		DurableSubject:       "26k 26k Execute BEAR-59",
		LastAppliedTitle:     "✅ 26k 26k 26k Execute BEAR-59",
		ManagedTokenDisplay:  "26k",
		ManagedTokenPosition: tokens.PositionStart,
	}
	first, err := Reconcile(record, state.StatusComplete, "", "", tokens.Display{Position: tokens.PositionEnd, Value: "26k"})
	if err != nil {
		t.Fatal(err)
	}
	if first.Title != "✅ Execute BEAR-59 · out 26k" || first.DurableSubject != "Execute BEAR-59" {
		t.Fatalf("first = %+v", first)
	}
	second, err := Reconcile(state.TaskRecord{
		CapturedTitle: first.Title, DurableSubject: first.DurableSubject, LastAppliedTitle: first.Title,
		ManagedTokenDisplay: first.ManagedTokenDisplay, ManagedTokenPosition: first.ManagedTokenPosition,
	}, state.StatusComplete, "", "", tokens.Display{Position: tokens.PositionEnd, Value: "26k"})
	if err != nil {
		t.Fatal(err)
	}
	if second.Title != first.Title || second.DurableSubject != first.DurableSubject {
		t.Fatalf("first=%+v second=%+v", first, second)
	}

	off, err := Reconcile(record, state.StatusComplete, "", "", tokens.Display{})
	if err != nil {
		t.Fatal(err)
	}
	if off.Title != "✅ Execute BEAR-59" || off.DurableSubject != "Execute BEAR-59" {
		t.Fatalf("off = %+v", off)
	}
}

func TestReconcileRecoversCurrentDisplayWithIncompleteOrStaleOwnership(t *testing.T) {
	for _, test := range []struct {
		name    string
		record  state.TaskRecord
		display tokens.Display
		want    string
	}{
		{
			name: "incomplete current ownership",
			record: state.TaskRecord{
				CapturedTitle:    "✅ 26k Execute BEAR-59",
				LastAppliedTitle: "✅ 26k Execute BEAR-59",
			},
			display: tokens.Display{Position: tokens.PositionStart, Value: "26k"},
			want:    "✅ 26k Execute BEAR-59",
		},
		{
			name: "stale position with repeated current display",
			record: state.TaskRecord{
				CapturedTitle:        "✅ 26k 26k Execute BEAR-59",
				DurableSubject:       "26k Execute BEAR-59",
				LastAppliedTitle:     "✅ 26k 26k Execute BEAR-59",
				ManagedTokenDisplay:  "26k",
				ManagedTokenPosition: tokens.PositionEnd,
			},
			display: tokens.Display{Position: tokens.PositionStart, Value: "26k"},
			want:    "✅ 26k Execute BEAR-59",
		},
		{
			name: "stale display while moving positions",
			record: state.TaskRecord{
				CapturedTitle:        "✅ 26k 26k Execute BEAR-59",
				DurableSubject:       "26k Execute BEAR-59",
				LastAppliedTitle:     "✅ 26k 26k Execute BEAR-59",
				ManagedTokenDisplay:  "20k",
				ManagedTokenPosition: tokens.PositionStart,
			},
			display: tokens.Display{Position: tokens.PositionEnd, Value: "26k"},
			want:    "✅ Execute BEAR-59 · out 26k",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := Reconcile(test.record, state.StatusComplete, "", "", test.display)
			if err != nil {
				t.Fatal(err)
			}
			if got.Title != test.want || got.DurableSubject != "Execute BEAR-59" {
				t.Fatalf("Reconcile() = %+v", got)
			}
		})
	}
}

func TestReconcilePreservesUnownedNumericText(t *testing.T) {
	for _, test := range []struct {
		name     string
		captured string
		display  tokens.Display
		want     string
	}{
		{name: "different start value", captured: "✅ 94k Execute BEAR-59", display: tokens.Display{Position: tokens.PositionStart, Value: "26k"}, want: "✅ 26k 94k Execute BEAR-59"},
		{name: "matching value in unconfigured prefix", captured: "✅ 26k Execute BEAR-59", display: tokens.Display{Position: tokens.PositionEnd, Value: "26k"}, want: "✅ 26k Execute BEAR-59 · out 26k"},
		{name: "matching ordinary suffix", captured: "✅ Execute BEAR-59 26k", display: tokens.Display{Position: tokens.PositionStart, Value: "26k"}, want: "✅ 26k Execute BEAR-59 26k"},
		{name: "different end value", captured: "✅ Execute BEAR-59 · out 94k", display: tokens.Display{Position: tokens.PositionEnd, Value: "26k"}, want: "✅ Execute BEAR-59 · out 94k · out 26k"},
		{name: "no canonical status ownership", captured: "26k Execute BEAR-59", display: tokens.Display{Position: tokens.PositionStart, Value: "26k"}, want: "✅ 26k 26k Execute BEAR-59"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := Reconcile(state.TaskRecord{CapturedTitle: test.captured}, state.StatusComplete, "", "", test.display)
			if err != nil {
				t.Fatal(err)
			}
			if got.Title != test.want {
				t.Fatalf("Reconcile() = %+v", got)
			}
		})
	}
}

func TestReconcilePreservesOwnedSubjectNumberAcrossDisplayChanges(t *testing.T) {
	for _, test := range []struct {
		name    string
		record  state.TaskRecord
		display tokens.Display
		want    string
	}{
		{
			name: "start to end",
			record: state.TaskRecord{
				CapturedTitle:        "✅ 20k 26k Release service",
				DurableSubject:       "26k Release service",
				LastAppliedTitle:     "✅ 20k 26k Release service",
				ManagedTokenDisplay:  "20k",
				ManagedTokenPosition: tokens.PositionStart,
			},
			display: tokens.Display{Position: tokens.PositionEnd, Value: "26k"},
			want:    "✅ 26k Release service · out 26k",
		},
		{
			name: "end to start",
			record: state.TaskRecord{
				CapturedTitle:        "✅ 26k Release service · out 20k",
				DurableSubject:       "26k Release service",
				LastAppliedTitle:     "✅ 26k Release service · out 20k",
				ManagedTokenDisplay:  "20k",
				ManagedTokenPosition: tokens.PositionEnd,
			},
			display: tokens.Display{Position: tokens.PositionStart, Value: "26k"},
			want:    "✅ 26k 26k Release service",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := Reconcile(test.record, state.StatusComplete, "", "", test.display)
			if err != nil {
				t.Fatal(err)
			}
			if got.Title != test.want || got.DurableSubject != "26k Release service" {
				t.Fatalf("Reconcile() = %+v", got)
			}
		})
	}
}

func TestReconcilePreservesMatchingNumericTextWithExistingOwnership(t *testing.T) {
	for _, test := range []struct {
		name    string
		record  state.TaskRecord
		display tokens.Display
		want    string
	}{
		{
			name: "explicit single token subject with incomplete ownership",
			record: state.TaskRecord{
				CapturedTitle:    "✅ 26k Release service",
				DurableSubject:   "26k Release service",
				LastAppliedTitle: "✅ 26k Release service",
			},
			display: tokens.Display{Position: tokens.PositionStart, Value: "26k"},
			want:    "✅ 26k 26k Release service",
		},
		{
			name: "authoritative subject with incomplete ownership",
			record: state.TaskRecord{
				CapturedTitle:    "✅ 26k 26k Release service",
				DurableSubject:   "26k Release service",
				LastAppliedTitle: "✅ 26k 26k Release service",
			},
			display: tokens.Display{Position: tokens.PositionStart, Value: "26k"},
			want:    "✅ 26k 26k Release service",
		},
		{
			name: "authoritative start subject while disabling",
			record: state.TaskRecord{
				CapturedTitle:        "✅ 26k 26k Release service",
				DurableSubject:       "26k Release service",
				LastAppliedTitle:     "✅ 26k 26k Release service",
				ManagedTokenDisplay:  "26k",
				ManagedTokenPosition: tokens.PositionStart,
			},
			want: "✅ 26k Release service",
		},
		{
			name: "authoritative end subject while disabling",
			record: state.TaskRecord{
				CapturedTitle:        "✅ Release service · out 26k · out 26k",
				DurableSubject:       "Release service · out 26k",
				LastAppliedTitle:     "✅ Release service · out 26k · out 26k",
				ManagedTokenDisplay:  "26k",
				ManagedTokenPosition: tokens.PositionEnd,
			},
			want: "✅ Release service · out 26k",
		},
		{
			name: "user edit at start boundary",
			record: state.TaskRecord{
				CapturedTitle:        "✅ 26k 26k User subject",
				LastAppliedTitle:     "✅ 26k Old subject",
				ManagedTokenDisplay:  "26k",
				ManagedTokenPosition: tokens.PositionStart,
			},
			display: tokens.Display{Position: tokens.PositionStart, Value: "26k"},
			want:    "✅ 26k 26k User subject",
		},
		{
			name: "user edit at end boundary",
			record: state.TaskRecord{
				CapturedTitle:        "✅ User subject · out 26k · out 26k",
				LastAppliedTitle:     "✅ Old subject · out 26k",
				ManagedTokenDisplay:  "26k",
				ManagedTokenPosition: tokens.PositionEnd,
			},
			display: tokens.Display{Position: tokens.PositionEnd, Value: "26k"},
			want:    "✅ User subject · out 26k · out 26k",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := Reconcile(test.record, state.StatusComplete, "", "", test.display)
			if err != nil {
				t.Fatal(err)
			}
			if got.Title != test.want {
				t.Fatalf("Reconcile() = %+v", got)
			}
		})
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

func TestAdoptSingleLeadingStatusPreservesCompleteRemainder(t *testing.T) {
	status, subject, ok := AdoptSingleLeadingStatus("✅ 26k User subject → user action")
	if !ok || status != state.StatusComplete || subject != "26k User subject → user action" {
		t.Fatalf("adoption = %q %q %t", status, subject, ok)
	}
	for _, title := range []string{"Release service", "✅ ❔ Release service", "✅✅ Release service"} {
		if _, _, ok := AdoptSingleLeadingStatus(title); ok {
			t.Fatalf("adopted non-strict title %q", title)
		}
	}
}
