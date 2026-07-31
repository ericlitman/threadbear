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
	p, stopped := isolatedLifecycle(t)
	foreignAgents := "# Mine\nkeep this exactly\n"
	mustWrite(t, p.agents, foreignAgents)
	preA := json.RawMessage(`{"matcher":"Bash","hooks":[{"type":"command","command":"a"}],"extension":{"n":1}}`)
	preB := json.RawMessage(`{"hooks":[{"command":"b","timeout":99,"type":"command"}]}`)
	postA := json.RawMessage(`{"matcher":"","hooks":[{"type":"command","command":"c"}]}`)
	writeHookFixture(t, p.hooks, preA, preB, postA)
	mustWrite(t, p.plist, "old heartbeat")
	mustWrite(t, filepath.Join(stateDir(), "core.json"), `{"tasks":{"kept":{"subject":"Stable subject","last_applied":"✅ Stable subject"},"ignored":{"subject":""}}}`)

	if _, err := install(context.Background(), false, true); err != nil {
		t.Fatal(err)
	}
	if *stopped != 1 {
		t.Fatalf("legacy service stops = %d, want 1", *stopped)
	}
	if _, err := os.Stat(p.plist); !os.IsNotExist(err) {
		t.Fatalf("legacy plist survived: %v", err)
	}
	if _, err := os.Stat(filepath.Join(stateDir(), "core.json")); !os.IsNotExist(err) {
		t.Fatalf("legacy state survived: %v", err)
	}
	stateData, err := os.ReadFile(filepath.Join(stateDir(), "native.json"))
	if err != nil || !strings.Contains(string(stateData), `"Stable subject"`) || strings.Contains(string(stateData), `"ignored"`) {
		t.Fatalf("legacy ownership migration = %q, err %v", stateData, err)
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
	if _, err := status(); err == nil {
		t.Fatal("status accepted a missing AGENTS managed block")
	}
	if err := manageBlock(p.agents, assets.AgentsManagedContent); err != nil {
		t.Fatal(err)
	}

	if _, err := install(context.Background(), false, true); err != nil {
		t.Fatal(err)
	}
	secondHooks, _ := os.ReadFile(p.hooks)
	if !reflect.DeepEqual(firstHooks, secondHooks) {
		t.Fatal("reinstall rewrote an already-correct hooks.json")
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
	p, stopped := isolatedLifecycle(t)
	if _, err := install(context.Background(), true, false); err != nil {
		t.Fatal(err)
	}
	if _, err := install(context.Background(), false, false); err == nil {
		t.Fatal("unconfirmed install succeeded")
	}
	if *stopped != 0 {
		t.Fatal("preview or rejected install stopped the legacy service")
	}
	for _, path := range []string{p.binary, p.agents, p.skill, p.hooks, stateDir()} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("non-mutating install created %s: %v", path, err)
		}
	}
}

func TestMalformedHooksFailBeforeLifecycleMutation(t *testing.T) {
	p, stopped := isolatedLifecycle(t)
	malformed := []byte(`{"hooks":{"PreToolUse":{"not":"an array"}}}`)
	mustWrite(t, p.hooks, string(malformed))
	if _, err := install(context.Background(), false, true); err == nil {
		t.Fatal("install accepted wrong-shaped hooks")
	}
	if *stopped != 0 {
		t.Fatal("failed install stopped the legacy service")
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

func TestStatusRequiresExactManagedAgentsBlock(t *testing.T) {
	p, _ := isolatedLifecycle(t)
	if _, err := install(context.Background(), false, true); err != nil {
		t.Fatal(err)
	}
	canonical, err := os.ReadFile(p.agents)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := status(); err != nil {
		t.Fatalf("canonical status: %v", err)
	}

	variants := map[string]string{
		"edited":    strings.Replace(string(canonical), "# ThreadBear", "# ThreadBear edited", 1),
		"empty":     blockStart + "\n" + blockEnd + "\n",
		"duplicate": string(canonical) + string(canonical),
		"reversed":  blockEnd + "\n" + strings.TrimSpace(assets.AgentsManagedContent) + "\n" + blockStart + "\n",
	}
	for name, value := range variants {
		t.Run(name, func(t *testing.T) {
			mustWrite(t, p.agents, value)
			if _, err := status(); err == nil {
				t.Fatalf("status accepted %s managed AGENTS block", name)
			}
			mustWrite(t, p.agents, string(canonical))
		})
	}
}

func TestMalformedLegacyStateFailsBeforeLifecycleMutation(t *testing.T) {
	p, stopped := isolatedLifecycle(t)
	legacyPath := filepath.Join(stateDir(), "core.json")
	malformed := []byte(`{"tasks":`)
	mustWrite(t, legacyPath, string(malformed))
	mustWrite(t, p.plist, "legacy service sentinel")
	mustWrite(t, p.hooks, `{"owner":"unchanged"}`)

	if _, err := install(context.Background(), false, true); err == nil {
		t.Fatal("install accepted malformed legacy state")
	}
	if *stopped != 0 {
		t.Fatalf("failed install stopped the legacy service %d times", *stopped)
	}
	for path, want := range map[string][]byte{
		legacyPath: malformed,
		p.plist:    []byte("legacy service sentinel"),
		p.hooks:    []byte(`{"owner":"unchanged"}`),
	} {
		got, err := os.ReadFile(path)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("failed install changed %s: got %q, err %v", path, got, err)
		}
	}
	for _, path := range []string{p.binary, p.agents, p.skill, filepath.Join(stateDir(), "native.json")} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("failed install created %s: %v", path, err)
		}
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
	p, _ := isolatedLifecycle(t)
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

func isolatedLifecycle(t *testing.T) (lifecyclePaths, *int) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	oldStop := stopLegacyService
	stopped := 0
	stopLegacyService = func(context.Context) error { stopped++; return nil }
	t.Cleanup(func() { stopLegacyService = oldStop })
	return installPaths(), &stopped
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
