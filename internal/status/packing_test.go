package status

import (
	"sort"
	"strings"
	"testing"
	"time"
)

func TestPackBoundaryAndOversized(t *testing.T) {
	task := TaskEvidence{TaskID: "task-1", Revision: "rev-1", Latest: TurnEvidence{User: "classify this", FinalAgent: "done"}}
	size, err := PayloadSize([]TaskEvidence{task}, false)
	if err != nil {
		t.Fatal(err)
	}
	batches, oversized, err := PackTasks([]TaskEvidence{task}, size, false)
	if err != nil || len(batches) != 1 || len(oversized) != 0 || batches[0].SizeBytes != size {
		t.Fatalf("exact boundary batches=%+v oversized=%+v err=%v", batches, oversized, err)
	}
	batches, oversized, err = PackTasks([]TaskEvidence{task}, size-1, false)
	if err != nil || len(batches) != 0 || len(oversized) != 1 || oversized[0].SizeBytes != size {
		t.Fatalf("oversized batches=%+v oversized=%+v err=%v", batches, oversized, err)
	}
}

func TestPackUsesSerializedUTF8BytesAndPreservesCompleteText(t *testing.T) {
	ending := "SUCCESS AT THE END 🐻"
	message := strings.Repeat("早期失败。", 80) + ending
	task := TaskEvidence{TaskID: "task-unicode", Revision: "rev-unicode", Latest: TurnEvidence{User: "finish it", FinalAgent: message}}
	input, err := BuildClassifierInput([]TaskEvidence{task}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(input, message) || !strings.Contains(input, ending) {
		t.Fatal("complete UTF-8 evidence was not preserved")
	}
	size, err := PayloadSize([]TaskEvidence{task}, false)
	if err != nil {
		t.Fatal(err)
	}
	if size <= len(message) {
		t.Fatalf("measured size %d did not include prompt/schema/serialization overhead", size)
	}
}

func TestPackSplitsWithoutTaskCapOrOmission(t *testing.T) {
	tasks := make([]TaskEvidence, 25)
	maxSingle := 0
	for index := range tasks {
		tasks[index] = TaskEvidence{TaskID: "task-" + twoDigits(index), Revision: "rev-" + twoDigits(index), Latest: TurnEvidence{User: "request", FinalAgent: strings.Repeat("x", 150)}}
		size, err := PayloadSize([]TaskEvidence{tasks[index]}, false)
		if err != nil {
			t.Fatal(err)
		}
		if size > maxSingle {
			maxSingle = size
		}
	}
	batches, oversized, err := PackTasks(tasks, maxSingle+350, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(oversized) != 0 || len(batches) <= 1 {
		t.Fatalf("batches=%d oversized=%d", len(batches), len(oversized))
	}
	seen := make(map[string]int)
	for _, batch := range batches {
		if batch.SizeBytes > maxSingle+350 {
			t.Fatalf("batch size %d exceeds budget", batch.SizeBytes)
		}
		for _, task := range batch.Tasks {
			seen[task.TaskID]++
		}
	}
	if len(seen) != len(tasks) {
		t.Fatalf("packed %d of %d tasks", len(seen), len(tasks))
	}
	for _, task := range tasks {
		if seen[task.TaskID] != 1 {
			t.Fatalf("task %s packed %d times", task.TaskID, seen[task.TaskID])
		}
	}
}

func TestPackMixedWeightsCompletesPromptlyWithoutDroppingTasks(t *testing.T) {
	lengths := []int{37, 552, 928, 647, 969, 712, 346, 81, 433, 875}
	tasks := make([]TaskEvidence, 40)
	maxSingle := 0
	for index := range tasks {
		length := lengths[index%len(lengths)] + index*3
		tasks[index] = TaskEvidence{TaskID: "task-" + twoDigits(index), Revision: "rev-" + twoDigits(index), Latest: TurnEvidence{User: strings.Repeat("u", index%17), FinalAgent: strings.Repeat("x", length)}}
		size, err := PayloadSize([]TaskEvidence{tasks[index]}, false)
		if err != nil {
			t.Fatal(err)
		}
		if size > maxSingle {
			maxSingle = size
		}
	}
	budget := maxSingle + 1800
	started := time.Now()
	batches, oversized, err := PackTasks(tasks, budget, false)
	elapsed := time.Since(started)
	if err != nil {
		t.Fatal(err)
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("packing took %s", elapsed)
	}
	if len(oversized) != 0 {
		t.Fatalf("oversized=%d", len(oversized))
	}
	seen := make(map[string]int, len(tasks))
	for _, batch := range batches {
		if batch.SizeBytes > budget {
			t.Fatalf("batch size %d exceeds budget %d", batch.SizeBytes, budget)
		}
		for _, task := range batch.Tasks {
			seen[task.TaskID]++
		}
	}
	if len(seen) != len(tasks) {
		t.Fatalf("packed %d of %d tasks", len(seen), len(tasks))
	}
	for _, task := range tasks {
		if seen[task.TaskID] != 1 {
			t.Fatalf("task %s packed %d times", task.TaskID, seen[task.TaskID])
		}
	}
}

func TestPackFindsMinimumBatchCount(t *testing.T) {
	lengths := []int{552, 928, 37, 647, 969, 712, 346}
	tasks := make([]TaskEvidence, len(lengths))
	for index, length := range lengths {
		tasks[index] = TaskEvidence{TaskID: "task-" + string(rune('a'+index)), Revision: "r", Latest: TurnEvidence{FinalAgent: strings.Repeat("x", length)}}
	}
	// The budget rides on top of the prompt text, so it moves when the prompt
	// does: 4800 at the pre-BEAR-31 prompt, +512 for the conditional-caveat rule,
	// its anchored examples, and the stated-recommendation clamp.
	batches, oversized, err := PackTasks(tasks, 5312, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(oversized) != 0 || len(batches) != 2 {
		t.Fatalf("batches=%d oversized=%d", len(batches), len(oversized))
	}
}

func TestPackPreviousPassRequiresAndIncludesOnlyImmediatePrevious(t *testing.T) {
	previous := TurnEvidence{User: "original request", FinalAgent: "original answer"}
	task := TaskEvidence{TaskID: "task-1", Revision: "rev-1", Latest: TurnEvidence{User: "continue", FinalAgent: "done"}, Previous: &previous}
	first, err := BuildClassifierInput([]TaskEvidence{task}, false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(first, "original request") || strings.Contains(first, "original answer") {
		t.Fatal("first pass included previous evidence")
	}
	second, err := BuildClassifierInput([]TaskEvidence{task}, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(second, "original request") || !strings.Contains(second, "original answer") || !strings.Contains(second, `"previous_pass":true`) {
		t.Fatal("second pass omitted immediate previous evidence")
	}
}

func twoDigits(value int) string {
	return string(rune('a'+value/26)) + string(rune('a'+value%26))
}

func TestPackNeverExceedsGreedyGroupCount(t *testing.T) {
	// BEAR-17: on node exhaustion the search must return its best complete
	// packing, which is never worse than the greedy grouping it starts from.
	lengths := []int{37, 552, 928, 647, 969, 712, 346, 81, 433, 875}
	tasks := make([]TaskEvidence, 40)
	maxSingle := 0
	for index := range tasks {
		length := lengths[index%len(lengths)] + index*3
		tasks[index] = TaskEvidence{TaskID: "cap-" + twoDigits(index), Revision: "rev-" + twoDigits(index), Latest: TurnEvidence{User: strings.Repeat("u", index%13), FinalAgent: strings.Repeat("y", length)}}
		size, err := PayloadSize([]TaskEvidence{tasks[index]}, false)
		if err != nil {
			t.Fatal(err)
		}
		if size > maxSingle {
			maxSingle = size
		}
	}
	budget := maxSingle + 1500
	batches, oversized, err := PackTasks(tasks, budget, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(oversized) != 0 {
		t.Fatalf("oversized=%d", len(oversized))
	}
	seen := make(map[string]bool)
	for _, batch := range batches {
		for _, task := range batch.Tasks {
			seen[task.TaskID] = true
		}
	}
	if len(seen) != len(tasks) {
		t.Fatalf("packed %d unique tasks, want %d", len(seen), len(tasks))
	}
	emptySize, err := PayloadSize(nil, false)
	if err != nil {
		t.Fatal(err)
	}
	capacity := budget - emptySize + 1
	candidates := make([]packingCandidate, 0, len(tasks))
	for _, task := range tasks {
		size, err := PayloadSize([]TaskEvidence{task}, false)
		if err != nil {
			t.Fatal(err)
		}
		candidates = append(candidates, packingCandidate{task: task, size: size, weight: size - emptySize + 1})
	}
	sort.SliceStable(candidates, func(left, right int) bool {
		if candidates[left].weight == candidates[right].weight {
			return candidates[left].task.TaskID < candidates[right].task.TaskID
		}
		return candidates[left].weight > candidates[right].weight
	})
	greedy := greedyBatchGroups(candidates, capacity)
	if len(batches) > len(greedy) {
		t.Fatalf("batches = %d, greedy = %d; result must never exceed greedy", len(batches), len(greedy))
	}
}

func TestPackCapsTasksPerBatch(t *testing.T) {
	// A 71-task live batch produced UUID transcription errors; the cap bounds
	// how many IDs one response must echo, whatever the byte budget allows.
	tasks := make([]TaskEvidence, 50)
	for index := range tasks {
		tasks[index] = TaskEvidence{TaskID: "cap-" + twoDigits(index), Revision: "r", Latest: TurnEvidence{FinalAgent: "short"}}
	}
	batches, oversized, err := PackTasks(tasks, 1<<20, false)
	if err != nil || len(oversized) != 0 {
		t.Fatalf("oversized=%d err=%v", len(oversized), err)
	}
	if len(batches) < 3 {
		t.Fatalf("expected at least 3 capped batches, got %d", len(batches))
	}
	seen := 0
	for _, batch := range batches {
		if len(batch.Tasks) > maxBatchTasks {
			t.Fatalf("batch has %d tasks, cap is %d", len(batch.Tasks), maxBatchTasks)
		}
		seen += len(batch.Tasks)
	}
	if seen != len(tasks) {
		t.Fatalf("packed %d of %d tasks", seen, len(tasks))
	}
}
