//go:build integration

package appserver

import (
	"context"
	"database/sql"
	_ "modernc.org/sqlite"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestLiveEphemeralDoesNotPersist(t *testing.T) {
	process, caps, home := liveHarness(t)
	before := liveThreadIDs(t, home)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	client, err := Start(ctx, process, caps)
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.RunEphemeral(ctx, EphemeralRequest{Model: liveValue("THREADBEAR_LIVE_MODEL", "gpt-5.6-luna"), Effort: liveValue("THREADBEAR_LIVE_EFFORT", "medium"), Input: "Return the supplied JSON result without tools.", OutputSchema: map[string]any{"type": "object", "properties": map[string]any{"ok": map[string]any{"type": "boolean"}}, "required": []string{"ok"}, "additionalProperties": false}})
	if err != nil {
		client.Close()
		t.Fatal(err)
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	output := strings.TrimSpace(evidenceFromTurn(result.Turn).AgentMessage)
	if result.ThreadID == "" || !result.ToolRestriction.CompensatingSet() || output == "" {
		t.Fatalf("result=%+v output=%q", result, output)
	}
	after := liveThreadIDs(t, home)
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("ephemeral persisted: before=%v after=%v", before, after)
	}
}
func TestLiveNoticeDoesNotStartTurn(t *testing.T) {
	process, caps, home := liveHarness(t)
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
	before := waitForLiveThreadIDs(t, home, started.Thread.ID, 10*time.Second)
	notice := "🧵🐻 ThreadBear 99.0.0-live-proof is ready. Run threadbear update, or tell me “update ThreadBear.”"
	if err := client.InsertNotice(ctx, started.Thread.ID, notice); err != nil {
		client.Close()
		t.Fatal(err)
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
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("notice created task: before=%v after=%v", before, after)
	}
	data, err := os.ReadFile(*started.Thread.Path)
	if err != nil {
		t.Fatal(err)
	}
	if count := strings.Count(string(data), notice); count != 1 {
		t.Fatalf("notice count=%d", count)
	}
}
func liveHarness(t *testing.T) (ProcessSpec, Capabilities, string) {
	t.Helper()
	if os.Getenv("THREADBEAR_LIVE_CODEX") != "1" {
		t.Skip("set THREADBEAR_LIVE_CODEX=1 on a disposable Codex installation")
	}
	home := t.TempDir()
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
func waitForLiveThreadIDs(t *testing.T, home, threadID string, timeout time.Duration) []string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		ids := liveThreadIDs(t, home)
		for _, id := range ids {
			if id == threadID {
				return ids
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("thread %s did not materialize within %s; observed IDs: %v", threadID, timeout, ids)
		}
		time.Sleep(50 * time.Millisecond)
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
