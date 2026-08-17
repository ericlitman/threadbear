package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestLiveOnboardClassifierContract(t *testing.T) {
	if os.Getenv("THREADBEAR_LIVE_CLASSIFIER") != "1" {
		t.Skip("set THREADBEAR_LIVE_CLASSIFIER=1 for the installed Codex model contract")
	}
	tasks := []onboardEvidence{
		{TaskID: testFirstID, User: "Implement the requested small change and verify it.", Final: "Implemented the change. All tests pass and nothing remains."},
		{TaskID: testSecondID, User: "Configure the release color.", Final: "I need you to choose blue or green before I can continue."},
	}
	got, err := classifyOnboardBatch(t.Context(), tasks)
	if err != nil || len(got) != len(tasks) {
		t.Fatalf("live classifier contract = %#v, %v", got, err)
	}
	for index, status := range got {
		if !semanticStatus(status) && status != "unknown" {
			t.Fatalf("live classifier status %d = %q", index, status)
		}
	}
}

func TestLiveOnboardHistoryContract(t *testing.T) {
	if os.Getenv("THREADBEAR_LIVE_HISTORY") != "1" {
		t.Skip("set THREADBEAR_LIVE_HISTORY=1 for aggregate-only App Server history diagnostics")
	}
	activeTaskID := os.Getenv("CODEX_THREAD_ID")
	if !taskIDPattern.MatchString(activeTaskID) {
		t.Fatal("live history diagnostic requires CODEX_THREAD_ID")
	}
	client, err := startAppServer(t.Context(), 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer client.abort()
	nextRequestID := 2
	tasks, err := client.inventory(&nextRequestID)
	if err != nil {
		t.Fatal(err)
	}
	statuses, errorsByText := map[string]int{}, map[string]int{}
	eligible, withUser, withFinal := 0, 0, 0
	for _, task := range tasks {
		if task.ID == activeTaskID || task.RawFallback || task.Internal {
			continue
		}
		_, decorated, subjectErr := subjectFromTitle(task.Title)
		if subjectErr != nil || decorated {
			continue
		}
		eligible++
		turn, turnErr := client.latestTurn(&nextRequestID, task.ID)
		if turnErr != nil {
			errorsByText[turnErr.Error()]++
			continue
		}
		if turn == nil {
			statuses["<missing>"]++
			continue
		}
		statuses[turn.Status]++
		user, final := turnEvidence(*turn)
		if strings.TrimSpace(user) != "" {
			withUser++
		}
		if strings.TrimSpace(final) != "" {
			withFinal++
		}
	}
	client.close()
	t.Logf("aggregate history: total=%d eligible=%d statuses=%v errors=%v with_user=%d with_final=%d",
		len(tasks), eligible, statuses, errorsByText, withUser, withFinal)
}

func TestOnboardClassifierUsesFixedEphemeralCodexContract(t *testing.T) {
	directory := t.TempDir()
	commandPath := filepath.Join(directory, "codex")
	argumentsPath := filepath.Join(directory, "arguments")
	workingPath := filepath.Join(directory, "working")
	response := fmt.Sprintf(`{"results":[{"task_id":%q,"status":"blocked"}]}`, testFirstID)
	script := `#!/bin/sh
set -eu
printf '%s\n' "$@" > "$THREADBEAR_ARGUMENTS"
pwd > "$THREADBEAR_WORKING"
output=
while [ "$#" -gt 0 ]; do
  if [ "$1" = "--output-last-message" ]; then output=$2; shift 2; else shift; fi
done
test -n "$output"
printf '%s' "$THREADBEAR_RESPONSE" > "$output"
`
	if err := os.WriteFile(commandPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("THREADBEAR_ARGUMENTS", argumentsPath)
	t.Setenv("THREADBEAR_WORKING", workingPath)
	t.Setenv("THREADBEAR_RESPONSE", response)
	previous := locateCodex
	locateCodex = func(context.Context) (codexCompatibility, error) {
		return codexCompatibility{Path: commandPath, Version: "0.148.0"}, nil
	}
	t.Cleanup(func() { locateCodex = previous })
	got, err := classifyOnboardBatch(t.Context(), []onboardEvidence{{TaskID: testFirstID, User: "request", Final: "external service is down"}})
	if err != nil || !reflect.DeepEqual(got, []string{"blocked"}) {
		t.Fatalf("classifier result = %#v, %v", got, err)
	}
	arguments, err := os.ReadFile(argumentsPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		"exec\n", "--ephemeral\n", "--ignore-user-config\n", "--ignore-rules\n",
		"--skip-git-repo-check\n", "--disable\nshell_tool\n--disable\ncode_mode_host\n",
		"--model\n" + onboardModel + "\n", "--sandbox\nread-only\n",
		"model_reasoning_effort=\"" + onboardEffort + "\"\n",
	} {
		if !strings.Contains(string(arguments), required) {
			t.Fatalf("classifier arguments omit %q: %q", required, arguments)
		}
	}
	working, err := os.ReadFile(workingPath)
	if err != nil {
		t.Fatal(err)
	}
	repository, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	temporary := strings.TrimSpace(string(working))
	if strings.HasPrefix(temporary, repository) {
		t.Fatalf("classifier ran inside repository: %s", temporary)
	}
	if _, err := os.Stat(temporary); !os.IsNotExist(err) {
		t.Fatalf("classifier temporary directory remains: %v", err)
	}
}

func TestExactFooterStatusRecognizesOnlyHistoricalGrammar(t *testing.T) {
	for _, test := range []struct{ footer, status string }{
		{"🧵🐻 complete", "complete"},
		{"🧵🐻 automation", "automation"},
		{"🧵🐻 next steps (you): approve", "next_steps"},
		{"🧵🐻 next steps (agent): implement", "next_steps"},
		{"🧵🐻 next steps (external): deploy", "next_steps"},
		{"🧵🐻 needs input (you): choose", "needs_input"},
		{"🧵🐻 blocked (external): service down", "blocked"},
	} {
		if got, ok := exactFooterStatus("answer\n\n" + test.footer + "\n"); !ok || got != test.status {
			t.Errorf("exactFooterStatus(%q) = %q, %t", test.footer, got, ok)
		}
	}
	for _, footer := range []string{
		"🧵🐻 complete later", "🧵🐻 automation: nightly", "🧵🐻 next steps (you): ",
		"🧵🐻 needs input (agent): choose", "🧵🐻 blocked (external):", "✅ done",
	} {
		if got, ok := exactFooterStatus(footer); ok || got != "" {
			t.Errorf("unsafe footer %q = %q, %t", footer, got, ok)
		}
	}
}

func TestTurnEvidenceUsesOnlyFinalAssistantItem(t *testing.T) {
	turn := appServerTurn{Status: "completed", Items: []appServerTurnItem{
		{Type: "userMessage", Content: json.RawMessage(`[{"type":"inputText","text":"latest request"}]`)},
		{Type: "agentMessage", Phase: "commentary", Text: "working"},
		{Type: "agentMessage", Phase: "final_answer", Content: json.RawMessage(`[{"type":"outputText","text":"finished"}]`)},
	}}
	if user, final := turnEvidence(turn); user != "latest request" || final != "finished" {
		t.Fatalf("turn evidence = %q / %q", user, final)
	}
	turn.Items = append(turn.Items, appServerTurnItem{Type: "agent_message", Text: "historical final without phase"})
	if _, final := turnEvidence(turn); final != "historical final without phase" {
		t.Fatalf("phase-less completed final = %q", final)
	}
	long := strings.Repeat("🧵", 5000)
	if got := boundedText(long, 4096); len(got) > 4096 || !strings.Contains(got, "…") || !strings.HasPrefix(got, "🧵") || !strings.HasSuffix(got, "🧵") {
		t.Fatalf("bounded text bytes/shape = %d / %q", len(got), got[:20])
	}
}

func TestDecodeClassifierResultsRequiresExactRowsAndOrder(t *testing.T) {
	tasks := []onboardEvidence{{TaskID: testFirstID}, {TaskID: testSecondID}}
	valid := []byte(fmt.Sprintf(`{"results":[{"task_id":%q,"status":"complete"},{"task_id":%q,"status":"unknown"}]}`, testFirstID, testSecondID))
	if got, err := decodeClassifierResults(valid, tasks); err != nil || !reflect.DeepEqual(got, []string{"complete", "unknown"}) {
		t.Fatalf("valid classifier result = %#v, %v", got, err)
	}
	for name, value := range map[string]string{
		"missing":     fmt.Sprintf(`{"results":[{"task_id":%q,"status":"complete"}]}`, testFirstID),
		"duplicate":   fmt.Sprintf(`{"results":[{"task_id":%q,"status":"complete"},{"task_id":%q,"status":"blocked"}]}`, testFirstID, testFirstID),
		"reordered":   fmt.Sprintf(`{"results":[{"task_id":%q,"status":"complete"},{"task_id":%q,"status":"blocked"}]}`, testSecondID, testFirstID),
		"extra-field": fmt.Sprintf(`{"results":[{"task_id":%q,"status":"complete","why":"x"},{"task_id":%q,"status":"blocked"}]}`, testFirstID, testSecondID),
		"bad-status":  fmt.Sprintf(`{"results":[{"task_id":%q,"status":"running"},{"task_id":%q,"status":"blocked"}]}`, testFirstID, testSecondID),
	} {
		if got, err := decodeClassifierResults([]byte(value), tasks); err == nil || got != nil {
			t.Errorf("%s classifier result = %#v, %v", name, got, err)
		}
	}
}

func TestOnboardStreamBatchesSequentiallyAndFailsClosedPerBatch(t *testing.T) {
	_, index := testIndex(t)
	index.setTitle(t, testActiveID, "Installing ThreadBear")
	index.setTitle(t, testAlreadyID, "✅ Already decorated")
	index.setRaw(t, testRawID)
	index.setInternalTask(t, testDelegatedID, "Internal worker", completedTurn("request", "work result without a footer"))
	for number := 1; number <= 10; number++ {
		id := fmt.Sprintf("10000000-0000-0000-0000-%012x", number)
		index.setTask(t, id, fmt.Sprintf("Task %d", number), completedTurn("request", "work result without a footer"))
	}
	previous := runOnboardClassifier
	var batches []int
	runOnboardClassifier = func(_ context.Context, tasks []onboardEvidence) ([]string, error) {
		batches = append(batches, len(tasks))
		if len(batches) == 2 {
			return nil, fmt.Errorf("model unavailable")
		}
		statuses := make([]string, len(tasks))
		for index := range statuses {
			statuses[index] = "complete"
		}
		return statuses, nil
	}
	t.Cleanup(func() { runOnboardClassifier = previous })
	var output bytes.Buffer
	if err := runOnboardStream(t.Context(), testActiveID, &output); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(batches, []int{8, 2}) {
		t.Fatalf("classifier batches = %v", batches)
	}
	records := decodeOnboardStream(t, output.Bytes())
	if len(records) != 11 {
		t.Fatalf("stream record count = %d", len(records))
	}
	for index := 0; index < 8; index++ {
		if records[index].Status != "complete" || records[index].Provenance != "inferred" {
			t.Fatalf("inferred record %d = %#v", index, records[index])
		}
	}
	for index := 8; index < 10; index++ {
		if records[index].Status != "unknown" || records[index].Provenance != "unknown" {
			t.Fatalf("failed batch record %d = %#v", index, records[index])
		}
	}
	if summary := records[10]; summary.Kind != "summary" || summary.Total != 14 || summary.Eligible != 10 ||
		summary.Inferred != 8 || summary.Unknown != 2 || summary.Skipped != 4 {
		t.Fatalf("summary = %#v", summary)
	}
	requests := fixtureRequests(t)
	if countFixtureMethod(requests, "thread/list") != 1 || countFixtureMethod(requests, "thread/turns/list") != 10 ||
		countFixtureMethod(requests, "thread/name/set") != 0 {
		t.Fatalf("onboarding RPCs = %#v", requests)
	}
}

func TestOnboardStreamUsesExactAndLeavesNewestIncompleteUnknown(t *testing.T) {
	_, index := testIndex(t)
	index.setTask(t, testFirstID, "Exact", completedTurn("request", "done\n🧵🐻 complete"))
	index.setTask(t, testSecondID, "Interrupted", []appServerTurn{{ID: "turn-2", Status: "interrupted", Items: []appServerTurnItem{{Type: "agentMessage", Phase: "final_answer", Text: "🧵🐻 complete"}}}})
	previous := runOnboardClassifier
	runOnboardClassifier = func(_ context.Context, _ []onboardEvidence) ([]string, error) {
		t.Fatal("exact and interrupted rows must not invoke model")
		return nil, nil
	}
	t.Cleanup(func() { runOnboardClassifier = previous })
	var output bytes.Buffer
	if err := runOnboardStream(t.Context(), testActiveID, &output); err != nil {
		t.Fatal(err)
	}
	records := decodeOnboardStream(t, output.Bytes())
	if records[0].Status != "complete" || records[0].Provenance != "exact" ||
		records[1].Status != "unknown" || records[1].Provenance != "unknown" {
		t.Fatalf("mixed records = %#v", records)
	}
}

func completedTurn(user, final string) []appServerTurn {
	return []appServerTurn{{ID: "turn-1", Status: "completed", Items: []appServerTurnItem{
		{Type: "userMessage", Text: user},
		{Type: "agentMessage", Phase: "final_answer", Text: final},
	}}}
}

type onboardTestRecord struct {
	Kind          string `json:"kind"`
	TaskID        string `json:"task_id"`
	SnapshotTitle string `json:"snapshot_title"`
	Status        string `json:"status"`
	Provenance    string `json:"provenance"`
	Total         int    `json:"total"`
	Eligible      int    `json:"eligible"`
	Exact         int    `json:"exact"`
	Inferred      int    `json:"inferred"`
	Unknown       int    `json:"unknown"`
	Skipped       int    `json:"skipped"`
}

func decodeOnboardStream(t testing.TB, data []byte) []onboardTestRecord {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(data))
	var records []onboardTestRecord
	for {
		var record onboardTestRecord
		if err := decoder.Decode(&record); err != nil {
			if err == io.EOF {
				return records
			}
			t.Fatal(err)
		}
		records = append(records, record)
	}
}
