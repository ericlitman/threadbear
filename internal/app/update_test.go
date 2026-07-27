package app

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/ericlitman/threadbear/internal/output"
	"github.com/ericlitman/threadbear/internal/state"
	updatepkg "github.com/ericlitman/threadbear/internal/update"
)

type fakeUpdater struct {
	calls        int
	requested    string
	store        *state.Store
	observedLock bool
	err          error
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
	if u.err != nil {
		return updatepkg.Result{}, u.err
	}
	return updatepkg.Result{PreviousVersion: "1.1.0", InstalledVersion: "1.2.0", Changed: true, Resources: []string{"binary", "skill"}, Warnings: []string{"warning"}}, nil
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
	updated := result.(output.UpdateResult)
	if len(updated.Resources) != 2 || len(updated.Warnings) != 1 {
		t.Fatalf("update result=%+v", updated)
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

func TestUpdateManagedRefreshFailureHasStableRecoveryResult(t *testing.T) {
	store := state.NewStore(filepath.Join(t.TempDir(), "state"))
	underlying := errors.New("managed write failed")
	updater := &fakeUpdater{err: &updatepkg.ManagedRefreshError{Err: underlying}}
	result, err := UpdateHandler(store, updater)(context.Background(), Request{Command: CommandUpdate})
	if !errors.Is(err, underlying) {
		t.Fatalf("underlying error not preserved: %v", err)
	}
	failure := result.(output.ErrorResult)
	if failure.ErrorCode != "managed_refresh_failed" || failure.Step != "refresh_managed_surfaces" {
		t.Fatalf("failure=%+v", failure)
	}
	wantCause := "The new binary is installed. Managed surfaces will reconcile on the next heartbeat, or rerun threadbear update or threadbear configure."
	if failure.Cause != wantCause {
		t.Fatalf("cause=%q", failure.Cause)
	}
}

func TestUpdatePreRenameFailureRetainsGenericCode(t *testing.T) {
	store := state.NewStore(filepath.Join(t.TempDir(), "state"))
	updater := &fakeUpdater{err: errors.New("download failed")}
	result, err := UpdateHandler(store, updater)(context.Background(), Request{Command: CommandUpdate})
	if err == nil || result.(output.ErrorResult).ErrorCode != "update_failed" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}
