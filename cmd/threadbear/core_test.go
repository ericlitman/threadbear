package main

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type testTaskIndex struct {
	path  string
	tasks map[string]appServerFixtureTask
}

type appServerFixtureTask struct {
	Name           *string         `json:"name"`
	Preview        string          `json:"preview"`
	Turns          []appServerTurn `json:"turns,omitempty"`
	Ephemeral      bool            `json:"ephemeral,omitempty"`
	ParentThreadID *string         `json:"parent_thread_id,omitempty"`
}

func testIndex(t testing.TB) (string, *testTaskIndex) {
	t.Helper()
	root := t.TempDir()
	codexHome := filepath.Join(root, "codex")
	if err := os.Mkdir(codexHome, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", root)
	t.Setenv("CODEX_HOME", codexHome)
	index := &testTaskIndex{path: filepath.Join(root, "appserver-tasks.json"), tasks: make(map[string]appServerFixtureTask)}
	index.write(t)
	t.Setenv("THREADBEAR_APP_SERVER_TASKS", index.path)
	installAppServerFixture(t, "registry")
	return root, index
}

func (index *testTaskIndex) write(t testing.TB) {
	t.Helper()
	data, err := json.Marshal(index.tasks)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(index.path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func (index *testTaskIndex) setTitle(t testing.TB, id, title string) {
	t.Helper()
	index.tasks[id] = appServerFixtureTask{Name: &title, Preview: "private raw preview"}
	index.write(t)
}

func (index *testTaskIndex) setRaw(t testing.TB, id string) {
	t.Helper()
	index.tasks[id] = appServerFixtureTask{Preview: "private raw preview"}
	index.write(t)
}

func (index *testTaskIndex) setTask(t testing.TB, id, title string, turns []appServerTurn) {
	t.Helper()
	index.tasks[id] = appServerFixtureTask{Name: &title, Preview: "private raw preview", Turns: turns}
	index.write(t)
}

func (index *testTaskIndex) setInternalTask(t testing.TB, id, title string, turns []appServerTurn) {
	t.Helper()
	parent := testActiveID
	index.tasks[id] = appServerFixtureTask{Name: &title, Preview: "private raw preview", Turns: turns, ParentThreadID: &parent}
	index.write(t)
}

func (index *testTaskIndex) title(t testing.TB, id string) string {
	t.Helper()
	data, err := os.ReadFile(index.path)
	if err != nil {
		t.Fatal(err)
	}
	var tasks map[string]appServerFixtureTask
	if err := json.Unmarshal(data, &tasks); err != nil {
		t.Fatal(err)
	}
	if tasks[id].Name == nil {
		return ""
	}
	return *tasks[id].Name
}

func addTask(t testing.TB, index *testTaskIndex, root, id, title string, name any, source string, archived int) string {
	t.Helper()
	rollout := filepath.Join(root, id+".jsonl")
	if err := os.WriteFile(rollout, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	visible := title
	if value, ok := name.(string); ok {
		visible = value
	}
	index.setTitle(t, id, visible)
	_ = source
	_ = archived
	return rollout
}

func TestCurrentTitleReturnsStatelessMountedPolicy(t *testing.T) {
	root, _ := testIndex(t)
	result, err := runCurrentTitle(t.Context(), testTaskID, "complete")
	if err != nil || !result.Ready || result.TaskID != testTaskID || result.Status != "complete" ||
		result.Icon != "✅" || result.MaxTitleUnits != 60 ||
		!containsString(result.OwnedPrefixes, "🐻 ") || !containsString(result.OwnedPrefixes, "✅✦ ") ||
		!containsString(result.BlockedPrefixes, "🧵🐻") {
		t.Fatalf("title policy = %#v, %v", result, err)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"previous_title", "desired_title", "write_required", "subject"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("stateless title policy contains %q: %s", forbidden, encoded)
		}
	}
	if _, err := os.Stat(stateDir()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("ordinary policy created state: %v", err)
	}
	if _, err := os.Stat(os.Getenv("THREADBEAR_APP_SERVER_REQUESTS")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("ordinary policy started App Server: %v", err)
	}
	if matches, err := filepath.Glob(filepath.Join(root, "**", "state_*.sqlite")); err != nil || len(matches) != 0 {
		t.Fatalf("ordinary policy touched SQLite: %#v, %v", matches, err)
	}
}

func TestCurrentTitleRejectsInvalidContextWithoutSideEffects(t *testing.T) {
	_, _ = testIndex(t)
	for _, test := range []struct{ id, status string }{
		{"", "complete"}, {"not-a-task", "complete"}, {testTaskID, "running"},
	} {
		if _, err := runCurrentTitle(t.Context(), test.id, test.status); err == nil {
			t.Fatalf("invalid context %#v was accepted", test)
		}
	}
	if _, err := os.Stat(stateDir()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("invalid context created state: %v", err)
	}
}

func TestTitleCleanupPlanAndPreparationAreCompleteAndStateless(t *testing.T) {
	_, index := testIndex(t)
	index.setTitle(t, testActiveID, "✅ Active task")
	index.setTitle(t, testAlphaID, "Alpha")
	index.setTitle(t, testAlreadyID, "🐻 Beta")
	index.setRaw(t, testRawID)

	plan, err := runTitleCleanup(t.Context(), false, "")
	if err != nil || !plan.Ready || !plan.PlanComplete || !plan.ReadOnly ||
		plan.Total != 4 || plan.NeedsCleanup != 2 || plan.Unchanged != 1 || plan.Skipped != 1 {
		t.Fatalf("cleanup preview = %#v, %v", plan, err)
	}
	if bear := cleanupItemByID(t, plan.Items, testAlreadyID); bear.Outcome != cleanupNeedsUpdate || bear.DesiredTitle != "Beta" {
		t.Fatalf("bear preview item = %#v", bear)
	}
	clearFixtureRequests(t)

	prepared, err := runTitleCleanup(t.Context(), true, testActiveID)
	if err != nil || !prepared.Ready || !prepared.PlanComplete || prepared.ReadOnly ||
		prepared.Prepared != 2 || prepared.NeedsCleanup != 2 || prepared.Unchanged != 1 || prepared.Skipped != 1 {
		t.Fatalf("cleanup preparation = %#v, %v", prepared, err)
	}
	bear := cleanupItemByID(t, prepared.Items, testAlreadyID)
	if bear.Outcome != cleanupPrepared || bear.Title != "🐻 Beta" || bear.DesiredTitle != "Beta" {
		t.Fatalf("prepared item = %#v", bear)
	}
	if last := prepared.Items[len(prepared.Items)-1]; last.TaskID != testActiveID || last.DesiredTitle != "Active task" {
		t.Fatalf("active task was not prepared last: %#v", prepared.Items)
	}
	if requests := fixtureRequests(t); countFixtureMethod(requests, "thread/list") != 1 ||
		countFixtureMethod(requests, "thread/read") != 0 || countFixtureMethod(requests, "thread/name/set") != 0 {
		t.Fatalf("cleanup RPCs = %#v", requests)
	}
	if _, err := os.Stat(stateDir()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cleanup preparation created ThreadBear state: %v", err)
	}
}

func TestUnsafeActiveCleanupTaskStaysSkipped(t *testing.T) {
	_, index := testIndex(t)
	index.setRaw(t, testActiveID)
	result, err := runTitleCleanup(t.Context(), true, testActiveID)
	if err != nil || !result.Ready || result.NeedsCleanup != 0 || result.Prepared != 0 ||
		result.Unchanged != 0 || result.Skipped != 1 {
		t.Fatalf("unsafe active plan = %#v, %v", result, err)
	}
	item := cleanupItemByID(t, result.Items, testActiveID)
	if item.Title != "" || item.DesiredTitle != "" || item.Outcome != cleanupSkipped {
		t.Fatalf("unsafe active item = %#v", item)
	}
}

func TestTitleCleanupPreparesInferredPrefixRemoval(t *testing.T) {
	_, index := testIndex(t)
	index.setTitle(t, testFirstID, "🚨✦ Exact subject bytes ")
	result, err := runTitleCleanup(t.Context(), true, testActiveID)
	if err != nil || result.Prepared != 1 || result.NeedsCleanup != 1 {
		t.Fatalf("inferred cleanup = %#v, %v", result, err)
	}
	item := cleanupItemByID(t, result.Items, testFirstID)
	if item.Title != "🚨✦ Exact subject bytes " || item.DesiredTitle != "Exact subject bytes " {
		t.Fatalf("inferred cleanup item = %#v", item)
	}
}

func cleanupItemByID(t testing.TB, items []cleanupItem, id string) cleanupItem {
	t.Helper()
	for _, item := range items {
		if item.TaskID == id {
			return item
		}
	}
	t.Fatalf("missing cleanup item %q", id)
	return cleanupItem{}
}

func fixtureRequests(t testing.TB) []fixtureMessage {
	t.Helper()
	file, err := os.Open(os.Getenv("THREADBEAR_APP_SERVER_REQUESTS"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	var requests []fixtureMessage
	for {
		var request fixtureMessage
		if err := decoder.Decode(&request); err == io.EOF {
			return requests
		} else if err != nil {
			t.Fatal(err)
		}
		requests = append(requests, request)
	}
}

func clearFixtureRequests(t testing.TB) {
	t.Helper()
	if err := os.Remove(os.Getenv("THREADBEAR_APP_SERVER_REQUESTS")); err != nil {
		t.Fatal(err)
	}
}

func countFixtureMethod(requests []fixtureMessage, method string) int {
	count := 0
	for _, request := range requests {
		if request.Method == method {
			count++
		}
	}
	return count
}

func fixtureMethod(requests []fixtureMessage, method string, at int) fixtureMessage {
	for _, request := range requests {
		if request.Method == method {
			if at == 0 {
				return request
			}
			at--
		}
	}
	return fixtureMessage{}
}
