package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func testIndex(t testing.TB) (string, *sql.DB) {
	t.Helper()
	root := t.TempDir()
	codex := filepath.Join(root, "codex")
	if err := os.Mkdir(codex, 0o700); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(codex, "state_1.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	_, err = db.Exec(`CREATE TABLE threads (
		id TEXT PRIMARY KEY, updated_at_ms INTEGER, title TEXT, name TEXT, archived INTEGER,
		source TEXT, thread_source TEXT, rollout_path TEXT, first_user_message TEXT, preview TEXT)`)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", root)
	t.Setenv("CODEX_HOME", codex)
	return root, db
}

func addTask(t testing.TB, db *sql.DB, root, id, title string, name any, source string, archived int) string {
	t.Helper()
	rollout := filepath.Join(root, id+".jsonl")
	if err := os.WriteFile(rollout, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO threads VALUES (?,1,?,?,?,?,'',?,'',?)`, id, title, name, archived, source, rollout, id); err != nil {
		t.Fatal(err)
	}
	return rollout
}

func TestInventoryMatchesNativeAddressableTasks(t *testing.T) {
	root, db := testIndex(t)
	addTask(t, db, root, "desktop", "generated", "renamed", "vscode", 0)
	addTask(t, db, root, "cli", "", nil, "cli", 0)
	addTask(t, db, root, "mcp", "mcp title", nil, "mcp", 0)
	addTask(t, db, root, "exec", "exec title", nil, "exec", 0)
	addTask(t, db, root, "empty", "empty preview", nil, "vscode", 0)
	addTask(t, db, root, "archived", "old", nil, "vscode", 1)
	if _, err := db.Exec(`UPDATE threads SET preview='' WHERE id='empty'`); err != nil {
		t.Fatal(err)
	}
	tasks, err := inventory(context.Background())
	if err != nil || len(tasks) != 2 || tasks[0].ID != "cli" || tasks[1].ID != "desktop" || tasks[1].Title != "renamed" {
		t.Fatalf("inventory = %#v, %v", tasks, err)
	}
	got, found, err := oneTask(context.Background(), "desktop")
	if err != nil || !found || got.Title != "renamed" {
		t.Fatalf("oneTask = %#v, %v, %v", got, found, err)
	}
	for _, id := range []string{"mcp", "exec", "empty", "archived"} {
		if _, found, _ := oneTask(context.Background(), id); found {
			t.Fatalf("%s task was addressable", id)
		}
	}
	readOnly, err := openIndex()
	if err != nil {
		t.Fatal(err)
	}
	defer readOnly.Close()
	if _, err := readOnly.Exec(`INSERT INTO threads (id,archived) VALUES ('write',0)`); err == nil {
		t.Fatal("read-only index accepted a write")
	}
}

func TestSQLiteHomeFollowsCodexTOML(t *testing.T) {
	base := t.TempDir()
	t.Setenv("CODEX_HOME", base)
	if err := os.WriteFile(filepath.Join(base, "config.toml"), []byte(`sqlite_home = "state" # local database`), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := sqliteHome()
	if err != nil || got != filepath.Join(base, "state") {
		t.Fatalf("sqliteHome = %q, %v", got, err)
	}
}

func TestRolloutFooterUsesLatestExactTerminalMessage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	data := rolloutLine("response_item", map[string]any{"type": "message", "role": "assistant", "phase": "final_answer", "content": []map[string]string{{"text": "old\n\n🧵🐻 complete"}}}) +
		rolloutLine("response_item", map[string]any{"type": "message", "role": "assistant", "phase": "final_answer", "content": []map[string]string{{"text": "pick\n\n🧵🐻 needs input (you): choose the release region"}}})
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	got, ok := rolloutFooter(path)
	if !ok || got.Status != "needs_input" || got.Action != "choose the release region" {
		t.Fatalf("footer = %#v, %v", got, ok)
	}
	if err := os.WriteFile(path, []byte(rolloutLine("response_item", map[string]any{"type": "message", "role": "assistant", "phase": "final_answer", "content": []map[string]string{{"text": "legacy prose"}}})), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := rolloutFooter(path); ok {
		t.Fatal("legacy prose classified deterministically")
	}
}

func TestOrdinaryHooksRewriteVerifyAndBlockLostPost(t *testing.T) {
	root, db := testIndex(t)
	addTask(t, db, root, "task", "Stable subject", nil, "vscode", 0)
	if err := newStore(stateDir()).update(func(*state) (bool, error) { return false, nil }); err != nil {
		t.Fatal(err)
	}
	pre := hookPayload("PreToolUse", "task", "call-1", map[string]any{"title": runningMarker + ": Stable subject"}, nil)
	var output bytes.Buffer
	if err := hook(context.Background(), strings.NewReader(pre), &output); err != nil {
		t.Fatal(err)
	}
	proposed := rewrittenTitle(t, output.Bytes())
	if proposed != "⏳ Stable subject" {
		t.Fatalf("rewritten title = %q", proposed)
	}
	saved, _ := newStore(stateDir()).read()
	if saved.Tasks["task"].Pending == nil {
		t.Fatal("Pre did not record a proposal")
	}
	response, _ := json.Marshal(map[string]string{"threadId": "task", "title": proposed})
	post := hookPayload("PostToolUse", "task", "call-1", map[string]any{"title": proposed}, string(response))
	if err := hook(context.Background(), strings.NewReader(post), &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	saved, _ = newStore(stateDir()).read()
	if saved.Tasks["task"].Last != proposed || saved.Tasks["task"].Subject != "Stable subject" || saved.Tasks["task"].Pending != nil {
		t.Fatalf("committed state = %#v", saved.Tasks["task"])
	}

	// A setter success with a lost Post remains the sole admitted proposal.
	pre = hookPayload("PreToolUse", "task", "call-2", map[string]any{"title": "🧵🐻 next steps (agent): finish the tests"}, nil)
	output.Reset()
	if err := hook(context.Background(), strings.NewReader(pre), &output); err != nil {
		t.Fatal(err)
	}
	proposed = rewrittenTitle(t, output.Bytes())
	if _, err := db.Exec(`UPDATE threads SET name=? WHERE id='task'`, proposed); err != nil {
		t.Fatal(err)
	}
	pre = hookPayload("PreToolUse", "task", "call-3", map[string]any{"title": runningMarker + ": Changed model seed"}, nil)
	output.Reset()
	if err := hook(context.Background(), strings.NewReader(pre), &output); err != nil || !strings.Contains(output.String(), `"permissionDecision":"deny"`) {
		t.Fatalf("lost-Post proposal was not kept fail-closed: %q, %v", output.String(), err)
	}
}

func TestPlainTitlePassThroughStagesAndSettles(t *testing.T) {
	root, db := testIndex(t)
	addTask(t, db, root, "task", "Stable subject", nil, "vscode", 0)
	if err := newStore(stateDir()).update(func(*state) (bool, error) { return false, nil }); err != nil {
		t.Fatal(err)
	}
	requested := "User⁣  rename"
	pre := hookPayload("PreToolUse", "task", "plain", map[string]any{"title": requested}, nil)
	var output bytes.Buffer
	if err := hook(context.Background(), strings.NewReader(pre), &output); err != nil || output.Len() != 0 {
		t.Fatalf("plain Pre = %q, %v", output.String(), err)
	}
	saved, _ := newStore(stateDir()).read()
	if pending := saved.Tasks["task"].Pending; pending == nil || pending.Prior != "Stable subject" || pending.Proposed != requested || pending.Attempt != "" {
		t.Fatalf("plain proposal = %#v", pending)
	}
	response, _ := json.Marshal(map[string]string{"threadId": "task", "title": requested})
	post := hookPayload("PostToolUse", "task", "plain", map[string]any{"title": requested}, string(response))
	if err := hook(context.Background(), strings.NewReader(post), &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	saved, _ = newStore(stateDir()).read()
	if got := saved.Tasks["task"]; got.Subject != "User⁣ rename" || got.Last != requested || got.Pending != nil {
		t.Fatalf("plain committed state = %#v", got)
	}
}

func BenchmarkOrdinaryPreToolUse(b *testing.B) {
	root, db := testIndex(b)
	addTask(b, db, root, "task", "Stable subject", nil, "vscode", 0)
	if err := newStore(stateDir()).update(func(*state) (bool, error) { return false, nil }); err != nil {
		b.Fatal(err)
	}
	payload := hookPayload("PreToolUse", "task", "benchmark-pre", map[string]any{"title": runningMarker + ": Stable subject"}, nil)
	var output bytes.Buffer
	b.ResetTimer()
	for b.Loop() {
		output.Reset()
		if err := hook(context.Background(), strings.NewReader(payload), &output); err != nil {
			b.Fatal(err)
		}
	}
}

func TestPreToolUseWaitsWhileTitleLifecycleIsLocked(t *testing.T) {
	root, db := testIndex(t)
	addTask(t, db, root, "task", "Stable subject", nil, "vscode", 0)
	if err := newStore(stateDir()).update(func(*state) (bool, error) { return false, nil }); err != nil {
		t.Fatal(err)
	}
	lock, err := newStore(stateDir()).titleLock()
	if err != nil {
		t.Fatal(err)
	}
	payload := hookPayload("PreToolUse", "task", "locked", map[string]any{"title": runningMarker + ": Stable subject"}, nil)
	type result struct {
		output string
		err    error
	}
	done := make(chan result, 1)
	go func() {
		var output bytes.Buffer
		err := hook(context.Background(), strings.NewReader(payload), &output)
		done <- result{output: output.String(), err: err}
	}()
	select {
	case got := <-done:
		unlock(lock)
		t.Fatalf("locked PreToolUse returned early: %q, %v", got.output, got.err)
	case <-time.After(25 * time.Millisecond):
	}
	unlock(lock)
	var got result
	select {
	case got = <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("PreToolUse did not continue after title lock released")
	}
	if got.err != nil || !strings.Contains(got.output, `"permissionDecision":"allow"`) {
		t.Fatalf("released PreToolUse = %q, %v", got.output, got.err)
	}
	saved, err := newStore(stateDir()).read()
	if err != nil || saved.Tasks["task"].Pending == nil {
		t.Fatalf("released PreToolUse did not stage state: %#v, %v", saved.Tasks["task"], err)
	}
}

func TestPreToolUseQueuedBehindTeardownCannotRecreateState(t *testing.T) {
	root, db := testIndex(t)
	addTask(t, db, root, "task", "Stable subject", nil, "vscode", 0)
	if err := newStore(stateDir()).update(func(*state) (bool, error) { return false, nil }); err != nil {
		t.Fatal(err)
	}
	lock, err := newStore(stateDir()).titleLock()
	if err != nil {
		t.Fatal(err)
	}
	payload := hookPayload("PreToolUse", "task", "teardown", map[string]any{"title": runningMarker + ": Stable subject"}, nil)
	done := make(chan string, 1)
	go func() {
		var output bytes.Buffer
		_ = hook(context.Background(), strings.NewReader(payload), &output)
		done <- output.String()
	}()
	select {
	case output := <-done:
		unlock(lock)
		t.Fatalf("queued PreToolUse returned before teardown: %q", output)
	case <-time.After(25 * time.Millisecond):
	}
	if err := os.RemoveAll(stateDir()); err != nil {
		unlock(lock)
		t.Fatal(err)
	}
	unlock(lock)
	select {
	case output := <-done:
		if !strings.Contains(output, `"permissionDecision":"deny"`) {
			t.Fatalf("post-teardown PreToolUse = %q", output)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("post-teardown PreToolUse did not return")
	}
	if _, err := os.Stat(stateDir()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("queued PreToolUse recreated state: %v", err)
	}
}

func TestPostToolUseQueuedBehindTeardownCannotRecreateState(t *testing.T) {
	root, db := testIndex(t)
	addTask(t, db, root, "task", "Stable subject", nil, "vscode", 0)
	if err := newStore(stateDir()).update(func(*state) (bool, error) { return false, nil }); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	pre := hookPayload("PreToolUse", "task", "delayed-post", map[string]any{"title": runningMarker + ": Stable subject"}, nil)
	if err := hook(context.Background(), strings.NewReader(pre), &output); err != nil {
		t.Fatal(err)
	}
	proposed := rewrittenTitle(t, output.Bytes())
	response, _ := json.Marshal(map[string]string{"threadId": "task", "title": proposed})
	post := hookPayload("PostToolUse", "task", "delayed-post", map[string]any{"title": proposed}, string(response))
	lock, err := newStore(stateDir()).titleLock()
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- hook(context.Background(), strings.NewReader(post), &bytes.Buffer{}) }()
	select {
	case err := <-done:
		unlock(lock)
		t.Fatalf("queued PostToolUse returned before teardown: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	if err := os.RemoveAll(stateDir()); err != nil {
		unlock(lock)
		t.Fatal(err)
	}
	unlock(lock)
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("post-teardown PostToolUse unexpectedly succeeded")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("post-teardown PostToolUse did not return")
	}
	if _, err := os.Stat(stateDir()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("queued PostToolUse recreated state: %v", err)
	}
}

func TestDeniedSecondExplicitCallCannotClearFirstProposal(t *testing.T) {
	root, db := testIndex(t)
	addTask(t, db, root, "target", "Stable subject", nil, "vscode", 0)
	if err := newStore(stateDir()).update(func(*state) (bool, error) { return false, nil }); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	pre := hookPayload("PreToolUse", "controller", "failed-call", map[string]any{"threadId": "target", "title": "🧵🐻 complete"}, nil)
	if err := hook(context.Background(), strings.NewReader(pre), &output); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	second := hookPayload("PreToolUse", "controller", "denied-call", map[string]any{"threadId": "target", "title": "🧵🐻 next steps (agent): retry"}, nil)
	if err := hook(context.Background(), strings.NewReader(second), &output); err != nil || !strings.Contains(output.String(), `"permissionDecision":"deny"`) {
		t.Fatalf("second proposal = %q, %v", output.String(), err)
	}
	saved, err := newStore(stateDir()).read()
	if pending := saved.Tasks["target"].Pending; err != nil || pending == nil || pending.ToolUseID != "failed-call" || pending.CallerTaskID != "controller" {
		t.Fatalf("denied second call changed the first proposal: %#v, %v", saved.Tasks["target"], err)
	}
}

func TestConcurrentMigrationTitleWaveCommitsDistinctTargets(t *testing.T) {
	root, db := testIndex(t)
	const size = 8
	for i := range size {
		id := fmt.Sprintf("target-%d", i)
		addTask(t, db, root, id, "Subject "+id, nil, "vscode", 0)
	}
	if err := newStore(stateDir()).update(func(saved *state) (bool, error) {
		saved.MainTaskID, saved.ControllerTaskID, saved.Phase = "main", "controller", phaseMigrationRunning
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	lock, err := newStore(stateDir()).titleLock()
	if err != nil {
		t.Fatal(err)
	}
	unlock(lock)
	type result struct {
		id, title, output string
		err               error
	}
	prepared := make(chan result, size)
	for i := range size {
		id := fmt.Sprintf("target-%d", i)
		go func() {
			var output bytes.Buffer
			payload := hookPayload("PreToolUse", "controller", "call-"+id, map[string]any{"threadId": id, "title": "🧵🐻 complete"}, nil)
			err := hook(context.Background(), strings.NewReader(payload), &output)
			if err != nil {
				prepared <- result{id: id, output: output.String(), err: err}
				return
			}
			var value struct {
				Hook struct {
					Updated map[string]json.RawMessage `json:"updatedInput"`
				} `json:"hookSpecificOutput"`
			}
			err = json.Unmarshal(output.Bytes(), &value)
			var title string
			if err == nil {
				err = json.Unmarshal(value.Hook.Updated["title"], &title)
			}
			prepared <- result{id: id, title: title, output: output.String(), err: err}
		}()
	}
	posts := make(chan result, size)
	items := make([]result, 0, size)
	for range size {
		items = append(items, <-prepared)
	}
	for _, item := range items {
		if item.err != nil || item.title != "✅ Subject "+item.id {
			t.Fatalf("prepared %s = title %q, output %q, %v", item.id, item.title, item.output, item.err)
		}
	}
	for _, item := range items {
		go func() {
			response, _ := json.Marshal(map[string]string{"threadId": item.id, "title": item.title})
			payload := hookPayload("PostToolUse", "controller", "call-"+item.id, map[string]any{"threadId": item.id, "title": item.title}, string(response))
			posts <- result{id: item.id, title: item.title, err: hook(context.Background(), strings.NewReader(payload), &bytes.Buffer{})}
		}()
	}
	var postFailures []string
	for range size {
		if item := <-posts; item.err != nil {
			postFailures = append(postFailures, fmt.Sprintf("%s: %v", item.id, item.err))
		}
	}
	if len(postFailures) > 0 {
		t.Fatalf("commit failures: %s", strings.Join(postFailures, "; "))
	}
	saved, err := newStore(stateDir()).read()
	if err != nil {
		t.Fatal(err)
	}
	for i := range size {
		id := fmt.Sprintf("target-%d", i)
		if got := saved.Tasks[id]; got.Pending != nil || got.Last != "✅ Subject "+id || got.Status != "complete" {
			t.Fatalf("target state %s = %#v", id, got)
		}
	}
}

func TestPreToolUseContinuesWhileMaintenanceOperationIsLocked(t *testing.T) {
	root, db := testIndex(t)
	addTask(t, db, root, "task", "Stable subject", nil, "vscode", 0)
	if err := newStore(stateDir()).update(func(*state) (bool, error) { return false, nil }); err != nil {
		t.Fatal(err)
	}
	lock, err := newStore(stateDir()).operationLock()
	if err != nil {
		t.Fatal(err)
	}
	defer unlock(lock)
	payload := hookPayload("PreToolUse", "task", "update-overlap", map[string]any{"title": runningMarker + ": Stable subject"}, nil)
	var output bytes.Buffer
	if err := hook(context.Background(), strings.NewReader(payload), &output); err != nil || !strings.Contains(output.String(), `"permissionDecision":"allow"`) {
		t.Fatalf("operation-overlap PreToolUse = %q, %v", output.String(), err)
	}
}

func BenchmarkOrdinaryPostToolUse(b *testing.B) {
	root, db := testIndex(b)
	addTask(b, db, root, "task", "Stable subject", nil, "vscode", 0)
	if err := newStore(stateDir()).update(func(*state) (bool, error) { return false, nil }); err != nil {
		b.Fatal(err)
	}
	pre := hookPayload("PreToolUse", "task", "benchmark-post", map[string]any{"title": runningMarker + ": Stable subject"}, nil)
	var output bytes.Buffer
	if err := hook(context.Background(), strings.NewReader(pre), &output); err != nil {
		b.Fatal(err)
	}
	proposed := rewrittenTitle(b, output.Bytes())
	response, _ := json.Marshal(map[string]string{"threadId": "task", "title": proposed})
	post := hookPayload("PostToolUse", "task", "benchmark-post", map[string]any{"title": proposed}, string(response))
	b.ResetTimer()
	for b.Loop() {
		b.StopTimer()
		output.Reset()
		if err := hook(context.Background(), strings.NewReader(pre), &output); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
		if err := hook(context.Background(), strings.NewReader(post), &bytes.Buffer{}); err != nil {
			b.Fatal(err)
		}
	}
}

func TestFreshRunningSubjectSeedClosesFirstTitleRace(t *testing.T) {
	root, db := testIndex(t)
	if err := newStore(stateDir()).update(func(*state) (bool, error) { return false, nil }); err != nil {
		t.Fatal(err)
	}
	first := "Fix the login redirect. First call the title tool, then inspect the failure."
	addTask(t, db, root, "raw", first, nil, "vscode", 0)
	addTask(t, db, root, "short", "Fix login redirect", nil, "vscode", 0)
	addTask(t, db, root, "named", first, "Customer login", "vscode", 0)
	addTask(t, db, root, "truncated", truncateUTF16(first, 60), nil, "vscode", 0)
	addTask(t, db, root, "delegated", "<codex_delegation> <source_thread_id>private</source_thread_id> <input>Fix login</input></codex_delegation>", nil, "vscode", 0)
	if _, err := db.Exec(`UPDATE threads SET first_user_message=? WHERE id IN ('raw','short','named','truncated')`, first); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE threads SET first_user_message=title WHERE id='delegated'`); err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]string{
		"raw": "⏳ Model subject seed", "short": "⏳ Fix login redirect", "named": "⏳ Customer login",
		"truncated": "⏳ Model subject seed", "delegated": "⏳ Model subject seed",
	} {
		var output bytes.Buffer
		pre := hookPayload("PreToolUse", id, "call-"+id, map[string]any{"title": runningMarker + ": Model subject seed"}, nil)
		if err := hook(context.Background(), strings.NewReader(pre), &output); err != nil || rewrittenTitle(t, output.Bytes()) != want {
			t.Fatalf("%s rewrite = %q, %v", id, output.String(), err)
		}
	}
}

func TestFreshRunningSubjectSeedFailsClosedAndThenStaysOwned(t *testing.T) {
	root, db := testIndex(t)
	if err := newStore(stateDir()).update(func(*state) (bool, error) { return false, nil }); err != nil {
		t.Fatal(err)
	}
	first := "Investigate the first title race and preserve the stable subject."
	addTask(t, db, root, "task", first, nil, "vscode", 0)
	if _, err := db.Exec(`UPDATE threads SET first_user_message=? WHERE id='task'`, first); err != nil {
		t.Fatal(err)
	}
	indexed, found, err := oneTask(context.Background(), "task")
	if err != nil || !found || indexed.Title != first || indexed.FirstMessage != first || indexed.Name != "" {
		t.Fatalf("fresh task index = %#v, %v, %v", indexed, found, err)
	}
	stateBefore, err := currentStateOrEmpty()
	if err != nil || len(stateBefore.Tasks) != 0 {
		t.Fatalf("fresh state = %#v, %v", stateBefore, err)
	}
	var homeOutput bytes.Buffer
	homePre := hookPayload("PreToolUse", "task", "home", map[string]any{"title": homeTitle}, nil)
	if err := hook(context.Background(), strings.NewReader(homePre), &homeOutput); err != nil || rewrittenTitle(t, homeOutput.Bytes()) != homeTitle {
		t.Fatalf("persistent home title was not passed through: %q, %v", homeOutput.String(), err)
	}
	homeResponse, _ := json.Marshal(map[string]string{"threadId": "task", "title": homeTitle})
	homePost := hookPayload("PostToolUse", "task", "home", map[string]any{"title": homeTitle}, string(homeResponse))
	if err := hook(context.Background(), strings.NewReader(homePost), &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	homeState, _ := currentStateOrEmpty()
	if got := homeState.Tasks["task"]; len(homeState.Tasks) != 1 || got.Original != first || got.Subject != "" || got.Last != homeTitle || got.Pending != nil {
		t.Fatalf("persistent home title did not retain its prior subject: %#v", homeState.Tasks)
	}
	for _, marker := range []string{runningMarker, runningMarker + ":", runningMarker + ": ", runningMarker + ": bad  spacing", runningMarker + ": " + strings.Repeat("x", 59), homeTitle + " extra"} {
		var output bytes.Buffer
		pre := hookPayload("PreToolUse", "task", marker, map[string]any{"title": marker}, nil)
		if err := hook(context.Background(), strings.NewReader(pre), &output); err != nil || !strings.Contains(output.String(), `"permissionDecision":"deny"`) {
			t.Fatalf("marker %q did not fail closed: %q, %v", marker, output.String(), err)
		}
	}
	stateAfter, err := currentStateOrEmpty()
	if err != nil || len(stateAfter.Tasks) != 1 {
		t.Fatalf("denied markers changed state: %#v, %v", stateAfter, err)
	}
	var output bytes.Buffer
	pre := hookPayload("PreToolUse", "task", "seed", map[string]any{"title": runningMarker + ": First title race"}, nil)
	if err := hook(context.Background(), strings.NewReader(pre), &output); err != nil {
		t.Fatal(err)
	}
	proposed := rewrittenTitle(t, output.Bytes())
	response, _ := json.Marshal(map[string]string{"threadId": "task", "title": proposed})
	post := hookPayload("PostToolUse", "task", "seed", map[string]any{"title": proposed}, string(response))
	if err := hook(context.Background(), strings.NewReader(post), &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE threads SET title=? WHERE id='task'`, proposed); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	pre = hookPayload("PreToolUse", "task", "later", map[string]any{"title": runningMarker + ": Ignore this replacement"}, nil)
	if err := hook(context.Background(), strings.NewReader(pre), &output); err != nil || rewrittenTitle(t, output.Bytes()) != "⏳ First title race" {
		t.Fatalf("owned subject changed: %q, %v", output.String(), err)
	}
}

func TestPersistentHomeNeverReceivesStatusTitle(t *testing.T) {
	root, db := testIndex(t)
	if err := newStore(stateDir()).update(func(saved *state) (bool, error) {
		saved.MainTaskID, saved.Phase = "task", phaseMigrationComplete
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	addTask(t, db, root, "task", "fresh", nil, "vscode", 0)
	if _, err := db.Exec(`UPDATE threads SET first_user_message='fresh' WHERE id='task'`); err != nil {
		t.Fatal(err)
	}
	for _, item := range [][2]string{{"running-home", runningMarker + ": Replacement seed"}, {"complete-home", "🧵🐻 complete"}} {
		call, marker := item[0], item[1]
		var output bytes.Buffer
		pre := hookPayload("PreToolUse", "task", call, map[string]any{"title": marker}, nil)
		if err := hook(context.Background(), strings.NewReader(pre), &output); err != nil || rewrittenTitle(t, output.Bytes()) != mainTitle {
			t.Fatalf("%s rewrite = %q, %v", call, output.String(), err)
		}
		response, _ := json.Marshal(map[string]string{"threadId": "task", "title": mainTitle})
		post := hookPayload("PostToolUse", "task", call, map[string]any{"title": mainTitle}, string(response))
		if err := hook(context.Background(), strings.NewReader(post), &bytes.Buffer{}); err != nil {
			t.Fatal(err)
		}
	}
	saved, _ := currentStateOrEmpty()
	if got := saved.Tasks["task"]; got.Pending != nil || got.Subject != mainTitle || got.Last != mainTitle || got.Status != "complete" {
		t.Fatalf("persistent home state = %#v", got)
	}
}

func TestRestartFirstMessageProjectionPreservesOwnership(t *testing.T) {
	root, db := testIndex(t)
	first := "Restarted task exposes this long raw first message before Codex restores the committed title."
	delegation := "<codex_delegation><source_thread_id>private</source_thread_id><input>Fix login</input></codex_delegation>"
	addTask(t, db, root, "raw", first, nil, "vscode", 0)
	addTask(t, db, root, "truncated", truncateUTF16(first, 60), nil, "vscode", 0)
	addTask(t, db, root, "pending", first, nil, "vscode", 0)
	addTask(t, db, root, "fresh", first, nil, "vscode", 0)
	addTask(t, db, root, "delegated", delegation, nil, "vscode", 0)
	addTask(t, db, root, "renamed", "Manual user rename", nil, "vscode", 0)
	addTask(t, db, root, "named", first, "Explicit name", "vscode", 0)
	if _, err := db.Exec(`UPDATE threads SET first_user_message=? WHERE id IN ('raw','truncated','pending','fresh','renamed','named')`, first); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE threads SET first_user_message=? WHERE id='delegated'`, delegation); err != nil {
		t.Fatal(err)
	}
	if err := newStore(stateDir()).update(func(saved *state) (bool, error) {
		saved.Tasks["raw"] = taskState{Subject: "Committed owner", Last: "✅ Committed owner", Status: "complete"}
		saved.Tasks["truncated"] = taskState{Subject: "Committed owner", Last: "✅ Committed owner", Status: "complete"}
		saved.Tasks["pending"] = taskState{Pending: &pendingProposal{BaseSubject: "Pending owner", Proposed: first}}
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	for id, call := range map[string][2]string{
		"raw":       {runningMarker + ": Replacement seed", "⏳ Committed owner"},
		"truncated": {"🧵🐻 complete", "✅ Committed owner"},
		"renamed":   {runningMarker + ": Replacement seed", "⏳ Manual user rename"},
		"named":     {runningMarker + ": Replacement seed", "⏳ Explicit name"},
	} {
		var output bytes.Buffer
		pre := hookPayload("PreToolUse", id, "call-"+id, map[string]any{"title": call[0]}, nil)
		if err := hook(context.Background(), strings.NewReader(pre), &output); err != nil || rewrittenTitle(t, output.Bytes()) != call[1] {
			t.Fatalf("%s restart rewrite = %q, %v", id, output.String(), err)
		}
	}
	var pendingOutput bytes.Buffer
	pendingPre := hookPayload("PreToolUse", "pending", "call-pending", map[string]any{"title": runningMarker + ": Replacement seed"}, nil)
	if err := hook(context.Background(), strings.NewReader(pendingPre), &pendingOutput); err != nil || !strings.Contains(pendingOutput.String(), `"permissionDecision":"deny"`) {
		t.Fatalf("unsettled proposal was not kept fail-closed: %q, %v", pendingOutput.String(), err)
	}
	for _, id := range []string{"fresh", "delegated"} {
		var output bytes.Buffer
		pre := hookPayload("PreToolUse", id, "terminal-"+id, map[string]any{"title": "🧵🐻 complete"}, nil)
		if err := hook(context.Background(), strings.NewReader(pre), &output); err != nil || !strings.Contains(output.String(), `"permissionDecision":"deny"`) {
			t.Fatalf("%s ownerless terminal did not fail closed: %q, %v", id, output.String(), err)
		}
	}
	saved, err := currentStateOrEmpty()
	if err != nil || saved.Tasks["fresh"].Pending != nil || saved.Tasks["delegated"].Pending != nil {
		t.Fatalf("ownerless terminal changed state: %#v, %v", saved, err)
	}
}

func TestRunningMigrationControllerOwnsHistoricalFirstMessage(t *testing.T) {
	root, db := testIndex(t)
	first := "✅ ❔ echo hello"
	addTask(t, db, root, "target", first, nil, "vscode", 0)
	if _, err := db.Exec(`UPDATE threads SET first_user_message=? WHERE id='target'`, first); err != nil {
		t.Fatal(err)
	}
	if err := newStore(stateDir()).update(func(saved *state) (bool, error) {
		saved.MainTaskID, saved.ControllerTaskID, saved.Phase = "main", "controller", phaseMigrationRunning
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	pre := hookPayload("PreToolUse", "other", "denied", map[string]any{"threadId": "target", "title": unknownMarker}, nil)
	if err := hook(context.Background(), strings.NewReader(pre), &output); err != nil || !strings.Contains(output.String(), `"permissionDecision":"deny"`) {
		t.Fatalf("non-controller ownerless migration was not denied: %q, %v", output.String(), err)
	}
	output.Reset()
	pre = hookPayload("PreToolUse", "controller", "allowed", map[string]any{"threadId": "target", "title": unknownMarker}, nil)
	if err := hook(context.Background(), strings.NewReader(pre), &output); err != nil {
		t.Fatal(err)
	}
	proposed := rewrittenTitle(t, output.Bytes())
	if proposed != "❔ echo hello" {
		t.Fatalf("controller migration title = %q", proposed)
	}
	response, _ := json.Marshal(map[string]string{"threadId": "target", "title": proposed})
	post := hookPayload("PostToolUse", "controller", "allowed", map[string]any{"threadId": "target", "title": proposed}, string(response))
	if err := hook(context.Background(), strings.NewReader(post), &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	saved, _ := newStore(stateDir()).read()
	if got := saved.Tasks["target"]; got.Subject != "echo hello" || got.Last != proposed || got.Pending != nil {
		t.Fatalf("controller migration ownership = %#v", got)
	}
}

func TestPendingMigrationRegistersExactRuntimeControllerFromMarkedHomeDelegation(t *testing.T) {
	root, db := testIndex(t)
	addTask(t, db, root, "main", "ThreadBear", nil, "vscode", 0)
	addTask(t, db, root, "runtime-controller", "Migration controller", nil, "vscode", 0)
	first := "<codex_delegation>\n<source_thread_id>main</source_thread_id>\n<input>" + controllerMarker + " Follow the migration protocol.</input>\n</codex_delegation>"
	if _, err := db.Exec(`UPDATE threads SET thread_source='subagent', first_user_message=? WHERE id='runtime-controller'`, first); err != nil {
		t.Fatal(err)
	}
	if _, err := install("main", false, true, false); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	pre := hookPayload("PreToolUse", "runtime-controller", "register", map[string]any{"title": runningMarker + ": Migration controller"}, nil)
	if err := hook(context.Background(), strings.NewReader(pre), &output); err != nil || strings.Contains(output.String(), `"permissionDecision":"deny"`) {
		t.Fatalf("controller registration = %q, %v", output.String(), err)
	}
	saved, err := newStore(stateDir()).read()
	if err != nil || saved.Phase != phaseMigrationRunning || saved.ControllerTaskID != "runtime-controller" {
		t.Fatalf("registered controller state = %#v, %v", saved, err)
	}
}

func TestPendingMigrationRejectsUnmarkedControllerClaim(t *testing.T) {
	root, db := testIndex(t)
	addTask(t, db, root, "main", "ThreadBear", nil, "vscode", 0)
	addTask(t, db, root, "other", "Other task", nil, "vscode", 0)
	if _, err := db.Exec(`UPDATE threads SET first_user_message='<codex_delegation> <source_thread_id>main</source_thread_id> <input>Other work</input></codex_delegation>' WHERE id='other'`); err != nil {
		t.Fatal(err)
	}
	if _, err := install("main", false, true, false); err != nil {
		t.Fatal(err)
	}
	pre := hookPayload("PreToolUse", "other", "ordinary", map[string]any{"title": runningMarker + ": Other work"}, nil)
	if err := hook(context.Background(), strings.NewReader(pre), &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	saved, _ := newStore(stateDir()).read()
	if saved.Phase != phaseMigrationPending || saved.ControllerTaskID != "" {
		t.Fatalf("unmarked task claimed controller: %#v", saved)
	}
}

func TestPendingMigrationRejectsMarkedOrdinaryTaskClaim(t *testing.T) {
	root, db := testIndex(t)
	addTask(t, db, root, "main", "ThreadBear", nil, "vscode", 0)
	addTask(t, db, root, "ordinary", "Ordinary task", nil, "vscode", 0)
	first := "<codex_delegation>\n<source_thread_id>main</source_thread_id>\n<input>" + controllerMarker + " Follow the migration protocol.</input>\n</codex_delegation>"
	if _, err := db.Exec(`UPDATE threads SET thread_source='user', first_user_message=? WHERE id='ordinary'`, first); err != nil {
		t.Fatal(err)
	}
	if _, err := install("main", false, true, false); err != nil {
		t.Fatal(err)
	}
	pre := hookPayload("PreToolUse", "ordinary", "forged", map[string]any{"title": runningMarker + ": Migration controller"}, nil)
	if err := hook(context.Background(), strings.NewReader(pre), &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	saved, _ := newStore(stateDir()).read()
	if saved.Phase != phaseMigrationPending || saved.ControllerTaskID != "" {
		t.Fatalf("ordinary task claimed controller: %#v", saved)
	}
}

func TestControlTaskCleanupStagesAndCommitsStrippedSubject(t *testing.T) {
	root, db := testIndex(t)
	addTask(t, db, root, "target", "✅ ✅ ❔ hello", nil, "vscode", 0)
	if err := newStore(stateDir()).update(func(saved *state) (bool, error) {
		saved.MainTaskID = "main"
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	denied := hookPayload("PreToolUse", "other", "denied", map[string]any{"threadId": "target", "title": cleanupMarker}, nil)
	if err := hook(context.Background(), strings.NewReader(denied), &output); err != nil || !strings.Contains(output.String(), `"permissionDecision":"deny"`) {
		t.Fatalf("non-control cleanup was not denied: %q, %v", output.String(), err)
	}
	output.Reset()
	pre := hookPayload("PreToolUse", "main", "cleanup", map[string]any{"threadId": "target", "title": cleanupMarker}, nil)
	if err := hook(context.Background(), strings.NewReader(pre), &output); err != nil {
		t.Fatal(err)
	}
	proposed := rewrittenTitle(t, output.Bytes())
	if proposed != "hello" {
		t.Fatalf("cleanup title = %q", proposed)
	}
	response, _ := json.Marshal(map[string]string{"threadId": "target", "title": proposed})
	post := hookPayload("PostToolUse", "main", "cleanup", map[string]any{"threadId": "target", "title": proposed}, string(response))
	if err := hook(context.Background(), strings.NewReader(post), &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	saved, _ := newStore(stateDir()).read()
	if got := saved.Tasks["target"]; got.Subject != "hello" || got.Last != "hello" || got.Pending != nil {
		t.Fatalf("cleanup ownership = %#v", got)
	}
}

func TestControlTaskCleanupHandlesIconOnlyAndLiteralEmoji(t *testing.T) {
	root, db := testIndex(t)
	for id, title := range map[string]string{"icons": "❔ ❔ ❔", "emoji": "🎉 ✅ user title", "main": "✅ ✅ ThreadBear"} {
		addTask(t, db, root, id, title, nil, "vscode", 0)
	}
	if err := newStore(stateDir()).update(func(saved *state) (bool, error) {
		saved.MainTaskID = "main"
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]string{"icons": "Untitled task", "emoji": "🎉 ✅ user title", "main": "ThreadBear"} {
		var output bytes.Buffer
		input := map[string]any{"title": cleanupMarker}
		if id != "main" {
			input["threadId"] = id
		}
		pre := hookPayload("PreToolUse", "main", "cleanup-"+id, input, nil)
		if err := hook(context.Background(), strings.NewReader(pre), &output); err != nil || rewrittenTitle(t, output.Bytes()) != want {
			t.Fatalf("%s cleanup = %q, %v", id, output.String(), err)
		}
	}
}

func TestMigrationControllerStripsLegacyIconsFromNamedSubject(t *testing.T) {
	root, db := testIndex(t)
	addTask(t, db, root, "target", "ignored", "✅ ✅ Named subject", "vscode", 0)
	if err := newStore(stateDir()).update(func(saved *state) (bool, error) {
		saved.MainTaskID, saved.ControllerTaskID, saved.Phase = "main", "controller", phaseMigrationRunning
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	pre := hookPayload("PreToolUse", "controller", "migration", map[string]any{"threadId": "target", "title": unknownMarker}, nil)
	if err := hook(context.Background(), strings.NewReader(pre), &output); err != nil || rewrittenTitle(t, output.Bytes()) != "❔ Named subject" {
		t.Fatalf("migration cleanup = %q, %v", output.String(), err)
	}
}

func TestPostMismatchFailsClosed(t *testing.T) {
	root, db := testIndex(t)
	addTask(t, db, root, "task", "Subject", nil, "vscode", 0)
	_ = db
	_ = newStore(stateDir()).update(func(*state) (bool, error) { return false, nil })
	var output bytes.Buffer
	pre := hookPayload("PreToolUse", "task", "call", map[string]any{"title": runningMarker + ": Subject"}, nil)
	if err := hook(context.Background(), strings.NewReader(pre), &output); err != nil {
		t.Fatal(err)
	}
	title := rewrittenTitle(t, output.Bytes())
	wrong, _ := json.Marshal(map[string]string{"threadId": "other", "title": title})
	post := hookPayload("PostToolUse", "task", "call", map[string]any{"title": title}, string(wrong))
	if err := hook(context.Background(), strings.NewReader(post), &bytes.Buffer{}); err == nil {
		t.Fatal("mismatched native result was accepted")
	}
	saved, _ := newStore(stateDir()).read()
	if saved.Tasks["task"].Pending == nil || saved.Tasks["task"].Last != "" {
		t.Fatalf("mismatch committed state: %#v", saved.Tasks["task"])
	}
	extra := `{"threadId":"task","title":` + string(mustJSON(title)) + `,"extra":true}`
	post = hookPayload("PostToolUse", "task", "call", map[string]any{"title": title}, extra)
	if err := hook(context.Background(), strings.NewReader(post), &bytes.Buffer{}); err == nil {
		t.Fatal("native result with extra fields was accepted")
	}
}

func TestBulkMarkerRereadsExplicitTargetAndAdoptsRename(t *testing.T) {
	root, db := testIndex(t)
	addTask(t, db, root, "target", "Bulk subject", nil, "vscode", 0)
	_ = newStore(stateDir()).update(func(*state) (bool, error) { return false, nil })
	pre := hookPayload("PreToolUse", "installer", "bulk-1", map[string]any{"threadId": "target", "title": "🧵🐻 complete"}, nil)
	var output bytes.Buffer
	if err := hook(context.Background(), strings.NewReader(pre), &output); err != nil {
		t.Fatal(err)
	}
	desired := rewrittenTitle(t, output.Bytes())
	if desired != "✅ Bulk subject" {
		t.Fatalf("bulk title = %q", desired)
	}
	saved, _ := newStore(stateDir()).read()
	if pending := saved.Tasks["target"].Pending; pending.ToolUseID != "bulk-1" || pending.CallerTaskID != "installer" {
		t.Fatal("bulk proposal was not bound to native call")
	}
	response, _ := json.Marshal(map[string]string{"threadId": "target", "title": desired})
	post := hookPayload("PostToolUse", "installer", "bulk-1", map[string]any{"threadId": "target", "title": desired}, string(response))
	if err := hook(context.Background(), strings.NewReader(post), &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE threads SET name='User renamed' WHERE id='target'`); err != nil {
		t.Fatal(err)
	}
	pre = hookPayload("PreToolUse", "installer", "bulk-2", map[string]any{"threadId": "target", "title": "🧵🐻 blocked (external): restore the signing service"}, nil)
	output.Reset()
	if err := hook(context.Background(), strings.NewReader(pre), &output); err != nil {
		t.Fatal(err)
	}
	if got := rewrittenTitle(t, output.Bytes()); got != "🚨 User renamed → restore the signing service" {
		t.Fatalf("bulk rename was not adopted: %q", got)
	}
}

func TestHookRejectsOversizedInput(t *testing.T) {
	if err := hook(context.Background(), strings.NewReader(strings.Repeat("x", maxHookBytes+1)), &bytes.Buffer{}); err == nil {
		t.Fatal("oversized hook input accepted")
	}
}

func hookPayload(event, session, call string, input map[string]any, response any) string {
	value := map[string]any{
		"cwd": "/tmp", "hook_event_name": event, "model": "test", "permission_mode": "bypassPermissions",
		"session_id": session, "tool_input": input, "tool_name": titleTool, "tool_use_id": call,
		"transcript_path": "/tmp/rollout.jsonl", "turn_id": "turn",
	}
	if response != nil {
		value["tool_response"] = response
	}
	data, _ := json.Marshal(value)
	return string(data)
}

func rewrittenTitle(t testing.TB, data []byte) string {
	t.Helper()
	var value struct {
		Hook struct {
			Updated map[string]json.RawMessage `json:"updatedInput"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	title, err := stringField(value.Hook.Updated, "title", true)
	if err != nil {
		t.Fatal(err)
	}
	return title
}

func rolloutLine(kind string, payload any) string {
	data, _ := json.Marshal(map[string]any{"type": kind, "payload": payload})
	return string(data) + "\n"
}

func mustJSON(value any) []byte {
	data, _ := json.Marshal(value)
	return data
}
