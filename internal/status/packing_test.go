package status

import (
	"strings"
	"testing"
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

func TestPackFindsMinimumBatchCount(t *testing.T) {
	lengths := []int{552, 928, 37, 647, 969, 712, 346}
	tasks := make([]TaskEvidence, len(lengths))
	for index, length := range lengths {
		tasks[index] = TaskEvidence{TaskID: "task-" + string(rune('a'+index)), Revision: "r", Latest: TurnEvidence{FinalAgent: strings.Repeat("x", length)}}
	}
	batches, oversized, err := PackTasks(tasks, 4407, false)
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
