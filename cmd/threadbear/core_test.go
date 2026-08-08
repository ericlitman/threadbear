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
	Name    *string `json:"name"`
	Preview string  `json:"preview"`
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
		!containsString(result.OwnedPrefixes, "🐻 ") || !containsString(result.BlockedPrefixes, "🧵🐻") {
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

func TestOnboardingPlanAndPreparationAreCompleteAndStateless(t *testing.T) {
	_, index := testIndex(t)
	index.setTitle(t, testActiveID, "Active task")
	index.setTitle(t, testAlphaID, "Alpha")
	index.setTitle(t, testAlreadyID, "🐻 Beta")
	index.setRaw(t, testRawID)

	plan, err := runOnboarding(t.Context(), false, testActiveID)
	if err != nil || !plan.Ready || !plan.PlanComplete || !plan.ReadOnly || plan.OnboardingComplete ||
		plan.Total != 4 || plan.Safe != 3 || plan.NeedsUpdate != 1 || plan.Unchanged != 2 || plan.Skipped != 1 {
		t.Fatalf("onboarding preview = %#v, %v", plan, err)
	}
	if active := onboardingItemByID(t, plan.Items, testActiveID); active.Outcome != onboardingUnchanged {
		t.Fatalf("active preview item = %#v", active)
	}
	clearFixtureRequests(t)

	prepared, err := runOnboarding(t.Context(), true, testActiveID)
	if err != nil || !prepared.Ready || !prepared.PlanComplete || prepared.ReadOnly || prepared.OnboardingComplete ||
		prepared.Prepared != 1 || prepared.NeedsUpdate != 1 || prepared.Unchanged != 2 || prepared.Skipped != 1 {
		t.Fatalf("onboarding preparation = %#v, %v", prepared, err)
	}
	alpha := onboardingItemByID(t, prepared.Items, testAlphaID)
	if alpha.Outcome != onboardingPrepared || alpha.Title != "Alpha" || alpha.DesiredTitle != "🐻 Alpha" {
		t.Fatalf("prepared item = %#v", alpha)
	}
	if requests := fixtureRequests(t); countFixtureMethod(requests, "thread/list") != 1 ||
		countFixtureMethod(requests, "thread/read") != 0 || countFixtureMethod(requests, "thread/name/set") != 0 {
		t.Fatalf("onboarding RPCs = %#v", requests)
	}
	if _, err := os.Stat(stateDir()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("onboarding preparation created ThreadBear state: %v", err)
	}
}

func TestUnsafeActiveOnboardingTaskDoesNotInflateSafeCount(t *testing.T) {
	_, index := testIndex(t)
	index.setRaw(t, testActiveID)
	result, err := runOnboarding(t.Context(), false, testActiveID)
	if err != nil || !result.Ready || result.Safe != 0 || result.NeedsUpdate != 0 ||
		result.Unchanged != 1 || !result.OnboardingComplete {
		t.Fatalf("unsafe active plan = %#v, %v", result, err)
	}
	item := onboardingItemByID(t, result.Items, testActiveID)
	if item.Safe || item.Title != "" || item.Subject != "" || item.DesiredTitle != "" || item.Outcome != onboardingUnchanged {
		t.Fatalf("unsafe active item = %#v", item)
	}
}

func onboardingItemByID(t testing.TB, items []onboardingItem, id string) onboardingItem {
	t.Helper()
	for _, item := range items {
		if item.TaskID == id {
			return item
		}
	}
	t.Fatalf("missing onboarding item %q", id)
	return onboardingItem{}
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
