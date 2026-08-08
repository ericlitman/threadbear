package main

import (
	"errors"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testSubjectStore(t testing.TB) store {
	t.Helper()
	disk := newStore(filepath.Join(t.TempDir(), "state"))
	if err := os.MkdirAll(disk.subjectDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(disk.dir, "lifecycle.lock"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	return disk
}

func TestSubjectRecordsArePrivateAndPerTask(t *testing.T) {
	disk := testSubjectStore(t)
	if _, err := disk.readTask(testTaskID); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("absent read error = %v", err)
	}
	if err := disk.updateTask(testTaskID, func(record *taskState) (bool, error) {
		record.Subject = "Customer  outage "
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	dataPath, lockPath, err := disk.paths(testTaskID)
	if err != nil {
		t.Fatal(err)
	}
	for path, mode := range map[string]os.FileMode{
		disk.subjectDir():                         0o700,
		filepath.Join(disk.dir, "lifecycle.lock"): 0o600,
		dataPath: 0o600,
		lockPath: 0o600,
	} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != mode {
			t.Fatalf("%s mode = %o, want %o", path, info.Mode().Perm(), mode)
		}
	}
	got, err := disk.readTask(testTaskID)
	if err != nil || got.Subject != "Customer  outage " {
		t.Fatalf("record = %#v, %v", got, err)
	}
	data, err := os.ReadFile(dataPath)
	if err != nil || string(data) != "{\"subject\":\"Customer  outage \"}\n" {
		t.Fatalf("record bytes = %q, %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(disk.dir, "subjects.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("global subject map exists")
	}
}

func TestSubjectRecordCorruptionFailsOnlyThatRecord(t *testing.T) {
	disk := testSubjectStore(t)
	for _, item := range []struct{ id, subject string }{{testBadID, "bad"}, {testGoodID, "good"}} {
		if err := disk.updateTask(item.id, func(record *taskState) (bool, error) {
			record.Subject = item.subject
			return true, nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	badPath, _, _ := disk.paths(testBadID)
	if err := os.WriteFile(badPath, []byte(`{"subject":"bad","unexpected":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := disk.readTask(testBadID); err == nil {
		t.Fatal("corrupt record was accepted")
	}
	if got, err := disk.readTask(testGoodID); err != nil || got.Subject != "good" {
		t.Fatalf("healthy record = %#v, %v", got, err)
	}
}

func TestSubjectRecordRejectsUnsafePaths(t *testing.T) {
	disk := testSubjectStore(t)
	for _, id := range []string{"", "task", "../escape", "a/b", strings.Repeat("x", 129), strings.ToUpper(testDelegatedID)} {
		if err := disk.updateTask(id, func(*taskState) (bool, error) { return false, nil }); err == nil {
			t.Fatalf("unsafe task ID %q was accepted", id)
		}
	}
	realDir := t.TempDir()
	linkState := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(linkState, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(linkState, "lifecycle.lock"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realDir, filepath.Join(linkState, "subjects")); err != nil {
		t.Fatal(err)
	}
	if err := newStore(linkState).updateTask(testTaskID, func(record *taskState) (bool, error) {
		record.Subject = "subject"
		return true, nil
	}); err == nil {
		t.Fatal("symlink subject directory was accepted")
	}
}

func TestSubjectMutationErrorDoesNotSave(t *testing.T) {
	disk := testSubjectStore(t)
	want := errors.New("stop")
	if err := disk.updateTask(testTaskID, func(record *taskState) (bool, error) {
		record.Subject = "not saved"
		return false, want
	}); !errors.Is(err, want) {
		t.Fatalf("update error = %v", err)
	}
	if _, err := disk.readTask(testTaskID); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed mutation wrote state: %v", err)
	}
}

func TestSubjectWritesShareLifecycleFence(t *testing.T) {
	disk := testSubjectStore(t)
	firstEntered := make(chan struct{})
	releaseFirst := make(chan struct{})
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- disk.updateTask(testFirstID, func(record *taskState) (bool, error) {
			close(firstEntered)
			<-releaseFirst
			record.Subject = "first"
			return true, nil
		})
	}()
	<-firstEntered
	secondDone := make(chan error, 1)
	go func() {
		secondDone <- disk.updateTask(testSecondID, func(record *taskState) (bool, error) {
			record.Subject = "second"
			return true, nil
		})
	}()
	select {
	case err := <-secondDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("independent subject writes serialized on the lifecycle fence")
	}
	close(releaseFirst)
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
}

func TestSubjectWriteRefusesBusyTeardownFenceWithoutWaiting(t *testing.T) {
	disk := testSubjectStore(t)
	path := filepath.Join(disk.dir, "lifecycle.lock")
	lifecycle, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := unix.Flock(int(lifecycle.Fd()), unix.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	err = disk.updateTask(testLateID, func(record *taskState) (bool, error) {
		record.Subject = "late"
		return true, nil
	})
	if err == nil || !strings.Contains(err.Error(), "lifecycle is busy") {
		unlock(lifecycle)
		t.Fatalf("subject write with exclusive lifecycle fence = %v", err)
	}
	if elapsed := time.Since(started); elapsed > 250*time.Millisecond {
		unlock(lifecycle)
		t.Fatalf("subject write waited behind lifecycle teardown for %s", elapsed)
	}
	unlock(lifecycle)
	if _, err := disk.readTask(testLateID); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("refused subject write created state: %v", err)
	}
}

func TestSubjectWriteWithoutLifecycleFenceDoesNotCreateState(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "absent")
	disk := newStore(dir)
	if err := disk.updateTask(testLateID, func(record *taskState) (bool, error) {
		record.Subject = "late"
		return true, nil
	}); err == nil {
		t.Fatal("subject write without lifecycle fence succeeded")
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed subject write created state: %v", err)
	}
}

func TestResolveSubjectUsesFiniteOwnershipAndAdoptsRenameVerbatim(t *testing.T) {
	record := taskState{Subject: "Stable subject"}
	for _, icon := range ownedIcons {
		current := icon + " Stable subject"
		if got, err := resolveSubject(current, record); err != nil || got != record.Subject {
			t.Errorf("owned %q = %q, %v", current, got, err)
		}
	}
	rename := "✅ User  rename "
	if got, err := resolveSubject(rename, record); err != nil || got != rename {
		t.Fatalf("rename = %q, %v", got, err)
	}
	for _, operation := range []string{
		"🧵🐻 complete",
		"🧵🐻 automation",
		"🧵🐻 next steps (agent): finish the release",
		"🧵🐻 needs input (you): approve onboarding 190 safe tasks",
		"🧵🐻 blocked (external): restore the signing service",
		"⏳ ThreadBear is working",
		"⏳ ThreadBear is working: stale running title",
	} {
		if got, err := resolveSubject(operation, record); err != nil || got != record.Subject {
			t.Fatalf("operation recovery %q = %q, %v", operation, got, err)
		}
		if _, err := resolveSubject(operation, taskState{}); err == nil {
			t.Fatalf("unowned operation title %q was adopted", operation)
		}
	}
	for _, rename := range []string{"🧵🐻 Personal project", "🧵🐻 needs attention"} {
		if got, err := resolveSubject(rename, record); err != nil || got != rename {
			t.Fatalf("bear-prefixed user rename %q = %q, %v", rename, got, err)
		}
	}
	if _, err := resolveSubject("✅ Unowned", taskState{}); err == nil {
		t.Fatal("unowned legacy prefix was adopted")
	}
	if _, err := resolveSubject("<codex_delegation>raw", record); err == nil {
		t.Fatal("raw envelope was adopted as a rename")
	}
	if _, err := resolveSubject("<codex_internal_context source=\"goal\">raw", record); err == nil {
		t.Fatal("internal context was adopted as a rename")
	}
	if _, err := resolveSubject("✅ <codex_delegation>raw", record); err == nil {
		t.Fatal("decorated delegation envelope was adopted as a rename")
	}
	for _, envelope := range []string{
		"<app-context>Codex desktop context</app-context>",
		"<collaboration_mode>Default</collaboration_mode>",
		"<multi_agent_mode>active</multi_agent_mode>",
		"<permissions instructions>unrestricted</permissions instructions>",
	} {
		if _, err := resolveSubject(envelope, record); err == nil || !strings.Contains(err.Error(), "internal envelope") {
			t.Fatalf("internal envelope %q was adopted: %v", envelope, err)
		}
	}
}

func TestRenderTitlePreservesSubjectAndNeverTruncates(t *testing.T) {
	subject := "  🎉 Exact  whitespace "
	got, err := renderTitle("complete", subject)
	if err != nil || got != "✅ "+subject {
		t.Fatalf("render = %q, %v", got, err)
	}
	fit := strings.Repeat("x", 57)
	if got, err := renderTitle("blocked", fit); err != nil || utf16Units(got) != 60 {
		t.Fatalf("fitting title = %q (%d), %v", got, utf16Units(got), err)
	}
	if got, err := renderTitle("complete", strings.Repeat("x", 58)); err == nil || got != "" {
		t.Fatalf("too-long title = %q, %v", got, err)
	}
	emojiFit := strings.Repeat("🧵", 28)
	if got, err := renderTitle("complete", emojiFit); err != nil || got != "✅ "+emojiFit {
		t.Fatalf("emoji title = %q, %v", got, err)
	}
}
