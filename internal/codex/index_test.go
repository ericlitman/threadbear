package codex

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ericlitman/threadbear/internal/state"
	_ "modernc.org/sqlite"
)

func TestIndexDiscoversHighestNumberedDatabase(t *testing.T) {
	home := t.TempDir()
	copyFixture(t, "state_5.sqlite", filepath.Join(home, "state_4.sqlite"))
	copyFixture(t, "state_5.sqlite", filepath.Join(home, "state_12.sqlite"))
	if err := os.WriteFile(filepath.Join(home, "state.sqlite"), []byte("ignored"), 0600); err != nil {
		t.Fatal(err)
	}
	index, err := OpenIndex(home)
	if err != nil {
		t.Fatal(err)
	}
	defer index.Close()
	if got, want := index.Path(), filepath.Join(home, "state_12.sqlite"); got != want {
		t.Fatalf("Path() = %q, want %q", got, want)
	}
	if !strings.Contains(index.uri, "mode=ro") {
		t.Fatalf("index URI is not read-only: %s", index.uri)
	}
}

func TestIndexFailsClosedOnHigherSchemaDrift(t *testing.T) {
	home := t.TempDir()
	copyFixture(t, "state_5.sqlite", filepath.Join(home, "state_5.sqlite"))
	copyFixture(t, "schema-drift.sqlite", filepath.Join(home, "state_6.sqlite"))
	_, err := OpenIndex(home)
	if !errors.Is(err, ErrSchema) {
		t.Fatalf("OpenIndex() error = %v, want ErrSchema", err)
	}
}

func TestIndexFailsClosedOnUnboundedHigherVersion(t *testing.T) {
	home := t.TempDir()
	copyFixture(t, "state_5.sqlite", filepath.Join(home, "state_5.sqlite"))
	copyFixture(t, "schema-drift.sqlite", filepath.Join(home, "state_999999999999999999999999.sqlite"))
	_, err := OpenIndex(home)
	if !errors.Is(err, ErrSchema) {
		t.Fatalf("OpenIndex() error = %v, want ErrSchema", err)
	}
}

func TestIndexHandlesEscapedHomePath(t *testing.T) {
	home := filepath.Join(t.TempDir(), "Codex home #1")
	if err := os.Mkdir(home, 0700); err != nil {
		t.Fatal(err)
	}
	copyFixture(t, "state_5.sqlite", filepath.Join(home, "state_5.sqlite"))
	index, err := OpenIndex(home)
	if err != nil {
		t.Fatal(err)
	}
	defer index.Close()
	if _, err := index.Inventory(context.Background(), "control-123"); err != nil {
		t.Fatal(err)
	}
}

func TestIndexResolvesCodexHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	got, err := ResolveCodexHome()
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.Abs(home)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("ResolveCodexHome() = %q, want %q", got, want)
	}
}

func TestIndexReportsMissingDatabase(t *testing.T) {
	_, err := OpenIndex(t.TempDir())
	if !errors.Is(err, ErrIndexNotFound) {
		t.Fatalf("OpenIndex() error = %v, want ErrIndexNotFound", err)
	}
}

func TestInventoryIncludesEveryDesktopShape(t *testing.T) {
	index := openFixture(t, "state_5.sqlite")
	inventory, err := index.Inventory(context.Background(), "control-123")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(inventory.Tasks), 137; got != want {
		t.Fatalf("Inventory() returned %d tasks, want %d", got, want)
	}
	if !slices.IsSortedFunc(inventory.Tasks, func(left, right Task) int { return strings.Compare(left.TaskID, right.TaskID) }) {
		t.Fatal("Inventory() is not deterministically ordered")
	}
	byID := make(map[string]Task, len(inventory.Tasks))
	for _, task := range inventory.Tasks {
		byID[task.TaskID] = task
		if task.Archived {
			t.Fatalf("archived task returned: %s", task.TaskID)
		}
		if task.Source != "vscode" {
			t.Fatalf("non-Desktop task returned: %s", task.TaskID)
		}
	}
	for id, source := range map[string]string{
		"task-001": "user",
		"task-002": "automation",
		"task-003": "project",
		"task-004": "subagent",
	} {
		if got := byID[id].ThreadSource; got != source {
			t.Fatalf("%s thread source = %q, want %q", id, got, source)
		}
	}
	if got := byID["task-005"].RolloutPath; got != "" {
		t.Fatalf("missing rollout became %q", got)
	}
	if got, want := byID["task-007"].Revision, "1700001007"; got != want {
		t.Fatalf("task-007 revision = %q, want %q", got, want)
	}
	if got, want := byID["task-007"].Title, "Synthetic task 7"; got != want {
		t.Fatalf("task-007 title = %q, want %q", got, want)
	}
	if got, want := byID["task-007"].RolloutPath, "/synthetic/rollouts/task-007.jsonl"; got != want {
		t.Fatalf("task-007 rollout = %q, want %q", got, want)
	}
	if _, ok := byID["control-123"]; ok {
		t.Fatal("control task was not excluded")
	}
	if _, ok := byID["archived-1"]; ok {
		t.Fatal("archived task was included")
	}
	if _, ok := byID["cli-1"]; ok {
		t.Fatal("non-Desktop task was included")
	}
}

func TestInventoryDoesNotRequirePreview(t *testing.T) {
	index := openFixture(t, "state_5.sqlite")
	inventory, err := index.Inventory(context.Background(), "control-123")
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range inventory.Tasks {
		if task.TaskID == "task-006" {
			return
		}
	}
	t.Fatal("task with empty preview was omitted")
}

func TestInventoryComparisonSelectsChangesAndRetries(t *testing.T) {
	captured := Inventory{Tasks: []Task{
		{TaskID: "added", Revision: "1", Title: "Added"},
		{TaskID: "retry", Revision: "2", Title: "Retry"},
		{TaskID: "revision", Revision: "3", Title: "Revision"},
		{TaskID: "stable", Revision: "4", Title: "Stable"},
		{TaskID: "title", Revision: "5", Title: "New title"},
	}}
	committed := state.New()
	committed.Tasks = map[string]state.TaskRecord{
		"removed":  {TaskID: "removed", CapturedRevision: "1", LastAppliedTitle: "Removed"},
		"retry":    {TaskID: "retry", CapturedRevision: "2", LastAppliedTitle: "Retry", Retry: &state.Retry{Operation: "classify", ErrorCode: "temporary", Attempts: 1, LastAttemptAt: time.Unix(1, 0), NextAttemptAt: time.Unix(2, 0)}},
		"revision": {TaskID: "revision", CapturedRevision: "old", LastAppliedTitle: "Revision"},
		"stable":   {TaskID: "stable", CapturedRevision: "4", LastAppliedTitle: "Stable"},
		"title":    {TaskID: "title", CapturedRevision: "5", LastAppliedTitle: "Old title"},
	}
	comparison := CompareInventory(captured, committed)
	gotChanged := make([]string, 0, len(comparison.Changed))
	for _, task := range comparison.Changed {
		gotChanged = append(gotChanged, task.TaskID)
	}
	if want := []string{"added", "retry", "revision", "title"}; !reflect.DeepEqual(gotChanged, want) {
		t.Fatalf("changed IDs = %v, want %v", gotChanged, want)
	}
	if want := []string{"removed"}; !reflect.DeepEqual(comparison.RemovedIDs, want) {
		t.Fatalf("removed IDs = %v, want %v", comparison.RemovedIDs, want)
	}
	if comparison.Unchanged() {
		t.Fatal("comparison reported changes as unchanged")
	}
}

func TestInventoryComparisonRecognizesUnchangedGeneration(t *testing.T) {
	captured := Inventory{Tasks: []Task{{TaskID: "task-a", Revision: "10", Title: "Title"}}}
	committed := state.New()
	committed.Tasks["task-a"] = state.TaskRecord{TaskID: "task-a", CapturedRevision: "10", LastAppliedTitle: "Title"}
	if comparison := CompareInventory(captured, committed); !comparison.Unchanged() {
		t.Fatalf("unchanged comparison = %#v", comparison)
	}
}

func TestSchemaIsValidatedBeforeEveryInventory(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "state_5.sqlite")
	copyFixture(t, "state_5.sqlite", path)
	index, err := OpenIndex(home)
	if err != nil {
		t.Fatal(err)
	}
	defer index.Close()
	if _, err := index.Inventory(context.Background(), "control-123"); err != nil {
		t.Fatal(err)
	}
	writer, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Exec("ALTER TABLE threads RENAME COLUMN title TO heading"); err != nil {
		writer.Close()
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := index.Inventory(context.Background(), "control-123"); !errors.Is(err, ErrSchema) {
		t.Fatalf("Inventory() error = %v, want ErrSchema", err)
	}
}

func TestSchemaDriftFixtureIsRejected(t *testing.T) {
	home := t.TempDir()
	copyFixture(t, "schema-drift.sqlite", filepath.Join(home, "state_5.sqlite"))
	_, err := OpenIndex(home)
	if !errors.Is(err, ErrSchema) || !strings.Contains(err.Error(), "rollout_path") {
		t.Fatalf("OpenIndex() error = %v, want rollout_path schema error", err)
	}
}

func TestIndexIsStrictlyReadOnly(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "state_5.sqlite")
	copyFixture(t, "state_5.sqlite", path)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	beforeEntries := directoryNames(t, home)
	index, err := OpenIndex(home)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := index.Inventory(context.Background(), "control-123"); err != nil {
		index.Close()
		t.Fatal(err)
	}
	transaction, err := index.db.Begin()
	if err != nil {
		index.Close()
		t.Fatal(err)
	}
	if _, err := transaction.Exec("UPDATE threads SET title = 'mutated'"); err == nil {
		transaction.Rollback()
		index.Close()
		t.Fatal("read-only index allowed a transactional write")
	}
	if err := transaction.Rollback(); err != nil {
		index.Close()
		t.Fatal(err)
	}
	if err := index.Close(); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("read-only inventory changed database bytes")
	}
	if afterEntries := directoryNames(t, home); !reflect.DeepEqual(afterEntries, beforeEntries) {
		t.Fatalf("read-only inventory changed directory entries: %v to %v", beforeEntries, afterEntries)
	}
}

func TestInventoryBoundaryHasNoProcessOrModelSurface(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), entry.Name(), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imported := range parsed.Imports {
			path := strings.Trim(imported.Path.Value, `"`)
			if path == "os/exec" || strings.Contains(path, "appserver") || strings.Contains(path, "model") {
				t.Fatalf("inventory package imports forbidden process/model surface %q", path)
			}
		}
	}
}

func openFixture(t *testing.T, name string) *Index {
	t.Helper()
	home := t.TempDir()
	copyFixture(t, name, filepath.Join(home, "state_5.sqlite"))
	index, err := OpenIndex(home)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := index.Close(); err != nil {
			t.Error(err)
		}
	})
	return index
}

func copyFixture(t *testing.T, name, destination string) {
	t.Helper()
	source := filepath.Join("..", "..", "testdata", "index", name)
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, data, 0600); err != nil {
		t.Fatal(err)
	}
}

func directoryNames(t *testing.T, path string) []string {
	t.Helper()
	names := make([]string, 0)
	err := filepath.WalkDir(path, func(current string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if current != path {
			names = append(names, strings.TrimPrefix(current, path+string(filepath.Separator)))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(names)
	return names
}
