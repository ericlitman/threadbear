package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestAppServerCurrentBudgetFitsTerminalCall(t *testing.T) {
	if appServerCurrentTimeout != 3*time.Second {
		t.Fatalf("App Server current-task timeout = %s", appServerCurrentTimeout)
	}
}

func TestAppServerInventoryExhaustsPagesBeforeDedupe(t *testing.T) {
	_, _ = testIndex(t)
	installAppServerFixture(t, "multipage")
	result, err := runOnboarding(t.Context(), false, "")
	if err != nil {
		t.Fatal(err)
	}
	tasks := result.Items
	if len(tasks) != 4 {
		t.Fatalf("inventory = %#v", tasks)
	}
	for index, id := range []string{testAlphaID, testRawID, testDelegatedID, testDuplicateID} {
		if tasks[index].TaskID != id {
			t.Fatalf("inventory order = %#v", tasks)
		}
	}
	if tasks[3].Title != "First duplicate title" || tasks[1].Safe {
		t.Fatalf("inventory authority = %#v", tasks)
	}
}

func TestAppServerInventoryFailsBeforeStateWrites(t *testing.T) {
	_, _ = testIndex(t)
	installAppServerFixture(t, "page-failure")
	if result, err := runOnboarding(t.Context(), false, ""); err == nil || result.PlanComplete || !strings.Contains(err.Error(), "page 2") {
		t.Fatalf("failed inventory = %#v, %v", result, err)
	}
	entries, err := os.ReadDir(newStore(stateDir()).subjectDir())
	if err != nil || len(entries) != 0 {
		t.Fatalf("failed inventory wrote state: %#v, %v", entries, err)
	}
}

func TestAppServerNonzeroExitCannotOverturnCompleteProof(t *testing.T) {
	_, index := testIndex(t)
	index.setTitle(t, testTaskID, "Stable subject")
	installAppServerFixture(t, "close-nonzero")
	if result, err := runCurrentTitle(t.Context(), testTaskID, "complete"); err != nil || !result.Ready || result.Title != "✅ Stable subject" {
		t.Fatalf("current proof = %#v, %v", result, err)
	}
	clearFixtureRequests(t)
	index.setTitle(t, testAlphaID, "Alpha")
	if result, err := runOnboarding(t.Context(), true, testActiveID); err != nil || !result.Ready || !result.OnboardingComplete || result.Updated != 2 {
		t.Fatalf("onboarding proof = %#v, %v", result, err)
	}
}

func TestAppServerCurrentFailuresStartOnlyOnce(t *testing.T) {
	for _, test := range []struct {
		name, scenario, contains string
	}{
		{"missing", "current-missing", "current task is absent"},
		{"protocol", "current-protocol", "current thread/list page"},
		{"response ID", "current-response-id", "unexpected Codex App Server response ID"},
		{"timeout", "current-timeout", "context deadline exceeded"},
		{"unclean", "current-unclean", "set Codex App Server task name"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, index := testIndex(t)
			if test.scenario == "current-unclean" {
				index.setTitle(t, testTaskID, "Stable subject")
			}
			if test.scenario == "current-timeout" {
				setAppServerCurrentBudget(t, 150*time.Millisecond)
			}
			starts := installAppServerFixture(t, test.scenario)
			if _, err := runCurrentTitle(t.Context(), testTaskID, "complete"); err == nil || !strings.Contains(err.Error(), test.contains) {
				t.Fatalf("runCurrentTitle err=%v", err)
			}
			data, err := os.ReadFile(starts)
			if test.scenario == "current-timeout" && os.IsNotExist(err) {
				return // CommandContext may kill the wrapper before its one start marker.
			}
			if err != nil || string(data) != "x" {
				t.Fatalf("App Server starts = %q, %v", data, err)
			}
		})
	}
}

func installAppServerFixture(t testing.TB, scenario string) string {
	t.Helper()
	dir := t.TempDir()
	path, starts := filepath.Join(dir, "codex"), filepath.Join(dir, "starts")
	requests := filepath.Join(dir, "requests.jsonl")
	raceMarker := filepath.Join(dir, "concurrent-rename")
	script := "#!/bin/sh\nprintf x >> \"$THREADBEAR_APP_SERVER_STARTS\"\nexec \"$THREADBEAR_TEST_BINARY\" -test.run=^TestAppServerFixtureProcess$ -- \"$@\"\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("THREADBEAR_TEST_BINARY", os.Args[0])
	t.Setenv("THREADBEAR_APP_SERVER_FIXTURE", scenario)
	t.Setenv("THREADBEAR_APP_SERVER_STARTS", starts)
	t.Setenv("THREADBEAR_APP_SERVER_REQUESTS", requests)
	t.Setenv("THREADBEAR_APP_SERVER_RACE_MARKER", raceMarker)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return starts
}

func TestAppServerFixtureProcess(t *testing.T) {
	scenario := os.Getenv("THREADBEAR_APP_SERVER_FIXTURE")
	if scenario == "" {
		return
	}
	if len(os.Args) < 3 || os.Args[len(os.Args)-2] != "app-server" || os.Args[len(os.Args)-1] != "--stdio" {
		t.Fatalf("fixture args = %#v", os.Args)
	}
	decoder, encoder := json.NewDecoder(os.Stdin), json.NewEncoder(os.Stdout)
	initialize := readFixtureMessage(t, decoder)
	if initialize.ID != 1 || initialize.Method != "initialize" {
		t.Fatalf("initialize = %#v", initialize)
	}
	fixtureLogRequest(t, initialize)
	if err := encoder.Encode(map[string]any{"id": 1, "result": map[string]any{"serverInfo": map[string]any{"name": "fixture"}}}); err != nil {
		t.Fatal(err)
	}
	if err := encoder.Encode(map[string]any{"method": "server/notification", "params": map[string]any{}}); err != nil {
		t.Fatal(err)
	}
	initialized := readFixtureMessage(t, decoder)
	fixtureLogRequest(t, initialized)
	if initialized.ID != 0 || initialized.Method != "initialized" {
		t.Fatalf("initialized = %#v", initialized)
	}
	setCalls := make(map[string]int)
	listCalls := 0
	for {
		request, err := decodeFixtureMessage(decoder)
		if err == io.EOF {
			if scenario == "close-nonzero" {
				os.Exit(7)
			}
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		fixtureLogRequest(t, request)
		switch request.Method {
		case "thread/list":
			listCalls++
			serveFixtureList(t, scenario, listCalls, request, encoder)
		case "thread/read":
			serveFixtureRead(t, scenario, setCalls, request, encoder)
		case "thread/name/set":
			serveFixtureSet(t, scenario, setCalls, request, encoder)
		default:
			t.Fatalf("unexpected fixture method %q", request.Method)
		}
	}
}

func serveFixtureList(t testing.TB, scenario string, call int, request fixtureMessage, encoder *json.Encoder) {
	t.Helper()
	switch scenario {
	case "current-timeout":
		time.Sleep(2 * time.Second)
		return
	case "current-protocol":
		_, _ = fmt.Fprint(os.Stdout, `{"id":2,"result":`)
		os.Exit(0)
	case "current-response-id":
		_ = encoder.Encode(map[string]any{"id": request.ID + 7, "result": map[string]any{"data": []any{}, "nextCursor": nil}})
		return
	case "current-missing":
		_ = encoder.Encode(map[string]any{"id": request.ID, "result": map[string]any{
			"data": []map[string]any{{"id": testOtherID, "name": "Other", "preview": "private"}}, "nextCursor": nil,
		}})
		return
	case "multipage", "page-failure":
		serveFixturePages(t, scenario, request, encoder)
		return
	}
	tasks := fixtureReadTasks(t)
	ids := make([]string, 0, len(tasks))
	for id := range tasks {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	rows := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		rows = append(rows, map[string]any{"id": id, "name": tasks[id].Name, "preview": tasks[id].Preview})
	}
	if err := encoder.Encode(map[string]any{"id": request.ID, "result": map[string]any{"data": rows, "nextCursor": nil}}); err != nil {
		t.Fatal(err)
	}
	if scenario == "current-concurrent-rename" && call == 1 {
		// The initial title is already on the wire. Apply the user's rename before
		// reading ThreadBear's next (and only) set request.
		rename := "User rename during delayed write"
		task := tasks[testTaskID]
		task.Name = &rename
		tasks[testTaskID] = task
		fixtureWriteTasks(t, tasks)
		if err := os.WriteFile(os.Getenv("THREADBEAR_APP_SERVER_RACE_MARKER"), []byte(rename+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if scenario == "current-unclean" && call == 1 {
		os.Exit(7)
	}
}

func serveFixturePages(t testing.TB, scenario string, request fixtureMessage, encoder *json.Encoder) {
	t.Helper()
	var cursor string
	_ = json.Unmarshal(request.Params["cursor"], &cursor)
	if cursor == "" {
		_ = encoder.Encode(map[string]any{"id": request.ID, "result": map[string]any{
			"data":       []map[string]any{{"id": testRawID, "name": nil}, {"id": testDuplicateID, "name": "First duplicate title"}},
			"nextCursor": "page-2",
		}})
		return
	}
	if scenario == "page-failure" {
		_ = encoder.Encode(map[string]any{"id": request.ID, "error": map[string]any{"code": -32000}})
		return
	}
	_ = encoder.Encode(map[string]any{"id": request.ID, "result": map[string]any{
		"data": []map[string]any{
			{"id": testDelegatedID, "name": "Delegated task"}, {"id": testAlphaID, "name": "Alpha"},
			{"id": testDuplicateID, "name": "Later duplicate title"},
		},
		"nextCursor": nil,
	}})
}

func serveFixtureRead(t testing.TB, scenario string, setCalls map[string]int, request fixtureMessage, encoder *json.Encoder) {
	t.Helper()
	id := fixtureStringParam(t, request, "threadId")
	tasks := fixtureReadTasks(t)
	task, ok := tasks[id]
	if !ok {
		_ = encoder.Encode(map[string]any{"id": request.ID, "error": map[string]any{"code": -32004}})
		return
	}
	name := task.Name
	if scenario == "onboarding-slow-readback" && setCalls[id] > 0 {
		time.Sleep(300 * time.Millisecond)
	}
	if scenario == "onboarding-races" {
		switch id {
		case testDriftID:
			value := "Renamed after snapshot"
			name = &value
		case testBlankAfterID:
			name = nil
		case testUnconfirmedID:
			// The setter acknowledges this target but its exact readback never changes.
			_ = setCalls[id]
		}
	}
	_ = encoder.Encode(map[string]any{"id": request.ID, "result": map[string]any{
		"thread": map[string]any{"id": id, "name": name, "preview": task.Preview},
	}})
}

func serveFixtureSet(t testing.TB, scenario string, setCalls map[string]int, request fixtureMessage, encoder *json.Encoder) {
	t.Helper()
	id, name := fixtureStringParam(t, request, "threadId"), fixtureStringParam(t, request, "name")
	setCalls[id]++
	tasks := fixtureReadTasks(t)
	if scenario != "current-readback-mismatch" && !(scenario == "onboarding-races" && id == testUnconfirmedID) {
		task := tasks[id]
		task.Name = &name
		tasks[id] = task
		fixtureWriteTasks(t, tasks)
	}
	if scenario == "current-set-error" {
		_ = encoder.Encode(map[string]any{"id": request.ID, "error": map[string]any{"code": -32001}})
		return
	}
	// The real response is an empty object. Its contents are never title proof.
	_ = encoder.Encode(map[string]any{"id": request.ID, "result": map[string]any{}})
}

type fixtureMessage struct {
	ID     int                        `json:"id"`
	Method string                     `json:"method"`
	Params map[string]json.RawMessage `json:"params"`
}

func readFixtureMessage(t testing.TB, decoder *json.Decoder) fixtureMessage {
	t.Helper()
	message, err := decodeFixtureMessage(decoder)
	if err != nil {
		t.Fatal(err)
	}
	return message
}

func decodeFixtureMessage(decoder *json.Decoder) (fixtureMessage, error) {
	var message fixtureMessage
	err := decoder.Decode(&message)
	return message, err
}

func fixtureStringParam(t testing.TB, request fixtureMessage, key string) string {
	t.Helper()
	var value string
	if json.Unmarshal(request.Params[key], &value) != nil {
		t.Fatalf("%s param = %s", key, request.Params[key])
	}
	return value
}

func fixtureLogRequest(t testing.TB, request fixtureMessage) {
	t.Helper()
	file, err := os.OpenFile(os.Getenv("THREADBEAR_APP_SERVER_REQUESTS"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.NewEncoder(file).Encode(request); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func fixtureReadTasks(t testing.TB) map[string]appServerFixtureTask {
	t.Helper()
	data, err := os.ReadFile(os.Getenv("THREADBEAR_APP_SERVER_TASKS"))
	if err != nil {
		t.Fatal(err)
	}
	var tasks map[string]appServerFixtureTask
	if json.Unmarshal(data, &tasks) != nil || tasks == nil {
		t.Fatal("invalid fixture task registry")
	}
	return tasks
}

func fixtureWriteTasks(t testing.TB, tasks map[string]appServerFixtureTask) {
	t.Helper()
	data, err := json.Marshal(tasks)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(os.Getenv("THREADBEAR_APP_SERVER_TASKS"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}
