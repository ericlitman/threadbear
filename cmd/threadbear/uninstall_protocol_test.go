package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func (s store) waitLock() (*os.File, error) { return s.openLock("operation.lock", unix.LOCK_EX, true) }

func uninstall(ctx context.Context, confirmed bool) (any, error) {
	if !confirmed {
		return nil, os.ErrPermission
	}
	operationLock, err := newStore(stateDir()).waitLock()
	if err != nil {
		return nil, err
	}
	defer unlock(operationLock)
	value, err := currentStateOrEmpty()
	if err != nil {
		return nil, err
	}
	if value.Phase == phaseMigrationRunning {
		return nil, errors.New("cannot uninstall while installation migration is running; stop the controller first")
	}
	if value.ArchivePending != nil {
		return nil, errors.New("cannot uninstall while a native archive operation is pending; reconcile it first")
	}
	if value.MainTaskID != "" {
		main, found, err := archiveTaskByID(ctx, value.MainTaskID)
		if err != nil {
			return nil, err
		}
		if found && stripStatusIcons(main.Title) != main.Title {
			return nil, errors.New("uninstall requires title cleanup from the ThreadBear control task")
		}
	}
	return uninstallLocked(ctx, value)
}

func addUninstallOwner(t testing.TB, db *sql.DB, root string) {
	t.Helper()
	addTask(t, db, root, "requester", "Uninstall owner", nil, "vscode", 0)
}

func TestArchivedControlUninstallPersistsInitiatorAndAuthorizesCleanup(t *testing.T) {
	root, db := testIndex(t)
	p := installPaths()
	addTask(t, db, root, "main", "⏳ Control task", nil, "vscode", 1)
	addTask(t, db, root, "controller", "⏳ Completed controller", nil, "vscode", 1)
	addUninstallOwner(t, db, root)
	if _, err := install("main", false, true, false); err != nil {
		t.Fatal(err)
	}
	if err := newStore(stateDir()).update(func(value *state) (bool, error) {
		value.ControllerTaskID, value.Phase = "controller", phaseMigrationComplete
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer
	if code := run(context.Background(), []string{"uninstall", "--prepare", "--initiator-task-id", "requester", "--json"}, strings.NewReader(""), &output, &bytes.Buffer{}); code != 0 {
		t.Fatalf("prepare code %d: %s", code, output.String())
	}
	for _, want := range []string{`"prepared":true`, `"initiator_task_id":"requester"`, `"main_task_id":"main"`, `"main_archived":true`} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("prepare output missing %s: %s", want, output.String())
		}
	}
	output.Reset()
	if code := run(context.Background(), []string{"uninstall", "--prepare", "--initiator-task-id", "requester", "--json"}, strings.NewReader(""), &output, &bytes.Buffer{}); code != 0 || !strings.Contains(output.String(), `"resumed":true`) {
		t.Fatalf("same-owner resume code %d: %s", code, output.String())
	}
	output.Reset()
	if code := run(context.Background(), []string{"uninstall", "--prepare", "--initiator-task-id", "other", "--json"}, strings.NewReader(""), &output, &bytes.Buffer{}); code != 1 {
		t.Fatalf("second owner code %d: %s", code, output.String())
	}
	ordinary := hookPayload("PreToolUse", "requester", "owner-running", map[string]any{"title": runningMarker + ": Uninstall owner"}, nil)
	output.Reset()
	if err := hook(context.Background(), strings.NewReader(ordinary), &output); err != nil || !strings.Contains(output.String(), `"permissionDecision":"deny"`) {
		t.Fatalf("ordinary title during uninstall = %q, %v", output.String(), err)
	}
	for _, plain := range []string{"Renamed", homeTitle} {
		output.Reset()
		payload := hookPayload("PreToolUse", "other", "plain-during-uninstall", map[string]any{"threadId": "main", "title": plain}, nil)
		if err := hook(context.Background(), strings.NewReader(payload), &output); err != nil || !strings.Contains(output.String(), `"permissionDecision":"deny"`) {
			t.Fatalf("plain title %q during uninstall = %q, %v", plain, output.String(), err)
		}
	}

	if _, err := db.Exec(`UPDATE threads SET archived=0 WHERE id='main'`); err != nil {
		t.Fatal(err)
	}
	denied := hookPayload("PreToolUse", "other", "other-cleanup", map[string]any{"threadId": "main", "title": cleanupMarker}, nil)
	output.Reset()
	if err := hook(context.Background(), strings.NewReader(denied), &output); err != nil || !strings.Contains(output.String(), `"permissionDecision":"deny"`) {
		t.Fatalf("other cleanup = %q, %v", output.String(), err)
	}
	pre := hookPayload("PreToolUse", "requester", "owner-cleanup", map[string]any{"threadId": "main", "title": cleanupMarker}, nil)
	output.Reset()
	if err := hook(context.Background(), strings.NewReader(pre), &output); err != nil {
		t.Fatal(err)
	}
	proposed := rewrittenTitle(t, output.Bytes())
	if proposed != "Control task" {
		t.Fatalf("owner cleanup title = %q", proposed)
	}
	if _, err := db.Exec(`UPDATE threads SET title=?, archived=1 WHERE id='main'`, proposed); err != nil {
		t.Fatal(err)
	}
	response, _ := json.Marshal(map[string]string{"threadId": "main", "title": proposed})
	post := hookPayload("PostToolUse", "requester", "owner-cleanup", map[string]any{"threadId": "main", "title": proposed}, string(response))
	if err := hook(context.Background(), strings.NewReader(post), &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}

	output.Reset()
	if code := run(context.Background(), []string{"uninstall", "--initiator-task-id", "requester", "--noninteractive", "--confirm", "--json"}, strings.NewReader(""), &output, &bytes.Buffer{}); code != 0 {
		t.Fatalf("commit code %d: %s", code, output.String())
	}
	for _, path := range []string{p.binary, p.skill, stateDir()} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("uninstall left %s: %v", path, err)
		}
	}
	output.Reset()
	if code := run(context.Background(), []string{"uninstall", "--initiator-task-id", "requester", "--noninteractive", "--confirm", "--json"}, strings.NewReader(""), &output, &bytes.Buffer{}); code != 0 || !strings.Contains(output.String(), `"uninstalled":true`) {
		t.Fatalf("retained-candidate no-op code %d: %s", code, output.String())
	}
}

func TestArchivedControlUninstallPrepareRequiresActiveUserInitiator(t *testing.T) {
	root, db := testIndex(t)
	addTask(t, db, root, "main", "Control task", nil, "vscode", 1)
	addTask(t, db, root, "archived", "Archived", nil, "vscode", 1)
	addTask(t, db, root, "invisible", "Invisible", nil, "vscode", 0)
	addTask(t, db, root, "nonuser", "Automation", nil, "mcp", 0)
	if _, err := db.Exec(`UPDATE threads SET preview='' WHERE id='invisible'`); err != nil {
		t.Fatal(err)
	}
	if _, err := install("main", false, true, false); err != nil {
		t.Fatal(err)
	}
	if err := newStore(stateDir()).update(func(value *state) (bool, error) {
		value.Phase = phaseMigrationComplete
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"missing", "archived", "invisible", "nonuser"} {
		if _, err := prepareUninstall(context.Background(), id); err == nil {
			t.Fatalf("prepare accepted initiator %q", id)
		}
		value, _ := newStore(stateDir()).read()
		if value.UninstallPending != nil {
			t.Fatalf("failed prepare persisted owner %q", id)
		}
	}
}

func TestArchivedControlUninstallAbortRestoresOrdinaryOperation(t *testing.T) {
	root, db := testIndex(t)
	addTask(t, db, root, "main", "⏳ Control task", nil, "vscode", 1)
	addUninstallOwner(t, db, root)
	if _, err := install("main", false, true, false); err != nil {
		t.Fatal(err)
	}
	if err := newStore(stateDir()).update(func(value *state) (bool, error) {
		value.Phase = phaseMigrationComplete
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := prepareUninstall(context.Background(), "requester"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE threads SET archived=0 WHERE id='main'`); err != nil {
		t.Fatal(err)
	}
	if _, err := completeUninstall(context.Background(), "requester", false, true); err == nil {
		t.Fatal("abort accepted unrestored archive state")
	}
	if _, err := db.Exec(`UPDATE threads SET archived=1 WHERE id='main'`); err != nil {
		t.Fatal(err)
	}
	if err := newStore(stateDir()).update(func(value *state) (bool, error) {
		value.Tasks["requester"] = taskState{Pending: &pendingProposal{ToolUseID: "unknown", Prior: "Uninstall owner", Proposed: "Owner"}}
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	result, err := completeUninstall(context.Background(), "requester", false, true)
	if err != nil || result.(map[string]any)["aborted"] != true {
		t.Fatalf("abort = %#v, %v", result, err)
	}
	value, _ := newStore(stateDir()).read()
	if value.UninstallPending != nil || value.Tasks["requester"].Pending != nil {
		t.Fatalf("abort left pending state: %#v", value)
	}
	if _, err := os.Stat(installPaths().binary); err != nil {
		t.Fatalf("abort removed ThreadBear: %v", err)
	}
}

func TestArchivedControlUninstallPrepareReconcilesPendingNativeTitle(t *testing.T) {
	root, db := testIndex(t)
	addTask(t, db, root, "main", "Control task", nil, "vscode", 1)
	addUninstallOwner(t, db, root)
	if _, err := install("main", false, true, false); err != nil {
		t.Fatal(err)
	}
	if err := newStore(stateDir()).update(func(value *state) (bool, error) { value.Phase = phaseMigrationComplete; return true, nil }); err != nil {
		t.Fatal(err)
	}
	plain := hookPayload("PreToolUse", "other", "in-flight-plain", map[string]any{"threadId": "main", "title": "Renamed"}, nil)
	if err := hook(context.Background(), strings.NewReader(plain), &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if code := run(context.Background(), []string{"uninstall", "--prepare", "--initiator-task-id", "requester", "--json"}, strings.NewReader(""), &output, &bytes.Buffer{}); code != 0 || !strings.Contains(output.String(), `"drifted_titles":1`) {
		t.Fatalf("pending title prepare code %d: %s", code, output.String())
	}
	value, _ := newStore(stateDir()).read()
	if value.Tasks["main"].Pending != nil {
		t.Fatal("prepare left reconciled title pending")
	}
}

func TestRetainedCandidateFinishesBinaryRemovalAfterStateCommit(t *testing.T) {
	root, db := testIndex(t)
	addTask(t, db, root, "main", "Control task", nil, "vscode", 1)
	p := installPaths()
	if _, err := install("main", false, true, false); err != nil {
		t.Fatal(err)
	}
	if err := newStore(stateDir()).update(func(value *state) (bool, error) {
		value.Phase = phaseMigrationComplete
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := manageBlock(p.agents, ""); err != nil {
		t.Fatal(err)
	}
	hooks, write, err := editHooks(p.hooks, p.binary, false)
	if err != nil {
		t.Fatal(err)
	}
	if write {
		if len(hooks) == 0 {
			if err := removeFiles(p.hooks); err != nil {
				t.Fatal(err)
			}
		} else if err := writeAtomic(p.hooks, hooks, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := removeFiles(p.skill); err != nil {
		t.Fatal(err)
	}
	if err := removeFiles(newStore(stateDir()).path()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p.binary); err != nil {
		t.Fatalf("fixture lost installed binary: %v", err)
	}
	var output bytes.Buffer
	if code := run(context.Background(), []string{"uninstall", "--initiator-task-id", "requester", "--noninteractive", "--confirm", "--json"}, strings.NewReader(""), &output, &bytes.Buffer{}); code != 0 {
		t.Fatalf("retained candidate finish code %d: %s", code, output.String())
	}
	if _, err := os.Stat(p.binary); !os.IsNotExist(err) {
		t.Fatalf("retained candidate left binary: %v", err)
	}
}

func TestArchivedControlUninstallResumeReconcilesUnknownAppliedTitle(t *testing.T) {
	root, db := testIndex(t)
	addTask(t, db, root, "main", "⏳ Control task", nil, "vscode", 1)
	addUninstallOwner(t, db, root)
	if _, err := install("main", false, true, false); err != nil {
		t.Fatal(err)
	}
	if err := newStore(stateDir()).update(func(value *state) (bool, error) {
		value.Phase = phaseMigrationComplete
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if code := run(context.Background(), []string{"uninstall", "--prepare", "--initiator-task-id", "requester", "--json"}, strings.NewReader(""), &output, &bytes.Buffer{}); code != 0 {
		t.Fatalf("prepare code %d: %s", code, output.String())
	}
	if _, err := db.Exec(`UPDATE threads SET archived=0 WHERE id='main'`); err != nil {
		t.Fatal(err)
	}
	pre := hookPayload("PreToolUse", "requester", "unknown-cleanup", map[string]any{"threadId": "main", "title": cleanupMarker}, nil)
	output.Reset()
	if err := hook(context.Background(), strings.NewReader(pre), &output); err != nil {
		t.Fatal(err)
	}
	proposed := rewrittenTitle(t, output.Bytes())
	if _, err := db.Exec(`UPDATE threads SET title=?, archived=1 WHERE id='main'`, proposed); err != nil {
		t.Fatal(err)
	}
	addTask(t, db, root, "prior", "⏳ Prior", nil, "vscode", 0)
	addTask(t, db, root, "renamed", "User rename", nil, "vscode", 0)
	if err := newStore(stateDir()).update(func(value *state) (bool, error) {
		value.Tasks["prior"] = taskState{Last: "previous", Pending: &pendingProposal{Prior: "⏳ Prior", Proposed: "Prior"}}
		value.Tasks["renamed"] = taskState{Pending: &pendingProposal{Prior: "⏳ Old", Proposed: "Old"}}
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if code := run(context.Background(), []string{"uninstall", "--prepare", "--initiator-task-id", "requester", "--json"}, strings.NewReader(""), &output, &bytes.Buffer{}); code != 0 || !strings.Contains(output.String(), `"reconciled_titles":2`) || !strings.Contains(output.String(), `"drifted_titles":1`) {
		t.Fatalf("unknown-result resume code %d: %s", code, output.String())
	}
	value, err := newStore(stateDir()).read()
	if err != nil || value.Tasks["main"].Pending != nil || value.Tasks["main"].Last != proposed || value.Tasks["prior"].Last != "previous" || value.Tasks["renamed"].Pending != nil {
		t.Fatalf("reconciled unknown title = %#v, %v", value.Tasks["main"], err)
	}
}

func TestArchivedControlUninstallCommitRequiresRestoredArchiveAndSettledTitles(t *testing.T) {
	root, db := testIndex(t)
	p := installPaths()
	addTask(t, db, root, "main", "⏳ Control task", nil, "vscode", 1)
	addUninstallOwner(t, db, root)
	if _, err := install("main", false, true, false); err != nil {
		t.Fatal(err)
	}
	if err := newStore(stateDir()).update(func(value *state) (bool, error) {
		value.Phase = phaseMigrationComplete
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if code := run(context.Background(), []string{"uninstall", "--prepare", "--initiator-task-id", "requester", "--json"}, strings.NewReader(""), &output, &bytes.Buffer{}); code != 0 {
		t.Fatalf("prepare code %d: %s", code, output.String())
	}
	if _, err := db.Exec(`UPDATE threads SET title='Control task', archived=0 WHERE id='main'`); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if code := run(context.Background(), []string{"uninstall", "--initiator-task-id", "requester", "--noninteractive", "--confirm", "--json"}, strings.NewReader(""), &output, &bytes.Buffer{}); code != 1 || !strings.Contains(output.String(), "archive state") {
		t.Fatalf("unrestored archive commit code %d: %s", code, output.String())
	}
	if _, err := os.Stat(p.binary); err != nil {
		t.Fatalf("blocked commit removed retry binary: %v", err)
	}
	if _, err := db.Exec(`UPDATE threads SET archived=1 WHERE id='main'`); err != nil {
		t.Fatal(err)
	}
	if err := newStore(stateDir()).update(func(value *state) (bool, error) {
		value.Tasks["main"] = taskState{Pending: &pendingProposal{ToolUseID: "unknown", Proposed: "Control task"}}
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if code := run(context.Background(), []string{"uninstall", "--initiator-task-id", "requester", "--noninteractive", "--confirm", "--json"}, strings.NewReader(""), &output, &bytes.Buffer{}); code != 1 || !strings.Contains(output.String(), "native title operation is pending") {
		t.Fatalf("unknown title commit code %d: %s", code, output.String())
	}
	if _, err := os.Stat(p.binary); err != nil {
		t.Fatalf("unknown title commit removed retry binary: %v", err)
	}
	if err := newStore(stateDir()).update(func(value *state) (bool, error) {
		record := value.Tasks["main"]
		record.Pending = nil
		value.Tasks["main"] = record
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if code := run(context.Background(), []string{"uninstall", "--initiator-task-id", "requester", "--noninteractive", "--confirm", "--json"}, strings.NewReader(""), &output, &bytes.Buffer{}); code != 0 {
		t.Fatalf("settled resumed commit code %d: %s", code, output.String())
	}
}
