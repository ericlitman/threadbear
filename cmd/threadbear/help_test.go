package main

import (
	"bytes"
	"context"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ericlitman/threadbear/internal/app"
)

var updateHelpGolden = flag.Bool("update", false, "update help golden files")

func TestHelpCommandMetadataComplete(t *testing.T) {
	commands := app.AllCommands()
	if len(commandSpecs) != len(commands) {
		t.Fatalf("commandSpecs has %d entries; want %d", len(commandSpecs), len(commands))
	}
	seen := make(map[app.Command]bool, len(commandSpecs))
	for _, spec := range commandSpecs {
		if seen[spec.command] {
			t.Fatalf("duplicate help metadata for %q", spec.command)
		}
		seen[spec.command] = true
		if spec.synopsis == "" {
			t.Fatalf("empty synopsis for %q", spec.command)
		}
		if spec.registerFlags == nil {
			t.Fatalf("nil flag registration for %q", spec.command)
		}
	}
	for _, command := range commands {
		if !command.Valid() {
			t.Fatalf("listed app.Command %q is not valid", command)
		}
		if !seen[command] {
			t.Fatalf("app.Command %q is missing help metadata", command)
		}
	}
}

func TestHelpGolden(t *testing.T) {
	var rendered strings.Builder
	rendered.WriteString(renderTopLevelHelp())
	for _, spec := range commandSpecs {
		rendered.WriteByte('\n')
		rendered.WriteString(renderCommandHelp(spec))
	}
	path := filepath.Join("testdata", "help.golden")
	if *updateHelpGolden {
		if err := os.WriteFile(path, []byte(rendered.String()), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if rendered.String() != string(want) {
		t.Fatalf("help output differs from %s; regenerate with go test ./cmd/threadbear -run TestHelp -update", path)
	}
}

func TestHelpExplicitFormsAndBareInvocation(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("CODEX_HOME", filepath.Join(t.TempDir(), "missing-codex-home"))
	topLevel := renderTopLevelHelp()
	for _, args := range [][]string{{"help"}, {"--help"}, {"-h"}} {
		assertHelpRun(t, args, 0, topLevel)
	}
	assertHelpRun(t, nil, 2, topLevel)
	for _, spec := range commandSpecs {
		want := renderCommandHelp(spec)
		command := string(spec.command)
		for _, args := range [][]string{{"help", command}, {command, "--help"}, {command, "-h"}, {command, "--json", "--help"}} {
			assertHelpRun(t, args, 0, want)
		}
		if !strings.Contains(want, "--json") || !strings.Contains(want, "help stays human-readable") {
			t.Fatalf("%s help does not document --json interplay", command)
		}
	}
	inspect, _ := commandSpecFor(app.CommandInspect)
	if want := renderCommandHelp(inspect); !strings.Contains(want, "threadbear inspect [flags] TASK_ID") || !strings.Contains(want, "Arguments:\n  TASK_ID") {
		t.Fatalf("inspect help omits its positional: %q", want)
	}
	request, err := parseRequest([]string{"configure", "--classifier-model", "--help"})
	if err != nil || request.Configure.ClassifierModel == nil || *request.Configure.ClassifierModel != "--help" {
		t.Fatalf("help-like flag value changed parsing: request=%+v err=%v", request, err)
	}
	if _, _, ok := requestedHelp([]string{"configure", "--bogus", "--help"}); ok {
		t.Fatal("help pre-scan masked an earlier invalid flag")
	}
}

func TestHelpUnknownCommandPreservesJSONShape(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run(context.Background(), []string{"bogus"}, &stdout, &stderr); code != 2 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "threadbear help") || stderr.Len() != 0 {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	stdout.Reset()
	if code := run(context.Background(), []string{"bogus", "--json"}, &stdout, &stderr); code != 2 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	want := `{"version":1,"operation":"dispatch","error_code":"unknown_command"}` + "\n"
	if stdout.String() != want || stderr.Len() != 0 {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func assertHelpRun(t *testing.T, args []string, wantCode int, wantOutput string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), args, &stdout, &stderr)
	if code != wantCode || stdout.String() != wantOutput || stderr.Len() != 0 {
		t.Fatalf("args=%q code=%d stdout=%q stderr=%q", args, code, stdout.String(), stderr.String())
	}
}
