package main

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ericlitman/threadbear/internal/codex"
	"github.com/ericlitman/threadbear/internal/state"
	"github.com/ericlitman/threadbear/internal/title"
	"github.com/ericlitman/threadbear/internal/tokens"
)

type cleanupInventoryFake struct {
	tasks  map[string]codex.Task
	calls  int
	before func(int)
	during func(context.Context, int) error
	errors map[int]error
}

func (f *cleanupInventoryFake) Inventory(ctx context.Context, _ string) (codex.Inventory, error) {
	f.calls++
	if f.before != nil {
		f.before(f.calls)
	}
	if f.during != nil {
		if err := f.during(ctx, f.calls); err != nil {
			return codex.Inventory{}, err
		}
	}
	if err := f.errors[f.calls]; err != nil {
		return codex.Inventory{}, err
	}
	ids := make([]string, 0, len(f.tasks))
	for id := range f.tasks {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	tasks := make([]codex.Task, 0, len(ids))
	for _, id := range ids {
		tasks = append(tasks, f.tasks[id])
	}
	return codex.Inventory{Tasks: tasks}, nil
}

type cleanupClientFake struct {
	inventory   *cleanupInventoryFake
	calls       []string
	fail        map[string]error
	persist     bool
	persistTask map[string]bool
	persisted   func(string) string
	closeErr    error
}

func (f *cleanupClientFake) SetTitle(_ context.Context, taskID, value string) error {
	f.calls = append(f.calls, taskID+"="+value)
	if err := f.fail[taskID]; err != nil {
		return err
	}
	persist := f.persist
	if f.persistTask != nil {
		persist = f.persistTask[taskID]
	}
	if persist {
		task := f.inventory.tasks[taskID]
		persisted := title.PersistedTitle
		if f.persisted != nil {
			persisted = f.persisted
		}
		task.Title = persisted(value)
		task.Revision += "x"
		f.inventory.tasks[taskID] = task
	}
	return nil
}

func (f *cleanupClientFake) Close() error { return f.closeErr }

func useVirtualCleanupTiming(cleaner *activeTitleCleaner) *int {
	now := time.Unix(0, 0)
	waits := 0
	cleaner.now = func() time.Time { return now }
	cleaner.wait = func(ctx context.Context, duration time.Duration) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		waits++
		now = now.Add(duration)
		return nil
	}
	return &waits
}

func cleanupState(records ...state.TaskRecord) state.State {
	committed := state.New()
	for _, record := range records {
		committed.Tasks[record.TaskID] = record
	}
	return committed
}

func cleanupRecord(taskID, title string) state.TaskRecord {
	return state.TaskRecord{
		TaskID: taskID, LastAppliedTitle: title, DurableSubject: strings.TrimSuffix(strings.TrimPrefix(title, "✅ "), " · 26k"),
		ManagedTokenDisplay: "26k", ManagedTokenPosition: tokens.PositionEnd,
	}
}

func TestActiveTitleCleanerSelectsManagedActiveTasksDeterministically(t *testing.T) {
	inventory := &cleanupInventoryFake{tasks: map[string]codex.Task{
		"b":        {TaskID: "b", Revision: "2", Title: "✅ Beta · 26k"},
		"a":        {TaskID: "a", Revision: "1", Title: "✅ Alpha · 26k"},
		"archived": {TaskID: "archived", Revision: "3", Title: "✅ Archived · 26k", Archived: true},
		"control":  {TaskID: "control", Revision: "4", Title: "✅ Control · 26k"},
		"other":    {TaskID: "other", Revision: "5", Title: "✅ Other · 26k"},
	}}
	client := &cleanupClientFake{inventory: inventory, persist: true}
	cleaner := activeTitleCleaner{inventory: inventory, open: func(context.Context) (titleCleanupClient, error) { return client, nil }}
	committed := cleanupState(cleanupRecord("a", "✅ Alpha · 26k"), cleanupRecord("b", "✅ Beta · 26k"), cleanupRecord("archived", "✅ Archived · 26k"), cleanupRecord("control", "✅ Control · 26k"))
	cleaned, err := cleaner.CleanActiveTitles(context.Background(), "control", committed)
	if err != nil || cleaned != 2 || !reflect.DeepEqual(client.calls, []string{"a=Alpha", "b=Beta"}) {
		t.Fatalf("cleaned=%d err=%v calls=%v", cleaned, err, client.calls)
	}
}

func TestActiveTitleCleanerAbortsOnRevisionOrTitleDriftBeforeWrite(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*codex.Task)
	}{
		{name: "revision", mutate: func(task *codex.Task) { task.Revision = "2" }},
		{name: "title", mutate: func(task *codex.Task) { task.Title = "Operator edit" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			inventory := &cleanupInventoryFake{tasks: map[string]codex.Task{"a": {TaskID: "a", Revision: "1", Title: "✅ Alpha · 26k"}}}
			inventory.before = func(call int) {
				if call == 2 {
					task := inventory.tasks["a"]
					test.mutate(&task)
					inventory.tasks["a"] = task
				}
			}
			client := &cleanupClientFake{inventory: inventory, persist: true}
			cleaner := activeTitleCleaner{inventory: inventory, open: func(context.Context) (titleCleanupClient, error) { return client, nil }}
			cleaned, err := cleaner.CleanActiveTitles(context.Background(), "control", cleanupState(cleanupRecord("a", "✅ Alpha · 26k")))
			if cleaned != 0 || err == nil || !strings.Contains(err.Error(), "drifted") || len(client.calls) != 0 {
				t.Fatalf("cleaned=%d err=%v calls=%v", cleaned, err, client.calls)
			}
		})
	}
}

func TestActiveTitleCleanerAbortsOnWriteAndPermanentVerificationFailures(t *testing.T) {
	for _, test := range []struct {
		name    string
		client  func(*cleanupInventoryFake) *cleanupClientFake
		virtual bool
		message string
	}{
		{name: "write", client: func(inventory *cleanupInventoryFake) *cleanupClientFake {
			return &cleanupClientFake{inventory: inventory, fail: map[string]error{"a": errors.New("setter unavailable")}, persist: true}
		}, message: "write task a title"},
		{name: "permanently absent", client: func(inventory *cleanupInventoryFake) *cleanupClientFake {
			return &cleanupClientFake{inventory: inventory}
		}, virtual: true, message: "not visible after application within 5s"},
	} {
		t.Run(test.name, func(t *testing.T) {
			inventory := &cleanupInventoryFake{tasks: map[string]codex.Task{"a": {TaskID: "a", Revision: "1", Title: "✅ Alpha · 26k"}}}
			client := test.client(inventory)
			cleaner := activeTitleCleaner{inventory: inventory, open: func(context.Context) (titleCleanupClient, error) { return client, nil }}
			var waits *int
			if test.virtual {
				waits = useVirtualCleanupTiming(&cleaner)
			}
			cleaned, err := cleaner.CleanActiveTitles(context.Background(), "control", cleanupState(cleanupRecord("a", "✅ Alpha · 26k")))
			if cleaned != 0 || err == nil || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("cleaned=%d err=%v", cleaned, err)
			}
			if waits != nil && *waits != int(titleCleanupSettleTimeout/titleCleanupSettleInterval) {
				t.Fatalf("waits=%d", *waits)
			}
		})
	}
}

func TestActiveTitleCleanerPartialFailureRetriesOnlyUnsettledTitles(t *testing.T) {
	inventory := &cleanupInventoryFake{tasks: map[string]codex.Task{
		"a": {TaskID: "a", Revision: "1", Title: "✅ Alpha · 26k"},
		"b": {TaskID: "b", Revision: "1", Title: "✅ Beta · 26k"},
	}}
	client := &cleanupClientFake{inventory: inventory, persistTask: map[string]bool{"a": true, "b": false}}
	cleaner := activeTitleCleaner{inventory: inventory, open: func(context.Context) (titleCleanupClient, error) { return client, nil }}
	useVirtualCleanupTiming(&cleaner)
	committed := cleanupState(cleanupRecord("a", "✅ Alpha · 26k"), cleanupRecord("b", "✅ Beta · 26k"))
	cleaned, err := cleaner.CleanActiveTitles(context.Background(), "control", committed)
	if cleaned != 1 || err == nil || !strings.Contains(err.Error(), "after cleaning 1 title(s)") {
		t.Fatalf("first cleaned=%d err=%v", cleaned, err)
	}
	client.persistTask["b"] = true
	cleaned, err = cleaner.CleanActiveTitles(context.Background(), "control", committed)
	if err != nil || cleaned != 1 || !reflect.DeepEqual(client.calls, []string{"a=Alpha", "b=Beta", "b=Beta"}) {
		t.Fatalf("retry cleaned=%d err=%v calls=%v", cleaned, err, client.calls)
	}
	cleaned, err = cleaner.CleanActiveTitles(context.Background(), "control", committed)
	if err != nil || cleaned != 0 {
		t.Fatalf("settled cleaned=%d err=%v", cleaned, err)
	}
}

func TestActiveTitleCleanerUsesPendingAppliedOwnership(t *testing.T) {
	inventory := &cleanupInventoryFake{tasks: map[string]codex.Task{"a": {TaskID: "a", Revision: "2", Title: "✅ Alpha · 27k"}}}
	client := &cleanupClientFake{inventory: inventory, persist: true}
	cleaner := activeTitleCleaner{inventory: inventory, open: func(context.Context) (titleCleanupClient, error) { return client, nil }}
	committed := cleanupState(cleanupRecord("a", "✅ Alpha · 26k"))
	plan := state.PendingTitlePlan{TaskID: "a", ExpectedTitle: "✅ Alpha · 26k", DesiredTitle: "✅ Alpha · 27k", DurableSubject: "Alpha", ManagedTokenDisplay: "27k", ManagedTokenPosition: tokens.PositionEnd}
	committed.PendingTitlePlans["a"] = plan
	cleaned, err := cleaner.CleanActiveTitles(context.Background(), "control", committed)
	if err != nil || cleaned != 1 || !reflect.DeepEqual(client.calls, []string{"a=Alpha"}) {
		t.Fatalf("cleaned=%d err=%v calls=%v", cleaned, err, client.calls)
	}
}

func TestActiveTitleCleanerAcceptsFullAndShortenedRestoration(t *testing.T) {
	subject := strings.Repeat("a", 58) + "😀tail"
	managed := "➡️ " + subject + " · 26k"
	for _, test := range []struct {
		name      string
		persisted func(string) string
		want      string
	}{
		{name: "full", persisted: func(value string) string { return value }, want: subject},
		{name: "shortened", persisted: title.PersistedTitle, want: title.PersistedTitle(subject)},
	} {
		t.Run(test.name, func(t *testing.T) {
			inventory := &cleanupInventoryFake{tasks: map[string]codex.Task{"a": {TaskID: "a", Revision: "1", Title: title.PersistedTitle(managed)}}}
			client := &cleanupClientFake{inventory: inventory, persist: true, persisted: test.persisted}
			cleaner := activeTitleCleaner{inventory: inventory, open: func(context.Context) (titleCleanupClient, error) { return client, nil }}
			record := state.TaskRecord{TaskID: "a", LastAppliedTitle: managed, DurableSubject: subject, ManagedTokenDisplay: "26k", ManagedTokenPosition: tokens.PositionEnd}
			cleaned, err := cleaner.CleanActiveTitles(context.Background(), "control", cleanupState(record))
			if err != nil || cleaned != 1 || len(client.calls) != 1 || inventory.tasks["a"].Title != test.want {
				t.Fatalf("cleaned=%d err=%v calls=%v task=%+v", cleaned, err, client.calls, inventory.tasks["a"])
			}
		})
	}
}

func TestActiveTitleCleanerWaitsForDelayedVisibility(t *testing.T) {
	inventory := &cleanupInventoryFake{tasks: map[string]codex.Task{"a": {TaskID: "a", Revision: "1", Title: "✅ Alpha · 26k"}}}
	inventory.before = func(call int) {
		if call == 5 {
			task := inventory.tasks["a"]
			task.Title = "Alpha"
			inventory.tasks["a"] = task
		}
	}
	client := &cleanupClientFake{inventory: inventory}
	cleaner := activeTitleCleaner{inventory: inventory, open: func(context.Context) (titleCleanupClient, error) { return client, nil }}
	waits := useVirtualCleanupTiming(&cleaner)
	cleaned, err := cleaner.CleanActiveTitles(context.Background(), "control", cleanupState(cleanupRecord("a", "✅ Alpha · 26k")))
	if err != nil || cleaned != 1 || *waits != 2 || inventory.calls != 5 {
		t.Fatalf("cleaned=%d err=%v waits=%d inventory_calls=%d", cleaned, err, *waits, inventory.calls)
	}
}

func TestActiveTitleCleanerFailsClosedOnInvalidPostWriteState(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*cleanupInventoryFake)
	}{
		{name: "missing", mutate: func(inventory *cleanupInventoryFake) { delete(inventory.tasks, "a") }},
		{name: "archived", mutate: func(inventory *cleanupInventoryFake) {
			task := inventory.tasks["a"]
			task.Archived = true
			inventory.tasks["a"] = task
		}},
		{name: "third title", mutate: func(inventory *cleanupInventoryFake) {
			task := inventory.tasks["a"]
			task.Title = "Operator edit"
			inventory.tasks["a"] = task
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			inventory := &cleanupInventoryFake{tasks: map[string]codex.Task{"a": {TaskID: "a", Revision: "1", Title: "✅ Alpha · 26k"}}}
			inventory.before = func(call int) {
				if call == 3 {
					test.mutate(inventory)
				}
			}
			client := &cleanupClientFake{inventory: inventory}
			cleaner := activeTitleCleaner{inventory: inventory, open: func(context.Context) (titleCleanupClient, error) { return client, nil }}
			cleaned, err := cleaner.CleanActiveTitles(context.Background(), "control", cleanupState(cleanupRecord("a", "✅ Alpha · 26k")))
			if cleaned != 0 || err == nil || !strings.Contains(err.Error(), "not visible after application") {
				t.Fatalf("cleaned=%d err=%v", cleaned, err)
			}
		})
	}
}

func TestActiveTitleCleanerFailsClosedOnPostWriteInventoryError(t *testing.T) {
	inventory := &cleanupInventoryFake{
		tasks:  map[string]codex.Task{"a": {TaskID: "a", Revision: "1", Title: "✅ Alpha · 26k"}},
		errors: map[int]error{3: errors.New("sqlite unavailable")},
	}
	client := &cleanupClientFake{inventory: inventory}
	cleaner := activeTitleCleaner{inventory: inventory, open: func(context.Context) (titleCleanupClient, error) { return client, nil }}
	cleaned, err := cleaner.CleanActiveTitles(context.Background(), "control", cleanupState(cleanupRecord("a", "✅ Alpha · 26k")))
	if cleaned != 0 || err == nil || !strings.Contains(err.Error(), "verify task a title: sqlite unavailable") {
		t.Fatalf("cleaned=%d err=%v", cleaned, err)
	}
}

func TestActiveTitleCleanerRejectsSuccessObservedAfterCancellation(t *testing.T) {
	inventory := &cleanupInventoryFake{tasks: map[string]codex.Task{"a": {TaskID: "a", Revision: "1", Title: "✅ Alpha · 26k"}}}
	ctx, cancel := context.WithCancel(context.Background())
	inventory.during = func(context.Context, int) error {
		if inventory.calls == 3 {
			cancel()
		}
		return nil
	}
	client := &cleanupClientFake{inventory: inventory, persist: true}
	cleaner := activeTitleCleaner{inventory: inventory, open: func(context.Context) (titleCleanupClient, error) { return client, nil }}
	cleaned, err := cleaner.CleanActiveTitles(ctx, "control", cleanupState(cleanupRecord("a", "✅ Alpha · 26k")))
	if cleaned != 0 || !errors.Is(err, context.Canceled) {
		t.Fatalf("cleaned=%d err=%v", cleaned, err)
	}
}

func TestActiveTitleCleanerFailsClosedOnSettlementCancellation(t *testing.T) {
	inventory := &cleanupInventoryFake{tasks: map[string]codex.Task{"a": {TaskID: "a", Revision: "1", Title: "✅ Alpha · 26k"}}}
	client := &cleanupClientFake{inventory: inventory}
	ctx, cancel := context.WithCancel(context.Background())
	cleaner := activeTitleCleaner{inventory: inventory, open: func(context.Context) (titleCleanupClient, error) { return client, nil }}
	cleaner.wait = func(context.Context, time.Duration) error {
		cancel()
		return ctx.Err()
	}
	cleaned, err := cleaner.CleanActiveTitles(ctx, "control", cleanupState(cleanupRecord("a", "✅ Alpha · 26k")))
	if cleaned != 0 || !errors.Is(err, context.Canceled) {
		t.Fatalf("cleaned=%d err=%v", cleaned, err)
	}
}

func TestTitleCleanupRecordRequiresSucceededSameTitleRefresh(t *testing.T) {
	record := cleanupRecord("a", "✅ Alpha · 26k")
	task := codex.Task{TaskID: "a", Title: record.LastAppliedTitle}
	plan := state.PendingTitlePlan{TaskID: "a", ExpectedTitle: task.Title, DesiredTitle: task.Title, DurableSubject: "Refreshed"}
	if got := titleCleanupRecord(record, plan, task); got.DurableSubject != record.DurableSubject {
		t.Fatalf("pending same-title refresh changed ownership: %+v", got)
	}
	plan.NativeOutcome = state.NativeTitleSucceeded
	if got := titleCleanupRecord(record, plan, task); got.DurableSubject != "Refreshed" {
		t.Fatalf("succeeded same-title refresh did not change ownership: %+v", got)
	}
}

func TestActiveTitleCleanerInventoryFailureAndSettledNoOp(t *testing.T) {
	inventory := &cleanupInventoryFake{tasks: map[string]codex.Task{"a": {TaskID: "a", Revision: "1", Title: "✅ Alpha · 26k"}}, errors: map[int]error{1: errors.New("sqlite unavailable")}}
	opens := 0
	cleaner := activeTitleCleaner{inventory: inventory, open: func(context.Context) (titleCleanupClient, error) {
		opens++
		return &cleanupClientFake{inventory: inventory, persist: true}, nil
	}}
	if _, err := cleaner.CleanActiveTitles(context.Background(), "control", cleanupState(cleanupRecord("a", "✅ Alpha · 26k"))); err == nil || !strings.Contains(err.Error(), "inventory active managed tasks") || opens != 0 {
		t.Fatalf("err=%v opens=%d", err, opens)
	}
	inventory.errors = nil
	inventory.tasks["a"] = codex.Task{TaskID: "a", Revision: "2", Title: "Alpha"}
	cleaned, err := cleaner.CleanActiveTitles(context.Background(), "control", cleanupState(cleanupRecord("a", "✅ Alpha · 26k")))
	if err != nil || cleaned != 0 || opens != 0 {
		t.Fatalf("cleaned=%d err=%v opens=%d", cleaned, err, opens)
	}
}
