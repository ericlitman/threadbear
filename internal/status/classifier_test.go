package status

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ericlitman/threadbear/internal/codex/appserver"
	"github.com/ericlitman/threadbear/internal/state"
)

type fakeEphemeralRunner struct {
	mu       sync.Mutex
	requests []appserver.EphemeralRequest
	run      func(int, appserver.EphemeralRequest) (appserver.EphemeralResult, error)
}

func (f *fakeEphemeralRunner) RunEphemeral(_ context.Context, request appserver.EphemeralRequest) (appserver.EphemeralResult, error) {
	f.mu.Lock()
	f.requests = append(f.requests, request)
	index := len(f.requests) - 1
	f.mu.Unlock()
	return f.run(index, request)
}

func TestClassifierConfiguredModelEffortAndOptionBMetadata(t *testing.T) {
	task := TaskEvidence{TaskID: "task-1", Revision: "rev-1", Latest: TurnEvidence{User: "implement it", FinalAgent: "Implemented and verified."}}
	runner := &fakeEphemeralRunner{run: func(_ int, request appserver.EphemeralRequest) (appserver.EphemeralResult, error) {
		return successfulEphemeral(responseText([]classifierWireItem{{TaskID: task.TaskID, TaskRevision: task.Revision, State: state.StatusComplete, DurableSubject: "Implement feature"}})), nil
	}}
	classifier := newTestClassifier(t, runner, 1<<20, "configured-model", "high")
	results := classifier.Classify(context.Background(), []TaskEvidence{task})
	if len(results) != 1 || results[0].Status != state.StatusComplete || results[0].Revision != task.Revision || results[0].Provenance != state.ProvenanceLuna {
		t.Fatalf("results=%+v", results)
	}
	if len(runner.requests) != 1 || runner.requests[0].Model != "configured-model" || runner.requests[0].Effort != "high" {
		t.Fatalf("requests=%+v", runner.requests)
	}
	if len(results[0].Metadata.UnprovenToolSources) != 0 {
		t.Fatalf("metadata=%+v", results[0].Metadata)
	}
	if !strings.Contains(runner.requests[0].Input, "Do not use tools") || runner.requests[0].OutputSchema == nil {
		t.Fatal("request did not carry tool prohibition and output schema")
	}
	if runner.requests[0].ToolConfig == nil || runner.requests[0].PermissionProfile != ":read-only" {
		t.Fatalf("request omitted tool-free controls: %+v", runner.requests[0])
	}
}

func TestClassifierSelectivePreviousExpansion(t *testing.T) {
	previous := TurnEvidence{User: "initial request", FinalAgent: "initial result"}
	tasks := []TaskEvidence{
		{TaskID: "task-a", Revision: "rev-a", Latest: TurnEvidence{User: "finish a", FinalAgent: "a complete"}},
		{TaskID: "task-b", Revision: "rev-b", Latest: TurnEvidence{User: "continue", FinalAgent: "done"}, Previous: &previous},
		{TaskID: "task-c", Revision: "rev-c", Latest: TurnEvidence{User: "finish c", FinalAgent: "c complete"}},
	}
	runner := &fakeEphemeralRunner{run: func(call int, request appserver.EphemeralRequest) (appserver.EphemeralResult, error) {
		envelope := decodePrompt(t, request.Input)
		if call == 0 {
			if envelope.PreviousPass || len(envelope.Tasks) != 3 {
				t.Fatalf("first envelope=%+v", envelope)
			}
			return successfulEphemeral(responseText([]classifierWireItem{
				{TaskID: "task-a", TaskRevision: "rev-a", State: state.StatusComplete, DurableSubject: "Task A"},
				{TaskID: "task-b", TaskRevision: "rev-b", State: state.StatusUnknown, RequestPrevious: true},
				{TaskID: "task-c", TaskRevision: "rev-c", State: state.StatusNextSteps, DurableSubject: "Task C", ManagedAction: "pressure-test requirements"},
			})), nil
		}
		if !envelope.PreviousPass || len(envelope.Tasks) != 1 || envelope.Tasks[0].TaskID != "task-b" || envelope.Tasks[0].Previous == nil || envelope.Tasks[0].Previous.User != "initial request" {
			t.Fatalf("second envelope=%+v", envelope)
		}
		return successfulEphemeral(responseText([]classifierWireItem{{TaskID: "task-b", TaskRevision: "rev-b", State: state.StatusNeedsInput, DurableSubject: "Continue task", ManagedAction: "choose a deployment target"}})), nil
	}}
	classifier := newTestClassifier(t, runner, 1<<20, "model", "medium")
	results := classifier.Classify(context.Background(), tasks)
	if len(runner.requests) != 2 {
		t.Fatalf("calls=%d", len(runner.requests))
	}
	want := []state.TaskStatus{state.StatusComplete, state.StatusNeedsInput, state.StatusNextSteps}
	for index := range results {
		if results[index].Status != want[index] {
			t.Fatalf("result[%d]=%+v", index, results[index])
		}
	}
}

func TestClassifierResumeRunsPreviousPassWithoutRepeatingFirstPass(t *testing.T) {
	task := TaskEvidence{TaskID: "resume-task", Revision: "rev", Latest: TurnEvidence{User: "continue", FinalAgent: "ambiguous"}}
	runner := &fakeEphemeralRunner{run: func(_ int, request appserver.EphemeralRequest) (appserver.EphemeralResult, error) {
		envelope := decodePrompt(t, request.Input)
		if !envelope.PreviousPass || len(envelope.Tasks) != 1 || envelope.Tasks[0].Previous == nil {
			t.Fatalf("envelope=%+v", envelope)
		}
		return successfulEphemeral(responseText([]classifierWireItem{{TaskID: task.TaskID, TaskRevision: task.Revision, State: state.StatusComplete, DurableSubject: "Resume task"}})), nil
	}}
	classifier := newTestClassifier(t, runner, 1<<20, "model", "medium")
	results, err := classifier.ClassifyWithProgress(context.Background(), []TaskEvidence{task}, func(_ context.Context, requested []TaskEvidence) []PreviousEvidenceResult {
		previous := TurnEvidence{User: "before", FinalAgent: "before result"}
		return []PreviousEvidenceResult{{TaskID: requested[0].TaskID, Revision: requested[0].Revision, Evidence: &previous}}
	}, ClassificationResume{PreviousRequested: map[string]string{task.TaskID: task.Revision}}, nil)
	if err != nil || len(runner.requests) != 1 || results[0].Status != state.StatusComplete {
		t.Fatalf("requests=%d results=%+v err=%v", len(runner.requests), results, err)
	}
}

func TestClassifierLoadsPreviousOnlyForRequestedTasks(t *testing.T) {
	tasks := []TaskEvidence{
		{TaskID: "task-a", Revision: "rev-a", Latest: TurnEvidence{User: "a", FinalAgent: "done"}},
		{TaskID: "task-b", Revision: "rev-b", Latest: TurnEvidence{User: "b", FinalAgent: "continue"}},
	}
	runner := &fakeEphemeralRunner{run: func(call int, request appserver.EphemeralRequest) (appserver.EphemeralResult, error) {
		envelope := decodePrompt(t, request.Input)
		if call == 0 {
			return successfulEphemeral(responseText([]classifierWireItem{
				{TaskID: "task-a", TaskRevision: "rev-a", State: state.StatusComplete, DurableSubject: "Task A"},
				{TaskID: "task-b", TaskRevision: "rev-b", State: state.StatusUnknown, RequestPrevious: true},
			})), nil
		}
		if len(envelope.Tasks) != 1 || envelope.Tasks[0].TaskID != "task-b" || envelope.Tasks[0].Previous == nil {
			t.Fatalf("second envelope=%+v", envelope)
		}
		return successfulEphemeral(responseText([]classifierWireItem{{TaskID: "task-b", TaskRevision: "rev-b", State: state.StatusComplete, DurableSubject: "Task B"}})), nil
	}}
	loaded := []string{}
	results := newTestClassifier(t, runner, 1<<20, "model", "medium").ClassifyWithPrevious(context.Background(), tasks, func(_ context.Context, requested []TaskEvidence) []PreviousEvidenceResult {
		for _, task := range requested {
			loaded = append(loaded, task.TaskID)
		}
		previous := TurnEvidence{User: "before", FinalAgent: "before answer"}
		return []PreviousEvidenceResult{{TaskID: "task-b", Revision: "rev-b", Evidence: &previous}}
	})
	if strings.Join(loaded, ",") != "task-b" || len(runner.requests) != 2 || results[0].Status != state.StatusComplete || results[1].Status != state.StatusComplete {
		t.Fatalf("loaded=%v calls=%d results=%+v", loaded, len(runner.requests), results)
	}
}

type cancelEphemeralRunner struct{ calls atomic.Int32 }

func (r *cancelEphemeralRunner) RunEphemeral(ctx context.Context, _ appserver.EphemeralRequest) (appserver.EphemeralResult, error) {
	r.calls.Add(1)
	<-ctx.Done()
	return appserver.EphemeralResult{}, ctx.Err()
}

func TestClassifierCancellationStopsSchedulingNewBatches(t *testing.T) {
	tasks := make([]TaskEvidence, 5)
	for index := range tasks {
		tasks[index] = TaskEvidence{TaskID: "cancel-" + string(rune('a'+index)), Revision: "r", Latest: TurnEvidence{FinalAgent: strings.Repeat("x", 500)}}
	}
	budget, err := PayloadSize([]TaskEvidence{tasks[0]}, false)
	if err != nil {
		t.Fatal(err)
	}
	runner := &cancelEphemeralRunner{}
	classifier, err := NewClassifier(runner, ClassifierConfig{Model: "model", Effort: "medium", ContextBudgetBytes: budget, MaxParallelBatches: 2})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := classifier.ClassifyWithProgress(ctx, tasks, nil, ClassificationResume{}, nil)
		done <- err
	}()
	deadline := time.Now().Add(time.Second)
	for runner.calls.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
	if calls := runner.calls.Load(); calls > 2 {
		t.Fatalf("scheduled %d batches after cancellation", calls)
	}
}

func TestClassifierCapacityBatchesUseBoundedConcurrencyAndProgress(t *testing.T) {
	tasks := make([]TaskEvidence, 4)
	for index := range tasks {
		tasks[index] = TaskEvidence{TaskID: "task-" + string(rune('a'+index)), Revision: "r", Latest: TurnEvidence{FinalAgent: strings.Repeat("x", 500)}}
	}
	budget, err := PayloadSize([]TaskEvidence{tasks[0]}, false)
	if err != nil {
		t.Fatal(err)
	}
	var active atomic.Int32
	var maximum atomic.Int32
	runner := &fakeEphemeralRunner{run: func(_ int, request appserver.EphemeralRequest) (appserver.EphemeralResult, error) {
		current := active.Add(1)
		for {
			old := maximum.Load()
			if current <= old || maximum.CompareAndSwap(old, current) {
				break
			}
		}
		defer active.Add(-1)
		time.Sleep(10 * time.Millisecond)
		envelope := decodePrompt(t, request.Input)
		task := envelope.Tasks[0]
		return successfulEphemeral(responseText([]classifierWireItem{{TaskID: task.TaskID, TaskRevision: task.Revision, State: state.StatusComplete, DurableSubject: task.TaskID}})), nil
	}}
	classifier, err := NewClassifier(runner, ClassifierConfig{Model: "model", Effort: "medium", ContextBudgetBytes: budget, MaxParallelBatches: 2})
	if err != nil {
		t.Fatal(err)
	}
	events := []ClassificationBatchEvent{}
	results, err := classifier.ClassifyWithProgress(context.Background(), tasks, nil, ClassificationResume{}, func(event ClassificationBatchEvent) error { events = append(events, event); return nil })
	if err != nil || len(results) != len(tasks) || maximum.Load() != 2 {
		t.Fatalf("results=%d max=%d err=%v", len(results), maximum.Load(), err)
	}
	if len(events) != 5 || events[0].Total != 4 || events[len(events)-1].Completed != 4 {
		t.Fatalf("events=%+v", events)
	}
}

func TestClassifierContextSplittingAndBatchFailureIsolation(t *testing.T) {
	tasks := []TaskEvidence{
		{TaskID: "task-a", Revision: "rev-a", Latest: TurnEvidence{User: "a", FinalAgent: strings.Repeat("a", 500)}},
		{TaskID: "task-b", Revision: "rev-b", Latest: TurnEvidence{User: "b", FinalAgent: strings.Repeat("b", 500)}},
		{TaskID: "task-c", Revision: "rev-c", Latest: TurnEvidence{User: "c", FinalAgent: strings.Repeat("c", 500)}},
	}
	budget := 0
	for _, task := range tasks {
		size, err := PayloadSize([]TaskEvidence{task}, false)
		if err != nil {
			t.Fatal(err)
		}
		if size > budget {
			budget = size
		}
	}
	runner := &fakeEphemeralRunner{run: func(_ int, request appserver.EphemeralRequest) (appserver.EphemeralResult, error) {
		envelope := decodePrompt(t, request.Input)
		if len(envelope.Tasks) != 1 {
			t.Fatalf("batch contains %d tasks", len(envelope.Tasks))
		}
		task := envelope.Tasks[0]
		if task.TaskID == "task-b" {
			return appserver.EphemeralResult{}, errors.New("synthetic rate limit")
		}
		return successfulEphemeral(responseText([]classifierWireItem{{TaskID: task.TaskID, TaskRevision: task.Revision, State: state.StatusComplete, DurableSubject: strings.ToUpper(task.TaskID)}})), nil
	}}
	results := newTestClassifier(t, runner, budget, "model", "medium").Classify(context.Background(), tasks)
	if len(runner.requests) != 3 {
		t.Fatalf("calls=%d", len(runner.requests))
	}
	if results[0].Status != state.StatusComplete || results[2].Status != state.StatusComplete || results[1].Diagnostic == nil || results[1].Diagnostic.Code != "ephemeral_rate_limited" {
		t.Fatalf("results=%+v", results)
	}
}

func TestClassifierIndividuallyOversizedIsUnknownWithoutCall(t *testing.T) {
	task := TaskEvidence{TaskID: "task-large", Revision: "rev-large", Latest: TurnEvidence{User: "do it", FinalAgent: strings.Repeat("full evidence ", 100)}}
	size, err := PayloadSize([]TaskEvidence{task}, false)
	if err != nil {
		t.Fatal(err)
	}
	runner := &fakeEphemeralRunner{run: func(_ int, _ appserver.EphemeralRequest) (appserver.EphemeralResult, error) {
		t.Fatal("oversized task must not invoke the runner")
		return appserver.EphemeralResult{}, nil
	}}
	result := newTestClassifier(t, runner, size-1, "model", "medium").Classify(context.Background(), []TaskEvidence{task})[0]
	if len(runner.requests) != 0 || result.Diagnostic == nil || result.Diagnostic.Code != "task_exceeds_context_budget" || result.Revision != task.Revision {
		t.Fatalf("requests=%d result=%+v", len(runner.requests), result)
	}
}

func TestClassifierOnePreviousExpansionMaximum(t *testing.T) {
	previous := TurnEvidence{User: "before", FinalAgent: "before answer"}
	task := TaskEvidence{TaskID: "task-1", Revision: "rev-1", Latest: TurnEvidence{User: "continue", FinalAgent: "done"}, Previous: &previous}
	runner := &fakeEphemeralRunner{run: func(_ int, _ appserver.EphemeralRequest) (appserver.EphemeralResult, error) {
		return successfulEphemeral(responseText([]classifierWireItem{{TaskID: task.TaskID, TaskRevision: task.Revision, State: state.StatusUnknown, RequestPrevious: true}})), nil
	}}
	results := newTestClassifier(t, runner, 1<<20, "model", "medium").Classify(context.Background(), []TaskEvidence{task})
	if len(runner.requests) != 2 || results[0].Diagnostic == nil || results[0].Diagnostic.Code != "previous_expansion_exhausted" {
		t.Fatalf("calls=%d results=%+v", len(runner.requests), results)
	}
}

func TestClassifierSalvagesValidRowsAndDiagnosesTheRest(t *testing.T) {
	tasks := []TaskEvidence{
		{TaskID: "task-a", Revision: "rev-a", Latest: TurnEvidence{User: "a", FinalAgent: "a"}},
		{TaskID: "task-b", Revision: "rev-b", Latest: TurnEvidence{User: "b", FinalAgent: "b"}},
	}
	goodB := classifierWireItem{TaskID: "task-b", TaskRevision: "rev-b", State: state.StatusComplete, DurableSubject: "B"}
	tests := []struct {
		name  string
		text  string
		codeA string
	}{
		// task-a's row is defective in each variant; task-b's row is valid and
		// must survive - that is the salvage contract this replaces the old
		// all-or-nothing validation with.
		{name: "missing", text: responseText([]classifierWireItem{goodB}), codeA: "missing_from_response"},
		{name: "chimera id", text: responseText([]classifierWireItem{{TaskID: "task-a-mangled", TaskRevision: "rev-a", State: state.StatusComplete, DurableSubject: "A"}, goodB}), codeA: "missing_from_response"},
		{name: "revision", text: responseText([]classifierWireItem{{TaskID: "task-a", TaskRevision: "wrong", State: state.StatusComplete, DurableSubject: "A"}, goodB}), codeA: "response_revision_mismatch"},
		{name: "eighth state", text: `{"schema_revision":"threadbear.status.v1","results":[{"task_id":"task-a","task_revision":"rev-a","state":"previous","durable_subject":"","managed_action":"","request_previous":false},{"task_id":"task-b","task_revision":"rev-b","state":"complete","durable_subject":"B","managed_action":"","request_previous":false}]}`, codeA: "invalid_response_fields"},
		{name: "field combination", text: responseText([]classifierWireItem{{TaskID: "task-a", TaskRevision: "rev-a", State: state.StatusComplete, DurableSubject: "A", ManagedAction: "do more"}, goodB}), codeA: "invalid_response_fields"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runner := &fakeEphemeralRunner{run: func(_ int, _ appserver.EphemeralRequest) (appserver.EphemeralResult, error) {
				return successfulEphemeral(test.text), nil
			}}
			results := newTestClassifier(t, runner, 1<<20, "model", "medium").Classify(context.Background(), tasks)
			if results[0].Status != state.StatusUnknown || results[0].Diagnostic == nil || results[0].Diagnostic.Code != test.codeA {
				t.Fatalf("task-a result=%+v", results[0])
			}
			if results[1].Status != state.StatusComplete || results[1].Diagnostic != nil {
				t.Fatalf("task-b was not salvaged: %+v", results[1])
			}
		})
	}
}

func TestClassifierDuplicateRowsFirstValidWins(t *testing.T) {
	tasks := []TaskEvidence{{TaskID: "task-a", Revision: "rev-a", Latest: TurnEvidence{User: "a", FinalAgent: "a"}}}
	text := responseText([]classifierWireItem{
		{TaskID: "task-a", TaskRevision: "rev-a", State: state.StatusComplete, DurableSubject: "First"},
		{TaskID: "task-a", TaskRevision: "rev-a", State: state.StatusBlocked, DurableSubject: "Second", ManagedAction: "act"},
	})
	runner := &fakeEphemeralRunner{run: func(_ int, _ appserver.EphemeralRequest) (appserver.EphemeralResult, error) {
		return successfulEphemeral(text), nil
	}}
	result := newTestClassifier(t, runner, 1<<20, "model", "medium").Classify(context.Background(), tasks)[0]
	if result.Status != state.StatusComplete || result.DurableSubject != "First" || result.Diagnostic != nil {
		t.Fatalf("result=%+v", result)
	}
}

func TestClassifierBatchFatalResponsesStillRejectEverything(t *testing.T) {
	tasks := []TaskEvidence{
		{TaskID: "task-a", Revision: "rev-a", Latest: TurnEvidence{User: "a", FinalAgent: "a"}},
		{TaskID: "task-b", Revision: "rev-b", Latest: TurnEvidence{User: "b", FinalAgent: "b"}},
	}
	tests := []struct {
		name string
		text string
		code string
	}{
		{name: "schema revision", text: `{"schema_revision":"wrong","results":[]}`, code: "schema_revision_mismatch"},
		{name: "unknown field", text: `{"schema_revision":"threadbear.status.v1","results":[],"payload":"secret"}`, code: "malformed_classifier_response"},
		{name: "missing required field", text: `{"schema_revision":"threadbear.status.v1","results":[{"task_id":"task-a","task_revision":"rev-a","state":"complete","durable_subject":"A","managed_action":""},{"task_id":"task-b","task_revision":"rev-b","state":"complete","durable_subject":"B","managed_action":"","request_previous":false}]}`, code: "malformed_classifier_response"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runner := &fakeEphemeralRunner{run: func(_ int, _ appserver.EphemeralRequest) (appserver.EphemeralResult, error) {
				return successfulEphemeral(test.text), nil
			}}
			results := newTestClassifier(t, runner, 1<<20, "model", "medium").Classify(context.Background(), tasks)
			for _, result := range results {
				if result.Status != state.StatusUnknown || result.Diagnostic == nil || result.Diagnostic.Code != test.code {
					t.Fatalf("result=%+v", result)
				}
			}
		})
	}
}

func TestClassifierOutputItemAllowlistRejectsWholeBatchWithoutPayload(t *testing.T) {
	tasks := []TaskEvidence{{TaskID: "task-a", Revision: "rev-a", Latest: TurnEvidence{User: "a", FinalAgent: "a"}}, {TaskID: "task-b", Revision: "rev-b", Latest: TurnEvidence{User: "b", FinalAgent: "b"}}}
	runner := &fakeEphemeralRunner{run: func(_ int, _ appserver.EphemeralRequest) (appserver.EphemeralResult, error) {
		result := successfulEphemeral("unused")
		result.Turn.Items = []appserver.TurnItem{{Type: "mcp/toolCall", Phase: "completed", Text: "TOP SECRET PAYLOAD"}, {Type: "agentMessage", Phase: "final_answer", Text: "{}"}}
		return result, nil
	}}
	results := newTestClassifier(t, runner, 1<<20, "model", "medium").Classify(context.Background(), tasks)
	for _, result := range results {
		if result.Diagnostic == nil || result.Diagnostic.Code != "unexpected_output_item" || result.Diagnostic.OffendingItem != "mcp/toolCall" || strings.Contains(result.Diagnostic.Message, "TOP SECRET") || strings.Contains(result.Diagnostic.OffendingItem, "TOP SECRET") {
			t.Fatalf("result=%+v", result)
		}
	}

	runner = &fakeEphemeralRunner{run: func(_ int, _ appserver.EphemeralRequest) (appserver.EphemeralResult, error) {
		result := successfulEphemeral("unused")
		result.Turn.Items = []appserver.TurnItem{{Type: "mcp/toolCall", Phase: "completed", Text: "TOP SECRET PAYLOAD"}}
		return result, nil
	}}
	result := newTestClassifier(t, runner, 1<<20, "model", "medium").Classify(context.Background(), tasks[:1])[0]
	if result.Diagnostic == nil || result.Diagnostic.OffendingItem != "mcp/toolCall" {
		t.Fatalf("result=%+v", result)
	}

	for _, items := range [][]appserver.TurnItem{
		nil,
		{{Type: "agentMessage", Phase: "final_answer", Text: "{}"}, {Type: "agent_message", Phase: "finalAnswer", Text: "{}"}},
	} {
		runner = &fakeEphemeralRunner{run: func(_ int, _ appserver.EphemeralRequest) (appserver.EphemeralResult, error) {
			value := successfulEphemeral("unused")
			value.Turn.Items = items
			return value, nil
		}}
		result = newTestClassifier(t, runner, 1<<20, "model", "medium").Classify(context.Background(), tasks[:1])[0]
		if result.Diagnostic == nil || result.Diagnostic.Code != "unexpected_output_item" {
			t.Fatalf("items=%+v result=%+v", items, result)
		}
		if len(items) == 0 && result.Diagnostic.OffendingItem != "" {
			t.Fatalf("empty items exposed offending detail: %+v", result)
		}
		if len(items) > 0 && result.Diagnostic.OffendingItem != "agentMessage" {
			t.Fatalf("multiple items exposed non-type detail: %+v", result)
		}
	}
}

func TestClassifierAllowsInputAndReasoningAroundFinalAssistant(t *testing.T) {
	task := TaskEvidence{TaskID: "task-a", Revision: "rev-a", Latest: TurnEvidence{User: "a", FinalAgent: "a"}}
	runner := &fakeEphemeralRunner{run: func(_ int, _ appserver.EphemeralRequest) (appserver.EphemeralResult, error) {
		result := successfulEphemeral(responseText([]classifierWireItem{{TaskID: task.TaskID, TaskRevision: task.Revision, State: state.StatusComplete, DurableSubject: "Task A"}}))
		result.Turn.Items = []appserver.TurnItem{
			{Type: "userMessage", Text: "classifier input"},
			{Type: "reasoning", Phase: "SECRET PHASE", Text: "SECRET BEFORE", Content: json.RawMessage(`not-json-before`)},
			result.Turn.Items[0],
			{Type: "reasoning", Phase: "SECRET PHASE", Text: "SECRET AFTER", Content: json.RawMessage(`not-json-after`)},
		}
		return result, nil
	}}
	result := newTestClassifier(t, runner, 1<<20, "model", "medium").Classify(context.Background(), []TaskEvidence{task})[0]
	if result.Status != state.StatusComplete || result.Diagnostic != nil {
		t.Fatalf("result=%+v", result)
	}
}

func TestClassifierRejectsToolAmongReasoningWithoutPayload(t *testing.T) {
	const secret = "TOP SECRET TOOL PAYLOAD"
	task := TaskEvidence{TaskID: "task-a", Revision: "rev-a", Latest: TurnEvidence{User: "a", FinalAgent: "a"}}
	runner := &fakeEphemeralRunner{run: func(_ int, _ appserver.EphemeralRequest) (appserver.EphemeralResult, error) {
		result := successfulEphemeral("unused")
		result.Turn.Items = []appserver.TurnItem{
			{Type: "reasoning", Text: "ignored reasoning"},
			{Type: "mcp/toolCall", Phase: secret, Text: secret, Content: json.RawMessage(`"TOP SECRET TOOL PAYLOAD"`)},
			{Type: "reasoning", Text: "ignored reasoning"},
			{Type: "agentMessage", Phase: "final_answer", Text: "{}"},
		}
		return result, nil
	}}
	result := newTestClassifier(t, runner, 1<<20, "model", "medium").Classify(context.Background(), []TaskEvidence{task})[0]
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if result.Diagnostic == nil || result.Diagnostic.Code != "unexpected_output_item" || result.Diagnostic.OffendingItem != "mcp/toolCall" || strings.Contains(string(encoded), secret) {
		t.Fatalf("result=%s", encoded)
	}
}

func TestClassifierObservedToolItemOutranksControlAndTurnFailures(t *testing.T) {
	task := TaskEvidence{TaskID: "task-a", Revision: "rev-a", Latest: TurnEvidence{User: "a", FinalAgent: "a"}}
	runner := &fakeEphemeralRunner{run: func(_ int, _ appserver.EphemeralRequest) (appserver.EphemeralResult, error) {
		result := successfulEphemeral("unused")
		result.ToolRestriction.DynamicToolsDisabled = false
		result.Turn.Status = "failed"
		result.Turn.Items = []appserver.TurnItem{{Type: "hosted/toolResult", Text: "SECRET PAYLOAD"}}
		return result, nil
	}}
	result := newTestClassifier(t, runner, 1<<20, "model", "medium").Classify(context.Background(), []TaskEvidence{task})[0]
	if result.Diagnostic == nil || result.Diagnostic.Code != "unexpected_output_item" || result.Diagnostic.OffendingItem != "hosted/toolResult" || strings.Contains(result.Diagnostic.Message, "SECRET") {
		t.Fatalf("result=%+v", result)
	}
}

func TestClassifierIncompleteTurnWithOnlyInputReportsTurnFailure(t *testing.T) {
	task := TaskEvidence{TaskID: "task-a", Revision: "rev-a", Latest: TurnEvidence{User: "a", FinalAgent: "a"}}
	runner := &fakeEphemeralRunner{run: func(_ int, _ appserver.EphemeralRequest) (appserver.EphemeralResult, error) {
		result := successfulEphemeral("unused")
		result.Turn.Status = "failed"
		result.Turn.Items = []appserver.TurnItem{{Type: "userMessage", Text: "classifier input"}}
		return result, nil
	}}
	result := newTestClassifier(t, runner, 1<<20, "model", "medium").Classify(context.Background(), []TaskEvidence{task})[0]
	if result.Diagnostic == nil || result.Diagnostic.Code != "classifier_turn_incomplete" {
		t.Fatalf("result=%+v", result)
	}
}

func TestClassifierRejectsAmbiguousFinalAssistantContent(t *testing.T) {
	task := TaskEvidence{TaskID: "task-a", Revision: "rev-a", Latest: TurnEvidence{User: "a", FinalAgent: "a"}}
	runner := &fakeEphemeralRunner{run: func(_ int, _ appserver.EphemeralRequest) (appserver.EphemeralResult, error) {
		result := successfulEphemeral("SECRET TEXT")
		result.Turn.Items[0].Content = json.RawMessage(`"SECRET CONTENT"`)
		return result, nil
	}}
	result := newTestClassifier(t, runner, 1<<20, "model", "medium").Classify(context.Background(), []TaskEvidence{task})[0]
	if result.Diagnostic == nil || result.Diagnostic.Code != "malformed_classifier_response" || result.Diagnostic.OffendingItem != "agentMessage" || strings.Contains(result.Diagnostic.Message, "SECRET") {
		t.Fatalf("result=%+v", result)
	}
}

func TestClassifierOutputDiagnosticsRecordOnlyItemType(t *testing.T) {
	task := TaskEvidence{TaskID: "task-a", Revision: "rev-a", Latest: TurnEvidence{User: "a", FinalAgent: "a"}}
	runner := &fakeEphemeralRunner{run: func(_ int, _ appserver.EphemeralRequest) (appserver.EphemeralResult, error) {
		result := successfulEphemeral("unused")
		result.Turn.Items = []appserver.TurnItem{{Type: "agentMessage", Phase: "SECRET PHASE", Text: "SECRET PAYLOAD"}}
		return result, nil
	}}
	result := newTestClassifier(t, runner, 1<<20, "model", "medium").Classify(context.Background(), []TaskEvidence{task})[0]
	if result.Diagnostic == nil || result.Diagnostic.OffendingItem != "agentMessage" || strings.Contains(result.Diagnostic.OffendingItem, "SECRET") || strings.Contains(result.Diagnostic.Message, "SECRET") {
		t.Fatalf("result=%+v", result)
	}
}

func TestClassifierFailsClosedForBudgetControlsAndCallFailure(t *testing.T) {
	task := TaskEvidence{TaskID: "task-1", Revision: "rev-1", Latest: TurnEvidence{User: "a", FinalAgent: "b"}}
	runner := &fakeEphemeralRunner{run: func(_ int, _ appserver.EphemeralRequest) (appserver.EphemeralResult, error) {
		return appserver.EphemeralResult{}, errors.New("rate limited with payload SECRET")
	}}
	result := newTestClassifier(t, runner, 0, "model", "medium").Classify(context.Background(), []TaskEvidence{task})[0]
	if len(runner.requests) != 0 || result.Diagnostic == nil || result.Diagnostic.Code != "invalid_context_budget" {
		t.Fatalf("requests=%d result=%+v", len(runner.requests), result)
	}

	classifier := newTestClassifier(t, runner, 1<<20, "model", "medium")
	result = classifier.Classify(context.Background(), []TaskEvidence{task})[0]
	if result.Diagnostic == nil || result.Diagnostic.Code != "ephemeral_rate_limited" || strings.Contains(result.Diagnostic.Message, "SECRET") {
		t.Fatalf("result=%+v", result)
	}

	runner = &fakeEphemeralRunner{run: func(_ int, _ appserver.EphemeralRequest) (appserver.EphemeralResult, error) {
		value := successfulEphemeral(responseText([]classifierWireItem{{TaskID: task.TaskID, TaskRevision: task.Revision, State: state.StatusComplete, DurableSubject: "Task"}}))
		value.ToolRestriction.DynamicToolsDisabled = false
		return value, nil
	}}
	result = newTestClassifier(t, runner, 1<<20, "model", "medium").Classify(context.Background(), []TaskEvidence{task})[0]
	if result.Diagnostic == nil || result.Diagnostic.Code != "tool_controls_unconfirmed" {
		t.Fatalf("result=%+v", result)
	}

	runner = &fakeEphemeralRunner{run: func(_ int, _ appserver.EphemeralRequest) (appserver.EphemeralResult, error) {
		value := successfulEphemeral(responseText([]classifierWireItem{{TaskID: task.TaskID, TaskRevision: task.Revision, State: state.StatusComplete, DurableSubject: "Task"}}))
		value.ToolRestriction.ConfigOverride = false
		return value, nil
	}}
	result = newTestClassifier(t, runner, 1<<20, "model", "medium").Classify(context.Background(), []TaskEvidence{task})[0]
	if result.Diagnostic == nil || result.Diagnostic.Code != "tool_controls_unconfirmed" {
		t.Fatalf("permission profile substituted for read-only sandbox: %+v", result)
	}
}

func TestClassifierSyntheticCorpus(t *testing.T) {
	type corpusCase struct {
		ID          string `json:"id"`
		Revision    string `json:"revision"`
		LatestUser  string `json:"latest_user"`
		LatestAgent string `json:"latest_agent"`
	}
	type expectedCase struct {
		ID             string           `json:"id"`
		State          state.TaskStatus `json:"state"`
		DurableSubject string           `json:"durable_subject"`
		ManagedAction  string           `json:"managed_action"`
	}
	var cases []corpusCase
	var expected []expectedCase
	readJSONFixture(t, "../../testdata/status/cases.json", &cases)
	readJSONFixture(t, "../../testdata/status/expected.json", &expected)
	if len(cases) != len(expected) || len(cases) < 4 {
		t.Fatalf("cases=%d expected=%d", len(cases), len(expected))
	}
	tasks := make([]TaskEvidence, len(cases))
	wire := make([]classifierWireItem, len(expected))
	for index := range cases {
		if cases[index].ID != expected[index].ID {
			t.Fatalf("fixture ID mismatch at %d", index)
		}
		tasks[index] = TaskEvidence{TaskID: cases[index].ID, Revision: cases[index].Revision, Latest: TurnEvidence{User: cases[index].LatestUser, FinalAgent: cases[index].LatestAgent}}
		wire[index] = classifierWireItem{TaskID: expected[index].ID, TaskRevision: cases[index].Revision, State: expected[index].State, DurableSubject: expected[index].DurableSubject, ManagedAction: expected[index].ManagedAction}
	}
	runner := &fakeEphemeralRunner{run: func(_ int, request appserver.EphemeralRequest) (appserver.EphemeralResult, error) {
		for _, item := range cases {
			if !strings.Contains(request.Input, item.LatestAgent) {
				t.Fatalf("complete corpus message for %s omitted", item.ID)
			}
		}
		return successfulEphemeral(responseText(wire)), nil
	}}
	results := newTestClassifier(t, runner, 1<<20, "gpt-5.6-luna", "medium").Classify(context.Background(), tasks)
	for index, result := range results {
		if result.Status != expected[index].State || result.DurableSubject != expected[index].DurableSubject || result.ManagedAction != expected[index].ManagedAction {
			t.Fatalf("result[%d]=%+v expected=%+v", index, result, expected[index])
		}
	}
}

func TestClassifierStructuredOutputSchemaTypesConstAndEnumStrings(t *testing.T) {
	classifier := newTestClassifier(t, &fakeEphemeralRunner{}, 1<<20, "model", "medium")
	properties := classifier.schema["properties"].(map[string]any)
	if properties["schema_revision"].(map[string]any)["type"] != "string" {
		t.Fatalf("schema_revision schema=%+v", properties["schema_revision"])
	}
	results := properties["results"].(map[string]any)
	items := results["items"].(map[string]any)
	resultProperties := items["properties"].(map[string]any)
	if resultProperties["state"].(map[string]any)["type"] != "string" {
		t.Fatalf("state schema=%+v", resultProperties["state"])
	}
}

func newTestClassifier(t *testing.T, runner EphemeralRunner, budget int, model, effort string) *Classifier {
	t.Helper()
	classifier, err := NewClassifier(runner, ClassifierConfig{Model: model, Effort: effort, ContextBudgetBytes: budget})
	if err != nil {
		t.Fatal(err)
	}
	return classifier
}

func successfulEphemeral(text string) appserver.EphemeralResult {
	return appserver.EphemeralResult{
		ThreadID:        "ephemeral-test",
		Turn:            appserver.Turn{ID: "turn-test", Status: "completed", Items: []appserver.TurnItem{{Type: "agentMessage", Phase: "final_answer", Text: text}}},
		ToolRestriction: appserver.ToolRestriction{ConfigOverride: true, PermissionProfile: true, EnvironmentsDisabled: true, DynamicToolsDisabled: true, ApprovalsDisabled: true, OutputConstrained: true},
	}
}

func responseText(items []classifierWireItem) string {
	data, err := json.Marshal(classifierResponse{SchemaRevision: SchemaRevision, Results: items})
	if err != nil {
		panic(err)
	}
	return string(data)
}

func decodePrompt(t *testing.T, input string) promptEnvelope {
	t.Helper()
	parts := strings.SplitN(input, "\nINPUT\n", 2)
	if len(parts) != 2 {
		t.Fatalf("missing input envelope: %q", input)
	}
	var envelope promptEnvelope
	if err := json.Unmarshal([]byte(parts[1]), &envelope); err != nil {
		t.Fatal(err)
	}
	return envelope
}

func readJSONFixture(t *testing.T, path string, target any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		t.Fatal(err)
	}
}
