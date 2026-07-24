package app

import (
	"context"
	"errors"
	"testing"

	"github.com/ericlitman/threadbear/internal/install"
	"github.com/ericlitman/threadbear/internal/output"
)

func TestInstallHandlerMapsTypedFailureWithoutParsing(t *testing.T) {
	cause := errors.New("install the Codex CLI")
	handler := InstallHandler(func(bool) (install.Installer, func() error, error) {
		return install.Installer{}, func() error { return nil }, install.Fail("resolve_codex_executable", cause)
	})
	result, err := handler(context.Background(), Request{Command: CommandInstall})
	failure := result.(output.ErrorResult)
	if !errors.Is(err, cause) || failure.ErrorCode != "install_failed" || failure.Step != "resolve_codex_executable" || failure.Cause != cause.Error() {
		t.Fatalf("result=%+v err=%v", failure, err)
	}
	cancelled := installErrorResult(install.ErrCancelled)
	if cancelled.ErrorCode != "cancelled" || cancelled.Step != "" || cancelled.Cause != "" {
		t.Fatalf("cancelled=%+v", cancelled)
	}
}
