package app

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ericlitman/threadbear/internal/state"
	updatepkg "github.com/ericlitman/threadbear/internal/update"
)

type fakeUpdater struct {
	calls        int
	requested    string
	store        *state.Store
	observedLock bool
}

func (u *fakeUpdater) Update(_ context.Context, requested string) (updatepkg.Result, error) {
	u.calls++
	u.requested = requested
	if u.store != nil {
		lock, err := u.store.AcquireLock()
		if err != nil {
			u.observedLock = true
		} else {
			_ = lock.Close()
		}
	}
	return updatepkg.Result{PreviousVersion: "1.1.0", InstalledVersion: "1.2.0", Changed: true}, nil
}

func TestUpdateCommandIsRegisteredAndUsesSharedLock(t *testing.T) {
	store := state.NewStore(filepath.Join(t.TempDir(), "state"))
	updater := &fakeUpdater{store: store}
	service := NewWithOperatorCommands("1.1.0", OperatorDependencies{Store: store, Update: updater})
	lock, err := store.AcquireLock()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Dispatch(context.Background(), Request{Command: CommandUpdate}); err == nil {
		t.Fatal("held lock was ignored")
	}
	_ = lock.Close()
	result, err := service.Dispatch(context.Background(), Request{Command: CommandUpdate, Version: "1.2.0"})
	if err != nil || updater.calls != 1 || updater.requested != "1.2.0" || !updater.observedLock {
		t.Fatalf("result=%+v updater=%+v err=%v", result, updater, err)
	}
}

func TestUpdateVersionValidation(t *testing.T) {
	if err := (Request{Command: CommandUpdate, Version: "1.2.3"}).Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (Request{Command: CommandUpdate, Version: "v1.2.3"}).Validate(); err == nil {
		t.Fatal("leading v accepted")
	}
}
