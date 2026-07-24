package app

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ericlitman/threadbear/internal/output"
)

type managedAgentsFake struct {
	calls   int
	enabled bool
}

func (m *managedAgentsFake) Apply(enabled bool) (bool, error) {
	m.calls++
	changed := m.enabled != enabled
	m.enabled = enabled
	return changed, nil
}

func (m *managedAgentsFake) Preview(enabled bool) (string, error) {
	return fmt.Sprintf("AGENTS managed block enabled=%t", enabled), nil
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
