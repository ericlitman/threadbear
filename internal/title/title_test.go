package title

import (
	"errors"
	"strings"
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
	got, err := Reconcile(record, state.StatusNextSteps, "", "create plan", tokens.Display{})
	if err != nil {
		t.Fatal(err)
	}
	wantSubject := "User subject → this arrow text is mine"
	if got.DurableSubject != wantSubject || got.Title != "➡️ "+wantSubject+" → create plan" {
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
	got, err := Reconcile(record, state.StatusNextSteps, "", "create plan", tokens.Display{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "➡️ Requirements review → create plan" {
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
		{name: "repeated end", captured: "✅ Execute BEAR-59 · out 26k · out 26k", display: tokens.Display{Position: tokens.PositionEnd, Value: "26k"}, want: "✅ Execute BEAR-59 · out 26k · out 26k · 26k"},
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

func TestReconcileReplacesManagedEndDisplayBeforePersistedEllipsis(t *testing.T) {
	const captured = "❔ BEAR-59 — validate post-turn title actuator · out 670k ·…"
	record := state.TaskRecord{
		CapturedTitle:        captured,
		DurableSubject:       "BEAR-59 — validate post-turn title actuator",
		LastAppliedTitle:     "❔ BEAR-59 — validate post-turn title actuator · out 670k",
		ManagedTokenDisplay:  "670k",
		ManagedTokenPosition: tokens.PositionEnd,
	}
	first, err := Reconcile(record, state.StatusUnknown, "", "", tokens.Display{Position: tokens.PositionEnd, Value: "700k"})
	if err != nil {
		t.Fatal(err)
	}
	if first.Title != "❔ BEAR-59 — validate post-turn title actuator ·… · 700k" || first.DurableSubject != "BEAR-59 — validate post-turn title actuator ·…" {
		t.Fatalf("first = %+v", first)
	}
	second, err := Reconcile(state.TaskRecord{
		CapturedTitle: first.Title, DurableSubject: first.DurableSubject, LastAppliedTitle: first.Title,
		ManagedTokenDisplay: first.ManagedTokenDisplay, ManagedTokenPosition: first.ManagedTokenPosition,
	}, state.StatusUnknown, "", "", tokens.Display{Position: tokens.PositionEnd, Value: "700k"})
	if err != nil {
		t.Fatal(err)
	}
	if second != first {
		t.Fatalf("first=%+v second=%+v", first, second)
	}
}

func TestReconcileRemovesManagedBoundaryTokenContamination(t *testing.T) {
	for _, test := range []struct {
		name      string
		record    state.TaskRecord
		position  tokens.Position
		display   string
		suggested string
		want      string
		subject   string
	}{
		{
			name: "same start display",
			record: state.TaskRecord{
				CapturedTitle: "🚨 52k 52k Run SYMPH-294 pilot", DurableSubject: "52k Run SYMPH-294 pilot",
				LastAppliedTitle: "🚨 52k 52k Run SYMPH-294 pilot", ManagedTokenDisplay: "52k", ManagedTokenPosition: tokens.PositionStart,
				Status: state.StatusBlocked,
			},
			position: tokens.PositionStart, display: "52k",
			suggested: "Run SYMPH-294 pilot",
			want:      "🚨 52k Run SYMPH-294 pilot", subject: "Run SYMPH-294 pilot",
		},
		{
			name: "settled changed start display",
			record: state.TaskRecord{
				CapturedTitle: "✅ 220k 210k Improve ThreadBear onboarding", DurableSubject: "210k Improve ThreadBear onboarding",
				LastAppliedTitle: "✅ 220k 210k Improve ThreadBear onboarding", ManagedTokenDisplay: "220k", ManagedTokenPosition: tokens.PositionStart,
				Status: state.StatusComplete,
			},
			position: tokens.PositionStart, display: "220k",
			suggested: "Improve ThreadBear onboarding",
			want:      "✅ 220k Improve ThreadBear onboarding", subject: "Improve ThreadBear onboarding",
		},
		{
			name: "settled changed end display",
			record: state.TaskRecord{
				CapturedTitle: "✅ Release service · 210k · 220k", DurableSubject: "Release service · 210k",
				LastAppliedTitle: "✅ Release service · 210k · 220k", ManagedTokenDisplay: "220k", ManagedTokenPosition: tokens.PositionEnd,
				Status: state.StatusComplete,
			},
			position: tokens.PositionEnd, display: "220k",
			suggested: "Release service",
			want:      "✅ Release service · 220k", subject: "Release service",
		},
		{
			name: "contamination before legitimate numeric start subject",
			record: state.TaskRecord{
				CapturedTitle: "✅ 220k 220k 210k Improve ThreadBear onboarding", DurableSubject: "220k 210k Improve ThreadBear onboarding",
				LastAppliedTitle: "✅ 220k 220k 210k Improve ThreadBear onboarding", ManagedTokenDisplay: "220k", ManagedTokenPosition: tokens.PositionStart,
				Status: state.StatusComplete,
			},
			position: tokens.PositionStart, display: "220k",
			suggested: "210k Improve ThreadBear onboarding",
			want:      "✅ 220k 210k Improve ThreadBear onboarding", subject: "210k Improve ThreadBear onboarding",
		},
		{
			name: "contamination after legitimate numeric end subject",
			record: state.TaskRecord{
				CapturedTitle: "✅ Release service · 210k · 220k · 220k", DurableSubject: "Release service · 210k · 220k",
				LastAppliedTitle: "✅ Release service · 210k · 220k · 220k", ManagedTokenDisplay: "220k", ManagedTokenPosition: tokens.PositionEnd,
				Status: state.StatusComplete,
			},
			position: tokens.PositionEnd, display: "220k",
			suggested: "Release service · 210k",
			want:      "✅ Release service · 210k · 220k", subject: "Release service · 210k",
		},
		{
			name: "legitimate numeric subject",
			record: state.TaskRecord{
				CapturedTitle: "✅ 20k 26k Release service", DurableSubject: "26k Release service",
				LastAppliedTitle: "✅ 20k 26k Release service", ManagedTokenDisplay: "20k", ManagedTokenPosition: tokens.PositionStart,
				Status: state.StatusComplete,
			},
			position:  tokens.PositionStart,
			display:   "20k",
			suggested: "26k Release service",
			want:      "✅ 20k 26k Release service",
			subject:   "26k Release service",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			first, err := Reconcile(test.record, test.record.Status, test.suggested, "", tokens.Display{Position: test.position, Value: test.display})
			if err != nil {
				t.Fatal(err)
			}
			if first.Title != test.want || first.DurableSubject != test.subject {
				t.Fatalf("first = %+v", first)
			}
			second, err := Reconcile(state.TaskRecord{
				CapturedTitle: first.Title, DurableSubject: first.DurableSubject, LastAppliedTitle: first.Title,
				ManagedTokenDisplay: first.ManagedTokenDisplay, ManagedTokenPosition: first.ManagedTokenPosition,
				Status: test.record.Status,
			}, test.record.Status, "", "", tokens.Display{Position: test.position, Value: test.display})
			if err != nil {
				t.Fatal(err)
			}
			if second != first {
				t.Fatalf("first=%+v second=%+v", first, second)
			}
		})
	}
}

func TestSubjectNeedsClassificationOnlyForUntouchedTokenBoundaries(t *testing.T) {
	owned := state.TaskRecord{
		CapturedTitle: "✅ 20k 26k Release service", DurableSubject: "26k Release service",
		LastAppliedTitle: "✅ 20k 26k Release service", ManagedTokenDisplay: "20k", ManagedTokenPosition: tokens.PositionStart,
	}
	for _, test := range []struct {
		name     string
		record   state.TaskRecord
		current  string
		position tokens.Position
		want     bool
	}{
		{name: "new status title with numeric boundary", current: "✅ 404 Investigate outage", position: tokens.PositionStart, want: true},
		{name: "exact managed numeric subject", record: owned, current: owned.CapturedTitle, position: tokens.PositionStart, want: true},
		{name: "Luna record can still contain legacy contamination", record: state.TaskRecord{CapturedTitle: owned.CapturedTitle, DurableSubject: owned.DurableSubject, LastAppliedTitle: owned.LastAppliedTitle, ManagedTokenDisplay: owned.ManagedTokenDisplay, ManagedTokenPosition: owned.ManagedTokenPosition, Provenance: state.ProvenanceLuna}, current: owned.CapturedTitle, position: tokens.PositionStart, want: true},
		{name: "exact managed clean subject", record: state.TaskRecord{CapturedTitle: "✅ 20k Release service", DurableSubject: "Release service", LastAppliedTitle: "✅ 20k Release service", ManagedTokenDisplay: "20k", ManagedTokenPosition: tokens.PositionStart}, current: "✅ 20k Release service", position: tokens.PositionStart},
		{name: "divergent user edit", record: owned, current: "✅ 20k 404 Investigate outage", position: tokens.PositionStart},
		{name: "no status ownership", current: "404 Investigate outage", position: tokens.PositionStart},
		{name: "other configured boundary", current: "✅ 404 Investigate outage", position: tokens.PositionEnd},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := SubjectNeedsClassification(test.record, test.current, test.position); got != test.want {
				t.Fatalf("SubjectNeedsClassification() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestReconcileConvergesOwnedTokenDuplicatesAcrossPositionMigration(t *testing.T) {
	for _, test := range []struct {
		name    string
		record  state.TaskRecord
		display tokens.Display
		want    string
		subject string
	}{
		{
			name: "start to end",
			record: state.TaskRecord{
				CapturedTitle:        "➡️ 1.1m 1.1m 1.1m Stabilize Linear CLI … · out 1.1m",
				DurableSubject:       "1.1m 1.1m 1.1m Stabilize Linear CLI …",
				LastAppliedTitle:     "➡️ 1.1m 1.1m 1.1m Stabilize Linear CLI … · out 1.1m",
				ManagedTokenDisplay:  "1.1m",
				ManagedTokenPosition: tokens.PositionEnd,
			},
			display: tokens.Display{Position: tokens.PositionEnd, Value: "1.1m"},
			want:    "➡️ Stabilize Linear CLI … · 1.1m",
			subject: "Stabilize Linear CLI …",
		},
		{
			name: "end to start",
			record: state.TaskRecord{
				CapturedTitle:        "➡️ 1.1m Stabilize Linear CLI … · out 1.1m · out 1.1m · out 1.1m",
				DurableSubject:       "Stabilize Linear CLI … · out 1.1m · out 1.1m · out 1.1m",
				LastAppliedTitle:     "➡️ 1.1m Stabilize Linear CLI … · out 1.1m · out 1.1m · out 1.1m",
				ManagedTokenDisplay:  "1.1m",
				ManagedTokenPosition: tokens.PositionStart,
			},
			display: tokens.Display{Position: tokens.PositionStart, Value: "1.1m"},
			want:    "➡️ 1.1m Stabilize Linear CLI …",
			subject: "Stabilize Linear CLI …",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			first, err := Reconcile(test.record, state.StatusNextSteps, "", "", test.display)
			if err != nil {
				t.Fatal(err)
			}
			if first.Title != test.want || first.DurableSubject != test.subject {
				t.Fatalf("first = %+v", first)
			}
			second, err := Reconcile(state.TaskRecord{
				CapturedTitle: first.Title, DurableSubject: first.DurableSubject, LastAppliedTitle: first.Title,
				ManagedTokenDisplay: first.ManagedTokenDisplay, ManagedTokenPosition: first.ManagedTokenPosition,
			}, state.StatusNextSteps, "", "", test.display)
			if err != nil {
				t.Fatal(err)
			}
			if second != first {
				t.Fatalf("first=%+v second=%+v", first, second)
			}
		})
	}
}

func TestReconcileRecognizesExactCodexShortening(t *testing.T) {
	const oldDisplay = "330k"
	fullSubject := oldDisplay + " " + oldDisplay + " " + strings.Repeat("Long managed subject ", 4)
	lastApplied := "➡️ " + fullSubject + " · out " + oldDisplay
	record := state.TaskRecord{
		CapturedTitle:        truncateUTF16(lastApplied, 59) + "…",
		DurableSubject:       fullSubject,
		LastAppliedTitle:     lastApplied,
		ManagedTokenDisplay:  oldDisplay,
		ManagedTokenPosition: tokens.PositionEnd,
	}
	first, err := Reconcile(record, state.StatusNextSteps, "", "review the complete retained action", tokens.Display{Position: tokens.PositionEnd, Value: "340k"})
	if err != nil {
		t.Fatal(err)
	}
	wantSubject := strings.TrimSpace(strings.TrimPrefix(fullSubject, oldDisplay+" "+oldDisplay+" "))
	if first.DurableSubject != wantSubject || first.ManagedAction != "review the complete retained action" || !strings.HasSuffix(first.Title, " · 340k") || utf16Units(first.Title) > 60 {
		t.Fatalf("first = %+v units=%d", first, utf16Units(first.Title))
	}
	second, err := Reconcile(state.TaskRecord{
		CapturedTitle: first.Title, DurableSubject: first.DurableSubject, ManagedAction: first.ManagedAction, LastAppliedTitle: first.Title,
		ManagedTokenDisplay: first.ManagedTokenDisplay, ManagedTokenPosition: first.ManagedTokenPosition,
	}, state.StatusNextSteps, "", first.ManagedAction, tokens.Display{Position: tokens.PositionEnd, Value: "340k"})
	if err != nil {
		t.Fatal(err)
	}
	if second != first {
		t.Fatalf("first=%+v second=%+v", first, second)
	}
}

func TestReconcilePreservesArbitraryDivergentShortTitle(t *testing.T) {
	lastApplied := "✅ " + strings.Repeat("Managed title ", 6) + " · out 26k"
	captured := truncateUTF16(lastApplied, 59) + "… user edit"
	record := state.TaskRecord{
		CapturedTitle: captured, DurableSubject: "Old subject", LastAppliedTitle: lastApplied,
		ManagedTokenDisplay: "26k", ManagedTokenPosition: tokens.PositionEnd,
	}
	got, err := Reconcile(record, state.StatusComplete, "", "", tokens.Display{Position: tokens.PositionEnd, Value: "26k"})
	if err != nil {
		t.Fatal(err)
	}
	if got.DurableSubject != stripStatusPrefixes(captured) {
		t.Fatalf("Reconcile() = %+v", got)
	}
}

func TestReconcileBoundsUTF16AndRetainsFullState(t *testing.T) {
	subject := strings.Repeat("Launch 😀 service ", 6)
	action := strings.Repeat("review 😀 rollout ", 4)
	got, err := Reconcile(state.TaskRecord{CapturedTitle: subject}, state.StatusNextSteps, "", action, tokens.Display{Position: tokens.PositionEnd, Value: "1.6m"})
	if err != nil {
		t.Fatal(err)
	}
	if utf16Units(got.Title) > 60 || !strings.HasSuffix(got.Title, " · 1.6m") || got.DurableSubject != strings.TrimSpace(subject) || got.ManagedAction != strings.TrimSpace(action) {
		t.Fatalf("Reconcile() = %+v units=%d", got, utf16Units(got.Title))
	}
	if got := truncateUTF16(strings.Repeat("a", 58)+"😀", 59); got != strings.Repeat("a", 58) {
		t.Fatalf("surrogate split = %q", got)
	}
}

func TestOwnedTokenBoundariesRecognizeLegacyCanonicalAndUnicodeWhitespace(t *testing.T) {
	for _, value := range []string{
		"Release · out 26k",
		"Release · 26k",
		"Release · out 26k · 26k",
		"Release · 26k · out 26k",
	} {
		if got := stripOwnedTokenCopies(value, "26k", tokens.PositionEnd); got != "Release" {
			t.Fatalf("stripOwnedTokenCopies(%q) = %q", value, got)
		}
	}
	const prefixed = "26k\u200326k\tRelease"
	if ownedTokenCopies(prefixed, "26k", tokens.PositionStart) != 2 || stripOwnedTokenCopies(prefixed, "26k", tokens.PositionStart) != "Release" {
		t.Fatalf("unicode prefix was not recognized")
	}
}

func TestReconcilePreservesSingleMatchingTokenTextAtOppositeBoundary(t *testing.T) {
	for _, test := range []struct {
		name    string
		record  state.TaskRecord
		display tokens.Display
	}{
		{
			name: "single start subject with end ownership",
			record: state.TaskRecord{
				CapturedTitle:        "✅ 26k Release service · 26k",
				DurableSubject:       "26k Release service",
				LastAppliedTitle:     "✅ 26k Release service · 26k",
				ManagedTokenDisplay:  "26k",
				ManagedTokenPosition: tokens.PositionEnd,
			},
			display: tokens.Display{Position: tokens.PositionEnd, Value: "26k"},
		},
		{
			name: "single end subject with start ownership",
			record: state.TaskRecord{
				CapturedTitle:        "✅ 26k Release service · 26k",
				DurableSubject:       "Release service · 26k",
				LastAppliedTitle:     "✅ 26k Release service · 26k",
				ManagedTokenDisplay:  "26k",
				ManagedTokenPosition: tokens.PositionStart,
			},
			display: tokens.Display{Position: tokens.PositionStart, Value: "26k"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := Reconcile(test.record, state.StatusComplete, "", "", test.display)
			if err != nil {
				t.Fatal(err)
			}
			if got.Title != test.record.CapturedTitle || got.DurableSubject != test.record.DurableSubject {
				t.Fatalf("Reconcile() = %+v", got)
			}
		})
	}
}

func TestReconcilePreservesOppositeBoundaryCopiesAfterUserEdit(t *testing.T) {
	for _, test := range []struct {
		name     string
		captured string
		want     string
		subject  string
	}{
		{
			name:     "repeated managed value",
			captured: "✅ 26k 26k User subject · custom · out 26k",
			want:     "✅ 26k 26k User subject · custom · 26k",
			subject:  "26k 26k User subject · custom",
		},
		{
			name:     "single matching value",
			captured: "✅ 26k User subject · custom · out 26k",
			want:     "✅ 26k User subject · custom · 26k",
			subject:  "26k User subject · custom",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			record := state.TaskRecord{
				CapturedTitle:        test.captured,
				DurableSubject:       "Old subject",
				LastAppliedTitle:     "✅ Old subject · out 26k",
				ManagedTokenDisplay:  "26k",
				ManagedTokenPosition: tokens.PositionEnd,
			}
			got, err := Reconcile(record, state.StatusComplete, "", "", tokens.Display{Position: tokens.PositionEnd, Value: "26k"})
			if err != nil {
				t.Fatal(err)
			}
			if got.Title != test.want || got.DurableSubject != test.subject {
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
	if first.Title != "✅ 26k 26k Execute BEAR-59 · 26k" || first.DurableSubject != "26k 26k Execute BEAR-59" {
		t.Fatalf("first = %+v", first)
	}
	second, err := Reconcile(state.TaskRecord{
		CapturedTitle: first.Title, DurableSubject: first.DurableSubject, LastAppliedTitle: first.Title,
		ManagedTokenDisplay: first.ManagedTokenDisplay, ManagedTokenPosition: first.ManagedTokenPosition,
	}, state.StatusComplete, "", "", tokens.Display{Position: tokens.PositionEnd, Value: "26k"})
	if err != nil {
		t.Fatal(err)
	}
	if second.Title != "✅ Execute BEAR-59 · 26k" || second.DurableSubject != "Execute BEAR-59" {
		t.Fatalf("first=%+v second=%+v", first, second)
	}
	third, err := Reconcile(state.TaskRecord{
		CapturedTitle: second.Title, DurableSubject: second.DurableSubject, LastAppliedTitle: second.Title,
		ManagedTokenDisplay: second.ManagedTokenDisplay, ManagedTokenPosition: second.ManagedTokenPosition,
	}, state.StatusComplete, "", "", tokens.Display{Position: tokens.PositionEnd, Value: "26k"})
	if err != nil {
		t.Fatal(err)
	}
	if third != second {
		t.Fatalf("second=%+v third=%+v", second, third)
	}

	off, err := Reconcile(record, state.StatusComplete, "", "", tokens.Display{})
	if err != nil {
		t.Fatal(err)
	}
	if off.Title != "✅ 26k 26k Execute BEAR-59" || off.DurableSubject != "26k 26k Execute BEAR-59" {
		t.Fatalf("off = %+v", off)
	}
}

func TestReconcileRecoversCurrentDisplayWithIncompleteOrStaleOwnership(t *testing.T) {
	for _, test := range []struct {
		name    string
		record  state.TaskRecord
		display tokens.Display
		want    string
		subject string
	}{
		{
			name: "incomplete current ownership",
			record: state.TaskRecord{
				CapturedTitle:    "✅ 26k Execute BEAR-59",
				LastAppliedTitle: "✅ 26k Execute BEAR-59",
			},
			display: tokens.Display{Position: tokens.PositionStart, Value: "26k"},
			want:    "✅ 26k 26k Execute BEAR-59",
			subject: "26k Execute BEAR-59",
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
			want:    "✅ 26k 26k Execute BEAR-59",
			subject: "26k Execute BEAR-59",
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
			want:    "✅ 26k Execute BEAR-59 · 26k",
			subject: "26k Execute BEAR-59",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := Reconcile(test.record, state.StatusComplete, "", "", test.display)
			if err != nil {
				t.Fatal(err)
			}
			if got.Title != test.want || got.DurableSubject != test.subject {
				t.Fatalf("Reconcile() = %+v", got)
			}
		})
	}
}

func TestReconcilePreservesUnownedNumericText(t *testing.T) {
	for _, test := range []struct {
		name      string
		captured  string
		display   tokens.Display
		suggested string
		want      string
	}{
		{name: "different start value", captured: "✅ 94k Execute BEAR-59", display: tokens.Display{Position: tokens.PositionStart, Value: "26k"}, want: "✅ 26k 94k Execute BEAR-59"},
		{name: "matching value in unconfigured prefix", captured: "✅ 26k Execute BEAR-59", display: tokens.Display{Position: tokens.PositionEnd, Value: "26k"}, want: "✅ 26k Execute BEAR-59 · 26k"},
		{name: "matching ordinary suffix", captured: "✅ Execute BEAR-59 26k", display: tokens.Display{Position: tokens.PositionStart, Value: "26k"}, want: "✅ 26k Execute BEAR-59 26k"},
		{name: "different end value", captured: "✅ Execute BEAR-59 · out 94k", display: tokens.Display{Position: tokens.PositionEnd, Value: "26k"}, want: "✅ Execute BEAR-59 · out 94k · 26k"},
		{name: "no canonical status ownership", captured: "26k Execute BEAR-59", display: tokens.Display{Position: tokens.PositionStart, Value: "26k"}, want: "✅ 26k 26k Execute BEAR-59"},
		{name: "classified legitimate start value", captured: "✅ 220k 210k Improve ThreadBear onboarding", display: tokens.Display{Position: tokens.PositionStart, Value: "220k"}, suggested: "210k Improve ThreadBear onboarding", want: "✅ 220k 210k Improve ThreadBear onboarding"},
		{name: "classified legitimate end value", captured: "✅ Release service · 210k · 220k", display: tokens.Display{Position: tokens.PositionEnd, Value: "220k"}, suggested: "Release service · 210k", want: "✅ Release service · 210k · 220k"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := Reconcile(state.TaskRecord{CapturedTitle: test.captured}, state.StatusComplete, test.suggested, "", test.display)
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
			want:    "✅ 26k Release service · 26k",
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
				DurableSubject:       "Release service · 26k",
				LastAppliedTitle:     "✅ Release service · out 26k · out 26k",
				ManagedTokenDisplay:  "26k",
				ManagedTokenPosition: tokens.PositionEnd,
			},
			want: "✅ Release service · 26k",
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
			want:    "✅ User subject · out 26k · 26k",
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
	if end.Title != "➡️ Release service → review the rollout · 1.6m" || end.ManagedTokenDisplay != "1.6m" || end.ManagedTokenPosition != tokens.PositionEnd {
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
		CapturedTitle:        "➡️ Release service → review the rollout · 1.6m",
		DurableSubject:       "Release service",
		ManagedAction:        "review the rollout",
		LastAppliedTitle:     "➡️ Release service → review the rollout · 1.6m",
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

func TestCleanupRestoresExactRetainedTitleAndRecognizesSettledResult(t *testing.T) {
	record := state.TaskRecord{
		LastAppliedTitle:     "➡️ Release service → review the complete rollout · 1.6m",
		DurableSubject:       "Release service",
		ManagedAction:        "review the complete rollout",
		ManagedTokenDisplay:  "1.6m",
		ManagedTokenPosition: tokens.PositionEnd,
	}
	got, changed := Cleanup(record, record.LastAppliedTitle)
	if !changed || got != "Release service → review the complete rollout" {
		t.Fatalf("cleanup = %q, %t", got, changed)
	}
	persisted := PersistedTitle(got)
	got, changed = Cleanup(record, persisted)
	if changed || got != persisted {
		t.Fatalf("settled cleanup = %q, %t", got, changed)
	}
}

func TestCleanupRecognizesOwnedTokenBoundaries(t *testing.T) {
	for _, test := range []struct {
		name     string
		current  string
		position tokens.Position
		want     string
	}{
		{name: "start", current: "✅ 26k 🧪 26k tasks end in 26k", position: tokens.PositionStart, want: "🧪 26k tasks end in 26k"},
		{name: "canonical end", current: "✅ 🧪 26k tasks end in 26k · 26k", position: tokens.PositionEnd, want: "🧪 26k tasks end in 26k"},
		{name: "legacy end", current: "✅ 🧪 26k tasks end in 26k · out 26k", position: tokens.PositionEnd, want: "🧪 26k tasks end in 26k"},
	} {
		t.Run(test.name, func(t *testing.T) {
			record := state.TaskRecord{LastAppliedTitle: test.current, ManagedTokenDisplay: "26k", ManagedTokenPosition: test.position}
			got, changed := Cleanup(record, test.current)
			if !changed || got != test.want {
				t.Fatalf("cleanup = %q, %t", got, changed)
			}
		})
	}
}

func TestCleanupPreservesDivergentUserTextOutsideExactOwnership(t *testing.T) {
	record := state.TaskRecord{
		LastAppliedTitle:     "✅ Old subject · 26k",
		DurableSubject:       "Old subject",
		ManagedTokenDisplay:  "26k",
		ManagedTokenPosition: tokens.PositionEnd,
	}
	for _, test := range []struct {
		current string
		want    string
	}{
		{current: "✅ 🧪 26k new subject 26k · 26k", want: "🧪 26k new subject 26k"},
		{current: "🧪 ✅ 26k new subject · 26k", want: "🧪 ✅ 26k new subject"},
		{current: "🚨 user changed every boundary · 26k", want: "🚨 user changed every boundary"},
		{current: "✅ user replaced the recorded value · 27k", want: "user replaced the recorded value · 27k"},
	} {
		got, changed := Cleanup(record, test.current)
		if got != test.want || changed != (test.want != test.current) {
			t.Fatalf("cleanup(%q) = %q, %t", test.current, got, changed)
		}
	}
}

func TestCleanupRequiresRecordedTitleOwnership(t *testing.T) {
	current := "✅ User title · 26k"
	got, changed := Cleanup(state.TaskRecord{ManagedTokenDisplay: "26k", ManagedTokenPosition: tokens.PositionEnd}, current)
	if changed || got != current {
		t.Fatalf("cleanup = %q, %t", got, changed)
	}
}
