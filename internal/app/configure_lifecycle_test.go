package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ericlitman/threadbear/internal/config"
	"github.com/ericlitman/threadbear/internal/output"
)

type managedAgentsFake struct {
	calls        int
	enabled      bool
	stale        bool
	resource     string
	restoreCalls int
	restoreErr   error
	applyErr     error
	applyMutates bool
}

func (m *managedAgentsFake) Apply(enabled bool) (bool, error) {
	m.calls++
	if m.applyErr != nil {
		if m.applyMutates {
			m.enabled = enabled
			m.stale = false
		}
		return false, m.applyErr
	}
	changed := m.enabled != enabled || m.stale
	m.enabled = enabled
	m.stale = false
	return changed, nil
}

func (m *managedAgentsFake) Preview(enabled bool) (ManagedAgentsPreview, error) {
	preview := ManagedAgentsPreview{
		Detail:  fmt.Sprintf("AGENTS managed block enabled=%t", enabled),
		Changed: m.enabled != enabled || m.stale,
	}
	if preview.Changed && m.resource != "" {
		preview.Resources = []string{m.resource}
	}
	return preview, nil
}

func (m *managedAgentsFake) Snapshot() (any, error) {
	return struct {
		enabled bool
		stale   bool
	}{enabled: m.enabled, stale: m.stale}, nil
}

func (m *managedAgentsFake) Restore(value any) error {
	m.restoreCalls++
	snapshot := value.(struct {
		enabled bool
		stale   bool
	})
	m.enabled = snapshot.enabled
	m.stale = snapshot.stale
	return m.restoreErr
}

func TestConfigurePreviewConfirmationAndManagedAgents(t *testing.T) {
	store := commandStore(t, time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC))
	disabled := false
	managed := &managedAgentsFake{enabled: true}
	request := Request{Command: CommandConfigure, DryRun: true, Configure: ConfigPatch{AgentsEnabled: &disabled}}
	previewCalls := 0
	previewWriter := func(output.PreviewResult) error { previewCalls++; return nil }
	result, err := ConfigureHandler(store, nil, previewWriter, nil, managed)(context.Background(), request)
	if err != nil || managed.calls != 0 || previewCalls != 0 {
		t.Fatalf("result=%+v err=%v managed=%+v previewCalls=%d", result, err, managed, previewCalls)
	}
	preview := result.(output.PreviewResult)
	if len(preview.Effects) != 2 || len(preview.Details) < 2 {
		t.Fatalf("preview=%+v", preview)
	}
	request.DryRun = false
	result, err = ConfigureHandler(store, nil, previewWriter, nil, managed)(context.Background(), request)
	if err == nil || result.(output.ErrorResult).ErrorCode != "confirmation_required" || managed.calls != 0 {
		t.Fatalf("result=%+v err=%v managed=%+v", result, err, managed)
	}
	request.Confirm = true
	result, err = ConfigureHandler(store, nil, previewWriter, nil, managed)(context.Background(), request)
	action := result.(output.ActionResult)
	if err != nil || !action.Changed || managed.calls != 1 || managed.enabled || previewCalls != 2 || len(action.Preview) != 0 {
		t.Fatalf("result=%+v err=%v managed=%+v previewCalls=%d", result, err, managed, previewCalls)
	}
	result, err = ConfigureHandler(store, nil, nil, nil, managed)(context.Background(), request)
	if err != nil || result.(output.ActionResult).Changed || managed.calls != 1 {
		t.Fatalf("result=%+v err=%v managed=%+v", result, err, managed)
	}
}

func TestConfigurePreviewsAndConfirmsInOneInvocation(t *testing.T) {
	store := commandStore(t, time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC))
	disabled := false
	managed := &managedAgentsFake{enabled: true}
	previewed := 0
	confirmed := false
	preview := func(result output.PreviewResult) error {
		previewed++
		current, err := store.LoadConfig()
		if err != nil || !current.AgentsEnabled || len(result.Details) < 2 {
			t.Fatalf("preview=%+v current=%+v err=%v", result, current, err)
		}
		return nil
	}
	confirm := func() (bool, error) {
		confirmed = true
		current, err := store.LoadConfig()
		if err != nil || !current.AgentsEnabled || previewed != 1 {
			t.Fatalf("confirmation order current=%+v previewed=%d err=%v", current, previewed, err)
		}
		return true, nil
	}
	result, err := ConfigureHandler(store, nil, preview, confirm, managed)(context.Background(), Request{Command: CommandConfigure, Configure: ConfigPatch{AgentsEnabled: &disabled}})
	if err != nil || previewed != 1 || !confirmed || !result.(output.ActionResult).Changed {
		t.Fatalf("result=%+v err=%v previewed=%d confirmed=%t", result, err, previewed, confirmed)
	}
}

func TestNoninteractiveConfigurePreviewsWithoutPrompting(t *testing.T) {
	store := commandStore(t, time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC))
	disabled := false
	managed := &managedAgentsFake{enabled: true}
	previews := 0
	prompts := 0
	result, err := ConfigureHandler(store, nil, func(output.PreviewResult) error { previews++; return nil }, func() (bool, error) { prompts++; return true, nil }, managed)(context.Background(), Request{Command: CommandConfigure, NonInteractive: true, Configure: ConfigPatch{AgentsEnabled: &disabled}})
	if err == nil || result.(output.ErrorResult).ErrorCode != "confirmation_required" || previews != 1 || prompts != 0 || managed.calls != 0 {
		t.Fatalf("result=%+v err=%v previews=%d prompts=%d managed=%+v", result, err, previews, prompts, managed)
	}
}

func TestConfigureRefreshesStaleManagedAgentsWhenConfigIsUnchanged(t *testing.T) {
	store := commandStore(t, time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC))
	managed := &managedAgentsFake{enabled: true, stale: true}
	request := Request{Command: CommandConfigure, NonInteractive: true, Confirm: true}

	dryRun := request
	dryRun.DryRun = true
	result, err := ConfigureHandler(store, nil, nil, nil, managed)(context.Background(), dryRun)
	preview := result.(output.PreviewResult)
	if err != nil || managed.calls != 0 || len(preview.Effects) != 1 || preview.Effects[0] != "agents" {
		t.Fatalf("preview=%+v err=%v managed=%+v", preview, err, managed)
	}

	result, err = ConfigureHandler(store, nil, nil, nil, managed)(context.Background(), request)
	action := result.(output.ActionResult)
	if err != nil || !action.Changed || managed.calls != 1 || managed.stale {
		t.Fatalf("result=%+v err=%v managed=%+v", result, err, managed)
	}

	result, err = ConfigureHandler(store, nil, nil, nil, managed)(context.Background(), request)
	action = result.(output.ActionResult)
	if err != nil || action.Changed || managed.calls != 1 {
		t.Fatalf("idempotent result=%+v err=%v managed=%+v", result, err, managed)
	}
}

func TestConfigureRefreshesStaleSkillWithUnchangedPreference(t *testing.T) {
	store := commandStore(t, time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC))
	managed := &managedAgentsFake{enabled: true, stale: true, resource: "skill"}
	request := Request{Command: CommandConfigure, NonInteractive: true, Confirm: true}
	previewRequest := request
	previewRequest.DryRun = true
	result, err := ConfigureHandler(store, nil, nil, nil, managed)(context.Background(), previewRequest)
	if err != nil {
		t.Fatal(err)
	}
	preview := result.(output.PreviewResult)
	if len(preview.Effects) != 1 || preview.Effects[0] != "skill" {
		t.Fatalf("preview=%+v", preview)
	}
	result, err = ConfigureHandler(store, nil, nil, nil, managed)(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	action := result.(output.ActionResult)
	if !action.Changed || len(action.ResourceIDs) != 1 || action.ResourceIDs[0] != "skill" {
		t.Fatalf("action=%+v", action)
	}
}

func TestConfigureRestoresManagedSurfacesWhenConfigWriteFails(t *testing.T) {
	base := commandStore(t, time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC))
	store := &failingCommandStore{Store: base, saveConfigErr: fmt.Errorf("synthetic config failure")}
	disabled := false
	managed := &managedAgentsFake{enabled: true, stale: true, resource: "skill"}
	result, err := ConfigureHandler(store, nil, nil, nil, managed)(context.Background(), Request{Command: CommandConfigure, NonInteractive: true, Confirm: true, Configure: ConfigPatch{AgentsEnabled: &disabled}})
	if err == nil || result.(output.ErrorResult).ErrorCode != "config_write_failed" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if managed.calls != 1 || managed.restoreCalls != 1 || !managed.enabled || !managed.stale {
		t.Fatalf("managed=%+v", managed)
	}
}

func TestConfigureManagedApplyFailureReportsSchedulerRollbackFailure(t *testing.T) {
	store := commandStore(t, time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC))
	managed := &managedAgentsFake{enabled: true, stale: true, applyErr: errors.New("managed apply failed"), applyMutates: true, restoreErr: errors.New("managed rollback failed")}
	launch := &commandLaunchAgent{applyErrs: []error{nil, errors.New("scheduler rollback failed")}}
	seconds := 60
	result, err := ConfigureHandler(store, launch, nil, nil, managed)(context.Background(), Request{Command: CommandConfigure, NonInteractive: true, Confirm: true, Configure: ConfigPatch{HeartbeatSeconds: &seconds}})
	if err == nil || result.(output.ErrorResult).ErrorCode != "launch_agent_rollback_failed" || launch.applyCalls != 2 || managed.restoreCalls != 1 {
		t.Fatalf("result=%+v err=%v launch=%+v managed=%+v", result, err, launch, managed)
	}
	for _, message := range []string{"managed apply failed", "managed rollback failed", "scheduler rollback failed"} {
		if !strings.Contains(err.Error(), message) {
			t.Fatalf("joined err missing %q: %v", message, err)
		}
	}
}

func TestConfigureManagedApplyFailureRestoresPreApplySnapshot(t *testing.T) {
	store := commandStore(t, time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC))
	disabled := false
	managed := &managedAgentsFake{enabled: true, stale: true, applyErr: errors.New("managed apply failed"), applyMutates: true}
	result, err := ConfigureHandler(store, nil, nil, nil, managed)(context.Background(), Request{Command: CommandConfigure, NonInteractive: true, Confirm: true, Configure: ConfigPatch{AgentsEnabled: &disabled}})
	if err == nil || result.(output.ErrorResult).ErrorCode != "agents_apply_failed" || managed.restoreCalls != 1 {
		t.Fatalf("result=%+v err=%v managed=%+v", result, err, managed)
	}
	if !managed.enabled || !managed.stale {
		t.Fatalf("managed snapshot was not restored: %+v", managed)
	}
}

func TestConfigureManagedApplyFailureReportsManagedRollbackFailure(t *testing.T) {
	store := commandStore(t, time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC))
	disabled := false
	managed := &managedAgentsFake{enabled: true, stale: true, applyErr: errors.New("managed apply failed"), restoreErr: errors.New("managed rollback failed")}
	result, err := ConfigureHandler(store, nil, nil, nil, managed)(context.Background(), Request{Command: CommandConfigure, NonInteractive: true, Confirm: true, Configure: ConfigPatch{AgentsEnabled: &disabled}})
	if err == nil || result.(output.ErrorResult).ErrorCode != "managed_rollback_failed" || managed.restoreCalls != 1 {
		t.Fatalf("result=%+v err=%v managed=%+v", result, err, managed)
	}
	if !strings.Contains(err.Error(), "managed apply failed") || !strings.Contains(err.Error(), "managed rollback failed") {
		t.Fatalf("joined err=%v", err)
	}
}

func TestConfigureConfigSaveAttemptsAndJoinsBothRollbacks(t *testing.T) {
	base := commandStore(t, time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC))
	store := &failingCommandStore{Store: base, saveConfigErr: errors.New("config save failed")}
	managed := &managedAgentsFake{enabled: true, stale: true, restoreErr: errors.New("managed rollback failed")}
	launch := &commandLaunchAgent{applyErrs: []error{nil, errors.New("scheduler rollback failed")}}
	seconds := 60
	result, err := ConfigureHandler(store, launch, nil, nil, managed)(context.Background(), Request{Command: CommandConfigure, NonInteractive: true, Confirm: true, Configure: ConfigPatch{HeartbeatSeconds: &seconds}})
	if err == nil || result.(output.ErrorResult).ErrorCode != "launch_agent_rollback_failed" || launch.applyCalls != 2 || managed.restoreCalls != 1 {
		t.Fatalf("result=%+v err=%v launch=%+v managed=%+v", result, err, launch, managed)
	}
	for _, message := range []string{"config save failed", "managed rollback failed", "scheduler rollback failed"} {
		if !strings.Contains(err.Error(), message) {
			t.Fatalf("joined err missing %q: %v", message, err)
		}
	}
}

func TestConfigureConfigSaveReportsManagedRollbackFailure(t *testing.T) {
	base := commandStore(t, time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC))
	store := &failingCommandStore{Store: base, saveConfigErr: errors.New("config save failed")}
	managed := &managedAgentsFake{enabled: true, stale: true, restoreErr: errors.New("managed rollback failed")}
	disabled := false
	result, err := ConfigureHandler(store, nil, nil, nil, managed)(context.Background(), Request{Command: CommandConfigure, NonInteractive: true, Confirm: true, Configure: ConfigPatch{AgentsEnabled: &disabled}})
	if err == nil || result.(output.ErrorResult).ErrorCode != "managed_rollback_failed" || managed.restoreCalls != 1 {
		t.Fatalf("result=%+v err=%v managed=%+v", result, err, managed)
	}
	if !strings.Contains(err.Error(), "config save failed") || !strings.Contains(err.Error(), "managed rollback failed") {
		t.Fatalf("joined err=%v", err)
	}
}

func TestApplyConfigPatchAutoUpdate(t *testing.T) {
	value := config.Default("control")
	disabled := false
	got := applyConfigPatch(value, ConfigPatch{AutoUpdateEnabled: &disabled})
	if got.AutoUpdateEnabled || value.AutoUpdateEnabled == got.AutoUpdateEnabled {
		t.Fatalf("before=%+v after=%+v", value, got)
	}
}
