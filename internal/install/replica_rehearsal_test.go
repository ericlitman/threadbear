package install

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func replicaRehearsalFunction(t *testing.T, name string) string {
	t.Helper()
	script, err := os.ReadFile("../../scripts/replica-rehearsal.sh")
	if err != nil {
		t.Fatal(err)
	}
	source := string(script)
	start := strings.Index(source, name+"() {")
	if start < 0 {
		t.Fatalf("%s function is missing", name)
	}
	end := strings.Index(source[start:], "\n}\n")
	if end < 0 {
		t.Fatalf("%s function boundary is missing", name)
	}
	return source[start : start+end+2]
}

func runReplicaRehearsalFunction(t *testing.T, function, call string, arguments ...string) (string, error) {
	t.Helper()
	harness := "set -eu\n" + replicaRehearsalFunction(t, function) + "\n" + call + "\n"
	command := exec.Command("sh", append([]string{"-c", harness, "test"}, arguments...)...)
	output, err := command.CombinedOutput()
	return strings.TrimSpace(string(output)), err
}

func writeReplicaRehearsalJSON(t *testing.T, value any) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "input.json")
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func deterministicStatus(inventory, mechanical, luna int, overrides ...map[string]any) map[string]any {
	result := map[string]any{
		"last_completed_heartbeat": "2026-07-30T14:00:00Z",
		"pending_retries":          0,
		"first_sweep": map[string]any{
			"phase":                           "deterministic",
			"inventory_tasks":                 inventory,
			"changed_tasks":                   inventory,
			"latest_turn_reads":               inventory,
			"mechanically_resolved":           mechanical,
			"luna_candidates":                 luna,
			"first_pass_batches_total":        0,
			"first_pass_batches_completed":    0,
			"previous_pass_batches_total":     0,
			"previous_pass_batches_completed": 0,
			"model_duration_ms":               0,
			"mutation_duration_ms":            0,
			"retry_count":                     0,
			"rate_limit_count":                0,
			"started_at":                      "2026-07-30T13:59:55Z",
			"first_progress_at":               "2026-07-30T14:00:00Z",
			"updated_at":                      "2026-07-30T14:00:00Z",
		},
	}
	for _, values := range overrides {
		for key, value := range values {
			if key == "last_completed_heartbeat" || key == "pending_retries" {
				if value == nil {
					delete(result, key)
				} else {
					result[key] = value
				}
				continue
			}
			result["first_sweep"].(map[string]any)[key] = value
		}
	}
	return result
}

func TestReplicaRehearsalRestoresClassifierModeOnlyAfterCapture(t *testing.T) {
	fake := filepath.Join(t.TempDir(), "launchctl")
	if err := os.WriteFile(fake, []byte(`#!/bin/sh
printf '%s\n' "$*" >> "$TRACE_FILE"
`), 0700); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name     string
		setup    string
		expected string
	}{
		{name: "uncaptured pre-existing value", setup: "unset original_classifier_mode", expected: ""},
		{name: "captured empty value", setup: "original_classifier_mode=", expected: "unsetenv THREADBEAR_CLASSIFIER_MODE"},
		{name: "captured non-empty value", setup: "original_classifier_mode=bounded", expected: "setenv THREADBEAR_CLASSIFIER_MODE bounded"},
	} {
		t.Run(test.name, func(t *testing.T) {
			trace := filepath.Join(t.TempDir(), "trace")
			harness := "set -eu\n" + replicaRehearsalFunction(t, "restore_classifier_mode") + "\nTRACE_FILE=$1\nexport TRACE_FILE\n" + test.setup + "\nrestore_classifier_mode \"$2\"\n"
			command := exec.Command("sh", "-c", harness, "test", trace, fake)
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("restore harness: %v: %s", err, output)
			}
			data, err := os.ReadFile(trace)
			if errors.Is(err, os.ErrNotExist) {
				data = nil
			} else if err != nil {
				t.Fatal(err)
			}
			if got := strings.TrimSpace(string(data)); got != test.expected {
				t.Fatalf("launchctl call=%q, want %q", got, test.expected)
			}
		})
	}
}

func TestReplicaRehearsalFirstSweepGate(t *testing.T) {
	tests := []struct {
		name      string
		status    map[string]any
		benchmark string
		output    string
		ok        bool
	}{
		{name: "all deterministic handoff", status: deterministicStatus(213, 213, 0), benchmark: "0", output: "handoff", ok: true},
		{name: "mixed deterministic and Luna handoff", status: deterministicStatus(200, 67, 133), benchmark: "0", output: "handoff", ok: true},
		{name: "benchmark refuses handoff", status: deterministicStatus(213, 213, 0), benchmark: "1", output: "benchmark_handoff", ok: true},
		{name: "converged remains settled", status: map[string]any{"first_sweep": map[string]any{"phase": "converged"}}, benchmark: "0", output: "settled", ok: true},
		{name: "retryable remains settled", status: map[string]any{"first_sweep": map[string]any{"phase": "retryable"}}, benchmark: "0", output: "settled", ok: true},
		{name: "missing completed heartbeat", status: deterministicStatus(213, 213, 0, map[string]any{"last_completed_heartbeat": nil}), benchmark: "0"},
		{name: "inventory and reads differ", status: deterministicStatus(213, 213, 0, map[string]any{"latest_turn_reads": 212}), benchmark: "0"},
		{name: "classification counts differ", status: deterministicStatus(213, 213, 0, map[string]any{"mechanically_resolved": 212}), benchmark: "0"},
		{name: "semantic batch already planned", status: deterministicStatus(213, 213, 0, map[string]any{"first_pass_batches_total": 1}), benchmark: "0"},
		{name: "model work already ran", status: deterministicStatus(213, 213, 0, map[string]any{"model_duration_ms": 1}), benchmark: "0"},
		{name: "retry already recorded", status: deterministicStatus(213, 213, 0, map[string]any{"retry_count": 1}), benchmark: "0"},
		{name: "rate limit already recorded", status: deterministicStatus(213, 213, 0, map[string]any{"rate_limit_count": 1}), benchmark: "0"},
		{name: "pending retry remains", status: deterministicStatus(213, 213, 0, map[string]any{"pending_retries": 1}), benchmark: "0"},
		{name: "completion timestamp contradicts handoff", status: deterministicStatus(213, 213, 0, map[string]any{"completed_at": "2026-07-30T14:00:00Z"}), benchmark: "0"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := writeReplicaRehearsalJSON(t, test.status)
			output, err := runReplicaRehearsalFunction(t, "first_sweep_gate", `first_sweep_gate "$1" "$2"`, path, test.benchmark)
			if (err == nil) != test.ok {
				t.Fatalf("first_sweep_gate error=%v output=%q, want ok=%t", err, output, test.ok)
			}
			if output != test.output {
				t.Fatalf("first_sweep_gate output=%q, want %q", output, test.output)
			}
		})
	}
}

func TestReplicaRehearsalDeterministicHandoffRequiresIdleProcessAndRemovedCycle(t *testing.T) {
	fake := filepath.Join(t.TempDir(), "launchctl")
	if err := os.WriteFile(fake, []byte(`#!/bin/sh
case "${LAUNCH_STATE:-}" in
  idle) printf '%s\n' 'state = waiting' ;;
  running) printf '%s\n' 'state = running' 'pid = 42' ;;
  pid) printf '%s\n' 'pid = 42' ;;
  *) exit 1 ;;
esac
`), 0700); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name       string
		launch     string
		cycle      bool
		wantAccept bool
	}{
		{name: "idle and cycle removed", launch: "idle", wantAccept: true},
		{name: "running process", launch: "running"},
		{name: "pid still present", launch: "pid"},
		{name: "cycle remains", launch: "idle", cycle: true},
		{name: "service missing"},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			cycle := filepath.Join(directory, "cycle.json")
			if test.cycle {
				if err := os.WriteFile(cycle, []byte("{}"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			harness := "set -eu\n" + replicaRehearsalFunction(t, "deterministic_handoff_idle") + "\nLAUNCH_STATE=$1\nexport LAUNCH_STATE\ndeterministic_handoff_idle \"$2\" service \"$3\"\n"
			command := exec.Command("sh", "-c", harness, "test", test.launch, fake, cycle)
			output, err := command.CombinedOutput()
			if (err == nil) != test.wantAccept {
				t.Fatalf("deterministic_handoff_idle error=%v output=%q, want accept=%t", err, output, test.wantAccept)
			}
		})
	}
}

func TestReplicaRehearsalSweepTimingsUsePersistedProgressAndHandoffBoundary(t *testing.T) {
	status := deterministicStatus(213, 213, 0)
	path := writeReplicaRehearsalJSON(t, status)
	output, err := runReplicaRehearsalFunction(t, "sweep_timings", `sweep_timings "$1" handoff "$2"`, path, "2026-07-30T14:00:03Z")
	if err != nil {
		t.Fatalf("sweep_timings: %v: %s", err, output)
	}
	if output != "5.000 8.000 deferred" {
		t.Fatalf("sweep_timings output=%q, want persisted first progress and handoff boundary", output)
	}
}

func TestReplicaRehearsalTitleHandoffGate(t *testing.T) {
	operationIDs := func(count int) []string {
		ids := make([]string, count)
		for index := range ids {
			ids[index] = "operation-" + strconv.Itoa(index)
		}
		return ids
	}
	tests := []struct {
		name       string
		result     map[string]any
		mechanical int
		luna       int
		output     string
		ok         bool
	}{
		{name: "all deterministic plans", result: map[string]any{"ready": true, "retryable": false, "operation_ids": operationIDs(213)}, mechanical: 213, output: "213 false", ok: true},
		{name: "mixed plans before continuation", result: map[string]any{"ready": true, "retryable": false, "operation_ids": operationIDs(67)}, mechanical: 67, luna: 133, output: "67 false", ok: true},
		{name: "all ambiguous continuation", result: map[string]any{"ready": true, "retryable": false, "continuation_due": true}, luna: 200, output: "0 true", ok: true},
		{name: "missing deterministic plan", result: map[string]any{"ready": true, "retryable": false, "operation_ids": operationIDs(66)}, mechanical: 67, luna: 133},
		{name: "active heartbeat", result: map[string]any{"ready": false, "retryable": true, "error_code": "heartbeat_active"}, mechanical: 67, luna: 133},
		{name: "ready response still marked retryable", result: map[string]any{"ready": true, "retryable": true, "operation_ids": operationIDs(67)}, mechanical: 67, luna: 133},
		{name: "ready response has error code", result: map[string]any{"ready": true, "retryable": false, "error_code": "heartbeat_cycle_active", "operation_ids": operationIDs(67)}, mechanical: 67, luna: 133},
		{name: "continuation missing for all ambiguous", result: map[string]any{"ready": true, "retryable": false}, luna: 200},
		{name: "unexpected continuation with plans", result: map[string]any{"ready": true, "retryable": false, "continuation_due": true, "operation_ids": operationIDs(67)}, mechanical: 67, luna: 133},
		{name: "duplicate operation ID", result: map[string]any{"ready": true, "retryable": false, "operation_ids": []string{"same", "same"}}, mechanical: 2},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := writeReplicaRehearsalJSON(t, test.result)
			output, err := runReplicaRehearsalFunction(t, "title_handoff_gate", `title_handoff_gate "$1" "$2" "$3"`, path, strconv.Itoa(test.mechanical), strconv.Itoa(test.luna))
			if (err == nil) != test.ok {
				t.Fatalf("title_handoff_gate error=%v output=%q, want ok=%t", err, output, test.ok)
			}
			if output != test.output {
				t.Fatalf("title_handoff_gate output=%q, want %q", output, test.output)
			}
		})
	}
}
