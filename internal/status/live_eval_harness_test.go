package status

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ericlitman/threadbear/internal/codex/appserver"
	"github.com/ericlitman/threadbear/internal/state"
)

func TestDecodeLiveEvalCorpusValidatesContract(t *testing.T) {
	valid := `{
		"schema_revision":"threadbear.live-eval.v1",
		"cases":[{
			"id":"case-1",
			"expected":"complete",
			"provenance":{"model":"gpt-5.6-sol","effort":"xhigh","source":"vscode","agents_block_version":"v3"},
			"facts":{"footer":{"message":"Finished.\n🧵🐻 complete","latest_turn_completed":true}},
			"latest":{"user":"finish it","final_agent":"Finished.\n🧵🐻 complete"}
		}]
	}`
	corpus, err := decodeLiveEvalCorpus([]byte(valid))
	if err != nil || len(corpus.Cases) != 1 {
		t.Fatalf("corpus=%+v err=%v", corpus, err)
	}
	duplicate := corpus
	duplicate.Cases = append(duplicate.Cases, duplicate.Cases[0])
	duplicateData, err := json.Marshal(duplicate)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		data string
	}{
		{"schema", strings.Replace(valid, "threadbear.live-eval.v1", "wrong", 1)},
		{"duplicate", string(duplicateData)},
		{"missing provenance", strings.Replace(valid, `"model":"gpt-5.6-sol"`, `"model":""`, 1)},
		{"invalid expected", strings.Replace(valid, `"expected":"complete"`, `"expected":"done"`, 1)},
		{"missing facts", omitLiveEvalField(t, valid, "facts")},
		{"missing footer", omitLiveEvalField(t, valid, "facts", "footer")},
		{"missing latest", omitLiveEvalField(t, valid, "latest")},
		{"split message", strings.Replace(valid, `"final_agent":"Finished.\n🧵🐻 complete"`, `"final_agent":"Different final answer."`, 1)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := decodeLiveEvalCorpus([]byte(test.data)); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestRunLiveEvalUsesDeterministicCascadeBeforeClassifier(t *testing.T) {
	corpus := liveEvalCorpus{
		SchemaRevision: liveEvalSchemaRevision,
		Cases: []liveEvalCase{
			{
				ID:       "footer-complete",
				Expected: state.StatusComplete,
				Provenance: liveEvalProvenance{
					Model: "gpt-5.6-sol", Effort: "xhigh", Source: "vscode", AgentsBlockVersion: "v3",
				},
				Facts: &liveEvalFacts{Footer: &liveEvalFooterInput{
					Message: "Finished.\n🧵🐻 complete", LatestTurnCompleted: true,
				}},
				Latest: &TurnEvidence{User: "finish", FinalAgent: "Finished.\n🧵🐻 complete"},
			},
			{
				ID:       "semantic-next",
				Expected: state.StatusNextSteps,
				Provenance: liveEvalProvenance{
					Model: "gpt-5.6-sol", Effort: "xhigh", Source: "vscode", AgentsBlockVersion: "disabled",
				},
				Facts:  &liveEvalFacts{Footer: &liveEvalFooterInput{Message: "Analysis complete; deploy the fix next.", LatestTurnCompleted: true}},
				Latest: &TurnEvidence{User: "analyze", FinalAgent: "Analysis complete; deploy the fix next."},
			},
		},
	}
	runner := &fakeEphemeralRunner{run: func(_ int, request appserver.EphemeralRequest) (appserver.EphemeralResult, error) {
		envelope := decodePrompt(t, request.Input)
		if len(envelope.Tasks) != 1 || envelope.Tasks[0].TaskID != "semantic-next" {
			t.Fatalf("classifier received %+v", envelope.Tasks)
		}
		return successfulEphemeral(responseText([]classifierWireItem{{
			TaskID:         "semantic-next",
			TaskRevision:   liveEvalRevision,
			State:          state.StatusNextSteps,
			DurableSubject: "Deploy the fix",
			ManagedAction:  "deploy the fix",
		}})), nil
	}}
	classifier := newTestClassifier(t, runner, 1<<20, "gpt-5.6-luna", "medium")
	report, err := runLiveEval(context.Background(), corpus, classifier)
	if err != nil {
		t.Fatal(err)
	}
	if len(runner.requests) != 1 || report.Total != 2 || report.Correct != 2 || report.FalseComplete != 0 || report.FalseNextSteps != 0 {
		t.Fatalf("requests=%d report=%+v", len(runner.requests), report)
	}
	if report.Deterministic != 1 || report.Classified != 1 || report.ByState[state.StatusComplete].Correct != 1 || report.ByState[state.StatusNextSteps].Correct != 1 {
		t.Fatalf("report=%+v", report)
	}
}

func TestRunLiveEvalRejectsCorpusWithoutClassifierOwnedCases(t *testing.T) {
	corpus := liveEvalCorpus{
		SchemaRevision: liveEvalSchemaRevision,
		Cases: []liveEvalCase{{
			ID:       "footer-complete",
			Expected: state.StatusComplete,
			Provenance: liveEvalProvenance{
				Model: "gpt-5.6-sol", Effort: "xhigh", Source: "vscode", AgentsBlockVersion: "v3",
			},
			Facts: &liveEvalFacts{Footer: &liveEvalFooterInput{
				Message: "Finished.\n🧵🐻 complete", LatestTurnCompleted: true,
			}},
			Latest: &TurnEvidence{User: "finish", FinalAgent: "Finished.\n🧵🐻 complete"},
		}},
	}
	runner := &fakeEphemeralRunner{run: func(_ int, _ appserver.EphemeralRequest) (appserver.EphemeralResult, error) {
		t.Fatal("classifier must not run")
		return appserver.EphemeralResult{}, nil
	}}
	classifier := newTestClassifier(t, runner, 1<<20, "gpt-5.6-luna", "medium")
	if _, err := runLiveEval(context.Background(), corpus, classifier); err == nil || !strings.Contains(err.Error(), "no classifier-owned cases") {
		t.Fatalf("err=%v", err)
	}
}

func TestScoreLiveEvalCountsReleaseBlockingFalsePositives(t *testing.T) {
	cases := []liveEvalCase{
		{ID: "a", Expected: state.StatusBlocked},
		{ID: "b", Expected: state.StatusComplete},
		{ID: "c", Expected: state.StatusNextSteps},
	}
	actual := map[string]state.TaskStatus{
		"a": state.StatusComplete,
		"b": state.StatusNextSteps,
		"c": state.StatusNextSteps,
	}
	report := scoreLiveEval(cases, actual, nil)
	if report.FalseComplete != 1 || report.FalseNextSteps != 1 || report.Correct != 1 {
		t.Fatalf("report=%+v", report)
	}
	if len(report.Errors) != 2 || report.ByState[state.StatusBlocked].Total != 1 || report.ByState[state.StatusNextSteps].Correct != 1 {
		t.Fatalf("report=%+v", report)
	}
}

func omitLiveEvalField(t *testing.T, data string, path ...string) string {
	t.Helper()
	var corpus map[string]any
	if err := json.Unmarshal([]byte(data), &corpus); err != nil {
		t.Fatal(err)
	}
	cases := corpus["cases"].([]any)
	current := cases[0].(map[string]any)
	for _, segment := range path[:len(path)-1] {
		current = current[segment].(map[string]any)
	}
	delete(current, path[len(path)-1])
	encoded, err := json.Marshal(corpus)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func TestAggregateLiveEvalSeriesSeparatesSystematicFromFlap(t *testing.T) {
	cases := []liveEvalCase{
		{ID: "stable", Expected: state.StatusComplete},
		{ID: "flappy", Expected: state.StatusComplete},
		{ID: "safe-miss", Expected: state.StatusRunning},
	}
	reports := []liveEvalReport{
		{Errors: []liveEvalError{
			{ID: "stable", Expected: state.StatusComplete, Actual: state.StatusNextSteps},
			{ID: "flappy", Expected: state.StatusComplete, Actual: state.StatusNextSteps},
			{ID: "safe-miss", Expected: state.StatusRunning, Actual: state.StatusUnknown},
		}},
		{Errors: []liveEvalError{
			{ID: "stable", Expected: state.StatusComplete, Actual: state.StatusNextSteps},
			{ID: "flappy", Expected: state.StatusComplete, Actual: state.StatusNextSteps},
		}},
		{Errors: []liveEvalError{
			{ID: "stable", Expected: state.StatusComplete, Actual: state.StatusNextSteps},
		}},
	}
	series := aggregateLiveEvalSeries(cases, reports)
	if series.Runs != 3 || series.Threshold != 3 {
		t.Fatalf("series=%+v", series)
	}
	if len(series.Systematic) != 1 || series.Systematic[0].ID != "stable" || series.Systematic[0].Dangerous != 3 {
		t.Fatalf("systematic=%+v", series.Systematic)
	}
	if len(series.Flapping) != 1 || series.Flapping[0].ID != "flappy" {
		t.Fatalf("flapping=%+v", series.Flapping)
	}
	if series.Caveat == "" {
		t.Fatal("series must carry the single-case floor caveat")
	}
}

func TestAggregateLiveEvalSeriesFlagsPersistentDiagnostics(t *testing.T) {
	cases := []liveEvalCase{{ID: "ghost", Expected: state.StatusComplete}}
	diagnostic := liveEvalDiagnostic{ID: "ghost", Code: "previous_evidence_unavailable", Message: "x"}
	reports := []liveEvalReport{
		{Diagnostics: []liveEvalDiagnostic{diagnostic}},
		{Diagnostics: []liveEvalDiagnostic{diagnostic}},
		{Diagnostics: []liveEvalDiagnostic{diagnostic}},
	}
	series := aggregateLiveEvalSeries(cases, reports)
	if len(series.Unscoreable) != 1 || series.Unscoreable[0].ID != "ghost" || series.Unscoreable[0].Diagnosed != 3 {
		t.Fatalf("unscoreable=%+v", series.Unscoreable)
	}
}

func TestScoreLiveEvalExcludesDiagnosedCases(t *testing.T) {
	cases := []liveEvalCase{
		{ID: "a", Expected: state.StatusComplete},
		{ID: "b", Expected: state.StatusComplete},
	}
	actual := map[string]state.TaskStatus{"a": state.StatusComplete}
	report := scoreLiveEval(cases, actual, map[string]bool{"b": true})
	if report.Correct != 1 || report.Unscored != 1 || len(report.Errors) != 0 {
		t.Fatalf("report=%+v", report)
	}
}

func TestDualAcceptedCasesScoreEitherStateAsCorrect(t *testing.T) {
	cases := []liveEvalCase{
		{ID: "ambiguous", Expected: state.StatusNextSteps, Accepted: []state.TaskStatus{state.StatusNextSteps, state.StatusComplete}},
		{ID: "single", Expected: state.StatusNextSteps},
	}
	actual := map[string]state.TaskStatus{
		"ambiguous": state.StatusComplete,
		"single":    state.StatusComplete,
	}
	report := scoreLiveEval(cases, actual, nil)
	if report.Correct != 1 || report.FalseComplete != 1 || len(report.Errors) != 1 || report.Errors[0].ID != "single" {
		t.Fatalf("report=%+v", report)
	}
	reports := []liveEvalReport{
		{Errors: []liveEvalError{{ID: "single", Expected: state.StatusNextSteps, Actual: state.StatusComplete}}},
		{Errors: []liveEvalError{{ID: "single", Expected: state.StatusNextSteps, Actual: state.StatusComplete}}},
		{Errors: []liveEvalError{{ID: "single", Expected: state.StatusNextSteps, Actual: state.StatusComplete}}},
	}
	series := aggregateLiveEvalSeries(cases, reports)
	if len(series.Systematic) != 1 || series.Systematic[0].ID != "single" {
		t.Fatalf("systematic=%+v", series.Systematic)
	}
}

func TestDecodeRejectsAcceptedSetMissingExpected(t *testing.T) {
	corpus := `{"schema_revision":"threadbear.live-eval.v1","cases":[{"id":"a","expected":"next_steps","accepted":["complete"],"provenance":{"model":"m","effort":"e","source":"s","agents_block_version":"v"},"facts":{"footer":{"message":"x","latest_turn_completed":true}},"latest":{"user":"u","final_agent":"x"}}]}`
	if _, err := decodeLiveEvalCorpus([]byte(corpus)); err == nil || !strings.Contains(err.Error(), "must contain its expected state") {
		t.Fatalf("err=%v", err)
	}
}
