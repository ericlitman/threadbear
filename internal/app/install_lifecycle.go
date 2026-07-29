package app

import (
	"context"
	"errors"

	"github.com/ericlitman/threadbear/internal/install"
	"github.com/ericlitman/threadbear/internal/output"
)

type InstallFactory func(bool) (install.Installer, func() error, error)

type UninstallFactory func(bool) (install.Uninstaller, func() error, error)

type SelfTestRunner interface {
	Run(context.Context, bool) output.SelfTestResult
}

func installErrorResult(err error) output.ErrorResult {
	result := output.ErrorResult{Operation: "install", ErrorCode: "install_failed"}
	if errors.Is(err, install.ErrCancelled) {
		result.ErrorCode = "cancelled"
		return result
	}
	if errors.Is(err, install.ErrControlTaskIDRequired) {
		result.ErrorCode = "control_task_id_required"
		result.Step = "select_control_task"
		result.Cause = err.Error()
		return result
	}
	var failure *install.InstallFailure
	if errors.As(err, &failure) {
		result.Step = failure.Step
		result.Cause = failure.Cause
	}
	return result
}

func InstallHandler(factory InstallFactory) Handler {
	return func(ctx context.Context, request Request) (output.Result, error) {
		if request.Command != CommandInstall {
			return commandError("install", "invalid_request", ErrInvalidRequest)
		}
		if factory == nil {
			return commandError("install", "dependency_unavailable", ErrUnavailable)
		}
		installer, closeInstaller, err := factory(!request.NonInteractive && !request.DryRun)
		if err != nil {
			var failure *install.InstallFailure
			if !errors.As(err, &failure) {
				err = install.Fail("initialize", err)
			}
			return installErrorResult(err), err
		}
		defer closeInstaller()
		result, err := installer.Install(ctx, install.InstallRequest{
			Patch: install.PreferencePatch{
				HeartbeatSeconds:             request.Configure.HeartbeatSeconds,
				ArchiveEnabled:               request.Configure.ArchiveEnabled,
				ArchiveAfterDays:             request.Configure.ArchiveAfterDays,
				RenameEnabled:                request.Configure.RenameEnabled,
				AutoUpdateEnabled:            request.Configure.AutoUpdateEnabled,
				TokenDisplay:                 request.Configure.TokenDisplay,
				AgentsEnabled:                request.Configure.AgentsEnabled,
				ClassifierModel:              request.Configure.ClassifierModel,
				ClassifierEffort:             request.Configure.ClassifierEffort,
				ClassifierContextBudgetBytes: request.Configure.ClassifierContextBudgetBytes,
			},
			ControlTaskID:  request.ControlTaskID,
			DryRun:         request.DryRun,
			NonInteractive: request.NonInteractive,
			Confirm:        request.Confirm,
		})
		if err != nil {
			return installErrorResult(err), err
		}
		if result.DryRun {
			return output.PreviewResult{Command: "install", Effects: result.Resources, Details: result.Preview.Lines, ControlTaskID: result.Config.ControlTaskID, SuppliedControlTaskID: result.SuppliedControlTaskID, ControlTaskDisposition: string(result.ControlTaskDisposition), WillUnarchiveControlTask: result.Unarchived}, nil
		}
		return output.LifecycleResult{
			Command: "install", Changed: result.Changed,
			Resources: result.Resources, ControlTaskID: result.Config.ControlTaskID,
			ControlTaskDisposition: string(result.ControlTaskDisposition), SuppliedControlTaskID: result.SuppliedControlTaskID,
			Unarchived: result.Unarchived, Reinstalled: result.Reinstalled, Warnings: result.Warnings,
		}, nil
	}
}

func uninstallErrorResult(err error) output.ErrorResult {
	result := output.ErrorResult{Operation: "uninstall", ErrorCode: "uninstall_failed"}
	if errors.Is(err, install.ErrCancelled) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		result.ErrorCode = "cancelled"
		return result
	}
	if errors.Is(err, install.ErrHeartbeatInFlight) {
		result.ErrorCode = "heartbeat_in_flight"
		result.Step = "wait_for_heartbeat"
		result.Cause = err.Error()
	}
	if errors.Is(err, install.ErrTitleCleanup) {
		result.ErrorCode = "title_cleanup_failed"
		result.Step = "clean_titles"
		result.Cause = err.Error()
	}
	return result
}

func UninstallHandler(factory UninstallFactory) Handler {
	return func(ctx context.Context, request Request) (output.Result, error) {
		if request.Command != CommandUninstall {
			return commandError("uninstall", "invalid_request", ErrInvalidRequest)
		}
		if factory == nil {
			return commandError("uninstall", "dependency_unavailable", ErrUnavailable)
		}
		uninstaller, closeUninstaller, err := factory(!request.NonInteractive)
		if err != nil {
			return commandError("uninstall", "dependency_unavailable", err)
		}
		defer closeUninstaller()
		result, err := uninstaller.Uninstall(ctx, install.UninstallRequest{
			NonInteractive: request.NonInteractive, Confirm: request.Confirm,
			ArchiveControlTask: request.ArchiveControlTask,
		})
		if err != nil {
			return uninstallErrorResult(err), err
		}
		return output.LifecycleResult{
			Command: "uninstall", Changed: result.Changed,
			Resources:           result.Resources,
			ArchivedControlTask: result.ArchivedControlTask, DeletedState: result.DeletedState, CleanedTitles: result.CleanedTitles,
		}, nil
	}
}

func SelfTestHandler(runner SelfTestRunner) Handler {
	return func(ctx context.Context, request Request) (output.Result, error) {
		if request.Command != CommandSelfTest {
			return commandError("self-test", "invalid_request", ErrInvalidRequest)
		}
		if runner == nil {
			return commandError("self-test", "dependency_unavailable", ErrUnavailable)
		}
		result := runner.Run(ctx, request.Candidate)
		if !result.OK {
			return result, errors.New("self-test failed")
		}
		return result, nil
	}
}
