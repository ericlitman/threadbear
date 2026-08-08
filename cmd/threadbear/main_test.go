package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestRunHasNoHookCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"hook"}, strings.NewReader("{}"), &stdout, &stderr)
	if code != 2 || !strings.Contains(stderr.String(), `unknown command "hook"`) {
		t.Fatalf("removed hook command = code %d, stdout %q, stderr %q", code, stdout.String(), stderr.String())
	}
}

func TestRunRejectsInvalidTitleStatusBeforeMutation(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"title", "--status", "waiting", "--json"}, strings.NewReader(""), &stdout, &stderr)
	if code != 1 {
		t.Fatalf("invalid title status code = %d, stderr %q", code, stderr.String())
	}
	var result map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil || result["ready"] != false || !strings.Contains(result["error"].(string), `unsupported ThreadBear status "waiting"`) {
		t.Fatalf("invalid title status = %#v, %v", result, err)
	}
}

func TestRunRequiresExplicitOnboardingMode(t *testing.T) {
	for _, args := range [][]string{{"onboard", "--json"}, {"onboard", "--confirm", "--json"}} {
		var stdout, stderr bytes.Buffer
		code := run(context.Background(), args, strings.NewReader(""), &stdout, &stderr)
		if code != 1 || !strings.Contains(stdout.String(), "onboarding requires --dry-run or --noninteractive --confirm") {
			t.Fatalf("onboard mode %v = code %d, stdout %q, stderr %q", args, code, stdout.String(), stderr.String())
		}
	}
}
