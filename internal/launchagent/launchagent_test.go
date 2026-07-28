package launchagent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ericlitman/threadbear/internal/config"
)

type fakeRunner struct {
	disabled            map[string]bool
	loaded              map[string]bool
	calls               []string
	printDisabledOutput *string
}

func newFakeRunner() *fakeRunner {
	return &fakeRunner{disabled: map[string]bool{}, loaded: map[string]bool{}}
}

func (r *fakeRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, name+" "+strings.Join(args, " "))
	if len(args) == 0 {
		return nil, errors.New("missing command")
	}
	switch args[0] {
	case "print-disabled":
		if r.printDisabledOutput != nil {
			return []byte(*r.printDisabledOutput), nil
		}
		var lines []string
		for label, disabled := range r.disabled {
			lines = append(lines, fmt.Sprintf("\"%s\" => %t", label, disabled))
		}
		return []byte(strings.Join(lines, "\n")), nil
	case "print":
		if r.loaded[args[1]] {
			return []byte("loaded"), nil
		}
		return []byte("Could not find service"), errors.New("exit status 113")
	case "bootstrap":
		if _, err := os.Stat(args[2]); err != nil {
			return nil, err
		}
		if r.disabled[Label] {
			return []byte("Bootstrap failed: service is disabled"), errors.New("exit status 5")
		}
		r.loaded[args[1]+"/"+Label] = true
		return nil, nil
	case "bootout":
		delete(r.loaded, args[1])
		return nil, nil
	case "kickstart":
		if !r.loaded[args[2]] {
			return nil, errors.New("service is not loaded")
		}
		return nil, nil
	case "enable":
		r.disabled[serviceLabel(args[1])] = false
		return nil, nil
	case "disable":
		r.disabled[serviceLabel(args[1])] = true
		return nil, nil
	default:
		return nil, fmt.Errorf("unexpected command %q", args[0])
	}
}

func serviceLabel(service string) string {
	parts := strings.Split(service, "/")
	return parts[len(parts)-1]
}

func testAdapter(t *testing.T, runner *fakeRunner) (*Adapter, string) {
	t.Helper()
	home := t.TempDir()
	adapter, err := New(Options{Home: home, BinaryPath: filepath.Join(home, ".local", "bin", "threadbear"), UID: 501, Runner: runner})
	if err != nil {
		t.Fatal(err)
	}
	return adapter, home
}

func TestRenderPlistUsesExplicitMinimalEnvironment(t *testing.T) {
	spec := PlistSpec{Label: Label, BinaryPath: "/Users/bear/bin/thread&bear", StartInterval: 300, Home: "/Users/bear", CodexHome: "/Users/bear/.codex", Path: "/custom/codex/bin:/custom/node/bin", LCAll: DefaultLocale, StdoutPath: "/Users/bear/log/out", StderrPath: "/Users/bear/log/err"}
	rendered, err := RenderPlist(spec)
	if err != nil {
		t.Fatal(err)
	}
	text := string(rendered)
	for _, expected := range []string{"<string>org.litman.threadbear</string>", "<string>/Users/bear/bin/thread&amp;bear</string>", "<string>heartbeat</string>", "<integer>300</integer>", "<string>Background</string>", "<false/>", "<key>HOME</key>", "<key>CODEX_HOME</key>", "<key>PATH</key>", "<key>LC_ALL</key>", "<key>StandardOutPath</key>", "<key>StandardErrorPath</key>"} {
		if !strings.Contains(text, expected) {
			t.Errorf("rendered plist missing %q", expected)
		}
	}
	if !strings.Contains(text, "<string>/custom/codex/bin:/custom/node/bin</string>") {
		t.Fatalf("plist does not contain exact persisted PATH: %s", text)
	}
	if strings.Contains(text, "USER</key>") || strings.Contains(text, "SHELL</key>") {
		t.Fatal("plist inherited non-minimal environment")
	}
}

func TestApplyIsAtomicPrivateIdempotentAndPreservesDisabledState(t *testing.T) {
	runner := newFakeRunner()
	adapter, _ := testAdapter(t, runner)
	cfg := config.Default("control")
	cfg.CodexExecutable = "/custom/codex/bin/codex"
	cfg.CodexSpawnPath = "/custom/codex/bin:/custom/node/bin"
	if err := adapter.Apply(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(adapter.plistPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("plist mode = %o, want 600", info.Mode().Perm())
	}
	firstCalls := len(runner.calls)
	if err := adapter.Apply(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	for _, call := range runner.calls[firstCalls:] {
		if strings.Contains(call, " disable ") || strings.Contains(call, " bootout ") || strings.Contains(call, " bootstrap ") || strings.Contains(call, " enable ") || strings.Contains(call, " kickstart ") {
			t.Fatalf("idempotent apply made mutating call: %s", call)
		}
	}
	runner.disabled[Label] = true
	delete(runner.loaded, adapter.service)
	cfg.HeartbeatSeconds = 900
	before := len(runner.calls)
	if err := adapter.Apply(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	for _, call := range runner.calls[before:] {
		if strings.Contains(call, " bootstrap ") || strings.Contains(call, " enable ") {
			t.Fatalf("disabled Apply activated job: %s", call)
		}
	}
	data, err := os.ReadFile(adapter.plistPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "<integer>900</integer>") {
		t.Fatal("Apply did not update interval")
	}
	if !runner.disabled[Label] {
		t.Fatal("Apply changed disabled state")
	}
}

func TestStageUsesConfiguredCodexSpawnPath(t *testing.T) {
	runner := newFakeRunner()
	adapter, home := testAdapter(t, runner)
	cfg := config.Default("control")
	cfg.CodexExecutable = filepath.Join(home, "custom", "codex")
	cfg.CodexSpawnPath = strings.Join([]string{filepath.Dir(cfg.CodexExecutable), "/opt/homebrew/bin", "/usr/local/bin", filepath.Join(home, ".local", "bin"), "/usr/bin", "/bin", "/usr/sbin", "/sbin"}, string(os.PathListSeparator))
	if _, err := adapter.Stage(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(adapter.plistPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), cfg.CodexSpawnPath) {
		t.Fatalf("plist PATH does not match configured App Server PATH: %s", data)
	}
}

func TestStageLeavesThreadBearDisabledAndUnloaded(t *testing.T) {
	runner := newFakeRunner()
	adapter, _ := testAdapter(t, runner)
	cfg := config.Default("control")
	cfg.CodexExecutable = "/custom/codex/bin/codex"
	cfg.CodexSpawnPath = "/custom/codex/bin:/custom/node/bin"
	if err := adapter.Apply(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.Stage(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	if !runner.disabled[Label] || runner.loaded[adapter.service] {
		t.Fatalf("staged state disabled=%t loaded=%t", runner.disabled[Label], runner.loaded[adapter.service])
	}
}

func TestEnableDisableAndRemoveAreIdempotent(t *testing.T) {
	runner := newFakeRunner()
	adapter, _ := testAdapter(t, runner)
	cfg := config.Default("control")
	cfg.CodexExecutable = "/custom/codex/bin/codex"
	cfg.CodexSpawnPath = "/custom/codex/bin:/custom/node/bin"
	runner.disabled[Label] = true
	if err := adapter.Apply(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	changed, err := adapter.Enable(context.Background())
	if err != nil || !changed {
		t.Fatalf("Enable = %v, %v", changed, err)
	}
	changed, err = adapter.Enable(context.Background())
	if err != nil || changed {
		t.Fatalf("second Enable = %v, %v", changed, err)
	}
	changed, err = adapter.Disable(context.Background())
	if err != nil || !changed {
		t.Fatalf("Disable = %v, %v", changed, err)
	}
	changed, err = adapter.Disable(context.Background())
	if err != nil || changed {
		t.Fatalf("second Disable = %v, %v", changed, err)
	}
	if err := adapter.Remove(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := adapter.Remove(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(adapter.plistPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("plist still exists: %v", err)
	}
}

func TestEnablePrecedesBootstrapAndPreservesIdempotency(t *testing.T) {
	runner := newFakeRunner()
	adapter, _ := testAdapter(t, runner)
	cfg := config.Default("control")
	cfg.CodexExecutable = "/custom/codex/bin/codex"
	cfg.CodexSpawnPath = "/custom/codex/bin:/custom/node/bin"
	if _, err := adapter.Stage(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	reported := fmt.Sprintf(`"%s" => false`, Label)
	runner.printDisabledOutput = &reported
	callStart := len(runner.calls)
	changed, err := adapter.Enable(context.Background())
	if err != nil || !changed {
		t.Fatalf("Enable = %v, %v", changed, err)
	}
	calls := runner.calls[callStart:]
	enableIndex, bootstrapIndex, kickstartIndex := -1, -1, -1
	for index, call := range calls {
		if strings.Contains(call, " enable "+adapter.service) {
			enableIndex = index
		}
		if strings.Contains(call, " bootstrap "+adapter.domain+" "+adapter.plistPath) {
			bootstrapIndex = index
		}
		if strings.Contains(call, " kickstart -k "+adapter.service) {
			kickstartIndex = index
		}
	}
	if enableIndex < 0 || bootstrapIndex < 0 || kickstartIndex < 0 || enableIndex >= bootstrapIndex || bootstrapIndex >= kickstartIndex {
		t.Fatalf("enable/bootstrap/kickstart order=%v", calls)
	}
	changed, err = adapter.Enable(context.Background())
	if err != nil || changed {
		t.Fatalf("repeated Enable = %v, %v", changed, err)
	}

	runner.printDisabledOutput = nil
	runner.disabled[Label] = true
	callStart = len(runner.calls)
	changed, err = adapter.Enable(context.Background())
	if err != nil || !changed {
		t.Fatalf("loaded disabled Enable = %v, %v", changed, err)
	}
	for _, call := range runner.calls[callStart:] {
		if strings.Contains(call, " bootstrap ") || strings.Contains(call, " kickstart ") {
			t.Fatalf("loaded disabled Enable reloaded service: %s", call)
		}
	}
	changed, err = adapter.Enable(context.Background())
	if err != nil || changed {
		t.Fatalf("enabled loaded Enable = %v, %v", changed, err)
	}
}

func TestDisabledRecognizesLaunchctlValues(t *testing.T) {
	tests := []struct {
		output   string
		disabled bool
	}{
		{output: fmt.Sprintf(`"%s" => disabled`, Label), disabled: true},
		{output: fmt.Sprintf(`"%s" => true`, Label), disabled: true},
		{output: fmt.Sprintf(`%s = true`, Label), disabled: true},
		{output: fmt.Sprintf(`"%s" => false`, Label)},
		{output: fmt.Sprintf(`"%s" => enabled`, Label)},
	}
	for _, test := range tests {
		t.Run(test.output, func(t *testing.T) {
			runner := newFakeRunner()
			runner.printDisabledOutput = &test.output
			adapter, _ := testAdapter(t, runner)
			disabled, err := adapter.disabled(context.Background(), Label)
			if err != nil || disabled != test.disabled {
				t.Fatalf("disabled = %v, %v; want %v", disabled, err, test.disabled)
			}
		})
	}
}

func TestHealthyRequiresPlistEnabledAndLoaded(t *testing.T) {
	runner := newFakeRunner()
	adapter, _ := testAdapter(t, runner)
	healthy, err := adapter.Healthy(context.Background())
	if err != nil || healthy {
		t.Fatalf("missing Healthy = %v, %v", healthy, err)
	}
	cfg := config.Default("control")
	cfg.CodexExecutable = "/custom/codex/bin/codex"
	cfg.CodexSpawnPath = "/custom/codex/bin:/custom/node/bin"
	if err := adapter.Apply(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	healthy, err = adapter.Healthy(context.Background())
	if err != nil || !healthy {
		t.Fatalf("loaded Healthy = %v, %v", healthy, err)
	}
	runner.disabled[Label] = true
	healthy, err = adapter.Healthy(context.Background())
	if err != nil || healthy {
		t.Fatalf("disabled Healthy = %v, %v", healthy, err)
	}
}

func TestExecRunnerUsesExplicitMinimalEnvironment(t *testing.T) {
	output, err := (ExecRunner{}).Run(context.Background(), "/usr/bin/env")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Fields(string(output))
	if len(lines) != 2 || !strings.Contains(string(output), "PATH=/usr/bin:/bin") || !strings.Contains(string(output), "LC_ALL=C") {
		t.Fatalf("environment=%q", output)
	}
}

func TestStageWritesPlistWithoutActivation(t *testing.T) {
	runner := newFakeRunner()
	adapter, _ := testAdapter(t, runner)
	cfg := config.Default("control")
	cfg.CodexExecutable = "/custom/codex/bin/codex"
	cfg.CodexSpawnPath = "/custom/codex/bin:/custom/node/bin"
	changed, err := adapter.Stage(context.Background(), cfg)
	if err != nil || !changed {
		t.Fatalf("Stage=%t, %v", changed, err)
	}
	if !runner.disabled[Label] || runner.loaded[adapter.service] {
		t.Fatalf("stage activated scheduler disabled=%v loaded=%v", runner.disabled, runner.loaded)
	}
	for _, call := range runner.calls {
		if strings.Contains(call, " bootstrap ") || strings.Contains(call, " enable ") || strings.Contains(call, " kickstart ") {
			t.Fatalf("activation before self-test: %s", call)
		}
	}
	changed, err = adapter.Enable(context.Background())
	if err != nil || !changed {
		t.Fatalf("Enable=%t, %v", changed, err)
	}
	healthy, err := adapter.Healthy(context.Background())
	if err != nil || !healthy {
		t.Fatalf("Healthy=%t, %v", healthy, err)
	}
}

func TestStageIdenticalEnabledLoadedJobDisablesAndUnloads(t *testing.T) {
	runner := newFakeRunner()
	adapter, _ := testAdapter(t, runner)
	cfg := config.Default("control")
	cfg.CodexExecutable = "/custom/codex/bin/codex"
	cfg.CodexSpawnPath = "/custom/codex/bin:/custom/node/bin"
	if _, err := adapter.Stage(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.Enable(context.Background()); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(adapter.plistPath)
	if err != nil {
		t.Fatal(err)
	}
	callStart := len(runner.calls)
	changed, err := adapter.Stage(context.Background(), cfg)
	if err != nil || !changed {
		t.Fatalf("Stage=%t, %v", changed, err)
	}
	if !runner.disabled[Label] || runner.loaded[adapter.service] {
		t.Fatalf("stage left scheduler active disabled=%v loaded=%v", runner.disabled, runner.loaded)
	}
	after, err := os.ReadFile(adapter.plistPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("identical Stage rewrote plist")
	}
	calls := strings.Join(runner.calls[callStart:], "\n")
	if !strings.Contains(calls, " disable ") || !strings.Contains(calls, " bootout ") {
		t.Fatalf("calls=%s", calls)
	}
}

func TestStageIdenticalDisabledUnloadedJobIsNoOp(t *testing.T) {
	runner := newFakeRunner()
	adapter, _ := testAdapter(t, runner)
	cfg := config.Default("control")
	cfg.CodexExecutable = "/custom/codex/bin/codex"
	cfg.CodexSpawnPath = "/custom/codex/bin:/custom/node/bin"
	if _, err := adapter.Stage(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	callStart := len(runner.calls)
	changed, err := adapter.Stage(context.Background(), cfg)
	if err != nil || changed {
		t.Fatalf("Stage=%t, %v", changed, err)
	}
	for _, call := range runner.calls[callStart:] {
		if strings.Contains(call, " disable ") || strings.Contains(call, " bootout ") {
			t.Fatalf("no-op Stage mutated scheduler: %s", call)
		}
	}
}

func TestEnableWithoutKickstartActivatesWithoutRunningHeartbeat(t *testing.T) {
	runner := newFakeRunner()
	adapter, _ := testAdapter(t, runner)
	cfg := config.Default("control")
	cfg.CodexExecutable = "/custom/codex/bin/codex"
	cfg.CodexSpawnPath = "/custom/codex/bin:/custom/node/bin"
	if _, err := adapter.Stage(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	start := len(runner.calls)
	changed, err := adapter.EnableWithoutKickstart(context.Background())
	if err != nil || !changed {
		t.Fatalf("EnableWithoutKickstart=%t, %v", changed, err)
	}
	calls := strings.Join(runner.calls[start:], "\n")
	if !strings.Contains(calls, " enable "+adapter.service) || !strings.Contains(calls, " bootstrap "+adapter.domain+" "+adapter.plistPath) {
		t.Fatalf("activation calls=%s", calls)
	}
	if strings.Contains(calls, " kickstart ") {
		t.Fatalf("no-kickstart activation ran heartbeat: %s", calls)
	}
}

func TestEnableKickstartsButEnableWithoutKickstartDoesNot(t *testing.T) {
	cfg := config.Default("control")
	cfg.CodexExecutable = "/custom/codex/bin/codex"
	cfg.CodexSpawnPath = "/custom/codex/bin:/custom/node/bin"

	installerRunner := newFakeRunner()
	installerAdapter, _ := testAdapter(t, installerRunner)
	if _, err := installerAdapter.Stage(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	start := len(installerRunner.calls)
	if changed, err := installerAdapter.EnableWithoutKickstart(context.Background()); err != nil || !changed {
		t.Fatalf("EnableWithoutKickstart=%t, %v", changed, err)
	}
	for _, call := range installerRunner.calls[start:] {
		if strings.Contains(call, " kickstart ") {
			t.Fatalf("installer enable kickstarted under lock: %s", call)
		}
	}

	explicitRunner := newFakeRunner()
	explicitAdapter, _ := testAdapter(t, explicitRunner)
	if _, err := explicitAdapter.Stage(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	start = len(explicitRunner.calls)
	if changed, err := explicitAdapter.Enable(context.Background()); err != nil || !changed {
		t.Fatalf("Enable=%t, %v", changed, err)
	}
	calls := strings.Join(explicitRunner.calls[start:], "\n")
	if !strings.Contains(calls, " bootstrap ") || !strings.Contains(calls, " kickstart -k "+explicitAdapter.service) {
		t.Fatalf("explicit enable did not bootstrap and kickstart: %s", calls)
	}
}
