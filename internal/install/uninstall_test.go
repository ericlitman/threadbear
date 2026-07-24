package install

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ericlitman/threadbear/internal/config"
	"github.com/ericlitman/threadbear/internal/state"
)

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

func TestUninstallDefaultsRetainStateAndTask(t *testing.T) {
	paths, store, scheduler, tasks := installedFixture(t)
	prompt := &fakePrompter{confirmed: true, choices: []bool{false, false}}
	result, err := (Uninstaller{Paths: paths, Store: store, Scheduler: scheduler, ControlTasks: tasks, Prompter: prompt}).Uninstall(context.Background(), UninstallRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if result.ArchivedControlTask || result.DeletedState || len(tasks.archived) != 0 {
		t.Fatalf("result=%+v archived=%v", result, tasks.archived)
	}
	if _, err := os.Stat(paths.StateDirectory); err != nil {
		t.Fatalf("state not retained: %v", err)
	}
	if _, err := os.Stat(paths.Binary); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("binary remains: %v", err)
	}
	if prompt.previews != 1 || prompt.confirms != 1 {
		t.Fatalf("prompt=%+v", prompt)
	}
}

func TestUninstallChoicesAndNoninteractiveConfirmation(t *testing.T) {
	paths, store, scheduler, tasks := installedFixture(t)
	u := Uninstaller{Paths: paths, Store: store, Scheduler: scheduler, ControlTasks: tasks}
	if _, err := u.Uninstall(context.Background(), UninstallRequest{NonInteractive: true}); err == nil {
		t.Fatal("missing confirmation accepted")
	}
	result, err := u.Uninstall(context.Background(), UninstallRequest{NonInteractive: true, Confirm: true, ArchiveControlTask: true, DeleteState: true})
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
	prompt := &fakePrompter{confirmed: false, choices: []bool{false, false}}
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

func TestDeleteStateUninstallRerunDoesNotRecreateState(t *testing.T) {
	paths, store, scheduler, tasks := installedFixture(t)
	u := Uninstaller{Paths: paths, Store: store, Scheduler: scheduler, ControlTasks: tasks}
	request := UninstallRequest{NonInteractive: true, Confirm: true, DeleteState: true}
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
	prompt := &fakePrompter{confirmed: true, choices: []bool{false, false}}
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
	if _, err := u.Uninstall(context.Background(), UninstallRequest{NonInteractive: true, Confirm: true, DeleteState: true}); err != nil {
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
	if !reflect.DeepEqual(scheduler.calls, []string{"loaded", "remove"}) {
		t.Fatalf("calls=%v", scheduler.calls)
	}
}
