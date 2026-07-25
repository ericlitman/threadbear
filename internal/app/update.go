package app

import (
	"context"

	"github.com/ericlitman/threadbear/internal/output"
	updatepkg "github.com/ericlitman/threadbear/internal/update"
)

type Updater interface {
	Update(context.Context, string) (updatepkg.Result, error)
}

func UpdateHandler(store OperatorStore, updater Updater) Handler {
	return func(ctx context.Context, request Request) (output.Result, error) {
		if request.Command != CommandUpdate {
			return commandError("update", "invalid_request", ErrInvalidRequest)
		}
		if store == nil || updater == nil {
			return commandError("update", "dependency_unavailable", ErrUnavailable)
		}
		lock, err := store.AcquireLock()
		if err != nil {
			return commandError("update", "update_locked", err)
		}
		defer lock.Close()
		result, err := updater.Update(ctx, request.Version)
		if err != nil {
			return commandError("update", "update_failed", err)
		}
		return output.UpdateResult{Changed: result.Changed, PreviousVersion: result.PreviousVersion, InstalledVersion: result.InstalledVersion}, nil
	}
}
