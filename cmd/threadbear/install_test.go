package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ericlitman/threadbear/assets"
)

func TestInstallReinstallAndUninstallPreserveForeignHooks(t *testing.T) {
	p := isolatedLifecycle(t)
	foreignAgents := "# Mine\nkeep this exactly\n"
	mustWrite(t, p.agents, foreignAgents)
	preA := json.RawMessage(`{"matcher":"Bash","hooks":[{"type":"command","command":"a"}],"extension":{"n":1}}`)
	preB := json.RawMessage(`{"hooks":[{"command":"b","timeout":99,"type":"command"}]}`)
	postA := json.RawMessage(`{"matcher":"","hooks":[{"type":"command","command":"c"}]}`)
	writeHookFixture(t, p.hooks, preA, preB, postA)
	if _, err := install("installer", false, true, false); err != nil {
		t.Fatal(err)
	}
	assertHookOrder(t, p.hooks, "PreToolUse", []json.RawMessage{preA, preB}, p.binary)
	assertHookOrder(t, p.hooks, "PostToolUse", []json.RawMessage{postA}, p.binary)
	skill, _ := os.ReadFile(p.skill)
	if !strings.HasPrefix(string(skill), "---\n") {
		t.Fatalf("installed skill lost YAML frontmatter: %q", skill)
	}
	firstHooks, _ := os.ReadFile(p.hooks)
	if _, err := status(context.Background()); err != nil {
		t.Fatalf("installed status: %v", err)
	}
	if err := manageBlock(p.agents, ""); err != nil {
		t.Fatal(err)
	}
	if err := manageBlock(p.agents, assets.AgentsManagedContent); err != nil {
		t.Fatal(err)
	}

	if _, err := install("installer", false, true, false); err != nil {
		t.Fatal(err)
	}
	secondHooks, _ := os.ReadFile(p.hooks)
	if !reflect.DeepEqual(firstHooks, secondHooks) {
		t.Fatal("reinstall rewrote an already-correct hooks.json")
	}
	if err := newStore(stateDir()).update(func(value *state) (bool, error) {
		value.Phase = phaseMigrationComplete
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := uninstall(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if _, err := uninstall(context.Background(), true); err != nil {
		t.Fatalf("repeated uninstall: %v", err)
	}
	assertHookOrder(t, p.hooks, "PreToolUse", []json.RawMessage{preA, preB}, "")
	assertHookOrder(t, p.hooks, "PostToolUse", []json.RawMessage{postA}, "")
	agents, _ := os.ReadFile(p.agents)
	if string(agents) != foreignAgents {
		t.Fatalf("foreign AGENTS content changed: %q", agents)
	}
	for _, path := range []string{p.binary, p.skill, stateDir()} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("uninstall left %s: %v", path, err)
		}
	}
}

func TestInstallDryRunAndConfirmationDoNotMutate(t *testing.T) {
	p := isolatedLifecycle(t)
	if _, err := install("installer", true, false, false); err != nil {
		t.Fatal(err)
	}
	if _, err := install("installer", false, false, false); err == nil {
		t.Fatal("unconfirmed install succeeded")
	}
	for _, path := range []string{p.binary, p.agents, p.skill, p.hooks, stateDir()} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("non-mutating install created %s: %v", path, err)
		}
	}
}

func TestReinstallRefusesLegacyPendingTitle(t *testing.T) {
	p := isolatedLifecycle(t)
	if _, err := install("installer", false, true, false); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(p.binary)
	if err := newStore(stateDir()).update(func(value *state) (bool, error) {
		value.Tasks["legacy"] = taskState{Pending: &pendingProposal{Prior: "Old", Proposed: "New"}}
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := install("installer", false, true, false); err == nil || !strings.Contains(err.Error(), "title operations") {
		t.Fatalf("reinstall with legacy pending = %v", err)
	}
	after, _ := os.ReadFile(p.binary)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("blocked reinstall replaced the binary")
	}
}

func TestReinstallUpgradesLegacyFormatBeforeOldHookCanWrite(t *testing.T) {
	isolatedLifecycle(t)
	if _, err := install("installer", false, true, false); err != nil {
		t.Fatal(err)
	}
	if err := newStore(stateDir()).update(func(value *state) (bool, error) {
		value.Format = 3
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(newStore(stateDir()).path())
	var onDisk state
	if json.Unmarshal(data, &onDisk) != nil || onDisk.Format != 3 {
		t.Fatalf("legacy fixture = %#v", onDisk)
	}
	if _, err := install("installer", false, true, false); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(newStore(stateDir()).path())
	if json.Unmarshal(data, &onDisk) != nil || onDisk.Format != stateFormat {
		t.Fatalf("upgraded state = %#v", onDisk)
	}
	if onDisk.Format == 3 {
		t.Fatal("an already-queued v2.2.0 hook would still accept the replaced state")
	}
}

func TestFailedLegacyReinstallLeavesOldReadableState(t *testing.T) {
	p := isolatedLifecycle(t)
	if _, err := install("installer", false, true, false); err != nil {
		t.Fatal(err)
	}
	if err := newStore(stateDir()).update(func(value *state) (bool, error) {
		value.Format = 3
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(p.binary)
	binDir := filepath.Dir(p.binary)
	if err := os.Chmod(binDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(binDir, 0o700) })
	if _, err := install("installer", false, true, false); err == nil {
		t.Fatal("reinstall unexpectedly replaced a binary in a read-only directory")
	}
	if err := os.Chmod(binDir, 0o700); err != nil {
		t.Fatal(err)
	}
	value, err := newStore(stateDir()).read()
	after, _ := os.ReadFile(p.binary)
	if err != nil || value.Format != 3 || !reflect.DeepEqual(before, after) {
		t.Fatalf("failed replacement changed old-readable installation: format=%d binary_equal=%v err=%v", value.Format, reflect.DeepEqual(before, after), err)
	}
}

func TestUninstallWaitsForOperationLockBeforeDeleting(t *testing.T) {
	p := isolatedLifecycle(t)
	if _, err := install("installer", false, true, false); err != nil {
		t.Fatal(err)
	}
	if err := newStore(stateDir()).update(func(value *state) (bool, error) {
		value.Phase = phaseMigrationComplete
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	operationLock, err := newStore(stateDir()).operationLock()
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := uninstall(context.Background(), true)
		done <- err
	}()
	select {
	case err := <-done:
		unlock(operationLock)
		t.Fatalf("uninstall returned while operation lock was held: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	for _, path := range []string{p.binary, p.skill, stateDir()} {
		if _, err := os.Stat(path); err != nil {
			unlock(operationLock)
			t.Fatalf("uninstall deleted %s while operation lock was held: %v", path, err)
		}
	}
	unlock(operationLock)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("uninstall did not resume after operation lock was released")
	}
	for _, path := range []string{p.binary, p.skill, stateDir()} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("uninstall left %s after operation lock release: %v", path, err)
		}
	}
}

func TestOperationLockDoesNotRecreateRemovedInstallation(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	store := newStore(dir)
	operationLock, err := store.waitLock()
	if err != nil {
		t.Fatal(err)
	}
	defer unlock(operationLock)
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if lock, err := store.operationLock(); err == nil {
		unlock(lock)
		t.Fatal("operation lock recreated an installation while uninstall held the removed lock inode")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("operation lock left a replacement state directory: %v", err)
	}
}

func TestInstallDebugCanariesAreExplicitOptIn(t *testing.T) {
	isolatedLifecycle(t)
	ordinary, err := install("installer", true, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ordinary.(map[string]any)["debug_canaries"]; ok {
		t.Fatal("ordinary install result disclosed debug canaries")
	}
	debug, err := install("installer", true, false, true)
	if err != nil {
		t.Fatal(err)
	}
	if debug.(map[string]any)["debug_canaries"] != true {
		t.Fatalf("debug install result = %#v", debug)
	}
}

func TestStatusRejectsModifiedManagedGuidance(t *testing.T) {
	p := isolatedLifecycle(t)
	if _, err := install("main", false, true, false); err != nil {
		t.Fatal(err)
	}
	agents, _ := os.ReadFile(p.agents)
	skill, _ := os.ReadFile(p.skill)
	for name, change := range map[string]func(){
		"agents": func() {
			mustWrite(t, p.agents, strings.Replace(string(agents), "# ThreadBear", "# ThreadBear edited", 1))
		},
		"skill": func() { mustWrite(t, p.skill, string(skill)+"edited\n") },
	} {
		t.Run(name, func(t *testing.T) {
			change()
			if _, err := status(context.Background()); err == nil {
				t.Fatalf("status accepted modified %s", name)
			}
			mustWrite(t, p.agents, string(agents))
			mustWrite(t, p.skill, string(skill))
		})
	}
}

func TestUninstallRejectsModifiedManagedGuidanceBeforeMutation(t *testing.T) {
	p := isolatedLifecycle(t)
	if _, err := install("main", false, true, false); err != nil {
		t.Fatal(err)
	}
	if err := newStore(stateDir()).update(func(value *state) (bool, error) {
		value.Phase = phaseMigrationComplete
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	agents, _ := os.ReadFile(p.agents)
	hooks, _ := os.ReadFile(p.hooks)
	mustWrite(t, p.agents, strings.Replace(string(agents), "# ThreadBear", "# ThreadBear edited", 1))
	if _, err := uninstall(context.Background(), true); err == nil || !strings.Contains(err.Error(), "managed file was modified") {
		t.Fatalf("modified guidance uninstall = %v", err)
	}
	if got, _ := os.ReadFile(p.hooks); !reflect.DeepEqual(got, hooks) {
		t.Fatal("blocked uninstall changed hooks")
	}
	if _, err := os.Stat(p.binary); err != nil {
		t.Fatalf("blocked uninstall removed binary: %v", err)
	}
}

func TestUninstallRejectsMarkerlessManagedGuidance(t *testing.T) {
	p := isolatedLifecycle(t)
	if _, err := install("main", false, true, false); err != nil {
		t.Fatal(err)
	}
	if err := newStore(stateDir()).update(func(value *state) (bool, error) {
		value.Phase = phaseMigrationComplete
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	agents, _ := os.ReadFile(p.agents)
	markerless := strings.ReplaceAll(strings.ReplaceAll(string(agents), blockStart, ""), blockEnd, "")
	markerless = strings.Replace(markerless, "The footer must be", "The footer remains", 1)
	mustWrite(t, p.agents, markerless)
	if _, err := uninstall(context.Background(), true); err == nil || !strings.Contains(err.Error(), "managed file was modified") {
		t.Fatalf("markerless guidance uninstall = %v", err)
	}
	if _, err := os.Stat(p.binary); err != nil {
		t.Fatalf("blocked uninstall removed binary: %v", err)
	}
}

func TestUninstallKeepsBinaryUntilStateRemovalCommits(t *testing.T) {
	p := isolatedLifecycle(t)
	if _, err := install("main", false, true, false); err != nil {
		t.Fatal(err)
	}
	if err := newStore(stateDir()).update(func(value *state) (bool, error) {
		value.Phase = phaseMigrationComplete
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	stateParent := filepath.Dir(stateDir())
	if err := os.Chmod(stateParent, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(stateParent, 0o700) })
	if _, err := uninstall(context.Background(), true); err == nil {
		t.Fatal("uninstall succeeded while state directory could not be removed")
	}
	if _, err := os.Stat(p.binary); err != nil {
		t.Fatalf("failed state removal deleted retry binary: %v", err)
	}
	if err := os.Chmod(stateParent, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := uninstall(context.Background(), true); err != nil {
		t.Fatalf("resumed teardown: %v", err)
	}
	if _, err := os.Stat(p.binary); !os.IsNotExist(err) {
		t.Fatalf("resumed teardown left binary: %v", err)
	}
}

func TestMalformedHooksFailBeforeLifecycleMutation(t *testing.T) {
	p := isolatedLifecycle(t)
	malformed := []byte(`{"hooks":{"PreToolUse":{"not":"an array"}}}`)
	mustWrite(t, p.hooks, string(malformed))
	if _, err := install("installer", false, true, false); err == nil {
		t.Fatal("install accepted wrong-shaped hooks")
	}
	got, _ := os.ReadFile(p.hooks)
	if !reflect.DeepEqual(got, malformed) {
		t.Fatal("failed install changed hooks.json")
	}
	if _, err := os.Stat(p.binary); !os.IsNotExist(err) {
		t.Fatal("failed install copied the binary")
	}
	mustWrite(t, p.binary, "sentinel")
	if _, err := uninstall(context.Background(), true); err == nil {
		t.Fatal("uninstall accepted wrong-shaped hooks")
	}
	if got, _ := os.ReadFile(p.binary); string(got) != "sentinel" {
		t.Fatal("failed uninstall mutated installation")
	}
}

func TestOwnedHookQuotesBinaryPath(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "Eric O'Brien Bear", "threadbear")
	mustWrite(t, binary, "#!/bin/sh\nprintf '%s:%s' \"$#\" \"$1\"\n")
	if err := os.Chmod(binary, 0o700); err != nil {
		t.Fatal(err)
	}
	hooks := filepath.Join(t.TempDir(), "hooks.json")
	data, write, err := editHooks(hooks, binary, true)
	if err != nil || !write {
		t.Fatalf("edit hooks: write %v, err %v", write, err)
	}
	mustWrite(t, hooks, string(data))
	assertHookOrder(t, hooks, "PreToolUse", nil, binary)
	assertHookOrder(t, hooks, "PostToolUse", nil, binary)
	output, err := exec.Command("sh", "-c", quoteCommand(binary)).CombinedOutput()
	if err != nil || string(output) != "1:hook" {
		t.Fatalf("quoted command invoked %q, err %v", output, err)
	}
}

func TestUninstallRemovesOwnedOnlyHooksFile(t *testing.T) {
	p := isolatedLifecycle(t)
	data, _, err := editHooks(p.hooks, p.binary, true)
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, p.hooks, string(data))
	if _, err := uninstall(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p.hooks); !os.IsNotExist(err) {
		t.Fatalf("owned-only hooks file survived: %v", err)
	}
}

func TestInstallUpgradesOwnedHookTimeoutAndUninstallRemovesVariants(t *testing.T) {
	p := isolatedLifecycle(t)
	foreignPreA := json.RawMessage(`{"matcher":"codex_appset_thread_title","hooks":[{"type":"command","command":"foreign","timeout":5}]}`)
	foreignPreB := json.RawMessage(`{"matcher":"Bash","hooks":[{"type":"command","command":"pre"}]}`)
	foreignPreC := encodedJSON(map[string]any{"matcher": titleTool, "hooks": []any{map[string]any{"type": "prompt", "command": quoteCommand(p.binary), "timeout": 5}}})
	foreignPost := json.RawMessage(`{"matcher":"Bash","hooks":[{"type":"command","command":"post"}]}`)
	oldOwner := ownedHookWithTimeout(p.binary, 5)
	fixture := map[string]any{"hooks": map[string]any{
		"PreToolUse":  []json.RawMessage{oldOwner, foreignPreA, ownedHookJSON(p.binary), foreignPreB, foreignPreC, oldOwner},
		"PostToolUse": []json.RawMessage{foreignPost, oldOwner},
	}}
	data, _ := json.MarshalIndent(fixture, "", "  ")
	mustWrite(t, p.hooks, string(data))

	if _, err := install("installer", false, true, false); err != nil {
		t.Fatal(err)
	}
	assertHookOrder(t, p.hooks, "PreToolUse", []json.RawMessage{foreignPreA, foreignPreB, foreignPreC}, p.binary)
	assertHookOrder(t, p.hooks, "PostToolUse", []json.RawMessage{foreignPost}, p.binary)
	installed, _ := os.ReadFile(p.hooks)
	if _, err := install("installer", false, true, false); err != nil {
		t.Fatal(err)
	}
	reinstalled, _ := os.ReadFile(p.hooks)
	if !reflect.DeepEqual(installed, reinstalled) {
		t.Fatal("reinstall rewrote upgraded hooks.json")
	}
	mustWrite(t, p.hooks, string(data))
	if err := newStore(stateDir()).update(func(value *state) (bool, error) {
		value.Phase = phaseMigrationComplete
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := uninstall(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	assertHookOrder(t, p.hooks, "PreToolUse", []json.RawMessage{foreignPreA, foreignPreB, foreignPreC}, "")
	assertHookOrder(t, p.hooks, "PostToolUse", []json.RawMessage{foreignPost}, "")
}

func ownedHookWithTimeout(binary string, timeout int) json.RawMessage {
	data, _ := json.Marshal(map[string]any{"matcher": titleTool, "hooks": []any{map[string]any{"type": "command", "command": quoteCommand(binary), "timeout": timeout}}})
	return data
}

func sameJSON(a, b []byte) bool {
	var left, right any
	return json.Unmarshal(a, &left) == nil && json.Unmarshal(b, &right) == nil && reflect.DeepEqual(left, right)
}

func isolatedLifecycle(t *testing.T) lifecyclePaths {
	t.Helper()
	testIndex(t)
	return installPaths()
}

func writeHookFixture(t *testing.T, path string, preA, preB, postA json.RawMessage) {
	t.Helper()
	value := map[string]any{
		"owner": map[string]any{"preserve": true},
		"hooks": map[string]any{
			"PreToolUse":  []json.RawMessage{preA, preB},
			"PostToolUse": []json.RawMessage{postA},
			"Stop":        []any{map[string]any{"hooks": []any{map[string]any{"type": "command", "command": "stop"}}}},
		},
	}
	data, _ := json.MarshalIndent(value, "", "  ")
	mustWrite(t, path, string(data))
}

func assertHookOrder(t *testing.T, path, event string, foreign []json.RawMessage, binary string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var root struct {
		Hooks map[string][]json.RawMessage `json:"hooks"`
	}
	if json.Unmarshal(data, &root) != nil {
		t.Fatal("invalid hooks output")
	}
	wantLen := len(foreign)
	if binary != "" {
		wantLen++
	}
	if len(root.Hooks[event]) != wantLen {
		t.Fatalf("%s groups = %d, want %d", event, len(root.Hooks[event]), wantLen)
	}
	for i := range foreign {
		if !sameJSON(root.Hooks[event][i], foreign[i]) {
			t.Fatalf("%s foreign group %d changed: %s", event, i, root.Hooks[event][i])
		}
	}
	if binary != "" && !sameJSON(root.Hooks[event][len(foreign)], ownedHookJSON(binary)) {
		t.Fatalf("%s missing exact owned hook: %s", event, root.Hooks[event][len(foreign)])
	}
	if binary != "" {
		var owner struct {
			Matcher string `json:"matcher"`
			Hooks   []struct {
				Type, Command string
				Timeout       int
			} `json:"hooks"`
		}
		if json.Unmarshal(root.Hooks[event][len(foreign)], &owner) != nil || owner.Matcher != titleTool || len(owner.Hooks) != 1 || owner.Hooks[0].Type != "command" || owner.Hooks[0].Command != quoteCommand(binary) || owner.Hooks[0].Timeout != 1 {
			t.Fatalf("%s owned hook contract = %+v", event, owner)
		}
	}
}

func TestManagedGuidanceBoundsEachNativeTitleCall(t *testing.T) {
	guidance := assets.AgentsManagedContent
	for _, required := range []string{
		"const result = await Promise.race([",
		"tools.codex_app__set_thread_title({title:\"REPLACE WITH THE REQUIRED TITLE\"})",
		"new Promise(resolve => setTimeout(() => resolve({status:\"timeout\"}), 4000))",
		"Make exactly one native attempt.",
		"never retry or await that promise",
		"Explicit-target lifecycle mutations are governed by the installed ThreadBear skill instead.",
		"do not add this `Promise.race` unless the skill explicitly requires a four-second attempt",
	} {
		if !strings.Contains(guidance, required) {
			t.Errorf("managed guidance is missing bounded-call contract %q", required)
		}
	}
	if strings.Contains(guidance, "retry it once") {
		t.Fatal("managed guidance still permits a second native attempt")
	}
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
