//go:build integration

package appserver

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	_ "modernc.org/sqlite"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestLiveEphemeralDoesNotPersist(t *testing.T) {
	process, caps, home := liveHarness(t, true)
	before := liveThreadIDs(t, home)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	client, err := Start(ctx, process, caps)
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.RunEphemeral(ctx, EphemeralRequest{Model: liveValue("THREADBEAR_LIVE_MODEL", "gpt-5.6-luna"), Effort: liveValue("THREADBEAR_LIVE_EFFORT", "medium"), Input: "Return the supplied JSON result without tools.", OutputSchema: map[string]any{"type": "object", "properties": map[string]any{"ok": map[string]any{"type": "boolean"}}, "required": []string{"ok"}, "additionalProperties": false}, ToolConfig: ClassifierToolConfig(), PermissionProfile: ":read-only"})
	if err != nil {
		client.Close()
		t.Fatal(err)
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	output := strings.TrimSpace(evidenceFromTurn(result.Turn).AgentMessage)
	restriction := result.ToolRestriction
	if result.ThreadID == "" || output == "" || !restriction.EnvironmentsDisabled || !restriction.DynamicToolsDisabled || !restriction.ApprovalsDisabled || restriction.ReadOnlySandbox || !restriction.OutputConstrained || !restriction.ConfigOverride || !restriction.PermissionProfile || len(restriction.UnprovenToolSources) != 0 {
		t.Fatalf("result=%+v output=%q", result, output)
	}
	after := liveThreadIDs(t, home)
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("ephemeral persisted: before=%v after=%v", before, after)
	}
}
func TestLiveEphemeralClassifierStartsNoConfiguredHelpers(t *testing.T) {
	process, caps, home := liveHarness(t, true)
	markers := []string{filepath.Join(home, "openknowledge-started"), filepath.Join(home, "node-repl-started")}
	helpers := make([]string, len(markers))
	for index, marker := range markers {
		helper := filepath.Join(home, fmt.Sprintf("helper-%d.sh", index))
		script := "#!/bin/sh\nprintf started > " + marker + "\nsleep 30\n"
		if err := os.WriteFile(helper, []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
		helpers[index] = helper
	}
	config := fmt.Sprintf("[mcp_servers.openknowledge_canary]\ncommand = %q\n[mcp_servers.node_repl_canary]\ncommand = %q\n[features]\nplugins = true\nremote_plugin = true\napps = true\ncomputer_use = true\nshell_tool = true\nunified_exec = true\ncode_mode_host = true\ntool_suggest = true\n", helpers[0], helpers[1])
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	client, err := Start(ctx, process, caps)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	stop := make(chan struct{})
	descendants := make(chan []string, 1)
	go observeDescendantCommands(client.command.Process.Pid, stop, descendants)
	result, err := client.RunEphemeral(ctx, EphemeralRequest{Model: liveValue("THREADBEAR_LIVE_MODEL", "gpt-5.6-luna"), Effort: liveValue("THREADBEAR_LIVE_EFFORT", "medium"), Input: "Return JSON without using tools.", OutputSchema: map[string]any{"type": "object", "properties": map[string]any{"ok": map[string]any{"type": "boolean"}}, "required": []string{"ok"}, "additionalProperties": false}, ToolConfig: ClassifierToolConfig(), PermissionProfile: ":read-only"})
	close(stop)
	commands := <-descendants
	if err != nil {
		t.Fatal(err)
	}
	if !validToolRestrictionsForCanary(result.ToolRestriction) {
		t.Fatalf("restriction=%+v", result.ToolRestriction)
	}
	for _, command := range commands {
		lower := strings.ToLower(command)
		if strings.Contains(lower, "node") || strings.Contains(lower, "repl") || strings.Contains(lower, "mcp") || strings.Contains(lower, "helper-") {
			t.Fatalf("classifier started helper process %q", command)
		}
	}
	for _, marker := range markers {
		if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("configured helper started at %s: %v", filepath.Base(marker), err)
		}
	}
}

func validToolRestrictionsForCanary(restriction ToolRestriction) bool {
	return restriction.ConfigOverride && restriction.PermissionProfile && restriction.EnvironmentsDisabled && restriction.DynamicToolsDisabled && restriction.ApprovalsDisabled && restriction.OutputConstrained && !restriction.ReadOnlySandbox && len(restriction.UnprovenToolSources) == 0
}

func TestLiveNoticeDoesNotStartTurn(t *testing.T) {
	process, caps, home := liveHarness(t, false)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	client, err := Start(ctx, process, caps)
	if err != nil {
		t.Fatal(err)
	}
	if err := caps.requireMethod("thread/start"); err != nil {
		client.Close()
		t.Fatal(err)
	}
	var started struct {
		Thread Thread `json:"thread"`
	}
	if err := client.call(ctx, "thread/start", map[string]any{"ephemeral": false, "model": liveValue("THREADBEAR_LIVE_MODEL", "gpt-5.6-luna")}, &started); err != nil {
		client.Close()
		t.Fatal(err)
	}
	if started.Thread.ID == "" || started.Thread.Path == nil {
		client.Close()
		t.Fatalf("thread=%+v", started.Thread)
	}
	drainNotifications(client.Notifications())
	before := liveThreadIDs(t, home)
	notices := []string{
		"🧵🐻 ThreadBear 99.0.0-live-proof is ready. Run threadbear update, or tell me “update ThreadBear.”",
		"🧵🐻 I gave myself a quick brush-up: v99.0.0 → v99.0.1!\n- First live release note\n- Second live release note\nPrefer to update by hand? threadbear configure --auto-update=false",
	}
	for _, notice := range notices {
		if err := client.InsertNotice(ctx, started.Thread.ID, notice); err != nil {
			client.Close()
			t.Fatal(err)
		}
	}
	timer := time.NewTimer(500 * time.Millisecond)
	defer timer.Stop()
	waiting := true
	for waiting {
		select {
		case n := <-client.Notifications():
			if n.Method == "turn/started" || n.Method == "turn/completed" {
				client.Close()
				t.Fatalf("notice started turn: %s", n.Method)
			}
		case <-timer.C:
			waiting = false
		}
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	after := liveThreadIDs(t, home)
	assertNoUnexpectedLiveThreads(t, before, after, started.Thread.ID)
	data, err := os.ReadFile(*started.Thread.Path)
	if err != nil {
		t.Fatal(err)
	}
	for _, notice := range notices {
		if count := strings.Count(string(data), notice); count != 1 {
			t.Fatalf("notice count=%d text=%q", count, notice)
		}
	}
}
func observeDescendantCommands(rootPID int, stop <-chan struct{}, result chan<- []string) {
	observed := make(map[string]struct{})
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		output, err := exec.Command("ps", "-axo", "pid=,ppid=,comm=").Output()
		if err == nil {
			type process struct{ pid, ppid, command string }
			processes := make([]process, 0)
			for _, line := range strings.Split(string(output), "\n") {
				fields := strings.Fields(line)
				if len(fields) < 3 {
					continue
				}
				processes = append(processes, process{pid: fields[0], ppid: fields[1], command: strings.Join(fields[2:], " ")})
			}
			descendants := map[string]bool{fmt.Sprint(rootPID): true}
			changed := true
			for changed {
				changed = false
				for _, process := range processes {
					if descendants[process.ppid] && !descendants[process.pid] {
						descendants[process.pid] = true
						changed = true
					}
				}
			}
			for _, process := range processes {
				if process.pid != fmt.Sprint(rootPID) && descendants[process.pid] {
					observed[process.command] = struct{}{}
				}
			}
		}
		select {
		case <-stop:
			commands := make([]string, 0, len(observed))
			for command := range observed {
				commands = append(commands, command)
			}
			result <- commands
			return
		case <-ticker.C:
		}
	}
}

func liveHarness(t *testing.T, requireAuth bool) (ProcessSpec, Capabilities, string) {
	t.Helper()
	if os.Getenv("THREADBEAR_LIVE_CODEX") != "1" {
		t.Skip("set THREADBEAR_LIVE_CODEX=1 on a disposable Codex installation")
	}
	home := t.TempDir()
	if requireAuth {
		authFile := strings.TrimSpace(os.Getenv("THREADBEAR_LIVE_AUTH_FILE"))
		if authFile == "" {
			t.Skip("set THREADBEAR_LIVE_AUTH_FILE to an operator-supplied Codex auth.json for the ephemeral proof")
		}
		auth, err := os.ReadFile(authFile)
		if err != nil {
			t.Fatalf("read THREADBEAR_LIVE_AUTH_FILE: %v", err)
		}
		if err := os.WriteFile(filepath.Join(home, "auth.json"), auth, 0600); err != nil {
			t.Fatalf("copy disposable Codex auth.json: %v", err)
		}
	}
	process := DefaultProcessSpec(home)
	if executable := strings.TrimSpace(os.Getenv("THREADBEAR_LIVE_CODEX_BIN")); executable != "" {
		process.Path = executable
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	caps, err := DiscoverCapabilities(ctx, process)
	if err != nil {
		t.Fatal(err)
	}
	return process, caps, home
}
func assertNoUnexpectedLiveThreads(t *testing.T, before, after []string, allowedNewID string) {
	t.Helper()
	baseline := make(map[string]bool, len(before))
	for _, id := range before {
		baseline[id] = true
	}
	observed := make(map[string]bool, len(after))
	for _, id := range after {
		observed[id] = true
		if !baseline[id] && id != allowedNewID {
			t.Fatalf("notice created unexpected task %s: before=%v after=%v", id, before, after)
		}
	}
	for _, id := range before {
		if !observed[id] {
			t.Fatalf("notice removed baseline task %s: before=%v after=%v", id, before, after)
		}
	}
}
func liveThreadIDs(t *testing.T, home string) []string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(home, "state_*.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0)
	for _, path := range paths {
		db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
		if err != nil {
			t.Fatal(err)
		}
		rows, err := db.Query("SELECT id FROM threads ORDER BY id")
		if err != nil {
			db.Close()
			if strings.Contains(err.Error(), "no such table") {
				continue
			}
			t.Fatal(err)
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				db.Close()
				t.Fatal(err)
			}
			ids = append(ids, id)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			db.Close()
			t.Fatal(err)
		}
		rows.Close()
		db.Close()
	}
	return ids
}
func drainNotifications(n <-chan Notification) {
	for {
		select {
		case <-n:
		default:
			return
		}
	}
}
func liveValue(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
