package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ericlitman/threadbear/assets"
	_ "modernc.org/sqlite"
)

func TestDeterministicFooterAndAmbiguity(t *testing.T) {
	complete := rolloutLine("response_item", map[string]any{
		"type": "message", "role": "assistant", "phase": "final_answer",
		"content": []map[string]string{{"type": "output_text", "text": "Done.\n\n🧵🐻 complete"}},
	})
	need := rolloutLine("response_item", map[string]any{
		"type": "message", "role": "assistant", "phase": "final_answer",
		"content": []map[string]string{{"type": "output_text", "text": "Pick one.\n\n🧵🐻 needs input (you): choose the release region"}},
	})
	for _, tc := range []struct {
		name, data, status, action string
		resolved                   bool
	}{
		{"complete", complete, "complete", "", true},
		{"needs input", need, "needs_input", "choose the release region", true},
		{"legacy prose", rolloutLine("response_item", map[string]any{"type": "message", "role": "assistant", "phase": "final_answer", "content": []map[string]string{{"type": "output_text", "text": "I think this is done."}}}), "", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := analyze([]byte(tc.data))
			if got.Resolved != tc.resolved || got.Status != tc.status || got.Action != tc.action {
				t.Fatalf("analyze() = %#v", got)
			}
		})
	}
}

func TestFooterMustBeFinalAndConcrete(t *testing.T) {
	for _, message := range []string{
		"🧵🐻 complete\nextra",
		"> 🧵🐻 complete",
		"🧵🐻 needs input (you): decide",
		"🧵🐻 blocked (agent): restore the service",
	} {
		if result := parseFooter(message); result.Resolved {
			t.Fatalf("accepted %q", message)
		}
	}
}

func TestVisibleTitleIsAtMostSixtyUTF16Units(t *testing.T) {
	got := title("complete", strings.Repeat("🧵", 80), "")
	if utf16Len(got) > 60 || !strings.HasSuffix(got, "…") || !strings.HasPrefix(got, "✅ ") {
		t.Fatalf("title = %q (%d units)", got, utf16Len(got))
	}
}

func TestFreshStateAdoptsExistingTitleAsUserSubject(t *testing.T) {
	task := indexedTask{ID: "task-1", Revision: 3, Title: "🚨 Customer's literal title"}
	record, plan := decide(task, analysis{Resolved: true, Status: "complete"})
	if record.Subject != "Customer's literal title" {
		t.Fatalf("subject = %q", record.Subject)
	}
	if plan.ExpectedTitle != task.Title || plan.DesiredTitle != "✅ Customer's literal title" {
		t.Fatalf("plan = %#v", plan)
	}
}

func TestPlanIdentityBindsEvidenceAndOwnership(t *testing.T) {
	a := planID(plan{TaskID: "t", Revision: 1, ExpectedTitle: "A", DesiredTitle: "✅ A", Evidence: "one", Subject: "A"})
	b := planID(plan{TaskID: "t", Revision: 1, ExpectedTitle: "A", DesiredTitle: "✅ A", Evidence: "two", Subject: "A"})
	c := planID(plan{TaskID: "t", Revision: 1, ExpectedTitle: "A", DesiredTitle: "✅ A", Evidence: "one", Subject: "A hidden"})
	if a == b || a == c {
		t.Fatal("plan identity ignored evidence or ownership")
	}
}

func TestStateWriteIsPrivateAndAtomic(t *testing.T) {
	dir := t.TempDir()
	store := newStore(dir)
	value := freshState("control")
	if err := store.save(value); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, "core.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o", info.Mode().Perm())
	}
	loaded, err := store.load()
	if err != nil || loaded.Format != stateFormat || loaded.ControlTaskID != "control" {
		t.Fatalf("load = %#v, %v", loaded, err)
	}
}

func TestAmbiguityRequiresStableEvidenceBeforeLuna(t *testing.T) {
	record := taskRecord{Evidence: "same", AmbiguousPasses: 2}
	if !lunaEligible(record, "same") || lunaEligible(record, "changed") || lunaEligible(taskRecord{}, "same") {
		t.Fatal("ambiguity stability rule failed")
	}
}

func TestEvidenceIdentityBindsPathAndBoundary(t *testing.T) {
	data := []byte("same")
	if evidenceID("/a", 4, data) == evidenceID("/b", 4, data) ||
		evidenceID("/a", 4, data) == evidenceID("/a", 5, data) {
		t.Fatal("evidence identity omitted its source or fixed boundary")
	}
}

func TestReadEvidenceDropsPartialBoundaryLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	first := rolloutLine("response_item", map[string]any{"type": "message", "role": "assistant", "phase": "final_answer", "content": []map[string]string{{"text": "🧵🐻 complete"}}})
	if err := os.WriteFile(path, []byte(first+`{"type":"response_item"`), 0o600); err != nil {
		t.Fatal(err)
	}
	data, boundary, err := readEvidence(path)
	if err != nil || string(data) != first || boundary <= int64(len(first)) {
		t.Fatalf("readEvidence = %q, %d, %v", data, boundary, err)
	}
}

func TestRuntimePrecedence(t *testing.T) {
	var thread appThread
	thread.Status.Type = "active"
	thread.Status.ActiveFlags = []string{"waitingForApproval"}
	if got := runtimeResult(thread, ""); got.Status != "needs_input" {
		t.Fatalf("runtime = %#v", got)
	}
	thread.Status.ActiveFlags = nil
	if got := runtimeResult(thread, ""); got.Status != "running" {
		t.Fatalf("runtime = %#v", got)
	}
	thread.Status.Type = "idle"
	if got := runtimeResult(thread, "automation"); got.Status != "automation" {
		t.Fatalf("runtime = %#v", got)
	}
}

func TestDryRunScansWithoutCreatingState(t *testing.T) {
	root := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(root, "state_1.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE threads (
		id TEXT PRIMARY KEY, updated_at_ms INTEGER, title TEXT, name TEXT, archived INTEGER,
		source TEXT, thread_source TEXT, rollout_path TEXT)`)
	if err != nil {
		t.Fatal(err)
	}
	rollout := filepath.Join(root, "task.jsonl")
	if err := os.WriteFile(rollout, []byte(rolloutLine("response_item", map[string]any{"type": "message", "role": "assistant", "phase": "final_answer", "content": []map[string]string{{"text": "🧵🐻 complete"}}})), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO threads VALUES ('task',1,'subject',NULL,0,'vscode','',?)`, rollout)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	stateRoot := filepath.Join(root, "absent-state")
	t.Setenv("CODEX_SQLITE_HOME", root)
	t.Setenv("THREADBEAR_STATE_DIR", stateRoot)
	t.Setenv("THREADBEAR_CONTROL_TASK_ID", "control")
	result, err := heartbeat(context.Background(), true)
	if err != nil || result.Stats.Deterministic != 1 {
		t.Fatalf("heartbeat = %#v, %v", result, err)
	}
	if _, err := os.Stat(stateRoot); !os.IsNotExist(err) {
		t.Fatalf("dry run created %s", stateRoot)
	}
}

func TestHeartbeatStagesWithoutWritingTitle(t *testing.T) {
	root, db, rollout := testIndex(t, "task", "subject")
	writeRollout(t, rollout, rolloutLine("response_item", map[string]any{
		"type": "message", "role": "assistant", "phase": "final_answer",
		"content": []map[string]string{{"text": "🧵🐻 complete"}},
	}))
	stateRoot := filepath.Join(root, "state")
	t.Setenv("CODEX_SQLITE_HOME", root)
	t.Setenv("THREADBEAR_STATE_DIR", stateRoot)
	store := newStore(stateRoot)
	value := freshState("control")
	value.NativeBootstrap = false
	if err := store.save(value); err != nil {
		t.Fatal(err)
	}
	result, err := heartbeat(context.Background(), false)
	if err != nil || result.Stats.Staged != 1 {
		t.Fatalf("heartbeat = %#v, %v", result, err)
	}
	value, err = store.load()
	if err != nil || len(value.Plans) != 1 || value.Tasks["task"].LastApplied != "" {
		t.Fatalf("state = %#v, %v", value, err)
	}
	if value.LastScan.Staged != 1 {
		t.Fatalf("persisted staged count = %d", value.LastScan.Staged)
	}
	var title string
	if err := db.QueryRow(`SELECT COALESCE(name,title) FROM threads WHERE id='task'`).Scan(&title); err != nil || title != "subject" {
		t.Fatalf("title = %q, %v", title, err)
	}
}

func TestHeartbeatPrunesPlansForMissingTasks(t *testing.T) {
	root, db, rollout := testIndex(t, "task", "subject")
	writeRollout(t, rollout, rolloutLine("response_item", map[string]any{
		"type": "message", "role": "assistant", "phase": "final_answer",
		"content": []map[string]string{{"text": "🧵🐻 complete"}},
	}))
	t.Setenv("CODEX_SQLITE_HOME", root)
	t.Setenv("THREADBEAR_STATE_DIR", filepath.Join(root, "threadbear"))
	store := newStore(stateDir())
	value := freshState("control")
	value.NativeBootstrap = false
	if err := store.save(value); err != nil {
		t.Fatal(err)
	}
	if _, err := heartbeat(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM threads WHERE id='task'`); err != nil {
		t.Fatal(err)
	}
	if _, err := heartbeat(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	value, _ = store.load()
	if len(value.Plans) != 0 || len(value.Tasks) != 0 {
		t.Fatalf("missing task retained: %#v", value)
	}
}

func TestControlOperationAlwaysUsesNativeSetterAndGuardsTurn(t *testing.T) {
	root, db, rollout := testIndex(t, "control", "subject")
	writeRollout(t, rollout, rolloutLine("turn_context", map[string]any{"turn_id": "turn-1"})+
		rolloutLine("response_item", map[string]any{"type": "function_call_output", "output": strings.Repeat("x", 600*1024)}))
	t.Setenv("CODEX_SQLITE_HOME", root)
	t.Setenv("THREADBEAR_STATE_DIR", filepath.Join(root, "threadbear"))
	t.Setenv("CODEX_THREAD_ID", "control")
	store := newStore(stateDir())
	if err := store.save(freshState("control")); err != nil {
		t.Fatal(err)
	}
	if _, err := titlePlan(context.Background(), "stage", "", strings.NewReader("🧵🐻 complete")); err != nil {
		t.Fatal(err)
	}
	value, _ := store.load()
	var pending plan
	for _, pending = range value.Plans {
	}
	if _, err := db.Exec(`UPDATE threads SET name=? WHERE id='control'`, pending.DesiredTitle); err != nil {
		t.Fatal(err)
	}
	got, err := titlePlan(context.Background(), "operation", pending.ID, strings.NewReader(""))
	operation := got.(map[string]any)
	if err != nil || operation["action"] != "set" {
		t.Fatalf("same-title operation = %#v, %v", got, err)
	}
	f, err := os.OpenFile(rollout, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString(rolloutLine("turn_context", map[string]any{"turn_id": "turn-2"}))
	_ = f.Close()
	got, err = titlePlan(context.Background(), "operation", pending.ID, strings.NewReader(""))
	operation = got.(map[string]any)
	if err != nil || operation["disposition"] != "drifted" {
		t.Fatalf("new-turn operation = %#v, %v", got, err)
	}
}

func TestInstallRequiresExplicitControlTask(t *testing.T) {
	t.Setenv("CODEX_THREAD_ID", "ambient-task")
	if _, err := install(context.Background(), "", true, false); err == nil || !strings.Contains(err.Error(), "--control-task-id") {
		t.Fatalf("install error = %v", err)
	}
}

func TestInstallDryRunHasNoEffects(t *testing.T) {
	root, _, _ := testIndex(t, "control", "control task")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("CODEX_SQLITE_HOME", root)
	t.Setenv("THREADBEAR_STATE_DIR", filepath.Join(home, "state"))
	result, err := install(context.Background(), "control", true, false)
	if err != nil || result.(map[string]any)["dry_run"] != true {
		t.Fatalf("dry install = %#v, %v", result, err)
	}
	binary, _, _, _ := installPaths()
	if _, err := os.Stat(binary); !os.IsNotExist(err) {
		t.Fatalf("dry install created %s", binary)
	}
	if _, err := os.Stat(stateDir()); !os.IsNotExist(err) {
		t.Fatalf("dry install created %s", stateDir())
	}
}

func TestStatusChecksInstalledRuntime(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("THREADBEAR_STATE_DIR", filepath.Join(home, "state"))
	binary, _, _, plist := installPaths()
	if err := os.MkdirAll(filepath.Dir(binary), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(plist), 0o700); err != nil {
		t.Fatal(err)
	}
	writeRollout(t, binary, "binary")
	writeRollout(t, plist, "plist")
	if err := newStore(stateDir()).save(freshState("control")); err != nil {
		t.Fatal(err)
	}
	binDir := filepath.Join(home, "bin")
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		t.Fatal(err)
	}
	launchctl := filepath.Join(binDir, "launchctl")
	if err := os.WriteFile(launchctl, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+":"+os.Getenv("PATH"))
	if _, err := status(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(plist); err != nil {
		t.Fatal(err)
	}
	if _, err := status(); err == nil {
		t.Fatal("status accepted missing plist")
	}
}

func TestBootoutIgnoresOnlyMissingService(t *testing.T) {
	binDir := t.TempDir()
	launchctl := filepath.Join(binDir, "launchctl")
	t.Setenv("PATH", binDir+":"+os.Getenv("PATH"))
	if err := os.WriteFile(launchctl, []byte("#!/bin/sh\necho 'Boot-out failed: 3: No such process' >&2\nexit 3\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := bootout(context.Background(), "gui/1"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(launchctl, []byte("#!/bin/sh\necho denied >&2\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := bootout(context.Background(), "gui/1"); err == nil {
		t.Fatal("bootout ignored hard failure")
	}
}

func TestUninstallRemovesOnlyManagedRuntime(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("THREADBEAR_STATE_DIR", filepath.Join(home, "state"))
	binary, agents, skill, plist := installPaths()
	for _, path := range []string{binary, plist} {
		if err := writeAtomic(path, []byte("managed"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := writeAtomic(agents, []byte("keep this\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeManaged(agents, "managed agents"); err != nil {
		t.Fatal(err)
	}
	if err := writeManaged(skill, "managed skill"); err != nil {
		t.Fatal(err)
	}
	if err := newStore(stateDir()).save(freshState("control")); err != nil {
		t.Fatal(err)
	}
	binDir := filepath.Join(home, "bin")
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "launchctl"), []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+":"+os.Getenv("PATH"))
	if _, err := uninstall(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(agents)
	if err != nil || string(data) != "keep this\n" {
		t.Fatalf("unrelated guidance = %q, %v", data, err)
	}
	for _, path := range []string{binary, skill, plist, stateDir()} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("uninstall retained %s", path)
		}
	}
}

func TestHeartbeatAndAppServerAreDeadlineBound(t *testing.T) {
	if heartbeatLimit >= 5*time.Minute {
		t.Fatalf("heartbeat limit = %s", heartbeatLimit)
	}
	command := filepath.Join(t.TempDir(), "codex")
	if err := os.WriteFile(command, []byte("#!/bin/sh\nsleep 5 &\nwait\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	if _, err := openApp(ctx, command); err == nil || time.Since(started) > time.Second {
		t.Fatalf("deadline result after %s: %v", time.Since(started), err)
	}
}

func TestNativeCellRejectsUnacceptedReport(t *testing.T) {
	if !strings.Contains(assets.SkillManagedContent, `outcome === "succeeded" && report.accepted === 1`) ||
		!strings.Contains(assets.SkillManagedContent, `counts.rejected++; complete = false`) {
		t.Fatal("native cell can complete after an unaccepted report")
	}
}

func TestTitleReportRequiresExactNativeResult(t *testing.T) {
	root, _, _ := testIndex(t, "task", "✅ subject")
	t.Setenv("CODEX_SQLITE_HOME", root)
	t.Setenv("THREADBEAR_STATE_DIR", filepath.Join(root, "threadbear"))
	store := newStore(stateDir())
	value := freshState("control")
	pending := plan{ID: "operation", TaskID: "task", DesiredTitle: "✅ subject", Status: "complete", Subject: "subject"}
	value.Plans[pending.ID] = pending
	value.Tasks[pending.TaskID] = taskRecord{Subject: "subject"}
	if err := store.save(value); err != nil {
		t.Fatal(err)
	}
	wrong := `{"reports":[{"operation_id":"operation","outcome":"succeeded","task_id":"other","title":"✅ subject"}]}`
	got, err := titlePlan(context.Background(), "report", "", strings.NewReader(wrong))
	if err != nil || got.(map[string]any)["accepted"] != 0 {
		t.Fatalf("mismatched report = %#v, %v", got, err)
	}
	exact := `{"reports":[{"operation_id":"operation","outcome":"succeeded","task_id":"task","title":"✅ subject"}]}`
	got, err = titlePlan(context.Background(), "report", "", strings.NewReader(exact))
	if err != nil || got.(map[string]any)["accepted"] != 1 {
		t.Fatalf("exact report = %#v, %v", got, err)
	}
}

func TestTitleBatchIsSortedAndBounded(t *testing.T) {
	root := t.TempDir()
	t.Setenv("THREADBEAR_STATE_DIR", root)
	store := newStore(root)
	for _, count := range []int{8, 9} {
		value := freshState("control")
		for i := count - 1; i >= 0; i-- {
			id := string(rune('a' + i))
			value.Plans[id] = plan{ID: id, TaskID: id}
		}
		if err := store.save(value); err != nil {
			t.Fatal(err)
		}
		got, err := titlePlan(context.Background(), "batch", "", strings.NewReader(""))
		if err != nil {
			t.Fatal(err)
		}
		batch := got.(map[string]any)
		ids := batch["operation_ids"].([]string)
		if len(ids) != 8 || strings.Join(ids, "") != "abcdefgh" || batch["continuation_due"] != (count == 9) {
			t.Fatalf("%d plans -> %#v", count, batch)
		}
	}
}

func TestLunaResultValidation(t *testing.T) {
	expected := map[string]bool{"a": true, "b": true}
	valid := `{"results":[{"task_id":"a","status":"complete","action":""},{"task_id":"b","status":"needs_input","action":" choose region "}]}`
	results, err := decodeLuna([]byte(valid), expected, 2)
	if err != nil || results["b"].Action != "choose region" {
		t.Fatalf("valid result = %#v, %v", results, err)
	}
	for name, data := range map[string]string{
		"malformed": `{`,
		"missing":   `{"results":[{"task_id":"a","status":"complete","action":""}]}`,
		"duplicate": `{"results":[{"task_id":"a","status":"complete","action":""},{"task_id":"a","status":"complete","action":""}]}`,
		"unknown":   `{"results":[{"task_id":"a","status":"complete","action":""},{"task_id":"x","status":"complete","action":""}]}`,
		"status":    `{"results":[{"task_id":"a","status":"invented","action":""},{"task_id":"b","status":"complete","action":""}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := decodeLuna([]byte(data), expected, 2); err == nil {
				t.Fatal("invalid Luna result accepted")
			}
		})
	}
}

func TestLiveLunaBatch(t *testing.T) {
	if os.Getenv("THREADBEAR_LIVE_LUNA") != "1" {
		t.Skip("set THREADBEAR_LIVE_LUNA=1 for the opt-in model canary")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	tasks := []semanticCandidate{
		{Guard: guardedResult{Task: indexedTask{ID: "legacy-complete"}}, Text: "USER:\nFix the typo\nASSISTANT:\nFixed and tests pass."},
		{Guard: guardedResult{Task: indexedTask{ID: "legacy-question"}}, Text: "USER:\nDeploy it\nASSISTANT:\nWhich region should I use?"},
	}
	results, err := classifyLuna(ctx, tasks)
	if err != nil || len(results) != 2 || !results["legacy-complete"].Resolved || !results["legacy-question"].Resolved {
		t.Fatalf("classifyLuna = %#v, %v", results, err)
	}
}

func TestLiveAppServerRead(t *testing.T) {
	if os.Getenv("THREADBEAR_LIVE_APP") != "1" {
		t.Skip("set THREADBEAR_LIVE_APP=1 for the opt-in App Server canary")
	}
	id := os.Getenv("THREADBEAR_CANARY_TASK_ID")
	if id == "" {
		id = os.Getenv("CODEX_THREAD_ID")
	}
	if id == "" {
		t.Fatal("CODEX_THREAD_ID is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client, err := openApp(ctx, "codex")
	if err != nil {
		t.Fatal(err)
	}
	defer client.close()
	thread, err := client.readThread(id)
	if err != nil || thread.ID != id {
		t.Fatalf("readThread = %#v, %v", thread, err)
	}
}

func TestLiveNativePlanPrepare(t *testing.T) {
	if os.Getenv("THREADBEAR_LIVE_NATIVE") != "1" {
		t.Skip("set THREADBEAR_LIVE_NATIVE=1 for the retained-task canary")
	}
	id := os.Getenv("CODEX_THREAD_ID")
	if id == "" {
		t.Fatal("CODEX_THREAD_ID is required")
	}
	store := newStore(stateDir())
	if err := store.save(freshState(id)); err != nil {
		t.Fatal(err)
	}
	footer := "🧵🐻 next steps (agent): verify the native Desktop canary"
	if _, err := titlePlan(context.Background(), "stage", "", strings.NewReader(footer)); err != nil {
		t.Fatal(err)
	}
	value, err := store.load()
	if err != nil || len(value.Plans) != 1 {
		t.Fatalf("prepared state = %#v, %v", value, err)
	}
}

func rolloutLine(kind string, payload any) string {
	data, _ := json.Marshal(map[string]any{"type": kind, "payload": payload})
	return string(data) + "\n"
}

func testIndex(t *testing.T, id, title string) (string, *sql.DB, string) {
	t.Helper()
	root := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(root, "state_1.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`CREATE TABLE threads (
		id TEXT PRIMARY KEY, updated_at_ms INTEGER, title TEXT, name TEXT, archived INTEGER,
		source TEXT, thread_source TEXT, rollout_path TEXT)`); err != nil {
		t.Fatal(err)
	}
	rollout := filepath.Join(root, id+".jsonl")
	writeRollout(t, rollout, "")
	if _, err := db.Exec(`INSERT INTO threads VALUES (?,1,?,NULL,0,'vscode','',?)`, id, title, rollout); err != nil {
		t.Fatal(err)
	}
	return root, db, rollout
}

func writeRollout(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
