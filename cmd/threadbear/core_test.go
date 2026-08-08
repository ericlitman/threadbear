package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
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
	setAppServerCurrentBudget(t, appServerCurrentTimeout)
	root := t.TempDir()
	codex := filepath.Join(root, "codex")
	if err := os.Mkdir(codex, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", root)
	t.Setenv("CODEX_HOME", codex)
	if err := os.MkdirAll(newStore(stateDir()).subjectDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir(), "lifecycle.lock"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	index := &testTaskIndex{path: filepath.Join(root, "appserver-tasks.json"), tasks: make(map[string]appServerFixtureTask)}
	index.write(t)
	t.Setenv("THREADBEAR_APP_SERVER_TASKS", index.path)
	installAppServerFixture(t, "registry")
	return root, index
}

func setAppServerCurrentBudget(t testing.TB, budget time.Duration) {
	t.Helper()
	previous := appServerCurrentBudget
	appServerCurrentBudget = budget
	t.Cleanup(func() { appServerCurrentBudget = previous })
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

func TestCurrentTitlePlansOneAppNativeWriteFromExactListName(t *testing.T) {
	root, index := testIndex(t)
	addTask(t, index, root, testTaskID, "stale SQLite title", "Stable subject", "vscode", 0)
	result, err := runCurrentTitle(t.Context(), testTaskID, "complete")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Ready || !result.WriteRequired || result.Unchanged ||
		result.PreviousTitle != "Stable subject" || result.DesiredTitle != "✅ Stable subject" ||
		result.Reason != "app-native title write required" {
		t.Fatalf("title result = %#v", result)
	}
	if got := index.title(t, testTaskID); got != "Stable subject" {
		t.Fatalf("planner mutated native name = %q", got)
	}
	if record, err := newStore(stateDir()).readTask(testTaskID); err != nil || record.Subject != "Stable subject" {
		t.Fatalf("subject record = %#v, %v", record, err)
	}
	requests := fixtureRequests(t)
	if countFixtureMethod(requests, "thread/name/set") != 0 || countFixtureMethod(requests, "thread/list") != 1 ||
		countFixtureMethod(requests, "thread/read") != 0 {
		t.Fatalf("RPC sequence = %#v", requests)
	}
	list := fixtureMethod(requests, "thread/list", 0)
	var limit int
	var archived bool
	if json.Unmarshal(list.Params["limit"], &limit) != nil || limit != appServerListLimit ||
		json.Unmarshal(list.Params["archived"], &archived) != nil || archived ||
		fixtureStringParam(t, list, "sortKey") != "recency_at" ||
		fixtureStringParam(t, list, "sortDirection") != "desc" {
		t.Fatalf("current lookup params = %#v", list.Params)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"ready", "task_id", "status", "previous_title", "desired_title", "write_required", "unchanged", "reason"} {
		if _, exists := fields[expected]; !exists {
			t.Fatalf("planner omitted field %q: %s", expected, encoded)
		}
	}
	for _, obsolete := range []string{"title", "updated", "unconfirmed"} {
		if _, exists := fields[obsolete]; exists {
			t.Fatalf("planner emitted obsolete field %q: %s", obsolete, encoded)
		}
	}
}

func TestCurrentTitlePreservesFiniteOwnershipAndUserRename(t *testing.T) {
	_, index := testIndex(t)
	index.setTitle(t, testTaskID, "Initial subject")
	first, err := runCurrentTitle(t.Context(), testTaskID, "complete")
	if err != nil {
		t.Fatal(err)
	}
	index.setTitle(t, testTaskID, first.DesiredTitle) // Simulate the separate app-native setter.
	second, err := runCurrentTitle(t.Context(), testTaskID, "automation")
	if err != nil || second.DesiredTitle != "🤖 Initial subject" {
		t.Fatalf("second plan = %#v, %v", second, err)
	}
	index.setTitle(t, testTaskID, second.DesiredTitle) // Simulate the separate app-native setter.
	index.setTitle(t, testTaskID, "✅ Quarterly  close ")
	result, err := runCurrentTitle(t.Context(), testTaskID, "complete")
	if err != nil || result.DesiredTitle != "✅ ✅ Quarterly  close " || !result.WriteRequired {
		t.Fatalf("verbatim rename = %#v, %v", result, err)
	}
	if got := index.title(t, testTaskID); got != "✅ Quarterly  close " {
		t.Fatalf("planner mutated user rename = %q", got)
	}
	if record, err := newStore(stateDir()).readTask(testTaskID); err != nil || record.Subject != "✅ Quarterly  close " {
		t.Fatalf("renamed record = %#v, %v", record, err)
	}
}

func TestCurrentTitleAlreadyExactDoesNotCallSetter(t *testing.T) {
	_, index := testIndex(t)
	index.setTitle(t, testTaskID, "✅ Stable subject")
	if err := newStore(stateDir()).updateTask(testTaskID, func(record *taskState) (bool, error) {
		record.Subject = "Stable subject"
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	result, err := runCurrentTitle(t.Context(), testTaskID, "complete")
	if err != nil || !result.Ready || result.WriteRequired || !result.Unchanged ||
		result.PreviousTitle != "✅ Stable subject" || result.DesiredTitle != "✅ Stable subject" ||
		result.Reason != "native title already matches desired title" {
		t.Fatalf("unchanged result = %#v, %v", result, err)
	}
	requests := fixtureRequests(t)
	if countFixtureMethod(requests, "thread/name/set") != 0 || countFixtureMethod(requests, "thread/list") != 1 ||
		countFixtureMethod(requests, "thread/read") != 0 {
		t.Fatalf("unchanged RPCs = %#v", requests)
	}
}

func TestCurrentTitleDoesNotWriteUnsafeOrBlankNativeNames(t *testing.T) {
	for name, setup := range map[string]func(testing.TB, *testTaskIndex){
		"blank": func(t testing.TB, index *testTaskIndex) { index.setRaw(t, testTaskID) },
		"internal": func(t testing.TB, index *testTaskIndex) {
			index.setTitle(t, testTaskID, "<codex_delegation>private</codex_delegation>")
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, index := testIndex(t)
			setup(t, index)
			result, err := runCurrentTitle(t.Context(), testTaskID, "complete")
			if err == nil || result.PreviousTitle != "" || result.DesiredTitle != "" {
				t.Fatalf("unsafe result = %#v, %v", result, err)
			}
			requests := fixtureRequests(t)
			if countFixtureMethod(requests, "thread/name/set") != 0 || countFixtureMethod(requests, "thread/list") != 1 ||
				countFixtureMethod(requests, "thread/read") != 0 {
				t.Fatalf("unsafe planner RPCs = %#v", requests)
			}
			if _, err := newStore(stateDir()).readTask(testTaskID); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("unsafe task wrote subject state: %v", err)
			}
		})
	}
}

func TestCurrentTitleAcceptsBoundedNoCASConcurrentRenameWithoutWriting(t *testing.T) {
	_, index := testIndex(t)
	index.setTitle(t, testTaskID, "Stable subject")
	starts := installAppServerFixture(t, "current-concurrent-rename")

	result, err := runCurrentTitle(t.Context(), testTaskID, "complete")
	if err != nil || !result.Ready || !result.WriteRequired || result.Unchanged ||
		result.PreviousTitle != "Stable subject" || result.DesiredTitle != "✅ Stable subject" {
		t.Fatalf("concurrent rename result = %#v, %v", result, err)
	}
	marker, err := os.ReadFile(os.Getenv("THREADBEAR_APP_SERVER_RACE_MARKER"))
	if err != nil || string(marker) != "User rename during delayed write\n" {
		t.Fatalf("concurrent rename marker = %q, %v", marker, err)
	}
	if got := index.title(t, testTaskID); got != "User rename during delayed write" {
		t.Fatalf("planner overwrote concurrent rename = %q", got)
	}
	requests := fixtureRequests(t)
	if countFixtureMethod(requests, "thread/name/set") != 0 ||
		countFixtureMethod(requests, "thread/list") != 1 ||
		countFixtureMethod(requests, "thread/read") != 0 {
		t.Fatalf("no-CAS RPC sequence = %#v", requests)
	}
	if data, err := os.ReadFile(starts); err != nil || string(data) != "x" {
		t.Fatalf("App Server starts = %q, %v", data, err)
	}
}

func TestOnboardingPlanMatchesConfirmedCorpusAndSkipsActiveTask(t *testing.T) {
	_, index := testIndex(t)
	index.setTitle(t, testActiveID, "Active task")
	index.setTitle(t, testAlphaID, "Alpha")
	index.setTitle(t, testAlreadyID, "🐻 Beta")
	index.setRaw(t, testRawID)
	if err := newStore(stateDir()).updateTask(testAlreadyID, func(record *taskState) (bool, error) {
		record.Subject = "Beta"
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}

	plan, err := runOnboarding(t.Context(), false, testActiveID)
	if err != nil || !plan.Ready || !plan.PlanComplete || !plan.ReadOnly || plan.OnboardingComplete ||
		plan.Total != 4 || plan.Safe != 3 || plan.NeedsUpdate != 1 || plan.Unchanged != 2 || plan.Skipped != 1 {
		t.Fatalf("onboarding plan = %#v, %v", plan, err)
	}
	active := onboardingItemByID(t, plan.Items, testActiveID)
	if active.Outcome != onboardingUnchanged || active.Reason != "active task is handled by the terminal title writer" {
		t.Fatalf("active plan item = %#v", active)
	}
	if countFixtureMethod(fixtureRequests(t), "thread/read") != 0 || countFixtureMethod(fixtureRequests(t), "thread/name/set") != 0 {
		t.Fatal("read-only plan performed target RPCs")
	}
	if _, err := newStore(stateDir()).readTask(testAlphaID); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read-only plan wrote subject state: %v", err)
	}
	clearFixtureRequests(t)

	prepared, err := runOnboarding(t.Context(), true, testActiveID)
	if err != nil || !prepared.Ready || !prepared.PlanComplete || prepared.ReadOnly || prepared.OnboardingComplete ||
		prepared.Prepared != 1 || prepared.NeedsUpdate != 1 || prepared.Unchanged != 2 || prepared.Skipped != 1 {
		t.Fatalf("onboarding preparation = %#v, %v", prepared, err)
	}
	if index.title(t, testActiveID) != "Active task" || index.title(t, testAlphaID) != "Alpha" {
		t.Fatalf("planner mutated native titles: active=%q alpha=%q", index.title(t, testActiveID), index.title(t, testAlphaID))
	}
	requests := fixtureRequests(t)
	if countFixtureMethod(requests, "thread/name/set") != 0 || countFixtureMethod(requests, "thread/read") != 0 {
		t.Fatalf("onboarding RPCs = %#v", requests)
	}
	alpha := onboardingItemByID(t, prepared.Items, testAlphaID)
	if alpha.Outcome != onboardingPrepared || alpha.Reason != "app-native title write required" {
		t.Fatalf("prepared item = %#v", alpha)
	}
	encoded, err := json.Marshal(prepared)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	for _, obsolete := range []string{"updated", "unconfirmed"} {
		if _, exists := fields[obsolete]; exists {
			t.Fatalf("preparation emitted obsolete field %q: %s", obsolete, encoded)
		}
	}
	if strings.Contains(string(encoded), `"applied"`) {
		t.Fatalf("preparation emitted obsolete item field: %s", encoded)
	}
	if record, err := newStore(stateDir()).readTask(testAlphaID); err != nil || record.Subject != "Alpha" {
		t.Fatalf("prepared subject = %#v, %v", record, err)
	}
	if _, err := newStore(stateDir()).readTask(testActiveID); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("active task wrote state: %v", err)
	}
}

func TestOnboardingPreparesEverySafeSnapshotCandidateWithoutTargetRPCs(t *testing.T) {
	_, index := testIndex(t)
	for id, title := range map[string]string{
		testActiveID: "Active", testAlphaID: "Alpha", testAlreadyID: "🐻 Beta", testBlankAfterID: "Blank later",
		testDriftID: "Drift", testUnconfirmedID: "Unconfirmed",
	} {
		index.setTitle(t, id, title)
	}
	index.setRaw(t, testRawID)
	if err := newStore(stateDir()).updateTask(testAlreadyID, func(record *taskState) (bool, error) {
		record.Subject = "Beta"
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	result, err := runOnboarding(t.Context(), true, testActiveID)
	if err != nil || !result.Ready || !result.PlanComplete || result.ReadOnly || result.OnboardingComplete ||
		result.Total != 7 || result.Safe != 6 || result.NeedsUpdate != 4 || result.Prepared != 4 ||
		result.Unchanged != 2 || result.Skipped != 1 {
		t.Fatalf("snapshot onboarding = %#v, %v", result, err)
	}
	requests := fixtureRequests(t)
	if countFixtureMethod(requests, "thread/name/set") != 0 ||
		countFixtureMethod(requests, "thread/read") != 0 || countFixtureMethod(requests, "thread/list") != 1 {
		t.Fatalf("snapshot preparation calls = %#v", requests)
	}
	for _, id := range []string{testActiveID, testRawID} {
		if _, err := newStore(stateDir()).readTask(id); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s wrote subject state: %v", id, err)
		}
	}
	for _, id := range []string{testAlphaID, testBlankAfterID, testDriftID, testUnconfirmedID} {
		item := onboardingItemByID(t, result.Items, id)
		if item.Outcome != onboardingPrepared {
			t.Fatalf("prepared item = %#v", item)
		}
		if record, err := newStore(stateDir()).readTask(id); err != nil || record.Subject != item.Subject {
			t.Fatalf("prepared state for %s = %#v, %v", id, record, err)
		}
	}
}

func TestPreparedSubjectYieldsToLaterSafeUserRename(t *testing.T) {
	_, index := testIndex(t)
	index.setTitle(t, testAlphaID, "Alpha")
	prepared, err := runOnboarding(t.Context(), true, testActiveID)
	if err != nil || prepared.Prepared != 1 {
		t.Fatalf("initial preparation = %#v, %v", prepared, err)
	}
	index.setTitle(t, testAlphaID, "Renamed after snapshot")
	clearFixtureRequests(t)

	plan, err := runCurrentTitle(t.Context(), testAlphaID, "complete")
	if err != nil || !plan.Ready || plan.PreviousTitle != "Renamed after snapshot" ||
		plan.DesiredTitle != "✅ Renamed after snapshot" || !plan.WriteRequired {
		t.Fatalf("later rename plan = %#v, %v", plan, err)
	}
	if record, err := newStore(stateDir()).readTask(testAlphaID); err != nil || record.Subject != "Renamed after snapshot" {
		t.Fatalf("later rename subject = %#v, %v", record, err)
	}
	if got := index.title(t, testAlphaID); got != "Renamed after snapshot" {
		t.Fatalf("planner mutated later rename = %q", got)
	}
	if requests := fixtureRequests(t); countFixtureMethod(requests, "thread/read") != 0 ||
		countFixtureMethod(requests, "thread/name/set") != 0 {
		t.Fatalf("later rename RPCs = %#v", requests)
	}
}

func TestUnsafeActiveOnboardingTaskDoesNotInflateSafeCount(t *testing.T) {
	_, index := testIndex(t)
	index.setRaw(t, testActiveID)
	result, err := runOnboarding(t.Context(), false, testActiveID)
	if err != nil || !result.Ready || result.Safe != 0 || result.NeedsUpdate != 0 || result.Unchanged != 1 || !result.OnboardingComplete {
		t.Fatalf("unsafe active plan = %#v, %v", result, err)
	}
	item := onboardingItemByID(t, result.Items, testActiveID)
	if item.Safe || item.Title != "" || item.Subject != "" || item.DesiredTitle != "" || item.Outcome != onboardingUnchanged {
		t.Fatalf("unsafe active item = %#v", item)
	}
}

func TestCurrentPlannerCannotOutliveLifecycleFence(t *testing.T) {
	_, index := testIndex(t)
	index.setTitle(t, testTaskID, "Stable subject")
	path := filepath.Join(stateDir(), "lifecycle.lock")
	lifecycle, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := unix.Flock(int(lifecycle.Fd()), unix.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := runCurrentTitle(context.Background(), testTaskID, "complete")
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "lifecycle is busy") {
			unlock(lifecycle)
			t.Fatalf("planner with exclusive lifecycle fence = %v", err)
		}
	case <-time.After(250 * time.Millisecond):
		unlock(lifecycle)
		t.Fatal("planner waited behind lifecycle teardown")
	}
	unlock(lifecycle)
	if _, err := os.Stat(os.Getenv("THREADBEAR_APP_SERVER_REQUESTS")); err == nil {
		requests := fixtureRequests(t)
		if len(requests) != 0 {
			t.Fatalf("busy planner started App Server: %#v", requests)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
}

func TestConfirmedOnboardingHoldsOneFenceAcrossSerialPreparation(t *testing.T) {
	_, index := testIndex(t)
	index.setTitle(t, testAlphaID, "Alpha")
	index.setTitle(t, testOtherID, "Other")
	stateLock, err := newStore(stateDir()).lock(testAlphaID)
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := runOnboarding(context.Background(), true, testActiveID)
		done <- err
	}()
	deadline := time.Now().Add(time.Second)
	for _, err := os.Stat(os.Getenv("THREADBEAR_APP_SERVER_REQUESTS")); errors.Is(err, os.ErrNotExist) && time.Now().Before(deadline); _, err = os.Stat(os.Getenv("THREADBEAR_APP_SERVER_REQUESTS")) {
		time.Sleep(10 * time.Millisecond)
	}
	for countFixtureMethod(fixtureRequests(t), "thread/list") == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if countFixtureMethod(fixtureRequests(t), "thread/list") == 0 {
		unlock(stateLock)
		t.Fatal("onboarding did not complete its snapshot")
	}

	locked := make(chan *os.File, 1)
	lockErr := make(chan error, 1)
	go func() {
		lock, err := existingLifecycleLock("lifecycle.lock")
		if err != nil {
			lockErr <- err
			return
		}
		locked <- lock
	}()
	select {
	case lock := <-locked:
		unlock(lock)
		unlock(stateLock)
		t.Fatal("replacement lifecycle entered during onboarding")
	case err := <-lockErr:
		unlock(stateLock)
		t.Fatalf("replacement lifecycle failed while waiting: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	unlock(stateLock)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	select {
	case lock := <-locked:
		unlock(lock)
	case err := <-lockErr:
		t.Fatal(err)
	case <-time.After(time.Second):
		t.Fatal("replacement lifecycle did not resume after onboarding")
	}
	if requests := fixtureRequests(t); countFixtureMethod(requests, "thread/name/set") != 0 ||
		countFixtureMethod(requests, "thread/read") != 0 {
		t.Fatalf("preparation attempted target RPC: %#v", requests)
	}
}

func TestOnboardingDryRunDoesNotTakeLifecycleFence(t *testing.T) {
	_, index := testIndex(t)
	index.setTitle(t, testAlphaID, "Alpha")
	path := filepath.Join(stateDir(), "lifecycle.lock")
	lifecycle, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := unix.Flock(int(lifecycle.Fd()), unix.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	result, err := runOnboarding(t.Context(), false, testActiveID)
	unlock(lifecycle)
	if err != nil || !result.Ready || !result.ReadOnly || !result.PlanComplete || result.NeedsUpdate != 1 {
		t.Fatalf("dry-run under lifecycle operation = %#v, %v", result, err)
	}
}

func TestConfirmedOnboardingRefusesBusyLifecycleBeforePreparation(t *testing.T) {
	_, index := testIndex(t)
	index.setTitle(t, testAlphaID, "Alpha")
	path := filepath.Join(stateDir(), "lifecycle.lock")
	lifecycle, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := unix.Flock(int(lifecycle.Fd()), unix.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	result, err := runOnboarding(t.Context(), true, testActiveID)
	unlock(lifecycle)
	if err == nil || !strings.Contains(err.Error(), "lifecycle is busy") || result.Ready || result.PlanComplete {
		t.Fatalf("confirmed onboarding under lifecycle operation = %#v, %v", result, err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("confirmed onboarding waited behind lifecycle operation for %s", elapsed)
	}
	if _, err := os.Stat(os.Getenv("THREADBEAR_APP_SERVER_REQUESTS")); err == nil {
		if requests := fixtureRequests(t); len(requests) != 0 {
			t.Fatalf("busy confirmed onboarding started App Server: %#v", requests)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
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

func TestNoSQLiteDependency(t *testing.T) {
	root, index := testIndex(t)
	index.setTitle(t, testTaskID, "Visible")
	if _, err := runCurrentTitle(t.Context(), testTaskID, "complete"); err != nil {
		t.Fatal(err)
	}
	if matches, err := filepath.Glob(filepath.Join(root, "**", "state_*.sqlite")); err != nil || len(matches) != 0 {
		t.Fatalf("SQLite appeared: %#v, %v", matches, err)
	}
	if strings.Contains(index.title(t, testTaskID), "state_") {
		t.Fatal("unexpected fixture corruption")
	}
}
