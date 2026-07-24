package output

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestLifecycleResultHumanJSONParity(t *testing.T) {
	result := LifecycleResult{Command: "install", Changed: true, Resources: []string{"state", "binary"}, ControlTaskID: "control-1", Migrated: true}
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
	for _, fact := range []string{"install", "control-1", "binary,state", "migrated=true"} {
		if !strings.Contains(human.String(), fact) {
			t.Fatalf("human output missing %q: %q", fact, human.String())
		}
	}
	if decoded.Command != result.Command || !decoded.Changed || decoded.ControlTaskID != result.ControlTaskID || !decoded.Migrated || len(decoded.Resources) != 2 {
		t.Fatalf("decoded=%+v", decoded)
	}
}

func TestPreviewDetailsHumanJSONContract(t *testing.T) {
	result := PreviewResult{Command: "install", Effects: []string{"binary"}, Details: []string{"AGENTS.md: write managed block", "LaunchAgent staged disabled"}}
	var human, machine bytes.Buffer
	if err := Write(&human, FormatHuman, result); err != nil {
		t.Fatal(err)
	}
	if err := Write(&machine, FormatJSON, result); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(human.String(), "write managed block") || !strings.Contains(human.String(), "staged disabled") {
		t.Fatalf("human=%q", human.String())
	}
	var decoded PreviewResult
	if err := json.Unmarshal(machine.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Details) != 2 || decoded.Command != "install" {
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
