package install

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ericlitman/threadbear/internal/config"
	"github.com/ericlitman/threadbear/internal/state"
)

type lockedStore struct {
	Store
	err      error
	attempts int
}

func (s *lockedStore) AcquireLock() (Lock, error) {
	s.attempts++
	return nil, s.err
}

type observedStore struct {
	Store
	contended chan struct{}
	once      sync.Once
}

func (s *observedStore) AcquireLock() (Lock, error) {
	lock, err := s.Store.AcquireLock()
	if errors.Is(err, state.ErrLocked) {
		s.once.Do(func() { close(s.contended) })
	}
	return lock, err
}

type countingStore struct {
	Store
	locks int
}

func (s *countingStore) AcquireLock() (Lock, error) {
	s.locks++
	return s.Store.AcquireLock()
}

func installedFixture(t *testing.T) (Paths, *fakeStore, *fakeScheduler, *fakeTasks) {
	t.Helper()
	paths := PathsForHome(t.TempDir())
	cfg := config.Default("control-1")
	store := &fakeStore{config: cfg, state: state.New(), exists: true}
	scheduler := &fakeScheduler{}
	tasks := &fakeTasks{}
	if err := os.MkdirAll(filepath.Dir(paths.Binary), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.Binary, []byte("binary"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := WriteManagedBlock(paths.Agents, []byte("managed")); err != nil {
		t.Fatal(err)
	}
	if err := WriteManagedBlock(paths.Skill, []byte("skill")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(paths.StateDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	return paths, store, scheduler, tasks
}

func TestInteractiveUninstallDefaultsArchiveTaskAndDeleteState(t *testing.T) {
	paths, store, scheduler, tasks := installedFixture(t)
	prompt := &fakePrompter{confirmed: true, choices: []bool{true}}
	result, err := (Uninstaller{Paths: paths, Store: store, Scheduler: scheduler, ControlTasks: tasks, Prompter: prompt}).Uninstall(context.Background(), UninstallRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if !result.ArchivedControlTask || !result.DeletedState || len(tasks.archived) != 1 {
		t.Fatalf("result=%+v archived=%v", result, tasks.archived)
	}
	if _, err := os.Stat(paths.StateDirectory); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("state remains: %v", err)
	}
	if len(prompt.messages) != 1 || prompt.messages[0] != "Thanks for using ThreadBear. I'd love any feedback on why this wasn't for you. Drop me an email at eric@litman.org if you're open to sharing. Now, on to the uninstall!" {
		t.Fatalf("messages=%q", prompt.messages)
	}
	if !reflect.DeepEqual(prompt.choiceDefaults, []bool{true}) || !prompt.confirmDefault {
		t.Fatalf("choice defaults=%v confirm default=%t", prompt.choiceDefaults, prompt.confirmDefault)
	}
	wantEvents := []string{"message", "choose:Archive the ThreadBear control task", "preview", "confirm"}
	if !reflect.DeepEqual(prompt.events, wantEvents) {
		t.Fatalf("events=%v want=%v", prompt.events, wantEvents)
	}
	if prompt.previews != 1 || prompt.confirms != 1 {
		t.Fatalf("prompt=%+v", prompt)
	}
	if got := result.Preview.Lines[len(result.Preview.Lines)-1]; got != "persistent state "+paths.StateDirectory+": delete=true" {
		t.Fatalf("state preview=%q", got)
	}
}

func TestUninstallChoicesAndNoninteractiveConfirmation(t *testing.T) {
	paths, store, scheduler, tasks := installedFixture(t)
	u := Uninstaller{Paths: paths, Store: store, Scheduler: scheduler, ControlTasks: tasks}
	if _, err := u.Uninstall(context.Background(), UninstallRequest{NonInteractive: true}); err == nil {
		t.Fatal("missing confirmation accepted")
	}
	result, err := u.Uninstall(context.Background(), UninstallRequest{NonInteractive: true, Confirm: true, ArchiveControlTask: true})
	if err != nil {
		t.Fatal(err)
	}
	if !result.ArchivedControlTask || !result.DeletedState || len(tasks.archived) != 1 || tasks.archived[0] != "control-1" {
		t.Fatalf("result=%+v archived=%v", result, tasks.archived)
	}
	if _, err := os.Stat(paths.StateDirectory); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("state remains: %v", err)
	}
}

func TestUninstallCancellationMutatesNothing(t *testing.T) {
	paths, store, scheduler, tasks := installedFixture(t)
	prompt := &fakePrompter{confirmed: false, choices: []bool{false}}
	_, err := (Uninstaller{Paths: paths, Store: store, Scheduler: scheduler, ControlTasks: tasks, Prompter: prompt}).Uninstall(context.Background(), UninstallRequest{})
	if !errors.Is(err, ErrCancelled) {
		t.Fatalf("error=%v", err)
	}
	if len(scheduler.calls) != 0 || len(tasks.archived) != 0 {
		t.Fatalf("calls=%v archived=%v", scheduler.calls, tasks.archived)
	}
	if _, err := os.Stat(paths.Binary); err != nil {
		t.Fatalf("binary removed: %v", err)
	}
}

func TestUninstallRerunReportsUnchanged(t *testing.T) {
	paths, store, scheduler, tasks := installedFixture(t)
	u := Uninstaller{Paths: paths, Store: store, Scheduler: scheduler, ControlTasks: tasks}
	request := UninstallRequest{NonInteractive: true, Confirm: true}
	first, err := u.Uninstall(context.Background(), request)
	if err != nil || !first.Changed {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	second, err := u.Uninstall(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if second.Changed || len(second.Resources) != 0 {
		t.Fatalf("second=%+v", second)
	}
}

func TestInteractiveUninstallDefaultArchiveRerunReportsUnchanged(t *testing.T) {
	paths, _, scheduler, tasks := installedFixture(t)
	diskStore := NewDiskStore(paths)
	if err := diskStore.SaveConfig(config.Default("control-1")); err != nil {
		t.Fatal(err)
	}
	store := &countingStore{Store: diskStore}
	u := Uninstaller{Paths: paths, Store: store, Scheduler: scheduler, ControlTasks: tasks}
	firstPrompt := &fakePrompter{confirmed: true, choices: []bool{true}}
	u.Prompter = firstPrompt
	first, err := u.Uninstall(context.Background(), UninstallRequest{})
	if err != nil || !first.Changed {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	secondPrompt := &fakePrompter{confirmed: true, choices: []bool{true}}
	u.Prompter = secondPrompt
	second, err := u.Uninstall(context.Background(), UninstallRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if second.Changed || len(second.Resources) != 0 || len(tasks.archived) != 1 || store.locks != 1 {
		t.Fatalf("second=%+v archived=%v locks=%d", second, tasks.archived, store.locks)
	}
	if _, err := os.Stat(paths.StateDirectory); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("state recreated: %v", err)
	}
}

func TestUninstallRerunDoesNotRecreateState(t *testing.T) {
	paths, store, scheduler, tasks := installedFixture(t)
	u := Uninstaller{Paths: paths, Store: store, Scheduler: scheduler, ControlTasks: tasks}
	request := UninstallRequest{NonInteractive: true, Confirm: true}
	if _, err := u.Uninstall(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	result, err := u.Uninstall(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if result.Changed {
		t.Fatalf("rerun=%+v", result)
	}
	if _, err := os.Stat(paths.StateDirectory); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("state recreated: %v", err)
	}
}

func TestNoninteractiveUninstallPreviewsExactlyOnceBeforeMutation(t *testing.T) {
	paths, store, scheduler, tasks := installedFixture(t)
	previews := 0
	u := Uninstaller{Paths: paths, Store: store, Scheduler: scheduler, ControlTasks: tasks, Previewer: func(preview Preview) error {
		previews++
		if store.locks != 0 || len(scheduler.calls) != 0 || len(preview.Lines) != 6 {
			t.Fatalf("late or incomplete preview store=%+v calls=%v preview=%+v", store, scheduler.calls, preview)
		}
		return nil
	}}
	if _, err := u.Uninstall(context.Background(), UninstallRequest{NonInteractive: true, Confirm: true}); err != nil {
		t.Fatal(err)
	}
	if previews != 1 {
		t.Fatalf("previews=%d", previews)
	}
}

func TestInteractiveUninstallShowsPreviewExactlyOnce(t *testing.T) {
	paths, store, scheduler, tasks := installedFixture(t)
	prompt := &fakePrompter{confirmed: true, choices: []bool{false}}
	extra := 0
	u := Uninstaller{Paths: paths, Store: store, Scheduler: scheduler, ControlTasks: tasks, Prompter: prompt, Previewer: func(Preview) error { extra++; return nil }}
	if _, err := u.Uninstall(context.Background(), UninstallRequest{}); err != nil {
		t.Fatal(err)
	}
	if prompt.previews != 1 || extra != 0 {
		t.Fatalf("tty previews=%d fallback previews=%d", prompt.previews, extra)
	}
}

func TestRepeatedArchiveChoiceReportsUnchanged(t *testing.T) {
	paths, store, scheduler, tasks := installedFixture(t)
	u := Uninstaller{Paths: paths, Store: store, Scheduler: scheduler, ControlTasks: tasks}
	request := UninstallRequest{NonInteractive: true, Confirm: true, ArchiveControlTask: true}
	first, err := u.Uninstall(context.Background(), request)
	if err != nil || !first.ArchivedControlTask || !first.Changed {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	second, err := u.Uninstall(context.Background(), request)
	if err != nil || second.ArchivedControlTask || second.Changed {
		t.Fatalf("second=%+v err=%v", second, err)
	}
}

func TestUninstallReleasesLockBeforeDeletingState(t *testing.T) {
	paths, store, scheduler, tasks := installedFixture(t)
	stateExistedAtUnlock := false
	store.onLockClose = func() {
		_, err := os.Stat(paths.StateDirectory)
		stateExistedAtUnlock = err == nil
	}
	u := Uninstaller{Paths: paths, Store: store, Scheduler: scheduler, ControlTasks: tasks}
	if _, err := u.Uninstall(context.Background(), UninstallRequest{NonInteractive: true, Confirm: true}); err != nil {
		t.Fatal(err)
	}
	if !stateExistedAtUnlock {
		t.Fatal("state directory was deleted before the advisory lock was released")
	}
}

func TestUninstallLoadedJobWithoutArtifactsReportsChangedAndUnloads(t *testing.T) {
	paths := PathsForHome(t.TempDir())
	scheduler := &fakeScheduler{loaded: true}
	result, err := (Uninstaller{Paths: paths, Store: &fakeStore{}, Scheduler: scheduler, ControlTasks: &fakeTasks{}}).Uninstall(context.Background(), UninstallRequest{NonInteractive: true, Confirm: true})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || scheduler.loaded || !containsResource(result.Resources, "launchagent") {
		t.Fatalf("result=%+v scheduler=%+v", result, scheduler)
	}
	if !reflect.DeepEqual(scheduler.calls, []string{"loaded", "loaded", "remove"}) {
		t.Fatalf("calls=%v", scheduler.calls)
	}
}

func TestUninstallWaitsForRunningHeartbeatLaunchAgent(t *testing.T) {
	paths, _, scheduler, tasks := installedFixture(t)
	scheduler.loaded = true
	diskStore := NewDiskStore(paths)
	heartbeatLock, err := diskStore.AcquireLock()
	if err != nil {
		t.Fatal(err)
	}
	store := &observedStore{Store: diskStore, contended: make(chan struct{})}
	result := make(chan struct {
		value UninstallResult
		err   error
	}, 1)
	go func() {
		value, uninstallErr := (Uninstaller{Paths: paths, Store: store, Scheduler: scheduler, ControlTasks: tasks}).Uninstall(context.Background(), UninstallRequest{NonInteractive: true, Confirm: true})
		result <- struct {
			value UninstallResult
			err   error
		}{value: value, err: uninstallErr}
	}()
	select {
	case <-store.contended:
	case completed := <-result:
		t.Fatalf("uninstall returned while heartbeat held the lock: result=%+v err=%v", completed.value, completed.err)
	case <-time.After(2 * time.Second):
		t.Fatal("uninstall did not contend with the running heartbeat")
	}
	if err := heartbeatLock.Close(); err != nil {
		t.Fatal(err)
	}
	var completed struct {
		value UninstallResult
		err   error
	}
	select {
	case completed = <-result:
	case <-time.After(2 * time.Second):
		t.Fatal("uninstall did not finish after heartbeat released the lock")
	}
	if completed.err != nil || !completed.value.Changed || !completed.value.DeletedState || scheduler.loaded {
		t.Fatalf("result=%+v err=%v scheduler=%+v", completed.value, completed.err, scheduler)
	}
	if _, err := os.Stat(paths.StateDirectory); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("state remains: %v", err)
	}
	rerun, err := (Uninstaller{Paths: paths, Store: store, Scheduler: scheduler, ControlTasks: tasks}).Uninstall(context.Background(), UninstallRequest{NonInteractive: true, Confirm: true})
	if err != nil || rerun.Changed {
		t.Fatalf("rerun=%+v err=%v", rerun, err)
	}
	if _, err := os.Stat(paths.StateDirectory); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("rerun recreated state: %v", err)
	}
}

func TestAcquireUninstallLockReportsHeartbeatInFlight(t *testing.T) {
	store := &lockedStore{Store: &fakeStore{}, err: state.ErrLocked}
	_, err := acquireUninstallLock(context.Background(), store, 20*time.Millisecond, time.Millisecond)
	if !errors.Is(err, ErrHeartbeatInFlight) || !strings.Contains(err.Error(), "heartbeat in flight after waiting 20ms; rerunning uninstall is safe") {
		t.Fatalf("error=%v", err)
	}
	if store.attempts < 2 {
		t.Fatalf("attempts=%d", store.attempts)
	}
}

func TestAcquireUninstallLockHonorsCancellationAndOtherErrors(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	available := &lockedStore{Store: &fakeStore{}}
	if _, err := acquireUninstallLock(ctx, available, time.Second, time.Millisecond); !errors.Is(err, context.Canceled) || available.attempts != 0 {
		t.Fatalf("cancellation error=%v attempts=%d", err, available.attempts)
	}
	other := errors.New("lock file unavailable")
	failed := &lockedStore{Store: &fakeStore{}, err: other}
	if _, err := acquireUninstallLock(context.Background(), failed, time.Second, time.Millisecond); !errors.Is(err, other) || failed.attempts != 1 {
		t.Fatalf("error=%v attempts=%d", err, failed.attempts)
	}
}

func TestUninstallRechecksArtifactsUnderLock(t *testing.T) {
	paths := PathsForHome(t.TempDir())
	if err := os.MkdirAll(filepath.Dir(paths.Binary), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.Binary, []byte("binary"), 0o700); err != nil {
		t.Fatal(err)
	}
	store := &fakeStore{onAcquire: func() { _ = os.Remove(paths.Binary) }}
	scheduler := &fakeScheduler{}
	result, err := (Uninstaller{Paths: paths, Store: store, Scheduler: scheduler, ControlTasks: &fakeTasks{}}).Uninstall(context.Background(), UninstallRequest{NonInteractive: true, Confirm: true, ArchiveControlTask: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Changed || store.lockHeld || !reflect.DeepEqual(scheduler.calls, []string{"loaded", "loaded"}) {
		t.Fatalf("result=%+v store=%+v calls=%v", result, store, scheduler.calls)
	}
}
