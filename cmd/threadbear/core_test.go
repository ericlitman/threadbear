package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

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

func TestOrdinaryHooksRewriteVerifyAndRecoverLostPost(t *testing.T) {
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

	// A setter success with a lost Post remains provisional ownership on the next turn.
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
	if err := hook(context.Background(), strings.NewReader(pre), &output); err != nil {
		t.Fatal(err)
	}
	if got := rewrittenTitle(t, output.Bytes()); got != "⏳ Stable subject" {
		t.Fatalf("lost-Post recovery duplicated ownership: %q", got)
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

func TestPreToolUseRefusesWhileTitleLifecycleIsLocked(t *testing.T) {
	root, db := testIndex(t)
	addTask(t, db, root, "task", "Stable subject", nil, "vscode", 0)
	if err := newStore(stateDir()).update(func(*state) (bool, error) { return false, nil }); err != nil {
		t.Fatal(err)
	}
	lock, err := newStore(stateDir()).titleLock()
	if err != nil {
		t.Fatal(err)
	}
	defer unlock(lock)
	payload := hookPayload("PreToolUse", "task", "locked", map[string]any{"title": runningMarker + ": Stable subject"}, nil)
	var output bytes.Buffer
	if err := hook(context.Background(), strings.NewReader(payload), &output); err != nil || !strings.Contains(output.String(), `"permissionDecision":"deny"`) {
		t.Fatalf("locked PreToolUse = %q, %v", output.String(), err)
	}
	saved, err := newStore(stateDir()).read()
	if err != nil || saved.Tasks["task"].Pending != nil {
		t.Fatalf("locked PreToolUse staged state: %#v, %v", saved.Tasks["task"], err)
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
	if err := hook(context.Background(), strings.NewReader(homePre), &homeOutput); err != nil || homeOutput.Len() != 0 {
		t.Fatalf("persistent home title was not passed through: %q, %v", homeOutput.String(), err)
	}
	for _, marker := range []string{runningMarker, runningMarker + ":", runningMarker + ": ", runningMarker + ": bad  spacing", runningMarker + ": " + strings.Repeat("x", 59), homeTitle + " extra"} {
		var output bytes.Buffer
		pre := hookPayload("PreToolUse", "task", marker, map[string]any{"title": marker}, nil)
		if err := hook(context.Background(), strings.NewReader(pre), &output); err != nil || !strings.Contains(output.String(), `"permissionDecision":"deny"`) {
			t.Fatalf("marker %q did not fail closed: %q, %v", marker, output.String(), err)
		}
	}
	stateAfter, err := currentStateOrEmpty()
	if err != nil || len(stateAfter.Tasks) != 0 {
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
		saved.Tasks["pending"] = taskState{Pending: &pendingProposal{BaseSubject: "Pending owner"}}
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	for id, call := range map[string][2]string{
		"raw":       {runningMarker + ": Replacement seed", "⏳ Committed owner"},
		"truncated": {"🧵🐻 complete", "✅ Committed owner"},
		"pending":   {runningMarker + ": Replacement seed", "⏳ Pending owner"},
		"renamed":   {runningMarker + ": Replacement seed", "⏳ Manual user rename"},
		"named":     {runningMarker + ": Replacement seed", "⏳ Explicit name"},
	} {
		var output bytes.Buffer
		pre := hookPayload("PreToolUse", id, "call-"+id, map[string]any{"title": call[0]}, nil)
		if err := hook(context.Background(), strings.NewReader(pre), &output); err != nil || rewrittenTitle(t, output.Bytes()) != call[1] {
			t.Fatalf("%s restart rewrite = %q, %v", id, output.String(), err)
		}
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

func TestControlTaskCleanupHandlesPendingIconOnlyAndLiteralEmoji(t *testing.T) {
	root, db := testIndex(t)
	for id, title := range map[string]string{"icons": "❔ ❔ ❔", "emoji": "🎉 ✅ user title", "main": "✅ ✅ ThreadBear"} {
		addTask(t, db, root, id, title, nil, "vscode", 0)
	}
	if err := newStore(stateDir()).update(func(saved *state) (bool, error) {
		saved.MainTaskID = "main"
		saved.Tasks["icons"] = taskState{Pending: &pendingProposal{ToolUseID: "stale", Proposed: "❔ stale"}}
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
	if saved.Tasks["target"].Pending.ToolUseID != "bulk-1" {
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
