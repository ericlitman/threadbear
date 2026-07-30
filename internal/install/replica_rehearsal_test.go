package install

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestReplicaRehearsalRestoresClassifierModeOnlyAfterCapture(t *testing.T) {
	script, err := os.ReadFile("../../scripts/replica-rehearsal.sh")
	if err != nil {
		t.Fatal(err)
	}
	source := string(script)
	start := strings.Index(source, "restore_classifier_mode() {")
	if start < 0 {
		t.Fatal("restore_classifier_mode function is missing")
	}
	end := strings.Index(source[start:], "\n}\n\ncleanup() {")
	if end < 0 {
		t.Fatal("restore_classifier_mode function boundary is missing")
	}
	function := source[start : start+end+2]
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
			harness := "set -eu\n" + function + "\nTRACE_FILE=$1\nexport TRACE_FILE\n" + test.setup + "\nrestore_classifier_mode \"$2\"\n"
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
