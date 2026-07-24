package state

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ericlitman/threadbear/internal/config"
)

func TestStoreRoundTripAndPrivateModes(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "private", "threadbear")
	store := NewStore(dir)
	cfg := config.Default("control-123")
	stateValue := validState()
	cycle := validCycle()

	if err := store.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	if err := store.SaveState(stateValue); err != nil {
		t.Fatalf("SaveState() error = %v", err)
	}
	if err := store.SaveCycle(cycle); err != nil {
		t.Fatalf("SaveCycle() error = %v", err)
	}

	assertMode(t, dir, 0700)
	for _, name := range []string{configFileName, stateFileName, cycleFileName} {
		assertMode(t, filepath.Join(dir, name), 0600)
	}

	gotConfig, err := store.LoadConfig()
	if err != nil || !reflect.DeepEqual(gotConfig, cfg) {
		t.Fatalf("LoadConfig() = %#v, %v", gotConfig, err)
	}
	gotState, err := store.LoadState()
	if err != nil || !reflect.DeepEqual(gotState, stateValue) {
		t.Fatalf("LoadState() = %#v, %v", gotState, err)
	}
	if got, want := gotState.Tasks["task-1"].CapturedTitle, "✅ Ship ThreadBear"; got != want {
		t.Fatalf("captured title round trip = %q, want %q", got, want)
	}
	gotCycle, err := store.LoadCycle()
	if err != nil || !reflect.DeepEqual(gotCycle, cycle) {
		t.Fatalf("LoadCycle() = %#v, %v", gotCycle, err)
	}

	if err := store.RemoveCycle(); err != nil {
		t.Fatalf("RemoveCycle() error = %v", err)
	}
	if _, err := store.LoadCycle(); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("LoadCycle() after removal error = %v", err)
	}
}

func TestStoreCorrectsPermissiveModes(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "threadbear")
	if err := os.Mkdir(dir, 0777); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0777); err != nil {
		t.Fatal(err)
	}
	store := NewStore(dir)
	if err := store.SaveState(validState()); err != nil {
		t.Fatal(err)
	}
	assertMode(t, dir, 0700)
}

func TestLoadDoesNotCreateOrRepairPaths(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing", "threadbear")
	store := NewStore(missing)
	if _, err := store.LoadState(); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("LoadState() missing directory error = %v", err)
	}
	if _, err := os.Stat(missing); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("read created state directory: %v", err)
	}

	dir := privateTempDir(t)
	store = NewStore(dir)
	if err := store.SaveConfig(config.Default("control-123")); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveState(validState()); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveCycle(validCycle()); err != nil {
		t.Fatal(err)
	}
	files := []struct {
		name string
		load func() error
	}{
		{configFileName, func() error { _, err := store.LoadConfig(); return err }},
		{stateFileName, func() error { _, err := store.LoadState(); return err }},
		{cycleFileName, func() error { _, err := store.LoadCycle(); return err }},
	}
	for _, file := range files {
		path := filepath.Join(dir, file.name)
		if err := os.Chmod(path, 0644); err != nil {
			t.Fatal(err)
		}
		if err := file.load(); !errors.Is(err, ErrUnsafePath) {
			t.Fatalf("load %s permissive file error = %v", file.name, err)
		}
		assertMode(t, path, 0644)
		if err := os.Chmod(path, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadState(); !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("LoadState() permissive directory error = %v", err)
	}
	assertMode(t, dir, 0755)
}

func TestLockAcrossProcesses(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := privateTempDir(t)
	ready := filepath.Join(dir, "ready")
	release := filepath.Join(dir, "release")
	command := exec.Command(executable, "-test.run=^TestLockHelper$")
	command.Env = append(os.Environ(), "THREADBEAR_LOCK_HELPER=1", "THREADBEAR_LOCK_DIR="+dir, "THREADBEAR_LOCK_READY="+ready, "THREADBEAR_LOCK_RELEASE="+release)
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	finished := false
	t.Cleanup(func() {
		if finished {
			return
		}
		os.WriteFile(release, nil, 0600)
		command.Process.Kill()
		command.Wait()
	})
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("lock helper did not become ready")
		}
		time.Sleep(10 * time.Millisecond)
	}
	started := time.Now()
	lock, err := NewStore(dir).AcquireLock()
	if lock != nil {
		lock.Close()
	}
	if !errors.Is(err, ErrLocked) {
		t.Fatalf("cross-process AcquireLock() error = %v", err)
	}
	if elapsed := time.Since(started); elapsed > 250*time.Millisecond {
		t.Fatalf("non-blocking lock took %s", elapsed)
	}
	if err := os.WriteFile(release, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := command.Wait(); err != nil {
		t.Fatal(err)
	}
	finished = true
}

func TestLockHelper(t *testing.T) {
	if os.Getenv("THREADBEAR_LOCK_HELPER") != "1" {
		t.Skip("helper process only")
	}
	lock, err := NewStore(os.Getenv("THREADBEAR_LOCK_DIR")).AcquireLock()
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := os.WriteFile(os.Getenv("THREADBEAR_LOCK_READY"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(os.Getenv("THREADBEAR_LOCK_RELEASE")); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for release")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestAtomicWriteFailurePreservesDurableState(t *testing.T) {
	store := NewStore(privateTempDir(t))
	original := validState()
	if err := store.SaveState(original); err != nil {
		t.Fatal(err)
	}
	store.ops.rename = func(string, string) error { return errors.New("injected rename failure") }
	replacement := original
	replacement.Generation++
	if err := store.SaveState(replacement); err == nil {
		t.Fatal("SaveState() succeeded after injected interruption")
	}
	got, err := store.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if got.Generation != original.Generation {
		t.Fatalf("durable generation = %d, want %d", got.Generation, original.Generation)
	}
	matches, err := filepath.Glob(filepath.Join(store.dir, ".state.json.tmp-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary files remain: %v", matches)
	}
}

func TestFileSyncFailurePreservesDurableState(t *testing.T) {
	store := NewStore(privateTempDir(t))
	original := validState()
	if err := store.SaveState(original); err != nil {
		t.Fatal(err)
	}
	store.ops.syncFile = func(*os.File) error { return errors.New("injected sync failure") }
	replacement := original
	replacement.Generation++
	if err := store.SaveState(replacement); err == nil {
		t.Fatal("SaveState() succeeded after injected sync failure")
	}
	got, err := store.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if got.Generation != original.Generation {
		t.Fatalf("durable generation = %d, want %d", got.Generation, original.Generation)
	}
}

func TestNonBlockingLock(t *testing.T) {
	store := NewStore(privateTempDir(t))
	first, err := store.AcquireLock()
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := store.AcquireLock()
	if !errors.Is(err, ErrLocked) {
		if second != nil {
			second.Close()
		}
		t.Fatalf("second AcquireLock() error = %v, want %v", err, ErrLocked)
	}
	assertMode(t, filepath.Join(store.dir, lockFileName), 0600)
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	third, err := store.AcquireLock()
	if err != nil {
		t.Fatalf("AcquireLock() after release error = %v", err)
	}
	if err := third.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestLoadRejectsUnsupportedSchemas(t *testing.T) {
	dir := privateTempDir(t)
	store := NewStore(dir)
	for _, test := range []struct {
		name string
		file string
		data string
		load func() error
	}{
		{"old state", stateFileName, `{"schema_version":0}`, func() error { _, err := store.LoadState(); return err }},
		{"new state", stateFileName, `{"schema_version":2}`, func() error { _, err := store.LoadState(); return err }},
		{"new cycle", cycleFileName, `{"schema_version":2}`, func() error { _, err := store.LoadCycle(); return err }},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := os.WriteFile(filepath.Join(dir, test.file), []byte(test.data), 0600); err != nil {
				t.Fatal(err)
			}
			if err := test.load(); !errors.Is(err, ErrUnsupportedSchema) {
				t.Fatalf("load error = %v, want %v", err, ErrUnsupportedSchema)
			}
		})
	}
}

func TestInvalidStateNeverReplacesDestination(t *testing.T) {
	store := NewStore(privateTempDir(t))
	valid := validState()
	if err := store.SaveState(valid); err != nil {
		t.Fatal(err)
	}
	invalid := valid
	invalid.Tasks["task-1"] = TaskRecord{TaskID: "different"}
	if err := store.SaveState(invalid); err == nil {
		t.Fatal("SaveState() accepted invalid state")
	}
	got, err := store.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if got.Generation != valid.Generation {
		t.Fatalf("generation changed to %d", got.Generation)
	}
}

func TestStoreRejectsSymlinks(t *testing.T) {
	parent := t.TempDir()
	realDir := filepath.Join(parent, "real")
	if err := os.Mkdir(realDir, 0700); err != nil {
		t.Fatal(err)
	}
	linkedDir := filepath.Join(parent, "linked")
	if err := os.Symlink(realDir, linkedDir); err != nil {
		t.Fatal(err)
	}
	if err := NewStore(linkedDir).SaveState(validState()); !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("SaveState() error = %v, want %v", err, ErrUnsafePath)
	}

	store := NewStore(realDir)
	target := filepath.Join(parent, "target")
	if err := os.WriteFile(target, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(realDir, stateFileName)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadState(); !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("LoadState() error = %v, want %v", err, ErrUnsafePath)
	}
}

func TestStateAndCycleValidation(t *testing.T) {
	nullState := validState()
	nullState.Tasks = nil
	if err := nullState.Validate(); err == nil {
		t.Fatal("State.Validate() accepted null collections")
	}
	nullCycle := validCycle()
	nullCycle.Inventory = nil
	if err := nullCycle.Validate(); err == nil {
		t.Fatal("CycleCheckpoint.Validate() accepted null collections")
	}
	paddedArchive := validState()
	archive := paddedArchive.Archives["task-archived"]
	archive.CapturedRevision = " rev-a "
	paddedArchive.Archives["task-archived"] = archive
	if err := paddedArchive.Validate(); err == nil {
		t.Fatal("State.Validate() accepted a padded archive revision")
	}
	paddedCycle := validCycle()
	captured := paddedCycle.Inventory["task-1"]
	captured.Revision = " rev-1 "
	paddedCycle.Inventory["task-1"] = captured
	if err := paddedCycle.Validate(); err == nil {
		t.Fatal("CycleCheckpoint.Validate() accepted a padded revision")
	}

	retryState := validState()
	taskWithRetry := retryState.Tasks["task-1"]
	taskWithRetry.Retry = &Retry{Operation: " title ", ErrorCode: "stale_revision", Attempts: 1, LastAttemptAt: time.Now(), NextAttemptAt: time.Now().Add(time.Minute)}
	retryState.Tasks["task-1"] = taskWithRetry
	if err := retryState.Validate(); err == nil {
		t.Fatal("State.Validate() accepted an invalid retry code")
	}
	taskWithRetry.Retry = &Retry{Operation: "title", ErrorCode: "stale_revision", Attempts: 1}
	retryState.Tasks["task-1"] = taskWithRetry
	if err := retryState.Validate(); err == nil {
		t.Fatal("State.Validate() accepted retry without timestamps")
	}

	value := validState()
	value.Tasks["task-1"] = TaskRecord{
		TaskID:                  "task-1",
		CapturedRevision:        "rev-1",
		Status:                  "not-a-state",
		Provenance:              ProvenanceRuntime,
		StateStartedAt:          time.Now(),
		LastSubstantiveActivity: time.Now(),
	}
	if err := value.Validate(); err == nil {
		t.Fatal("State.Validate() accepted an invalid status")
	}

	cycle := validCycle()
	result := cycle.Results["task-1"]
	result.Revision = "stale"
	cycle.Results["task-1"] = result
	if err := cycle.Validate(); err == nil || !strings.Contains(err.Error(), "captured revision") {
		t.Fatalf("CycleCheckpoint.Validate() error = %v", err)
	}
}

func validState() State {
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	value := New()
	value.Generation = 7
	value.LastCompletedHeartbeat = &now
	value.Tasks["task-1"] = TaskRecord{
		TaskID:                  "task-1",
		CapturedRevision:        "rev-1",
		CapturedTitle:           "✅ Ship ThreadBear",
		Status:                  StatusComplete,
		Provenance:              ProvenanceFooter,
		StateStartedAt:          now.Add(-time.Hour),
		LastSubstantiveActivity: now.Add(-time.Hour),
		DurableSubject:          "Ship ThreadBear",
		LastAppliedTitle:        "✅ Ship ThreadBear",
	}
	value.Archives["task-archived"] = ArchiveRecord{
		TaskID:           "task-archived",
		ArchivedAt:       now,
		CapturedRevision: "rev-a",
		StateGeneration:  6,
	}
	value.DeliveredNoticeVersions = []string{"1.2.0"}
	return value
}

func validCycle() CycleCheckpoint {
	now := time.Date(2026, 7, 23, 12, 5, 0, 0, time.UTC)
	cycle := NewCycle("cycle-1", 7, now)
	cycle.Inventory["task-1"] = CapturedTask{
		TaskID:                  "task-1",
		Revision:                "rev-1",
		Title:                   "Ship ThreadBear",
		LastSubstantiveActivity: now.Add(-time.Hour),
	}
	cycle.Results["task-1"] = ClassificationResult{
		TaskID:         "task-1",
		Revision:       "rev-1",
		Status:         StatusComplete,
		Provenance:     ProvenanceFooter,
		DurableSubject: "Ship ThreadBear",
	}
	return cycle
}

func assertMode(t *testing.T, path string, want fs.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s mode = %04o, want %04o", path, got, want)
	}
}

func privateTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	return dir
}
