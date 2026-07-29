package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
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

func TestUninstallErrorResultNamesHeartbeatInFlight(t *testing.T) {
	err := fmt.Errorf("%w after waiting 30s; rerunning uninstall is safe", install.ErrHeartbeatInFlight)
	result := uninstallErrorResult(err)
	if result.ErrorCode != "heartbeat_in_flight" || result.Step != "wait_for_heartbeat" || result.Cause != err.Error() || !strings.Contains(result.Human(), "heartbeat in flight") || strings.Contains(result.Human(), "uninstall_failed") {
		t.Fatalf("result=%+v human=%q", result, result.Human())
	}
}

func TestUninstallErrorResultKeepsContextCancellationDistinct(t *testing.T) {
	cancelled := uninstallErrorResult(context.Canceled)
	if cancelled.ErrorCode != "cancelled" {
		t.Fatalf("cancelled=%+v", cancelled)
	}
	deadline := uninstallErrorResult(context.DeadlineExceeded)
	if deadline.ErrorCode != "cancelled" {
		t.Fatalf("deadline=%+v", deadline)
	}
}

func TestInstallErrorResultGuidesMissingControlTaskID(t *testing.T) {
	result := installErrorResult(install.Fail("control_task_id_required", install.ErrControlTaskIDRequired))
	if result.ErrorCode != "control_task_id_required" || result.Step != "select_control_task" || !strings.Contains(result.Cause, "--control-task-id") || !strings.Contains(result.Cause, "INSTALL.md") {
		t.Fatalf("result=%+v", result)
	}
}

func TestUninstallErrorResultNamesRetriableTitleCleanupFailure(t *testing.T) {
	err := fmt.Errorf("%w: after cleaning 1 title(s), task task-b drifted", install.ErrTitleCleanup)
	result := uninstallErrorResult(err)
	if result.ErrorCode != "title_cleanup_failed" || result.Step != "clean_titles" || result.Cause != err.Error() {
		t.Fatalf("result=%+v", result)
	}
}
