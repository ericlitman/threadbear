package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ericlitman/threadbear/assets"
)

type fakeLaunchctl struct {
	mu                   sync.Mutex
	loaded               bool
	bootstraps, bootouts int
	calls                [][]string
	bootstrapErr         error
	printErr             error
	printOutput          []byte
	bootoutStarted       chan struct{}
	continueBootout      <-chan struct{}
}

func (fake *fakeLaunchctl) run(_ context.Context, args ...string) ([]byte, error) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	fake.calls = append(fake.calls, append([]string(nil), args...))
	if len(args) == 0 {
		return nil, errors.New("missing launchctl operation")
	}
	switch args[0] {
	case "print":
		if fake.printErr != nil {
			return nil, fake.printErr
		}
		if !fake.loaded {
			return nil, errLaunchAgentNotLoaded
		}
		if fake.printOutput != nil {
			return append([]byte(nil), fake.printOutput...), nil
		}
		return managedLaunchctlPrint(updateAgentPath(), installPaths().binary), nil
	case "bootstrap":
		fake.bootstraps++
		if fake.bootstrapErr != nil {
			return nil, fake.bootstrapErr
		}
		fake.loaded = true
		return nil, nil
	case "bootout":
		if !fake.loaded {
			return nil, errLaunchAgentNotLoaded
		}
		if fake.bootoutStarted != nil {
			close(fake.bootoutStarted)
		}
		if fake.continueBootout != nil {
			fake.mu.Unlock()
			<-fake.continueBootout
			fake.mu.Lock()
		}
		fake.bootouts++
		fake.loaded = false
		return nil, nil
	default:
		return nil, errors.New("unexpected launchctl operation")
	}
}

func managedLaunchctlPrint(path, binary string) []byte {
	var output strings.Builder
	output.WriteString(updateAgentTarget() + " = {\n\tpath = " + path + "\n\tprogram = " + binary + "\n\targuments = {\n")
	for _, argument := range updateAgentArguments(binary) {
		output.WriteString("\t\t" + argument + "\n")
	}
	output.WriteString("\t}\n}\n")
	return []byte(output.String())
}

func TestInstallPreviewAndConfirmationHaveNoOnboardingSurface(t *testing.T) {
	p := isolatedLifecycle(t)
	preview, err := install(context.Background(), installOptions{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	got := preview.(map[string]any)
	if got["installed"] != false || got["automatic_updates_enabled"] != false || got["onboarding_requested"] != nil || got["next_request"] != nil {
		t.Fatalf("preview = %#v", got)
	}
	planned := got["planned_changes"].([]string)
	wantPlanned := []string{
		"manage update receipt " + p.updateReceipt,
		"replace managed AGENTS block in " + p.agents,
		"write skill " + p.skill,
		"install " + updateAgentLabel + " LaunchAgent " + p.launchAgent,
		"write binary " + p.binary,
	}
	if strings.Join(planned, "\n") != strings.Join(wantPlanned, "\n") {
		t.Fatalf("planned changes = %#v", planned)
	}
	for _, path := range []string{p.binary, p.agents, p.skill, p.launchAgent, stateDir()} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("preview created %s: %v", path, err)
		}
	}
	if _, err := install(context.Background(), installOptions{}); err == nil {
		t.Fatal("unconfirmed install succeeded")
	}
	result, err := install(context.Background(), installOptions{Confirmed: true})
	if err != nil {
		t.Fatal(err)
	}
	got = result.(map[string]any)
	if got["ready"] != true || got["installed"] != true || got["version"] != version || got["next_request"] != nil || got["onboarding_requested"] != nil || got["restart_required"] != true || got["automatic_updates_enabled"] != true {
		t.Fatalf("install = %#v", got)
	}
}

func TestLifecycleNeverTouchesCodexHooks(t *testing.T) {
	p := isolatedLifecycle(t)
	foreignAgents := "# Mine\nkeep this exactly\n"
	mustWrite(t, p.agents, foreignAgents)
	hooks := filepath.Join(codexHome(), "hooks.json")
	wantHooks := []byte(`{"owner":"user","hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"mine"}]}]}}` + "\n")
	mustWrite(t, hooks, string(wantHooks))
	if _, err := install(context.Background(), installOptions{Confirmed: true}); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(hooks)
	if !bytes.Equal(first, wantHooks) {
		t.Fatalf("install changed hooks.json: %q", first)
	}
	if _, err := install(context.Background(), installOptions{Confirmed: true}); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(hooks)
	if !bytes.Equal(first, second) {
		t.Fatal("idempotent reinstall changed hooks.json")
	}
	agents, _ := os.ReadFile(p.agents)
	if !strings.HasPrefix(string(agents), foreignAgents) || !managedBlockExact(p.agents) {
		t.Fatalf("managed AGENTS content = %q", agents)
	}
	skill, _ := os.ReadFile(p.skill)
	for label, text := range map[string]string{"AGENTS": string(agents), "skill": string(skill)} {
		if strings.Count(text, "tools.codex_app__set_thread_title") != 1 {
			t.Fatalf("%s must contain exactly one mounted app-native setter: %q", label, text)
		}
		for _, obsolete := range []string{"plan.updated", "plan.unconfirmed", "thread/name/set"} {
			if strings.Contains(text, obsolete) {
				t.Fatalf("%s contains obsolete detached-writer contract %q", label, obsolete)
			}
		}
	}
	if !strings.Contains(string(agents), "plan.owned_prefixes") || !strings.Contains(string(skill), `item.outcome === "prepared"`) {
		t.Fatalf("installed guidance lacks planner/prepared contract: AGENTS=%q skill=%q", agents, skill)
	}
	for _, required := range []string{
		"const decodeNative = value => {",
		`if (typeof value !== "string") return value;`,
		"return JSON.parse(value)",
		"decodeNative(await tools.codex_app__read_thread",
		"decodeNative(await tools.codex_app__set_thread_title",
	} {
		if !strings.Contains(string(agents), required) {
			t.Fatalf("installed AGENTS lacks JSON-string native result decoding %q: %q", required, agents)
		}
	}
	for _, required := range []string{
		"tools.write_stdin({",
		"tools.codex_app__read_thread({",
		"const parseNative = value => {",
		`if (typeof value !== "string") return value;`,
		"try { return JSON.parse(value); } catch { return null; }",
		"current = parseNative(await tools.codex_app__read_thread",
		"renamed = parseNative(await tools.codex_app__set_thread_title",
		"current?.thread?.id !== item.task_id",
		"current.thread.title !== item.title",
		"let updated = 0, drifted = 0, unconfirmed = 0",
		"updated + drifted + unconfirmed === prepared.length",
		"if (!accounted || drifted !== 0 || unconfirmed !== 0)",
	} {
		if !strings.Contains(string(skill), required) {
			t.Fatalf("installed skill lacks mounted revalidation contract %q: %q", required, skill)
		}
	}
	if strings.Count(string(skill), "tools.write_stdin({") != 1 || strings.Count(string(skill), "tools.codex_app__read_thread({") != 1 {
		t.Fatalf("installed skill must contain one preparation resume and one mounted reread: %q", skill)
	}
	if !exactFile(p.skill, []byte(assets.SkillManagedContent)) {
		t.Fatal("managed skill is not exact")
	}
	committed, err := uninstall(context.Background(), uninstallOptions{Commit: true, Confirmed: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range committed.(map[string]any)["planned_changes"].([]string) {
		if strings.Contains(change, "task title") {
			t.Fatalf("artifact commit claimed title cleanup: %#v", committed)
		}
	}
	after, _ := os.ReadFile(hooks)
	if !bytes.Equal(after, wantHooks) {
		t.Fatalf("uninstall changed hooks.json: %q", after)
	}
}

func TestCurrentLifecycleIgnoresMalformedCodexHooks(t *testing.T) {
	p := isolatedLifecycle(t)
	if _, err := install(context.Background(), installOptions{Confirmed: true}); err != nil {
		t.Fatal(err)
	}
	hooks := legacyHooksPath()
	want := []byte("this is user-owned and not JSON\n")
	mustWrite(t, hooks, string(want))
	if result, err := status(context.Background()); err != nil || !result.(map[string]any)["ready"].(bool) {
		t.Fatalf("status depended on hooks.json: %#v, %v", result, err)
	}
	if _, err := install(context.Background(), installOptions{Confirmed: true}); err != nil {
		t.Fatalf("reinstall depended on hooks.json: %v", err)
	}
	if _, err := uninstall(context.Background(), uninstallOptions{Commit: true, Confirmed: true}); err != nil {
		t.Fatalf("uninstall depended on hooks.json: %v", err)
	}
	if got, err := os.ReadFile(hooks); err != nil || !bytes.Equal(got, want) {
		t.Fatalf("current lifecycle changed hooks.json = %q, %v", got, err)
	}
	if _, err := os.Stat(p.binary); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("uninstall left binary: %v", err)
	}
}

func TestManagedAgentsRoundTripPreservesMissingTrailingNewline(t *testing.T) {
	p := isolatedLifecycle(t)
	original := []byte("# Mine\nkeep the missing final newline")
	mustWrite(t, p.agents, string(original))
	if _, err := install(context.Background(), installOptions{Confirmed: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := uninstall(context.Background(), uninstallOptions{Commit: true, Confirmed: true}); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(p.agents); err != nil || !bytes.Equal(got, original) {
		t.Fatalf("AGENTS round trip = %q, %v", got, err)
	}
}

func TestInstallReplacesExactLegacyStateOnlyWithReset(t *testing.T) {
	p := isolatedLifecycle(t)
	mainID := "019fdcbf-d225-7e00-9779-2472e54532e3"
	mustWrite(t, filepath.Join(stateDir(), "native.json"), `{"format":4,"main_task_id":"`+mainID+`","tasks":{}}`)
	hooks := legacyHooksPath()
	legacyCommand := quoteArgument(p.binary) + " hook"
	legacyHooks := `{"owner":"keep","hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"mine"}]},{"matcher":"codex_appset_thread_title","hooks":[{"type":"command","command":"` + legacyCommand + `","timeout":3}]}],"PostToolUse":[{"matcher":"codex_appset_thread_title","hooks":[{"type":"command","command":"` + legacyCommand + `","timeout":3}]}],"Stop":[{"hooks":[{"type":"command","command":"stop"}]}]}}` + "\n"
	mustWrite(t, hooks, legacyHooks)
	preview, err := install(context.Background(), installOptions{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if value := preview.(map[string]any); value["legacy_reset_required"] != true || value["legacy_main_task_id"] != mainID || value["legacy_automation_id"] != legacyAutomationID || value["legacy_automation_name"] != legacyAutomationName || value["legacy_automation_kind"] != legacyAutomationKind || value["legacy_automation_target_thread_id"] != mainID {
		t.Fatalf("legacy preview = %#v", value)
	}
	if got, err := os.ReadFile(hooks); err != nil || string(got) != legacyHooks {
		t.Fatalf("legacy preview changed hooks = %q, %v", got, err)
	}
	if _, err := install(context.Background(), installOptions{Confirmed: true}); err == nil || !strings.Contains(err.Error(), "--reset") {
		t.Fatalf("legacy install without reset = %v", err)
	}
	if _, err := os.Stat(p.binary); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("refused reset wrote binary: %v", err)
	}
	if _, err := install(context.Background(), installOptions{Confirmed: true, Reset: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(stateDir(), "native.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("legacy state remains: %v", err)
	}
	if got, err := os.ReadFile(hooks); err != nil || strings.Contains(string(got), legacyCommand) || !strings.Contains(string(got), `"command": "mine"`) || !strings.Contains(string(got), `"command": "stop"`) {
		t.Fatalf("legacy hook cleanup = %q, %v", got, err)
	}
	if _, err := install(context.Background(), installOptions{Confirmed: true, Reset: true}); err == nil {
		t.Fatal("reset without legacy state succeeded")
	}
	mustWrite(t, filepath.Join(stateDir(), "native.json"), `{"format":3,"tasks":{}}`)
	if _, err := install(context.Background(), installOptions{Confirmed: true, Reset: true}); err == nil || !strings.Contains(err.Error(), "not exact") {
		t.Fatalf("unsupported legacy reset = %v", err)
	}
	if _, err := os.Stat(filepath.Join(stateDir(), "native.json")); err != nil {
		t.Fatalf("refused reset deleted unknown state: %v", err)
	}
}

func TestInstallRejectsLegacyStateWithoutMainTaskIdentity(t *testing.T) {
	p := isolatedLifecycle(t)
	native := filepath.Join(stateDir(), "native.json")
	mustWrite(t, native, `{"format":4,"main_task_id":"","tasks":{}}`)
	for _, options := range []installOptions{{DryRun: true}, {Confirmed: true, Reset: true}} {
		if _, err := install(context.Background(), options); err == nil || !strings.Contains(err.Error(), "not exact") {
			t.Fatalf("legacy state without task identity = %v", err)
		}
		if _, err := os.Stat(p.binary); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("refused reset wrote binary: %v", err)
		}
		if _, err := os.Stat(native); err != nil {
			t.Fatalf("refused reset removed legacy state: %v", err)
		}
	}
}

func TestResetRerunsAfterPartialInstallWithoutDeletingNewSubjects(t *testing.T) {
	p := isolatedLifecycle(t)
	mainID := "019fdcbf-d225-7e00-9779-2472e54532e3"
	mustWrite(t, filepath.Join(stateDir(), "native.json"), `{"format":4,"main_task_id":"`+mainID+`","tasks":{}}`)
	fake := currentFakeLaunchctl(t)
	fake.bootstrapErr = errors.New("bootstrap unavailable")
	result, err := install(context.Background(), installOptions{Confirmed: true, Reset: true})
	partial := result.(map[string]any)
	if err == nil || partial["dry_run"] != false || partial["partial"] != true || partial["stage"] != "updater" || partial["restart_required"] != true || partial["safe_rerun"] != "repeat the same confirmed install command" {
		t.Fatalf("partial reset = %#v, %v", result, err)
	}
	if _, err := os.Stat(filepath.Join(stateDir(), "native.json")); err != nil {
		t.Fatalf("partial reset removed legacy admission state: %v", err)
	}
	subjectID := "019fc53a-4aa6-7221-ad51-165301675116"
	subjectPath := filepath.Join(legacySubjectDir(), subjectID+".json")
	mustWrite(t, subjectPath, `{"subject":"Keep this subject"}`+"\n")
	fake.bootstrapErr = nil
	if _, err := install(context.Background(), installOptions{Confirmed: true, Reset: true}); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(subjectPath); err != nil || string(got) != `{"subject":"Keep this subject"}`+"\n" {
		t.Fatalf("rerun subject = %q, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(stateDir(), "native.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("completed reset retained legacy state: %v", err)
	}
	if !regularExecutable(p.binary) {
		t.Fatal("completed reset did not install binary")
	}
}

func TestResetPostCleanupFailureUsesOrdinaryConfirmedRerun(t *testing.T) {
	p := isolatedLifecycle(t)
	mainID := "019fdcbf-d225-7e00-9779-2472e54532e3"
	native := filepath.Join(stateDir(), "native.json")
	mustWrite(t, native, `{"format":4,"main_task_id":"`+mainID+`","tasks":{}}`)
	oldPostResetStatus := postResetStatus
	postResetStatus = func(ctx context.Context) (any, error) {
		result, _ := status(ctx)
		return result, errors.New("post-cleanup status unavailable")
	}
	t.Cleanup(func() { postResetStatus = oldPostResetStatus })

	result, err := install(context.Background(), installOptions{Confirmed: true, Reset: true})
	partial := result.(map[string]any)
	wantRerun := confirmedInstallRerun(p)
	if err == nil || partial["partial"] != true || partial["stage"] != "status" || partial["legacy_reset_required"] != false || partial["safe_rerun"] != wantRerun {
		t.Fatalf("post-cleanup reset partial = %#v, %v; rerun want %q", partial, err, wantRerun)
	}
	if _, err := os.Stat(native); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("committed reset retained legacy admission state: %v", err)
	}
	if !regularExecutable(p.binary) {
		t.Fatal("post-cleanup reset failure removed the installed binary")
	}

	postResetStatus = oldPostResetStatus
	if _, err := install(context.Background(), installOptions{Confirmed: true}); err != nil {
		t.Fatalf("ordinary confirmed rerun failed: %v", err)
	}
	if result, err := status(context.Background()); err != nil || result.(map[string]any)["ready"] != true {
		t.Fatalf("ordinary confirmed rerun status = %#v, %v", result, err)
	}
}

func TestInstallSerializesBehindUpdateCheckLock(t *testing.T) {
	isolatedLifecycle(t)
	lock, err := lifecycleLock("update.lock")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, installErr := install(context.Background(), installOptions{Confirmed: true})
		done <- installErr
	}()
	select {
	case err := <-done:
		unlock(lock)
		t.Fatalf("manual install bypassed update.lock: %v", err)
	case <-time.After(50 * time.Millisecond):
		unlock(lock)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
}

func TestInstallDryRunRefusesCollidingLeaves(t *testing.T) {
	tests := map[string]func(*testing.T, lifecyclePaths){
		"foreign binary": func(t *testing.T, p lifecyclePaths) {
			mustWrite(t, p.binary, "#!/bin/sh\nexit 0\n")
			if err := os.Chmod(p.binary, 0o755); err != nil {
				t.Fatal(err)
			}
		},
		"agents symlink": func(t *testing.T, p lifecyclePaths) {
			target := filepath.Join(t.TempDir(), "AGENTS.md")
			mustWrite(t, target, "mine")
			if err := os.Symlink(target, p.agents); err != nil {
				t.Fatal(err)
			}
		},
		"skill directory leaf": func(t *testing.T, p lifecyclePaths) {
			if err := os.MkdirAll(p.skill, 0o700); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, prepare := range tests {
		t.Run(name, func(t *testing.T) {
			p := isolatedLifecycle(t)
			prepare(t, p)
			if _, err := install(context.Background(), installOptions{DryRun: true}); err == nil {
				t.Fatal("colliding dry run succeeded")
			}
			if _, err := os.Stat(p.launchAgent); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("dry run mutated updater: %v", err)
			}
		})
	}
}

func TestAutomaticInstallRefusesLegacyAndPostUninstallState(t *testing.T) {
	p := isolatedLifecycle(t)
	if _, err := install(context.Background(), installOptions{Confirmed: true}); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(p.binary)
	mustWrite(t, filepath.Join(stateDir(), "native.json"), `{"format":4,"tasks":{}}`)
	if _, err := install(context.Background(), installOptions{Confirmed: true, Automatic: true}); err == nil {
		t.Fatal("automatic install accepted legacy state")
	}
	after, _ := os.ReadFile(p.binary)
	if !bytes.Equal(before, after) {
		t.Fatal("refused automatic install replaced binary")
	}
	if err := os.Remove(filepath.Join(stateDir(), "native.json")); err != nil {
		t.Fatal(err)
	}

	lock, err := lifecycleLock("lifecycle.lock")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, installErr := install(context.Background(), installOptions{Confirmed: true, Automatic: true})
		done <- installErr
	}()
	select {
	case err := <-done:
		unlock(lock)
		t.Fatalf("automatic install bypassed lifecycle lock: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	if err := os.RemoveAll(stateDir()); err != nil {
		unlock(lock)
		t.Fatal(err)
	}
	if err := os.Remove(p.binary); err != nil {
		unlock(lock)
		t.Fatal(err)
	}
	unlock(lock)
	if err := <-done; err == nil || !strings.Contains(err.Error(), "lifecycle changed while the operation was waiting") {
		t.Fatalf("post-uninstall automatic install = %v", err)
	}
	if _, err := os.Stat(p.binary); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("automatic update reinstalled binary: %v", err)
	}
}

func TestAutomaticInstallFailureLeavesOldBinaryAndIsRerunnable(t *testing.T) {
	p := isolatedLifecycle(t)
	if _, err := install(context.Background(), installOptions{Confirmed: true}); err != nil {
		t.Fatal(err)
	}
	oldBinary := []byte("#!/bin/sh\necho old\n")
	if err := os.WriteFile(p.binary, oldBinary, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, p.skill, "old managed skill\n")
	fake := currentFakeLaunchctl(t)
	fake.loaded = false
	fake.bootstrapErr = errors.New("bootstrap unavailable")
	result, err := install(context.Background(), installOptions{Confirmed: true, Automatic: true})
	partial := result.(map[string]any)
	if err == nil || partial["dry_run"] != false || partial["partial"] != true || partial["stage"] != "updater" || partial["restart_required"] != true || partial["safe_rerun"] != "'"+p.binary+"' update --json" {
		t.Fatalf("automatic partial install = %#v, %v", result, err)
	}
	if got, err := os.ReadFile(p.binary); err != nil || !bytes.Equal(got, oldBinary) {
		t.Fatalf("failed automatic install binary = %q, %v", got, err)
	}
	fake.bootstrapErr = nil
	if _, err := install(context.Background(), installOptions{Confirmed: true, Automatic: true}); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(p.binary); err != nil || bytes.Equal(got, oldBinary) {
		t.Fatalf("rerun did not replace binary: %v", err)
	}
}

func TestStatusDoesNotReadCodexDatabase(t *testing.T) {
	p := isolatedLifecycle(t)
	if _, err := install(context.Background(), installOptions{Confirmed: true}); err != nil {
		t.Fatal(err)
	}
	matches, _ := filepath.Glob(filepath.Join(codexHome(), "state_*.sqlite"))
	for _, match := range matches {
		if err := os.Remove(match); err != nil {
			t.Fatal(err)
		}
	}
	result, err := status(context.Background())
	if err != nil || result.(map[string]any)["ready"] != true || result.(map[string]any)["automatic_updates_enabled"] != true {
		t.Fatalf("DB-independent status = %#v, %v", result, err)
	}
	if !regularExecutable(p.binary) {
		t.Fatal("installed binary disappeared")
	}
	mustWrite(t, filepath.Join(legacySubjectDir(), "corrupt.json"), "not-json")
	mustWrite(t, p.updateReceipt, "not-json")
	fake := currentFakeLaunchctl(t)
	fake.mu.Lock()
	fake.loaded = false
	fake.mu.Unlock()
	if err := os.Remove(p.launchAgent); err != nil {
		t.Fatal(err)
	}
	result, err = status(context.Background())
	value := result.(map[string]any)
	if err != nil || value["ready"] != true || value["installed"] != true || value["automatic_updates_enabled"] != false || value["update_receipt_error"] == nil {
		t.Fatalf("local status isolation = %#v, %v", value, err)
	}
}

func TestStatusRequiresPrivateRuntimeFenceAndNoLegacyState(t *testing.T) {
	isolatedLifecycle(t)
	if _, err := install(context.Background(), installOptions{Confirmed: true}); err != nil {
		t.Fatal(err)
	}
	lock := filepath.Join(stateDir(), "lifecycle.lock")
	if err := os.Remove(lock); err != nil {
		t.Fatal(err)
	}
	if result, err := status(context.Background()); err == nil || result.(map[string]any)["ready"] != false || result.(map[string]any)["installed"] != true {
		t.Fatalf("status accepted missing lifecycle fence: %#v, %v", result, err)
	}
	if _, err := install(context.Background(), installOptions{Confirmed: true}); err != nil {
		t.Fatalf("reinstall did not repair lifecycle fence: %v", err)
	}
	if err := os.Chmod(stateDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if result, err := status(context.Background()); err == nil || result.(map[string]any)["ready"] != false {
		t.Fatalf("status accepted public state root: %#v, %v", result, err)
	}
	if err := os.Chmod(stateDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(stateDir(), "native.json"), `{"format":4,"main_task_id":"019fdcbf-d225-7e00-9779-2472e54532e3","tasks":{}}`)
	if result, err := status(context.Background()); err == nil || result.(map[string]any)["ready"] != false || result.(map[string]any)["artifacts"].(map[string]bool)["legacy_state_absent"] {
		t.Fatalf("status accepted legacy state: %#v, %v", result, err)
	}
}

func TestStatusSeparatesPhysicalBinaryPresenceFromReadiness(t *testing.T) {
	p := isolatedLifecycle(t)
	if _, err := install(context.Background(), installOptions{Confirmed: true}); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p.binary, 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := status(context.Background())
	value := result.(map[string]any)
	if err == nil || value["ready"] != false || value["installed"] != true || value["artifacts"].(map[string]bool)["binary"] {
		t.Fatalf("non-executable binary status = %#v, %v", value, err)
	}
	if err := os.Remove(p.binary); err != nil {
		t.Fatal(err)
	}
	result, err = status(context.Background())
	value = result.(map[string]any)
	if err == nil || value["ready"] != false || value["installed"] != false {
		t.Fatalf("absent binary status = %#v, %v", value, err)
	}
}

func TestUninstallReturnsCompleteCleanupPreviewAndPreparation(t *testing.T) {
	isolatedLifecycle(t)
	requests := stubPagedAppServer(t)
	if _, err := install(context.Background(), installOptions{Confirmed: true}); err != nil {
		t.Fatal(err)
	}
	result, err := uninstall(context.Background(), uninstallOptions{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	value := result.(map[string]any)
	if value["ready"] != true || value["read_only"] != true || value["plan_complete"] != true || value["total"] != 3 || value["needs_cleanup"] != 1 || value["prepared"] != 0 || value["unchanged"] != 1 || value["skipped"] != 1 {
		t.Fatalf("cleanup preview = %#v", value)
	}
	items := value["items"].([]cleanupItem)
	legacy := cleanupItemByID(t, items, testLegacyID)
	if legacy.Outcome != cleanupNeedsUpdate || legacy.Title != "✅ Maybe owned" || legacy.DesiredTitle != "Maybe owned" {
		t.Fatalf("cleanup items = %#v", items)
	}
	t.Setenv("CODEX_THREAD_ID", testLegacyID)
	preparedResult, err := uninstall(context.Background(), uninstallOptions{Prepare: true, Confirmed: true})
	if err != nil {
		t.Fatal(err)
	}
	prepared := preparedResult.(map[string]any)
	preparedItems := prepared["items"].([]cleanupItem)
	if prepared["ready"] != true || prepared["read_only"] != false || prepared["prepared"] != 1 || preparedItems[len(preparedItems)-1].TaskID != testLegacyID || !regularExecutable(installPaths().binary) {
		t.Fatalf("cleanup preparation = %#v", prepared)
	}
	data, err := os.ReadFile(requests)
	if err != nil || !strings.Contains(string(data), `"method":"initialize"`) || !strings.Contains(string(data), `"cursor":"next"`) {
		t.Fatalf("App Server requests = %q, %v", data, err)
	}
	if _, err := os.Stat(legacySubjectDir()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cleanup planning created obsolete subject state: %v", err)
	}
}

func stubPagedAppServer(t *testing.T) string {
	t.Helper()
	dir, requests := t.TempDir(), filepath.Join(t.TempDir(), "requests.jsonl")
	script := `#!/bin/sh
if [ "$1" = --version ]; then echo 'codex-cli 0.147.0'; exit 0; fi
[ "$1" = app-server ] && [ "$2" = --stdio ] || exit 80
count=0
while IFS= read -r line; do
  printf '%s\n' "$line" >> "$TB_APP_SERVER_REQUESTS"
  count=$((count + 1))
  case "$count" in
    1) printf '%s\n' '{"id":1,"result":{"serverInfo":{"name":"fake"}}}' ;;
    2) ;;
    3) printf '%s\n' '{"method":"thread/started","params":{}}'
       printf '%s\n' '{"id":2,"result":{"data":[{"id":"00000000-0000-0000-0000-00000000000d","name":"Exact subject","preview":"safe"},{"id":"00000000-0000-0000-0000-000000000006","name":null,"preview":"<codex_delegation> private"}],"nextCursor":"next"}}' ;;
    4) printf '%s\n' '{"id":3,"result":{"data":[{"id":"00000000-0000-0000-0000-00000000000c","name":"✅ Maybe owned","preview":"legacy"},{"id":"00000000-0000-0000-0000-00000000000d","name":"Exact subject","preview":"duplicate"}],"nextCursor":null}}' ;;
    *) exit 81 ;;
  esac
done
`
	path := filepath.Join(dir, "codex")
	mustWrite(t, path, script)
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
	previous := locateCodex
	locateCodex = func(context.Context) (codexCompatibility, error) {
		return codexCompatibility{Path: path, Version: "0.147.0"}, nil
	}
	t.Cleanup(func() { locateCodex = previous })
	t.Setenv("TB_APP_SERVER_REQUESTS", requests)
	return requests
}

func TestLaunchAgentIsSilentDailyExactAndIdempotent(t *testing.T) {
	p := isolatedLifecycle(t)
	fake := currentFakeLaunchctl(t)
	if _, err := install(context.Background(), installOptions{Confirmed: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := install(context.Background(), installOptions{Confirmed: true}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(p.launchAgent)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, required := range []string{updateAgentLabel, p.binary, "<string>update</string>", "<string>--automatic</string>", "<string>--json</string>", "StartCalendarInterval", "EnvironmentVariables", "<key>HOME</key>", homeDir(), "<key>CODEX_HOME</key>", codexHome(), "StandardOutPath", "StandardErrorPath", "/dev/null"} {
		if !strings.Contains(text, required) {
			t.Errorf("plist lacks %q", required)
		}
	}
	for _, forbidden := range []string{"RunAtLoad", "KeepAlive", "onboard", "CODEX_THREAD_ID"} {
		if strings.Contains(text, forbidden) {
			t.Errorf("plist contains %q", forbidden)
		}
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.bootstraps != 1 || fake.bootouts != 0 {
		t.Fatalf("launchctl calls = bootstraps %d bootouts %d", fake.bootstraps, fake.bootouts)
	}
}

func TestLoadedUpdateAgentDriftIsReportedWithoutBreakingCoreReadiness(t *testing.T) {
	tests := map[string]func(string, lifecyclePaths) string{
		"plist path": func(output string, p lifecyclePaths) string {
			return strings.Replace(output, "path = "+p.launchAgent, "path = /tmp/foreign.plist", 1)
		},
		"program": func(output string, p lifecyclePaths) string {
			return strings.Replace(output, "program = "+p.binary, "program = /tmp/foreign", 1)
		},
		"arguments": func(output string, _ lifecyclePaths) string {
			return strings.Replace(output, "\t\t--automatic\n", "\t\t--foreign\n", 1)
		},
	}
	for name, drift := range tests {
		t.Run(name, func(t *testing.T) {
			p := isolatedLifecycle(t)
			fake := currentFakeLaunchctl(t)
			if _, err := install(context.Background(), installOptions{Confirmed: true}); err != nil {
				t.Fatal(err)
			}
			fake.mu.Lock()
			fake.printOutput = []byte(drift(string(managedLaunchctlPrint(p.launchAgent, p.binary)), p))
			fake.mu.Unlock()

			result, err := status(context.Background())
			value := result.(map[string]any)
			if err != nil || value["ready"] != true || value["installed"] != true || value["automatic_updates_enabled"] != false || value["updater_error"] == nil {
				t.Fatalf("status with loaded updater drift = %#v, %v", value, err)
			}
			if _, err := install(context.Background(), installOptions{DryRun: true}); err == nil {
				t.Fatal("install preflight accepted a foreign loaded updater")
			}
			if _, err := uninstall(context.Background(), uninstallOptions{DryRun: true}); err == nil {
				t.Fatal("uninstall preflight accepted a foreign loaded updater")
			}
			if !regularExecutable(p.binary) {
				t.Fatal("refused lifecycle preflight removed the binary")
			}
			fake.mu.Lock()
			bootouts := fake.bootouts
			fake.mu.Unlock()
			if bootouts != 0 {
				t.Fatal("refused lifecycle preflight booted out the foreign job")
			}
		})
	}
}

func TestLoadedUpdateAgentOperationalPrintFailureIsNotAbsence(t *testing.T) {
	p := isolatedLifecycle(t)
	fake := currentFakeLaunchctl(t)
	if _, err := install(context.Background(), installOptions{Confirmed: true}); err != nil {
		t.Fatal(err)
	}
	notFound := exec.Command("/bin/sh", "-c", "exit 113").Run()
	fake.mu.Lock()
	fake.printErr = notFound
	fake.mu.Unlock()
	if loaded, err := updateAgentLoaded(context.Background(), p.launchAgent, p.binary); err != nil || loaded {
		t.Fatalf("launchctl service-not-found result = loaded %t, %v", loaded, err)
	}
	exitError := exec.Command("/bin/sh", "-c", "exit 1").Run()
	fake.mu.Lock()
	fake.printErr = exitError
	fake.mu.Unlock()
	result, err := status(context.Background())
	value := result.(map[string]any)
	if err != nil || value["ready"] != true || value["automatic_updates_enabled"] != false || value["updater_error"] == nil {
		t.Fatalf("status with launchctl failure = %#v, %v", value, err)
	}
	if _, err := uninstall(context.Background(), uninstallOptions{DryRun: true}); err == nil {
		t.Fatal("uninstall treated an operational launchctl failure as an absent job")
	}
	if !regularExecutable(p.binary) {
		t.Fatal("refused uninstall removed the binary")
	}
	fake.mu.Lock()
	bootouts := fake.bootouts
	fake.mu.Unlock()
	if bootouts != 0 {
		t.Fatal("refused uninstall tried to boot out an unverified job")
	}
}

func TestUninstallInvalidatesPreopenedLifecycleWaiter(t *testing.T) {
	p := isolatedLifecycle(t)
	fake := currentFakeLaunchctl(t)
	if _, err := install(context.Background(), installOptions{Confirmed: true}); err != nil {
		t.Fatal(err)
	}
	started, proceed := make(chan struct{}), make(chan struct{})
	fake.mu.Lock()
	fake.bootoutStarted, fake.continueBootout = started, proceed
	fake.mu.Unlock()
	uninstalled := make(chan error, 1)
	go func() {
		_, err := uninstall(context.Background(), uninstallOptions{Commit: true, Confirmed: true})
		uninstalled <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("uninstall did not reach updater teardown")
	}
	waiter := make(chan error, 1)
	go func() {
		lock, err := existingLifecycleLock("lifecycle.lock")
		if err == nil {
			unlock(lock)
		}
		waiter <- err
	}()
	select {
	case err := <-waiter:
		t.Fatalf("lifecycle waiter bypassed uninstall: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	installer := make(chan error, 1)
	go func() {
		_, err := install(context.Background(), installOptions{Confirmed: true})
		installer <- err
	}()
	select {
	case err := <-installer:
		t.Fatalf("installer bypassed uninstall boundary: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(proceed)
	if err := <-uninstalled; err != nil {
		t.Fatal(err)
	}
	if err := <-installer; err == nil || !strings.Contains(err.Error(), "changed while the operation was waiting") {
		t.Fatalf("installer with stale update lock = %v", err)
	}
	if err := <-waiter; err == nil || !strings.Contains(err.Error(), "changed while the operation was waiting") {
		t.Fatalf("preopened waiter = %v", err)
	}
	if _, err := os.Stat(p.binary); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("uninstall retained binary: %v", err)
	}
	if _, err := os.Stat(stateDir()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("normal uninstall retained state directory: %v", err)
	}
	lock, err := lifecycleLock("lifecycle.lock")
	if err != nil {
		t.Fatalf("fresh lifecycle could not start after teardown: %v", err)
	}
	unlock(lock)
}

func TestUninstallWaitsForInFlightUpdaterBeforeTeardown(t *testing.T) {
	p := isolatedLifecycle(t)
	fake := currentFakeLaunchctl(t)
	if _, err := install(context.Background(), installOptions{Confirmed: true}); err != nil {
		t.Fatal(err)
	}
	updateLock, err := updateCheckLock()
	if err != nil {
		t.Fatal(err)
	}
	locked := true
	t.Cleanup(func() {
		if locked {
			unlock(updateLock)
		}
	})
	done := make(chan error, 1)
	go func() {
		_, uninstallErr := uninstall(context.Background(), uninstallOptions{Commit: true, Confirmed: true})
		done <- uninstallErr
	}()
	select {
	case err := <-done:
		t.Fatalf("uninstall bypassed in-flight updater: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	fake.mu.Lock()
	bootouts := fake.bootouts
	fake.mu.Unlock()
	if bootouts != 0 {
		t.Fatal("uninstall began teardown while an updater was active")
	}
	unlock(updateLock)
	locked = false
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(p.binary); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("uninstall retained binary: %v", err)
	}
}

func TestUninstallRemovesOwnedArtifactsAndPreservesNeighbors(t *testing.T) {
	p := isolatedLifecycle(t)
	fake := currentFakeLaunchctl(t)
	foreignAgents := "# Mine\nkeep\n"
	mustWrite(t, p.agents, foreignAgents)
	hooks := filepath.Join(codexHome(), "hooks.json")
	wantHooks := []byte(`{"owner":"user","hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"mine"}]}]}}` + "\n")
	mustWrite(t, hooks, string(wantHooks))
	if _, err := install(context.Background(), installOptions{Confirmed: true}); err != nil {
		t.Fatal(err)
	}
	neighbor := filepath.Join(filepath.Dir(p.skill), "notes.md")
	mustWrite(t, neighbor, "preserve")
	stateNeighbor := filepath.Join(stateDir(), "user-note.txt")
	subjectNeighbor := filepath.Join(legacySubjectDir(), "user-note.txt")
	ownedID := "019fc53a-4aa6-7221-ad51-165301675116"
	ownedRecord := filepath.Join(legacySubjectDir(), ownedID+".json")
	ownedLock := filepath.Join(legacySubjectDir(), ownedID+".lock")
	mustWrite(t, stateNeighbor, "preserve state neighbor")
	mustWrite(t, subjectNeighbor, "preserve subject neighbor")
	mustWrite(t, ownedRecord, `{"subject":"Owned subject"}`+"\n")
	mustWrite(t, ownedLock, "")
	preview, err := uninstall(context.Background(), uninstallOptions{DryRun: true})
	if err != nil || preview.(map[string]any)["plan_complete"] != true || preview.(map[string]any)["icons_may_remain"] != nil {
		t.Fatalf("uninstall preview = %#v, %v", preview, err)
	}
	if _, err := uninstall(context.Background(), uninstallOptions{Commit: true, Confirmed: true}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{p.binary, p.skill, p.launchAgent} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("uninstall left %s: %v", path, err)
		}
	}
	for _, path := range []string{ownedRecord, ownedLock, p.updateReceipt, filepath.Join(stateDir(), "update.lock"), filepath.Join(stateDir(), "lifecycle.lock")} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("uninstall left owned state %s: %v", path, err)
		}
	}
	if got, err := os.ReadFile(neighbor); err != nil || string(got) != "preserve" {
		t.Fatalf("skill neighbor = %q, %v", got, err)
	}
	if got, err := os.ReadFile(stateNeighbor); err != nil || string(got) != "preserve state neighbor" {
		t.Fatalf("state neighbor = %q, %v", got, err)
	}
	if got, err := os.ReadFile(subjectNeighbor); err != nil || string(got) != "preserve subject neighbor" {
		t.Fatalf("subject neighbor = %q, %v", got, err)
	}
	if got, err := os.ReadFile(p.agents); err != nil || string(got) != foreignAgents {
		t.Fatalf("foreign AGENTS = %q, %v", got, err)
	}
	if got, err := os.ReadFile(hooks); err != nil || !bytes.Equal(got, wantHooks) {
		t.Fatalf("foreign hooks = %q, %v", got, err)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.bootouts != 1 || fake.loaded {
		t.Fatalf("updater not removed first: %#v", fake)
	}
}

func TestUninstallDryRunRequiresCurrentInstallWithoutMutation(t *testing.T) {
	p := isolatedLifecycle(t)
	result, err := uninstall(context.Background(), uninstallOptions{DryRun: true})
	if err == nil || result.(map[string]any)["dry_run"] != true {
		t.Fatalf("missing install preview = %#v, %v", result, err)
	}
	for _, path := range []string{p.binary, p.agents, p.skill, p.launchAgent, stateDir()} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("uninstall preview created %s: %v", path, err)
		}
	}
}

func TestLifecycleIsolatesCorruptOwnedStateAndPreservesNeighbors(t *testing.T) {
	p := isolatedLifecycle(t)
	if _, err := install(context.Background(), installOptions{Confirmed: true}); err != nil {
		t.Fatal(err)
	}
	subjectDir := legacySubjectDir()
	owned := filepath.Join(subjectDir, "019fc53a-4aa6-7221-ad51-165301675116.json")
	unknownJSON := filepath.Join(subjectDir, "unknown.json")
	unknownLock := filepath.Join(subjectDir, "unknown.lock")
	link := filepath.Join(subjectDir, "note")
	target := filepath.Join(t.TempDir(), "note")
	mustWrite(t, owned, "not-json")
	mustWrite(t, unknownJSON, `{}`)
	mustWrite(t, unknownLock, "mine")
	mustWrite(t, target, "mine")
	mustWrite(t, p.updateReceipt, "not-json")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	if _, err := install(context.Background(), installOptions{Confirmed: true}); err != nil {
		t.Fatalf("isolated corruption blocked reinstall: %v", err)
	}
	if _, err := uninstall(context.Background(), uninstallOptions{DryRun: true}); err != nil {
		t.Fatalf("isolated corruption blocked uninstall preview: %v", err)
	}
	if _, err := uninstall(context.Background(), uninstallOptions{Commit: true, Confirmed: true}); err != nil {
		t.Fatalf("isolated corruption blocked uninstall: %v", err)
	}
	for _, path := range []string{owned, p.updateReceipt, p.binary} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("uninstall retained owned path %s: %v", path, err)
		}
	}
	for path, want := range map[string]string{unknownJSON: `{}`, unknownLock: "mine", target: "mine"} {
		if got, err := os.ReadFile(path); err != nil || string(got) != want {
			t.Fatalf("preserved neighbor %s = %q, %v", path, got, err)
		}
	}
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("preserved symlink = %v, %v", info, err)
	}
}

func TestUninstallRefusesUnsafeOwnedSubjectLeafBeforeMutation(t *testing.T) {
	p := isolatedLifecycle(t)
	if _, err := install(context.Background(), installOptions{Confirmed: true}); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "mine")
	mustWrite(t, target, "mine")
	if err := os.MkdirAll(legacySubjectDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	ownedLink := filepath.Join(legacySubjectDir(), "019fc53a-4aa6-7221-ad51-165301675116.lock")
	if err := os.Symlink(target, ownedLink); err != nil {
		t.Fatal(err)
	}
	if _, err := uninstall(context.Background(), uninstallOptions{DryRun: true}); err == nil || !strings.Contains(err.Error(), "owned subject path") {
		t.Fatalf("unsafe owned leaf preview = %v", err)
	}
	if _, err := os.Stat(p.binary); err != nil {
		t.Fatalf("failed preflight removed binary: %v", err)
	}
	fake := currentFakeLaunchctl(t)
	fake.mu.Lock()
	loaded := fake.loaded
	fake.mu.Unlock()
	if !loaded {
		t.Fatal("failed preflight booted out updater")
	}
}

func TestUninstallPartialNamesStageAndSafeRerun(t *testing.T) {
	p := isolatedLifecycle(t)
	if _, err := install(context.Background(), installOptions{Confirmed: true}); err != nil {
		t.Fatal(err)
	}
	fake := currentFakeLaunchctl(t)
	fake.bootoutStarted = make(chan struct{})
	continueBootout := make(chan struct{})
	fake.continueBootout = continueBootout
	type outcome struct {
		result any
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := uninstall(context.Background(), uninstallOptions{Commit: true, Confirmed: true})
		done <- outcome{result: result, err: err}
	}()
	<-fake.bootoutStarted
	backup := codexHome() + ".backup"
	if err := os.Rename(codexHome(), backup); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, codexHome(), "collision")
	close(continueBootout)
	got := <-done
	partial := got.result.(map[string]any)
	if got.err == nil || partial["partial"] != true || partial["stage"] != "managed_guidance" || partial["restart_required"] != true || partial["safe_rerun"] != "'"+p.binary+"' uninstall --commit --noninteractive --confirm --json" {
		t.Fatalf("uninstall partial = %#v, %v", partial, got.err)
	}
	if _, err := os.Stat(p.binary); err != nil {
		t.Fatalf("partial uninstall removed binary: %v", err)
	}
	if err := os.Remove(codexHome()); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(backup, codexHome()); err != nil {
		t.Fatal(err)
	}
	fence, err := existingLifecycleLock("lifecycle.lock")
	if err != nil {
		t.Fatal(err)
	}
	rerun := make(chan error, 1)
	go func() {
		_, err := uninstall(context.Background(), uninstallOptions{Commit: true, Confirmed: true})
		rerun <- err
	}()
	select {
	case err := <-rerun:
		unlock(fence)
		t.Fatalf("partial rerun bypassed active title fence: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	unlock(fence)
	if err := <-rerun; err != nil {
		t.Fatalf("safe rerun failed: %v", err)
	}
}

func TestUninstallLateBinaryFailureKeepsConfirmedRerunAdmissible(t *testing.T) {
	p := isolatedLifecycle(t)
	if _, err := install(context.Background(), installOptions{Confirmed: true}); err != nil {
		t.Fatal(err)
	}
	running, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(p.binary); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(running, p.binary); err != nil {
		t.Fatal(err)
	}
	binDir := filepath.Dir(p.binary)
	if err := os.Chmod(binDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(binDir, 0o700) })

	result, err := uninstall(context.Background(), uninstallOptions{Commit: true, Confirmed: true})
	partial := result.(map[string]any)
	if err == nil || partial["partial"] != true || partial["stage"] != "binary" || partial["safe_rerun"] != uninstallRerun(p) {
		t.Fatalf("late uninstall partial = %#v, %v", partial, err)
	}
	if !regularExecutable(p.binary) {
		t.Fatal("failed binary unlink left no callable rerun")
	}
	if partial, err := preflightUninstall(context.Background(), p); err != nil || !partial {
		t.Fatalf("failed binary unlink was not admitted as a self-binary rerun: partial=%t, err=%v", partial, err)
	}
	for _, path := range []string{p.skill, p.launchAgent} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("late partial retained removed surface %s: %v", path, err)
		}
	}
	if err := os.MkdirAll(filepath.Dir(p.skill), 0o700); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, p.skill, "foreign replacement")
	if result, err := uninstall(context.Background(), uninstallOptions{Commit: true, Confirmed: true}); err == nil || result.(map[string]any)["partial"] != false {
		t.Fatalf("partial rerun removed a replacement skill: %#v, %v", result, err)
	}
	if got, err := os.ReadFile(p.skill); err != nil || string(got) != "foreign replacement" {
		t.Fatalf("replacement skill = %q, %v", got, err)
	}
	if err := os.Remove(p.skill); err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(filepath.Dir(p.skill))

	if err := os.Chmod(binDir, 0o700); err != nil {
		t.Fatal(err)
	}
	result, err = uninstall(context.Background(), uninstallOptions{Commit: true, Confirmed: true})
	if err != nil || result.(map[string]any)["uninstalled"] != true {
		t.Fatalf("confirmed uninstall rerun = %#v, %v", result, err)
	}
	for _, path := range []string{p.binary, stateDir()} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("confirmed rerun retained %s: %v", path, err)
		}
	}
}

func TestUninstallRemovesDriftedOwnedSurfaceAndPreservesNeighbors(t *testing.T) {
	p := isolatedLifecycle(t)
	mustWrite(t, p.agents, "# Mine\nkeep\n")
	if _, err := install(context.Background(), installOptions{Confirmed: true}); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, p.skill, assets.SkillManagedContent+"edited\n")
	neighbor := filepath.Join(filepath.Dir(p.skill), "notes.md")
	mustWrite(t, neighbor, "preserve")
	agents, err := os.ReadFile(p.agents)
	if err != nil {
		t.Fatal(err)
	}
	drifted := strings.Replace(string(agents), "For every ordinary interactive turn", "For every edited interactive turn", 1)
	if drifted == string(agents) {
		t.Fatal("managed AGENTS fixture did not contain expected text")
	}
	mustWrite(t, p.agents, drifted)
	if _, err := uninstall(context.Background(), uninstallOptions{Commit: true, Confirmed: true}); err != nil {
		t.Fatalf("drifted uninstall = %v", err)
	}
	for _, path := range []string{p.binary, p.skill, p.launchAgent} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("drifted uninstall retained %s: %v", path, err)
		}
	}
	if got, err := os.ReadFile(neighbor); err != nil || string(got) != "preserve" {
		t.Fatalf("skill neighbor = %q, %v", got, err)
	}
	if got, err := os.ReadFile(p.agents); err != nil || string(got) != "# Mine\nkeep\n" {
		t.Fatalf("AGENTS neighbor content = %q, %v", got, err)
	}
}

func TestUninstallRefusesMalformedMarkersAndUnsafeSkillLeaf(t *testing.T) {
	for name, mutate := range map[string]func(*testing.T, lifecyclePaths){
		"missing marker": func(t *testing.T, p lifecyclePaths) {
			data, _ := os.ReadFile(p.agents)
			mustWrite(t, p.agents, strings.Replace(string(data), blockEnd, "", 1))
		},
		"duplicate marker": func(t *testing.T, p lifecyclePaths) {
			data, _ := os.ReadFile(p.agents)
			mustWrite(t, p.agents, string(data)+"\n"+blockStart+"\n")
		},
		"skill symlink": func(t *testing.T, p lifecyclePaths) {
			target := filepath.Join(t.TempDir(), "mine")
			mustWrite(t, target, "mine")
			if err := os.Remove(p.skill); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, p.skill); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			p := isolatedLifecycle(t)
			if _, err := install(context.Background(), installOptions{Confirmed: true}); err != nil {
				t.Fatal(err)
			}
			mutate(t, p)
			if _, err := uninstall(context.Background(), uninstallOptions{Commit: true, Confirmed: true}); err == nil {
				t.Fatal("unsafe uninstall preflight succeeded")
			}
			if _, err := os.Stat(p.binary); err != nil {
				t.Fatalf("failed preflight removed binary: %v", err)
			}
		})
	}
}

func TestInstallPreflightRefusesSkillDirectorySymlink(t *testing.T) {
	p := isolatedLifecycle(t)
	target := t.TempDir()
	userFile := filepath.Join(target, "notes.md")
	mustWrite(t, userFile, "preserve")
	if err := os.MkdirAll(filepath.Dir(filepath.Dir(p.skill)), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Dir(p.skill)); err != nil {
		t.Fatal(err)
	}
	if _, err := install(context.Background(), installOptions{DryRun: true}); err == nil || !strings.Contains(err.Error(), "managed parent") {
		t.Fatalf("symlinked dry run = %v", err)
	}
	if _, err := install(context.Background(), installOptions{Confirmed: true}); err == nil || !strings.Contains(err.Error(), "managed parent") {
		t.Fatalf("symlinked install = %v", err)
	}
	if info, err := os.Lstat(filepath.Dir(p.skill)); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("skill directory symlink = %#v, %v", info, err)
	}
	if got, err := os.ReadFile(userFile); err != nil || string(got) != "preserve" {
		t.Fatalf("skill neighbor = %q, %v", got, err)
	}
}

func isolatedLifecycle(t *testing.T) lifecyclePaths {
	t.Helper()
	root, db := testIndex(t)
	for _, id := range []string{testInstallerID, testMainID, testFirstID, testSecondID} {
		addTask(t, db, root, id, "Original "+id+" title", nil, "vscode", 0)
	}
	if err := os.RemoveAll(stateDir()); err != nil {
		t.Fatal(err)
	}
	stubLaunchctl(t)
	return installPaths()
}

var fakeLaunchctlByTest sync.Map

func stubLaunchctl(t *testing.T) *fakeLaunchctl {
	t.Helper()
	fake := &fakeLaunchctl{}
	old := launchctlRunner
	launchctlRunner = fake.run
	fakeLaunchctlByTest.Store(t, fake)
	t.Cleanup(func() {
		launchctlRunner = old
		fakeLaunchctlByTest.Delete(t)
	})
	return fake
}

func currentFakeLaunchctl(t *testing.T) *fakeLaunchctl {
	t.Helper()
	value, ok := fakeLaunchctlByTest.Load(t)
	if !ok {
		t.Fatal("launchctl was not stubbed")
	}
	return value.(*fakeLaunchctl)
}

func mustWrite(t *testing.T, path, value string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
		t.Fatal(err)
	}
}
