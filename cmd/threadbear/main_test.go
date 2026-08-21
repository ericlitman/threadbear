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

func TestRunTitleScriptEmitsOneEmbeddedMountedProgram(t *testing.T) {
	t.Setenv("CODEX_THREAD_ID", testTaskID)
	var stdout, stderr bytes.Buffer
	code := run(t.Context(), []string{"title-script", "--status", "complete"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 || stderr.Len() != 0 {
		t.Fatalf("title-script = code %d, stdout %q, stderr %q", code, stdout.String(), stderr.String())
	}
	value := stdout.String()
	for _, required := range []string{testTaskID, `"status":"complete"`, `"icon":"✅"`,
		"codex_app__read_thread", "codex_app__set_thread_title"} {
		if !strings.Contains(value, required) {
			t.Fatalf("title-script missing %q: %s", required, value)
		}
	}
	for _, forbidden := range []string{"exec_command", "thread/name/set", "state_N.sqlite"} {
		if strings.Contains(value, forbidden) {
			t.Fatalf("title-script contains %q: %s", forbidden, value)
		}
	}
	stdout.Reset()
	stderr.Reset()
	if code = run(t.Context(), []string{"title-script", "--status", "waiting"}, strings.NewReader(""), &stdout, &stderr); code != 1 ||
		!strings.Contains(stderr.String(), `unsupported ThreadBear status "waiting"`) || stdout.Len() != 0 {
		t.Fatalf("invalid title-script = code %d, stdout %q, stderr %q", code, stdout.String(), stderr.String())
	}
	for name, args := range map[string][]string{
		"missing status": {"title-script"},
		"extra argument": {"title-script", "--status", "complete", "extra"},
	} {
		stdout.Reset()
		stderr.Reset()
		if code = run(t.Context(), args, strings.NewReader(""), &stdout, &stderr); code == 0 || stdout.Len() != 0 {
			t.Fatalf("%s = code %d, stdout %q, stderr %q", name, code, stdout.String(), stderr.String())
		}
	}
	t.Setenv("CODEX_THREAD_ID", "")
	stdout.Reset()
	stderr.Reset()
	if code = run(t.Context(), []string{"title-script", "--status", "complete"}, strings.NewReader(""), &stdout, &stderr); code != 1 ||
		!strings.Contains(stderr.String(), "CODEX_THREAD_ID is unavailable or invalid") || stdout.Len() != 0 {
		t.Fatalf("missing task ID = code %d, stdout %q, stderr %q", code, stdout.String(), stderr.String())
	}
}

func TestRunHasNoOnboardCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"onboard", "--json"}, strings.NewReader(""), &stdout, &stderr)
	if code != 2 || !strings.Contains(stderr.String(), `unknown command "onboard"`) {
		t.Fatalf("removed onboard command = code %d, stdout %q, stderr %q", code, stdout.String(), stderr.String())
	}
}

func TestRunRequiresConfirmationForUninstallPreparation(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"uninstall", "--prepare", "--json"}, strings.NewReader(""), &stdout, &stderr)
	if code != 1 || !strings.Contains(stdout.String(), "uninstall preparation requires --noninteractive --confirm") {
		t.Fatalf("unconfirmed preparation = code %d, stdout %q, stderr %q", code, stdout.String(), stderr.String())
	}
}

func TestRunRefusesConfirmedUninstallWithoutAnExplicitPhase(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"uninstall", "--noninteractive", "--confirm", "--json"}, strings.NewReader(""), &stdout, &stderr)
	if code != 1 || !strings.Contains(stdout.String(), "confirmed uninstall must use --prepare or --commit") {
		t.Fatalf("unphased uninstall = code %d, stdout %q, stderr %q", code, stdout.String(), stderr.String())
	}
}

func TestRunAcceptsV301AutomaticUpdaterInstallInvocation(t *testing.T) {
	isolatedLifecycle(t)
	if _, err := install(context.Background(), installOptions{Confirmed: true}); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"install", "--automatic", "--no-onboard", "--noninteractive", "--confirm", "--json"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("v3.0.1 updater invocation = code %d, stdout %q, stderr %q", code, stdout.String(), stderr.String())
	}
}
