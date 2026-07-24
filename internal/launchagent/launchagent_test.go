package launchagent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/ericlitman/threadbear/internal/config"
)

type fakeRunner struct {
	disabled map[string]bool
	loaded   map[string]bool
	calls    []string
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
		data, err := os.ReadFile(args[2])
		if err != nil {
			return nil, err
		}
		label := Label
		if strings.Contains(string(data), LegacyLabel) {
			label = LegacyLabel
		}
		r.loaded[args[1]+"/"+label] = true
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

func TestCleanInstallLifecycleDoesNotOperateOnLegacyLabel(t *testing.T) {
	runner := newFakeRunner()
	adapter, _ := testAdapter(t, runner)
	cfg := config.Default("control")
	cfg.CodexExecutable = "/custom/codex/bin/codex"
	cfg.CodexSpawnPath = "/custom/codex/bin:/custom/node/bin"
	if _, err := adapter.Stage(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.Stage(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.Enable(context.Background()); err != nil {
		t.Fatal(err)
	}
	if healthy, err := adapter.Healthy(context.Background()); err != nil || !healthy {
		t.Fatalf("Healthy=%t, %v", healthy, err)
	}
	for _, call := range runner.calls {
		if strings.Contains(call, LegacyLabel) {
			t.Fatalf("clean install operated on legacy label: %s", call)
		}
	}
}

func TestLegacyStopVerifyAndIntervalDetection(t *testing.T) {
	runner := newFakeRunner()
	adapter, _ := testAdapter(t, runner)
	if err := os.MkdirAll(filepath.Dir(adapter.legacyPlistPath), 0o700); err != nil {
		t.Fatal(err)
	}
	legacy := `<?xml version="1.0"?><plist><dict><key>Label</key><string>org.litman.threadwatch</string><key>StartInterval</key><integer>420</integer></dict></plist>`
	if err := os.WriteFile(adapter.legacyPlistPath, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	interval, found, err := adapter.DetectLegacyInterval()
	if err != nil || !found || interval != 420 {
		t.Fatalf("DetectLegacyInterval = %d, %v, %v", interval, found, err)
	}
	runner.loaded[adapter.legacyService] = true
	if err := adapter.VerifyLegacyStopped(context.Background()); err == nil {
		t.Fatal("verification accepted loaded legacy job")
	}
	unrelated := filepath.Join(filepath.Dir(adapter.legacyLockPath), "keep.log")
	if err := os.MkdirAll(filepath.Dir(unrelated), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(unrelated, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := adapter.StopLegacy(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !runner.disabled[LegacyLabel] {
		t.Fatal("legacy label was not durably disabled")
	}
	if _, err := os.Stat(adapter.legacyPlistPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("legacy plist remains: %v", err)
	}
	if _, err := os.Stat(adapter.legacyPlistPath + ".disabled-by-threadbear"); err != nil {
		t.Fatalf("legacy plist was not quarantined: %v", err)
	}
	interval, found, err = adapter.DetectLegacyInterval()
	if err != nil || !found || interval != 420 {
		t.Fatalf("quarantined DetectLegacyInterval = %d, %v, %v", interval, found, err)
	}
	if data, err := os.ReadFile(unrelated); err != nil || string(data) != "keep" {
		t.Fatalf("unrelated legacy data changed: %q %v", data, err)
	}
	stopped, err := adapter.LegacyStopped(context.Background())
	if err != nil || !stopped {
		t.Fatalf("LegacyStopped = %v, %v", stopped, err)
	}
	if err := adapter.VerifyLegacyStopped(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestLegacyVerificationUsesInjectableRunLockProbe(t *testing.T) {
	runner := newFakeRunner()
	home := t.TempDir()
	called := ""
	adapter, err := New(Options{Home: home, BinaryPath: filepath.Join(home, "threadbear"), UID: 501, Runner: runner, LegacyLockProbe: func(path string) error { called = path; return errors.New("held") }})
	if err != nil {
		t.Fatal(err)
	}
	runner.disabled[LegacyLabel] = true
	if err := adapter.VerifyLegacyStopped(context.Background()); err == nil || !strings.Contains(err.Error(), "held") {
		t.Fatalf("error=%v", err)
	}
	if called != adapter.legacyLockPath {
		t.Fatalf("probe path=%q", called)
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

func TestVerifyLockAvailableRejectsRealAdvisoryFlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run.lock")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	defer syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	if err := verifyLockAvailable(path); err == nil {
		t.Fatal("held advisory flock was accepted")
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
