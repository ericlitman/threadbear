package output

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/ericlitman/threadbear/internal/state"
)

func TestLifecycleResultHumanJSONParity(t *testing.T) {
	result := LifecycleResult{Command: "install", Changed: true, Resources: []string{"state", "binary"}, ControlTaskID: "control-1", ControlTaskDisposition: "adopted"}
	var human, machine bytes.Buffer
	if err := Write(&human, FormatHuman, result); err != nil {
		t.Fatal(err)
	}
	if err := Write(&machine, FormatJSON, result); err != nil {
		t.Fatal(err)
	}
	var decoded LifecycleResult
	if err := json.Unmarshal(machine.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	for _, fact := range []string{"install", "control-1=adopted", "binary,state"} {
		if !strings.Contains(human.String(), fact) {
			t.Fatalf("human output missing %q: %q", fact, human.String())
		}
	}
	if decoded.Command != result.Command || !decoded.Changed || decoded.ControlTaskID != result.ControlTaskID || decoded.ControlTaskDisposition != "adopted" || len(decoded.Resources) != 2 || !strings.Contains(machine.String(), `"migrated":false`) {
		t.Fatalf("decoded=%+v", decoded)
	}
}

func TestUninstallLifecycleResultHasFriendlyHumanAndStableJSON(t *testing.T) {
	result := LifecycleResult{Command: "uninstall", Changed: true, Resources: []string{"binary"}, DeletedState: true}
	var human, machine bytes.Buffer
	if err := Write(&human, FormatHuman, result); err != nil {
		t.Fatal(err)
	}
	if err := Write(&machine, FormatJSON, result); err != nil {
		t.Fatal(err)
	}
	if human.String() != "ThreadBear is uninstalled. Take care out there.\n" {
		t.Fatalf("human=%q", human.String())
	}
	var unchanged bytes.Buffer
	if err := Write(&unchanged, FormatHuman, LifecycleResult{Command: "uninstall"}); err != nil {
		t.Fatal(err)
	}
	if unchanged.String() != human.String() {
		t.Fatalf("unchanged human=%q", unchanged.String())
	}
	want := `{"version":1,"command":"uninstall","changed":true,"resources":["binary"],"control_task_id":"","migrated":false,"reinstalled":false,"unarchived":false,"archived_control_task":false,"deleted_state":true,"preview":[]}` + "\n"
	if machine.String() != want {
		t.Fatalf("json=%q", machine.String())
	}
}

func TestHeartbeatManagedMutationHumanJSONParity(t *testing.T) {
	result := HeartbeatResult{CycleID: "managed-surfaces", ManagedResources: []string{"skill", "agents"}}
	if result.Empty() {
		t.Fatal("managed repair was empty")
	}
	var human, machine bytes.Buffer
	if err := Write(&human, FormatHuman, result); err != nil {
		t.Fatal(err)
	}
	if err := Write(&machine, FormatJSON, result); err != nil {
		t.Fatal(err)
	}
	if human.String() != machine.String() || !strings.Contains(machine.String(), `"managed_resources":["agents","skill"]`) {
		t.Fatalf("human=%q json=%q", human.String(), machine.String())
	}
}

func TestPreviewDetailsHumanJSONContract(t *testing.T) {
	result := PreviewResult{Command: "install", Effects: []string{"binary"}, Details: []string{"AGENTS.md: write managed block", "LaunchAgent staged disabled"}, WillUnarchiveControlTask: true}
	var human, machine bytes.Buffer
	if err := Write(&human, FormatHuman, result); err != nil {
		t.Fatal(err)
	}
	if err := Write(&machine, FormatJSON, result); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(human.String(), "write managed block") || !strings.Contains(human.String(), "staged disabled") || !strings.Contains(human.String(), "will be unarchived") {
		t.Fatalf("human=%q", human.String())
	}
	var decoded PreviewResult
	if err := json.Unmarshal(machine.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Details) != 2 || decoded.Command != "install" || !decoded.WillUnarchiveControlTask || strings.Contains(machine.String(), `"unarchived"`) {
		t.Fatalf("decoded=%+v", decoded)
	}
}

func TestMutationPreviewAndJSONResultUseSeparateStreams(t *testing.T) {
	preview := PreviewResult{Command: "uninstall", Effects: []string{"binary"}, Details: []string{"remove binary"}}
	result := LifecycleResult{Command: "uninstall", Changed: true, Resources: []string{"binary"}}
	var stdout, stderr bytes.Buffer
	if err := Write(&stderr, FormatJSON, preview); err != nil {
		t.Fatal(err)
	}
	if err := Write(&stdout, FormatJSON, result); err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(&stdout)
	var decoded LifecycleResult
	if err := decoder.Decode(&decoded); err != nil {
		t.Fatal(err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		t.Fatalf("stdout did not contain exactly one JSON object: %q err=%v", stdout.String(), err)
	}
	if strings.Contains(stdout.String(), "remove binary") || strings.Count(stderr.String(), "remove binary") != 1 {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestFinalActionDoesNotRepeatPreview(t *testing.T) {
	result := ActionResult{Command: "configure", Changed: true, ResourceIDs: []string{"config"}}
	var human bytes.Buffer
	if err := Write(&human, FormatHuman, result); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(human.String(), "preview") {
		t.Fatalf("final action repeated preview: %q", human.String())
	}
}

func TestNotInstalledErrorHumanJSONContract(t *testing.T) {
	result := ErrorResult{Operation: "status", ErrorCode: "not_installed"}
	var human, machine bytes.Buffer
	if err := Write(&human, FormatHuman, result); err != nil {
		t.Fatal(err)
	}
	if err := Write(&machine, FormatJSON, result); err != nil {
		t.Fatal(err)
	}
	if human.String() != "ThreadBear is not installed\n" {
		t.Fatalf("human=%q", human.String())
	}
	if machine.String() != `{"version":1,"operation":"status","error_code":"not_installed"}`+"\n" {
		t.Fatalf("json=%q", machine.String())
	}
}

func TestInstallErrorStepCauseHumanJSONParity(t *testing.T) {
	result := ErrorResult{Operation: "install", ErrorCode: "install_failed", Step: "resolve_codex_executable", Cause: "install the Codex CLI"}
	var human, machine bytes.Buffer
	if err := Write(&human, FormatHuman, result); err != nil {
		t.Fatal(err)
	}
	if err := Write(&machine, FormatJSON, result); err != nil {
		t.Fatal(err)
	}
	for _, fact := range []string{result.Step, result.Cause} {
		if !strings.Contains(human.String(), fact) || !strings.Contains(machine.String(), fact) {
			t.Fatalf("missing %q human=%q json=%q", fact, human.String(), machine.String())
		}
	}
	for _, invalid := range []ErrorResult{
		{Operation: "install", ErrorCode: "install_failed", Step: "resolve_codex_executable"},
		{Operation: "install", ErrorCode: "install_failed", Cause: "missing"},
		{Operation: "install", ErrorCode: "install_failed", Step: "Not Stable", Cause: "missing"},
	} {
		var buffer bytes.Buffer
		if err := Write(&buffer, FormatJSON, invalid); err == nil {
			t.Fatalf("accepted %+v", invalid)
		}
	}
}

func TestUpdateResultHumanJSONParity(t *testing.T) {
	result := UpdateResult{Changed: true, PreviousVersion: "1.1.0", InstalledVersion: "1.2.0"}
	var human bytes.Buffer
	if err := Write(&human, FormatHuman, result); err != nil {
		t.Fatal(err)
	}
	if human.String() != "ThreadBear updated 1.1.0 → 1.2.0\n" {
		t.Fatalf("human=%q", human.String())
	}
	var encoded bytes.Buffer
	if err := Write(&encoded, FormatJSON, result); err != nil {
		t.Fatal(err)
	}
	if encoded.String() != `{"version":1,"changed":true,"previous_version":"1.1.0","installed_version":"1.2.0"}`+"\n" {
		t.Fatalf("json=%q", encoded.String())
	}
}

func TestSelfTestManagedFailureIncludesActionableRemedy(t *testing.T) {
	result := SelfTestResult{OK: false, Checks: []CheckResult{{Name: "agents", OK: false, ErrorCode: "managed_surface_stale", Remedy: "run threadbear update or threadbear configure"}}}
	var human bytes.Buffer
	if err := Write(&human, FormatHuman, result); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(human.String(), "threadbear update or threadbear configure") {
		t.Fatalf("human=%q", human.String())
	}
	var encoded bytes.Buffer
	if err := Write(&encoded, FormatJSON, result); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(encoded.String(), `"remedy":"run threadbear update or threadbear configure"`) {
		t.Fatalf("json=%q", encoded.String())
	}
}

func TestOrdinaryHeartbeatJSONRetainsPriorShape(t *testing.T) {
	result := HeartbeatResult{CycleID: "cycle-1", Changed: []TaskChange{{TaskID: "task-1", State: state.StatusComplete}}}
	var encoded bytes.Buffer
	if err := Write(&encoded, FormatJSON, result); err != nil {
		t.Fatal(err)
	}
	want := `{"version":1,"cycle_id":"cycle-1","changed":[{"task_id":"task-1","state":"complete"}],"archived_ids":[],"restored_ids":[],"retries":[]}` + "\n"
	if encoded.String() != want || strings.Contains(encoded.String(), "managed_resources") {
		t.Fatalf("json=%q", encoded.String())
	}
}

func TestManagedRefreshFailureOutputExplainsInstalledBinaryRecovery(t *testing.T) {
	result := ErrorResult{
		Operation: "update",
		ErrorCode: "managed_refresh_failed",
		Step:      "refresh_managed_surfaces",
		Cause:     "The new binary is installed. Managed surfaces will reconcile on the next heartbeat, or rerun threadbear update or threadbear configure.",
	}
	var human bytes.Buffer
	if err := Write(&human, FormatHuman, result); err != nil {
		t.Fatal(err)
	}
	for _, message := range []string{"new binary is installed", "next heartbeat", "threadbear update or threadbear configure"} {
		if !strings.Contains(human.String(), message) {
			t.Fatalf("human output missing %q: %q", message, human.String())
		}
	}
	var encoded bytes.Buffer
	if err := Write(&encoded, FormatJSON, result); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(encoded.String(), `"error_code":"managed_refresh_failed"`) || !strings.Contains(encoded.String(), `"step":"refresh_managed_surfaces"`) {
		t.Fatalf("json=%q", encoded.String())
	}
}

func TestMissingControlTaskIDHumanGuidance(t *testing.T) {
	result := ErrorResult{Operation: "install", ErrorCode: "control_task_id_required", Step: "select_control_task", Cause: "open a Codex task, copy its ID, rerun with --control-task-id TASK_ID, and see INSTALL.md"}
	var human, machine bytes.Buffer
	if err := Write(&human, FormatHuman, result); err != nil {
		t.Fatal(err)
	}
	if err := Write(&machine, FormatJSON, result); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"Codex", "--control-task-id", "INSTALL.md"} {
		if !strings.Contains(human.String(), text) || !strings.Contains(machine.String(), text) {
			t.Fatalf("missing %q human=%q json=%q", text, human.String(), machine.String())
		}
	}
}

func TestLifecycleStayedHomeHumanNamesBothTaskIDs(t *testing.T) {
	result := LifecycleResult{Command: "install", ControlTaskID: "persisted-home", SuppliedControlTaskID: "calling-task", ControlTaskDisposition: "stayed_home", Reinstalled: true}
	human := result.Human()
	for _, want := range []string{"ThreadBear stayed home", "persisted-home", "calling-task"} {
		if !strings.Contains(human, want) {
			t.Fatalf("human=%q missing %q", human, want)
		}
	}
}

func TestStayedHomeResultSaysThreadBearStayedHome(t *testing.T) {
	result := LifecycleResult{Command: "install", ControlTaskID: "home", ControlTaskDisposition: "stayed_home", SuppliedControlTaskID: "other"}
	var human bytes.Buffer
	if err := Write(&human, FormatHuman, result); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(human.String(), "ThreadBear stayed home") {
		t.Fatalf("human=%q", human.String())
	}
}

func TestTitlePlanAndReportResultsValidateAndSortDeterministically(t *testing.T) {
	planA := TitlePlanItem{TaskID: "task-a", ExpectedRevision: "rev-a", ExpectedTitle: "A", DesiredTitle: "✅ A"}
	planA.OperationID = state.TitleOperationID(planA.TaskID, planA.ExpectedRevision, planA.ExpectedTitle, planA.DesiredTitle)
	planB := TitlePlanItem{TaskID: "task-b", ExpectedRevision: "rev-b", ExpectedTitle: "B", DesiredTitle: "✅ B"}
	planB.OperationID = state.TitleOperationID(planB.TaskID, planB.ExpectedRevision, planB.ExpectedTitle, planB.DesiredTitle)
	var encoded bytes.Buffer
	result := TitlePlanResult{Mode: "batch", Plans: []TitlePlanItem{planB, planA}, Dispositions: []TitlePlanDisposition{{TaskID: "task-d", Outcome: "no_op"}, {TaskID: "task-c", Outcome: "drifted"}}}
	if err := Write(&encoded, FormatJSON, result); err != nil {
		t.Fatal(err)
	}
	if strings.Index(encoded.String(), `"task_id":"task-a"`) > strings.Index(encoded.String(), `"task_id":"task-b"`) || strings.Index(encoded.String(), `"task_id":"task-c"`) > strings.Index(encoded.String(), `"task_id":"task-d"`) {
		t.Fatalf("title plan output is not sorted: %s", encoded.String())
	}
	encoded.Reset()
	if err := Write(&encoded, FormatJSON, TitleReportResult{AcceptedIDs: []string{"task-b", "task-a"}, RejectedIDs: []string{"task-d", "task-c"}}); err != nil {
		t.Fatal(err)
	}
	if encoded.String() != `{"version":1,"accepted_ids":["task-a","task-b"],"rejected_ids":["task-c","task-d"]}`+"\n" {
		t.Fatalf("title report output = %q", encoded.String())
	}
}

func TestTitlePlanAndReportResultsRejectInvalidShapes(t *testing.T) {
	valid := TitlePlanItem{TaskID: "task-a", ExpectedRevision: "rev-a", ExpectedTitle: "A", DesiredTitle: "✅ A"}
	valid.OperationID = state.TitleOperationID(valid.TaskID, valid.ExpectedRevision, valid.ExpectedTitle, valid.DesiredTitle)
	for _, result := range []Result{
		TitlePlanResult{Mode: "report"},
		TitlePlanResult{Mode: "batch", Plans: []TitlePlanItem{{OperationID: "wrong", TaskID: "task-a", ExpectedRevision: "rev-a", ExpectedTitle: "A", DesiredTitle: "✅ A"}}},
		TitlePlanResult{Mode: "batch", Plans: []TitlePlanItem{valid}, Dispositions: []TitlePlanDisposition{{TaskID: "task-a", Outcome: "no_op"}}},
		TitleReportResult{AcceptedIDs: []string{"task-a"}, RejectedIDs: []string{"task-a"}},
	} {
		var encoded bytes.Buffer
		if err := Write(&encoded, FormatJSON, result); err == nil {
			t.Fatalf("accepted invalid result: %+v", result)
		}
	}
}

func TestTitleActuatorEnvelopeHasExactVersionedShape(t *testing.T) {
	var encoded bytes.Buffer
	if err := Write(&encoded, FormatJSON, TitleActuatorResult{Program: "program"}); err != nil {
		t.Fatal(err)
	}
	if got, want := encoded.String(), "{\"version\":1,\"program\":\"program\"}\n"; got != want {
		t.Fatalf("actuator=%q want=%q", got, want)
	}
	encoded.Reset()
	if err := Write(&encoded, FormatJSON, TitleActuatorResult{}); err == nil {
		t.Fatal("empty actuator program was accepted")
	}
}

func TestTitleDispatchEnvelopeHasExactVersionedShapes(t *testing.T) {
	var noOp bytes.Buffer
	if err := Write(&noOp, FormatJSON, TitleDispatchResult{Disposition: "rename_disabled"}); err != nil {
		t.Fatal(err)
	}
	if got, want := noOp.String(), "{\"version\":1,\"allow\":false,\"disposition\":\"rename_disabled\"}\n"; got != want {
		t.Fatalf("no-op=%q want=%q", got, want)
	}
	allowed := TitleDispatchResult{Allow: true, Disposition: "dispatch", Child: &TitleDispatchChild{Model: "gpt-5.6-luna", Thinking: "medium", Target: TitleDispatchTarget{Type: "projectless", DirectoryName: "threadbear-title-actuator"}, Prompt: "THREADBEAR_TITLE_ACTUATOR_V1\nact"}}
	var machine bytes.Buffer
	if err := Write(&machine, FormatJSON, allowed); err != nil {
		t.Fatal(err)
	}
	want := "{\"version\":1,\"allow\":true,\"disposition\":\"dispatch\",\"child\":{\"model\":\"gpt-5.6-luna\",\"thinking\":\"medium\",\"target\":{\"type\":\"projectless\",\"directoryName\":\"threadbear-title-actuator\"},\"prompt\":\"THREADBEAR_TITLE_ACTUATOR_V1\\nact\"}}\n"
	if machine.String() != want {
		t.Fatalf("allowed=%q want=%q", machine.String(), want)
	}
}
