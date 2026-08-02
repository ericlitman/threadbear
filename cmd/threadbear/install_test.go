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
	if _, err := install("installer", false, true); err != nil {
		t.Fatal(err)
	}
	assertHookOrder(t, p.hooks, "PreToolUse", []json.RawMessage{preA, preB}, p.binary)
	assertHookOrder(t, p.hooks, "PostToolUse", []json.RawMessage{postA}, p.binary)
	skill, _ := os.ReadFile(p.skill)
	if !strings.HasPrefix(string(skill), "---\n") {
		t.Fatalf("installed skill lost YAML frontmatter: %q", skill)
	}
	firstHooks, _ := os.ReadFile(p.hooks)
	if _, err := status(); err != nil {
		t.Fatalf("installed status: %v", err)
	}
	if err := manageBlock(p.agents, ""); err != nil {
		t.Fatal(err)
	}
	if err := manageBlock(p.agents, assets.AgentsManagedContent); err != nil {
		t.Fatal(err)
	}

	if _, err := install("installer", false, true); err != nil {
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
	if _, err := install("installer", true, false); err != nil {
		t.Fatal(err)
	}
	if _, err := install("installer", false, false); err == nil {
		t.Fatal("unconfirmed install succeeded")
	}
	for _, path := range []string{p.binary, p.agents, p.skill, p.hooks, stateDir()} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("non-mutating install created %s: %v", path, err)
		}
	}
}

func TestStatusRejectsModifiedManagedGuidance(t *testing.T) {
	p := isolatedLifecycle(t)
	if _, err := install("main", false, true); err != nil {
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
			if _, err := status(); err == nil {
				t.Fatalf("status accepted modified %s", name)
			}
			mustWrite(t, p.agents, string(agents))
			mustWrite(t, p.skill, string(skill))
		})
	}
}

func TestMalformedHooksFailBeforeLifecycleMutation(t *testing.T) {
	p := isolatedLifecycle(t)
	malformed := []byte(`{"hooks":{"PreToolUse":{"not":"an array"}}}`)
	mustWrite(t, p.hooks, string(malformed))
	if _, err := install("installer", false, true); err == nil {
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
		if json.Unmarshal(root.Hooks[event][len(foreign)], &owner) != nil || owner.Matcher != titleTool || len(owner.Hooks) != 1 || owner.Hooks[0].Type != "command" || owner.Hooks[0].Command != quoteCommand(binary) || owner.Hooks[0].Timeout != 5 {
			t.Fatalf("%s owned hook contract = %+v", event, owner)
		}
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
