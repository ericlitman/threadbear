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

func testIndex(t *testing.T) (string, *sql.DB) {
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
		source TEXT, thread_source TEXT, rollout_path TEXT)`)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", root)
	t.Setenv("CODEX_HOME", codex)
	return root, db
}

func addTask(t *testing.T, db *sql.DB, root, id, title string, name any, source string, archived int) string {
	t.Helper()
	rollout := filepath.Join(root, id+".jsonl")
	if err := os.WriteFile(rollout, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO threads VALUES (?,1,?,?,?,?,'',?)`, id, title, name, archived, source, rollout); err != nil {
		t.Fatal(err)
	}
	return rollout
}

func TestInventoryIncludesEveryUnarchivedSourceAndExactTask(t *testing.T) {
	root, db := testIndex(t)
	addTask(t, db, root, "desktop", "generated", "renamed", "vscode", 0)
	addTask(t, db, root, "exec", "exec title", nil, "exec", 0)
	addTask(t, db, root, "cli", "", nil, "cli", 0)
	addTask(t, db, root, "archived", "old", nil, "vscode", 1)
	tasks, err := inventory(context.Background())
	if err != nil || len(tasks) != 3 || tasks[0].ID != "cli" || tasks[1].ID != "desktop" || tasks[1].Title != "renamed" {
		t.Fatalf("inventory = %#v, %v", tasks, err)
	}
	got, found, err := oneTask(context.Background(), "desktop")
	if err != nil || !found || got.Title != "renamed" {
		t.Fatalf("oneTask = %#v, %v, %v", got, found, err)
	}
	if _, found, _ := oneTask(context.Background(), "archived"); found {
		t.Fatal("archived task was addressable")
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
	pre := hookPayload("PreToolUse", "task", "call-1", map[string]any{"title": runningMarker}, nil)
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
	pre = hookPayload("PreToolUse", "task", "call-3", map[string]any{"title": runningMarker}, nil)
	output.Reset()
	if err := hook(context.Background(), strings.NewReader(pre), &output); err != nil {
		t.Fatal(err)
	}
	if got := rewrittenTitle(t, output.Bytes()); got != "⏳ Stable subject" {
		t.Fatalf("lost-Post recovery duplicated ownership: %q", got)
	}
}

func TestPostMismatchFailsClosed(t *testing.T) {
	root, db := testIndex(t)
	addTask(t, db, root, "task", "Subject", nil, "vscode", 0)
	_ = db
	_ = newStore(stateDir()).update(func(*state) (bool, error) { return false, nil })
	var output bytes.Buffer
	pre := hookPayload("PreToolUse", "task", "call", map[string]any{"title": runningMarker}, nil)
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
	addTask(t, db, root, "target", "Bulk subject", nil, "exec", 0)
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

func rewrittenTitle(t *testing.T, data []byte) string {
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
