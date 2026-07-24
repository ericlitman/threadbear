package config_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ericlitman/threadbear/internal/app"
	"github.com/ericlitman/threadbear/internal/config"
	"github.com/ericlitman/threadbear/internal/output"
	"github.com/ericlitman/threadbear/internal/state"
)

func TestDefault(t *testing.T) {
	got := config.Default("control-123")
	want := config.Config{
		SchemaVersion:    config.CurrentSchemaVersion,
		ControlTaskID:    "control-123",
		HeartbeatSeconds: 300,
		ArchiveEnabled:   true,
		ArchiveAfterDays: 14,
		RenameEnabled:    true,
		AgentsEnabled:    true,
		ClassifierModel:  "gpt-5.6-luna",
		ClassifierEffort: config.EffortMedium,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Default() = %#v, want %#v", got, want)
	}
	if err := got.Validate(); err != nil {
		t.Fatalf("default config is invalid: %v", err)
	}
}

func TestConfigValidation(t *testing.T) {
	valid := config.Default("control-123")
	tests := []struct {
		name   string
		mutate func(*config.Config)
		target error
	}{
		{"old schema", func(c *config.Config) { c.SchemaVersion = 0 }, config.ErrUnsupportedSchema},
		{"new schema", func(c *config.Config) { c.SchemaVersion = 2 }, config.ErrUnsupportedSchema},
		{"missing control task", func(c *config.Config) { c.ControlTaskID = "" }, nil},
		{"padded control task", func(c *config.Config) { c.ControlTaskID = " control-123 " }, nil},
		{"zero heartbeat", func(c *config.Config) { c.HeartbeatSeconds = 0 }, nil},
		{"negative archive days", func(c *config.Config) { c.ArchiveAfterDays = -1 }, nil},
		{"missing model", func(c *config.Config) { c.ClassifierModel = "" }, nil},
		{"invalid effort", func(c *config.Config) { c.ClassifierEffort = "extreme" }, nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value := valid
			test.mutate(&value)
			err := value.Validate()
			if err == nil {
				t.Fatal("Validate() succeeded")
			}
			if test.target != nil && !errors.Is(err, test.target) {
				t.Fatalf("Validate() error = %v, want %v", err, test.target)
			}
		})
	}
}

func TestDecodeStrictConfig(t *testing.T) {
	valid := config.Default("control-123")
	valid.ArchiveEnabled = false
	valid.RenameEnabled = false
	valid.AgentsEnabled = false
	data, err := json.Marshal(valid)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := config.Decode(data)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if !reflect.DeepEqual(decoded, valid) {
		t.Fatalf("Decode() = %#v, want %#v", decoded, valid)
	}

	var fields map[string]any
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	delete(fields, "archive_enabled")
	missing, _ := json.Marshal(fields)
	if _, err := config.Decode(missing); err == nil {
		t.Fatal("Decode() accepted a missing boolean")
	}
	fields["archive_enabled"] = false
	fields["unexpected"] = true
	unknown, _ := json.Marshal(fields)
	if _, err := config.Decode(unknown); err == nil {
		t.Fatal("Decode() accepted an unknown field")
	}
	if _, err := config.Decode(append(data, []byte(` {}`)...)); err == nil {
		t.Fatal("Decode() accepted multiple JSON values")
	}
}

func TestDecodeRejectsUnsupportedSchema(t *testing.T) {
	for _, schema := range []int{0, 2} {
		data := []byte(`{"schema_version":` + string(rune('0'+schema)) + `}`)
		_, err := config.Decode(data)
		if !errors.Is(err, config.ErrUnsupportedSchema) {
			t.Fatalf("schema %d error = %v", schema, err)
		}
	}
}

func TestOutputSerialization(t *testing.T) {
	idle := output.HeartbeatResult{}
	var buffer bytes.Buffer
	if err := output.Write(&buffer, output.FormatJSON, idle); err != nil {
		t.Fatal(err)
	}
	if buffer.Len() != 0 {
		t.Fatalf("idle heartbeat wrote %q", buffer.String())
	}

	changed := output.HeartbeatResult{
		CycleID: "cycle-1",
		Changed: []output.TaskChange{
			{TaskID: "task-b", State: state.StatusComplete},
			{TaskID: "task-a", State: state.StatusRunning},
		},
		ArchivedIDs: []string{"task-z", "task-c"},
		Retries:     []output.RetryResult{{TaskID: "task-r", Operation: "title", ErrorCode: "stale_revision"}},
	}
	if err := output.Write(&buffer, output.FormatHuman, changed); err != nil {
		t.Fatal(err)
	}
	first := buffer.String()
	buffer.Reset()
	if err := output.Write(&buffer, output.FormatJSON, changed); err != nil {
		t.Fatal(err)
	}
	if first != buffer.String() {
		t.Fatalf("heartbeat human/json records differ:\n%s\n%s", first, buffer.String())
	}
	buffer.Reset()
	if err := output.Write(&buffer, output.FormatHuman, &changed); err != nil {
		t.Fatal(err)
	}
	if buffer.String() != first {
		t.Fatalf("pointer heartbeat bypassed normalization: %s", buffer.String())
	}
	if strings.Index(first, "task-a") > strings.Index(first, "task-b") {
		t.Fatalf("task changes are not deterministic: %s", first)
	}

	inspect := output.InspectResult{
		TaskID:           "task-a",
		CapturedRevision: "rev-1",
		State:            state.StatusNeedsInput,
		Provenance:       state.ProvenanceFooter,
		ManagedAction:    "choose the release region",
	}
	buffer.Reset()
	if err := output.Write(&buffer, output.FormatJSON, inspect); err != nil {
		t.Fatal(err)
	}
	serialized := buffer.String()
	for _, forbidden := range []string{"message_body", "hidden_reasoning", "classifier_payload", "environment"} {
		if strings.Contains(serialized, forbidden) {
			t.Fatalf("result exposed %q: %s", forbidden, serialized)
		}
	}
	if !strings.Contains(inspect.Human(), "task-a") || !strings.Contains(serialized, "task-a") {
		t.Fatal("human and JSON output do not describe the same task")
	}
	for _, fact := range []string{"rev-1", "footer", "choose the release region", "archive eligible false"} {
		if !strings.Contains(inspect.Human(), fact) {
			t.Fatalf("inspect human output omitted %q: %s", fact, inspect.Human())
		}
	}

	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	status := output.StatusResult{
		InstalledVersion:       "1.2.0",
		LaunchAgentHealthy:     true,
		LastCompletedHeartbeat: &now,
		ControlTaskID:          "control-123",
		Preferences: output.Preferences{
			HeartbeatSeconds: 300,
			ArchiveEnabled:   true,
			ArchiveAfterDays: 14,
			RenameEnabled:    true,
			AgentsEnabled:    true,
			ClassifierModel:  "gpt-5.6-luna",
			ClassifierEffort: "medium",
		},
		PendingRetries:  2,
		LastUpdateCheck: &now,
	}
	for _, fact := range []string{"2026-07-23T12:00:00Z", "300s", "archive true/14d", "rename true", "AGENTS true", "gpt-5.6-luna/medium", "retries 2"} {
		if !strings.Contains(status.Human(), fact) {
			t.Fatalf("status human output omitted %q: %s", fact, status.Human())
		}
	}

	buffer.Reset()
	err := output.Write(&buffer, output.FormatJSON, output.ErrorResult{Operation: "status", ErrorCode: "user message leaked"})
	if err == nil || buffer.Len() != 0 {
		t.Fatalf("unsafe machine code serialized: %q, %v", buffer.String(), err)
	}
	invalidPointer := &output.ErrorResult{Version: 2, Operation: "status", ErrorCode: "failed"}
	for _, invalid := range []output.Result{
		invalidPointer,
		output.ErrorResult{Version: 2, Operation: "status", ErrorCode: "failed"},
		output.ErrorResult{Operation: "---", ErrorCode: "failed"},
		output.SelfTestResult{OK: false, Checks: []output.CheckResult{{Name: "state", OK: false}}},
		output.SelfTestResult{OK: true, Checks: []output.CheckResult{{Name: "state", OK: true, ErrorCode: "failed"}}},
		output.SelfTestResult{OK: true, Checks: []output.CheckResult{{Name: "state", OK: false, ErrorCode: "failed"}}},
		output.StatusResult{},
		output.InspectResult{TaskID: "task-a", CapturedRevision: "rev-1", State: "invalid", Provenance: state.ProvenanceFooter},
		output.HeartbeatResult{CycleID: "cycle-1", Changed: []output.TaskChange{{TaskID: "task-a", State: "invalid"}}},
		output.VersionResult{},
	} {
		buffer.Reset()
		if err := output.Write(&buffer, output.FormatJSON, invalid); err == nil || buffer.Len() != 0 {
			t.Fatalf("invalid result serialized: %#v, %q, %v", invalid, buffer.String(), err)
		}
	}
}

func TestApplicationPreservesTypedFailureResult(t *testing.T) {
	want := output.HeartbeatResult{CycleID: "cycle-1", ErrorCode: "partial_failure"}
	service := app.NewWithHandlers(map[app.Command]app.Handler{
		app.CommandHeartbeat: func(context.Context, app.Request) (output.Result, error) {
			return want, errors.New("one task failed")
		},
	})
	got, err := service.Dispatch(context.Background(), app.Request{Command: app.CommandHeartbeat})
	if err == nil {
		t.Fatal("Dispatch() did not preserve handler failure")
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Dispatch() result = %#v, want %#v", got, want)
	}
}
