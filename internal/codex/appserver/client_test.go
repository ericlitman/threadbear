package appserver

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFakeAppServerProcess(t *testing.T) {
	if os.Getenv("THREADBEAR_FAKE_SERVER") == "" {
		return
	}
	fakeMain()
	os.Exit(0)
}
func TestCapabilities(t *testing.T) {
	caps, err := DiscoverCapabilities(context.Background(), fakeProcess(t, "schema"))
	if err != nil {
		t.Fatal(err)
	}
	if caps.RecentTurnsMethod() != "thread/turns/list" || !caps.HasMethod("thread/inject_items") {
		t.Fatalf("caps=%+v", caps)
	}
	r := caps.ToolRestrictionCandidates()
	if !r.ConfigOverride || !r.PermissionProfile || !r.EnvironmentsDisabled || !r.DynamicToolsDisabled || !r.ApprovalsDisabled || !r.ReadOnlySandbox || !r.OutputConstrained {
		t.Fatalf("restriction=%+v", r)
	}
}
func TestProcessSpecResolvesExplicitPath(t *testing.T) {
	directory := t.TempDir()
	executable := filepath.Join(directory, "codex")
	if err := os.WriteFile(executable, []byte("synthetic"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir())
	process := ProcessSpec{Path: "codex", Env: []string{"PATH=" + directory}}
	command, err := process.command(context.Background(), "app-server")
	if err != nil {
		t.Fatal(err)
	}
	if command.Path != executable {
		t.Fatalf("command path=%q, want %q", command.Path, executable)
	}
	missing := ProcessSpec{Path: "codex", Env: []string{"PATH=" + t.TempDir()}}
	if _, err := missing.command(context.Background(), "app-server"); !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("missing command error=%v, want exec.ErrNotFound", err)
	}
}

func TestTransport(t *testing.T) {
	for _, tc := range []struct {
		name, scenario string
		timeout        time.Duration
		target         error
		contains       string
	}{{"malformed", "malformed", time.Second, nil, "decode App Server message"}, {"timeout", "timeout", 100 * time.Millisecond, context.DeadlineExceeded, ""}, {"exit", "exit", time.Second, nil, "App Server exited"}, {"unexpected", "unexpected", time.Second, ErrUnexpectedRequest, ""}} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), tc.timeout)
			defer cancel()
			_, err := Start(ctx, fakeProcess(t, tc.scenario), fixtureCaps(t))
			if err == nil {
				t.Fatal("expected error")
			}
			if tc.target != nil && !errors.Is(err, tc.target) {
				t.Fatalf("error=%v", err)
			}
			if tc.contains != "" && !strings.Contains(err.Error(), tc.contains) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}
func TestRequestCorrelation(t *testing.T) {
	c := startFake(t, "correlation", fixtureCaps(t))
	defer c.Close()
	type result struct {
		Value string `json:"value"`
	}
	firstDone := make(chan result, 1)
	errDone := make(chan error, 1)
	go func() {
		var value result
		errDone <- c.call(context.Background(), "synthetic/one", map[string]any{}, &value)
		firstDone <- value
	}()
	time.Sleep(20 * time.Millisecond)
	var second result
	if err := c.call(context.Background(), "synthetic/two", map[string]any{}, &second); err != nil {
		t.Fatal(err)
	}
	if err := <-errDone; err != nil {
		t.Fatal(err)
	}
	first := <-firstDone
	if first.Value != "one" || second.Value != "two" {
		t.Fatalf("responses crossed: first=%+v second=%+v", first, second)
	}
}

func TestInterleavedNotificationAndMinimalEnvironment(t *testing.T) {
	t.Setenv("THREADBEAR_SECRET", "hidden")
	c := startFake(t, "interleaved", fixtureCaps(t))
	defer c.Close()
	select {
	case n := <-c.Notifications():
		if n.Method != "thread/started" {
			t.Fatal(n.Method)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}
func TestEvidenceAndFallbacks(t *testing.T) {
	c := startFake(t, "normal", fixtureCaps(t))
	e, err := c.ReadRecentTurns(context.Background(), "task-1", "")
	c.Close()
	if err != nil {
		t.Fatal(err)
	}
	if e.ThreadStatus.Type != "active" || e.Latest == nil || len(e.Latest.AgentMessage) != 100010 || !strings.HasSuffix(e.Latest.AgentMessage, " completed") || e.Latest.Error == nil || e.Previous == nil {
		t.Fatalf("evidence=%+v", e)
	}
	caps := fixtureCaps(t)
	delete(caps.Methods, "thread/turns/list")
	c = startFake(t, "normal", caps)
	e, err = c.ReadRecentTurns(context.Background(), "task-1", "")
	c.Close()
	if err != nil || e.Latest == nil || e.Latest.UserMessage != "latest user" {
		t.Fatalf("fallback=%+v %v", e, err)
	}
	empty := Capabilities{Methods: map[string]bool{}, ThreadStartFields: map[string]bool{}, TurnStartFields: map[string]bool{}}
	c = startFake(t, "normal", empty)
	defer c.Close()
	e, err = c.ReadRecentTurns(context.Background(), "task-1", filepath.Join("..", "..", "..", "testdata", "appserver", "rollout.jsonl"))
	if err != nil || e.Latest == nil || e.Latest.Status != "interrupted" || e.Latest.AgentMessage != "aborted partial" || e.Previous == nil || e.Previous.Status != "failed" || e.Previous.Error == nil || e.Previous.Error.Message != "synthetic rollout failure" {
		t.Fatalf("rollout=%+v %v", e, err)
	}
}
func TestLatestAndPreviousEvidenceAreReadSeparately(t *testing.T) {
	c := startFake(t, "normal", fixtureCaps(t))
	defer c.Close()
	latest, err := c.ReadLatestTurn(context.Background(), "task-1", "")
	if err != nil {
		t.Fatal(err)
	}
	if latest.Latest == nil || latest.Latest.ID != "latest" || latest.Previous != nil || latest.RecencyAt == nil || *latest.RecencyAt != 1770000000 {
		t.Fatalf("latest=%+v", latest)
	}
	previous, err := c.ReadPreviousTurn(context.Background(), "task-1", "")
	if err != nil {
		t.Fatal(err)
	}
	if previous == nil || previous.ID != "previous" {
		t.Fatalf("previous=%+v", previous)
	}
	empty := Capabilities{Methods: map[string]bool{}, ThreadStartFields: map[string]bool{}, TurnStartFields: map[string]bool{}}
	fallback := startFake(t, "normal", empty)
	defer fallback.Close()
	latest, err = fallback.ReadLatestTurn(context.Background(), "task-1", filepath.Join("..", "..", "..", "testdata", "appserver", "rollout.jsonl"))
	if err != nil || latest.Latest == nil || latest.Previous != nil || latest.Latest.Status != "interrupted" {
		t.Fatalf("fallback latest=%+v err=%v", latest, err)
	}
	previous, err = fallback.ReadPreviousTurn(context.Background(), "task-1", filepath.Join("..", "..", "..", "testdata", "appserver", "rollout.jsonl"))
	if err != nil || previous == nil || previous.Status != "failed" {
		t.Fatalf("fallback previous=%+v err=%v", previous, err)
	}
}

func TestMutations(t *testing.T) {
	c := startFake(t, "normal", fixtureCaps(t))
	defer c.Close()
	if err := c.SetTitle(context.Background(), "task-1", "✅ Synthetic"); err != nil {
		t.Fatal(err)
	}
	if err := c.Archive(context.Background(), "task-1"); err != nil {
		t.Fatal(err)
	}
	thread, err := c.Unarchive(context.Background(), "task-1")
	if err != nil || thread.ID != "task-1" {
		t.Fatalf("thread=%+v err=%v", thread, err)
	}
	before, err := c.ReadLatestTurn(context.Background(), "control-1", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := c.InsertNotice(context.Background(), "control-1", "🧵🐻 notice"); err != nil {
		t.Fatal(err)
	}
	delivered, err := c.ReadPersistedAssistantMessage(context.Background(), "control-1", "🧵🐻 notice")
	if err != nil || !delivered.Found || delivered.SkippedLines != 0 {
		t.Fatalf("delivered=%+v err=%v", delivered, err)
	}
	after, err := c.ReadLatestTurn(context.Background(), "control-1", "")
	if err != nil || before.Latest == nil || after.Latest == nil || before.Latest.ID != after.Latest.ID {
		t.Fatalf("before=%+v after=%+v err=%v", before, after, err)
	}
	caps := fixtureCaps(t)
	delete(caps.Methods, "thread/archive")
	u := startFake(t, "normal", caps)
	defer u.Close()
	if !errors.Is(u.Archive(context.Background(), "task-1"), ErrCapability) {
		t.Fatal("missing capability was not rejected")
	}
	f := startFake(t, "mutation-error", fixtureCaps(t))
	defer f.Close()
	if err := f.SetTitle(context.Background(), "task-1", "x"); err == nil || !strings.Contains(err.Error(), "synthetic mutation failure") {
		t.Fatalf("error=%v", err)
	}
}
func TestPersistedAssistantMessageMatching(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	text := "🧵🐻 exact notice"
	message := map[string]any{"type": "response_item", "payload": map[string]any{"type": "message", "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": text}}}}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	encoder := json.NewEncoder(file)
	if err := encoder.Encode(message); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if _, err := file.WriteString(`{"type":"response_item"`); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	result, err := rolloutHasAssistantMessage(path, text)
	if err != nil || !result.Found || result.SkippedLines != 0 {
		t.Fatalf("truncated result=%+v err=%v", result, err)
	}

	file, err = os.OpenFile(path, os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	encoder = json.NewEncoder(file)
	decoys := []any{
		map[string]any{"type": "event_msg", "payload": map[string]any{"message": text}},
		map[string]any{"type": "response_item", "payload": map[string]any{"type": "message", "role": "user", "content": []any{map[string]any{"type": "input_text", "text": text}}}},
		map[string]any{"type": "response_item", "payload": map[string]any{"type": "message", "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": "different"}}}},
	}
	for _, decoy := range decoys {
		if err := encoder.Encode(decoy); err != nil {
			file.Close()
			t.Fatal(err)
		}
	}
	if _, err := file.WriteString(`{bad
`); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := encoder.Encode(message); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	result, err = rolloutHasAssistantMessage(path, text)
	if err != nil || !result.Found || result.SkippedLines != 1 {
		t.Fatalf("skipped result=%+v err=%v", result, err)
	}

	if err := os.WriteFile(path, []byte(`{bad
also bad
`), 0600); err != nil {
		t.Fatal(err)
	}
	result, err = rolloutHasAssistantMessage(path, text)
	if err == nil || result.SkippedLines != 2 {
		t.Fatalf("corrupt result=%+v err=%v", result, err)
	}
	if _, err := rolloutHasAssistantMessage(filepath.Join(t.TempDir(), "missing.jsonl"), text); err == nil {
		t.Fatal("missing rollout was accepted")
	}
}

func TestMutationNotificationsHaveNoFixedBufferCap(t *testing.T) {
	c := startFake(t, "notification-burst", fixtureCaps(t))
	defer c.Close()
	if err := c.Archive(context.Background(), "task-1"); err != nil {
		t.Fatal(err)
	}
	if err := c.SetTitle(context.Background(), "task-1", "✅ Still connected"); err != nil {
		t.Fatal(err)
	}
}

func TestEphemeralControls(t *testing.T) {
	c := startFake(t, "normal", fixtureCaps(t))
	defer c.Close()
	result, err := c.RunEphemeral(context.Background(), EphemeralRequest{Model: "gpt-5.6-luna", Effort: "medium", Input: "classify", OutputSchema: map[string]any{"type": "object"}, ToolConfig: map[string]any{"tools": map[string]any{"enabled": false}}, PermissionProfile: "threadbear"})
	if err != nil {
		t.Fatal(err)
	}
	output := evidenceFromTurn(result.Turn).AgentMessage
	if result.ThreadID != "ephemeral-1" || result.Turn.Status != "completed" || output != `{"state":"complete"}` || !result.ToolRestriction.ConfigOverride || !result.ToolRestriction.PermissionProfile || result.ToolRestriction.ReadOnlySandbox || !result.ToolRestriction.EnvironmentsDisabled || !result.ToolRestriction.DynamicToolsDisabled || !result.ToolRestriction.ApprovalsDisabled || !result.ToolRestriction.OutputConstrained || strings.Join(result.ToolRestriction.UnprovenToolSources, ",") != "core,mcp,extension,hosted" {
		t.Fatalf("result=%+v output=%q", result, output)
	}
	legacy, err := c.RunEphemeral(context.Background(), EphemeralRequest{Model: "gpt-5.6-luna", Effort: "medium", Input: "classify", OutputSchema: map[string]any{"type": "object"}})
	if err != nil {
		t.Fatal(err)
	}
	legacyRestriction := legacy.ToolRestriction
	if !legacyRestriction.EnvironmentsDisabled || !legacyRestriction.DynamicToolsDisabled || !legacyRestriction.ApprovalsDisabled || !legacyRestriction.ReadOnlySandbox || !legacyRestriction.OutputConstrained || legacyRestriction.ConfigOverride || legacyRestriction.PermissionProfile || strings.Join(legacyRestriction.UnprovenToolSources, ",") != "core,mcp,extension,hosted" {
		t.Fatalf("legacy restriction=%+v", legacyRestriction)
	}
	caps := fixtureCaps(t)
	delete(caps.ThreadStartFields, "dynamicTools")
	u := startFake(t, "normal", caps)
	defer u.Close()
	_, err = u.RunEphemeral(context.Background(), EphemeralRequest{Model: "m", Effort: "medium", OutputSchema: map[string]any{}})
	if !errors.Is(err, ErrCapability) {
		t.Fatalf("error=%v", err)
	}
}
func fixtureCaps(t *testing.T) Capabilities {
	t.Helper()
	caps, err := LoadCapabilities(filepath.Join("..", "..", "..", "testdata", "appserver", "schema"))
	if err != nil {
		t.Fatal(err)
	}
	return caps
}
func startFake(t *testing.T, scenario string, caps Capabilities) *Client {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	t.Cleanup(cancel)
	c, err := Start(ctx, fakeProcess(t, scenario), caps)
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func fakeProcess(t *testing.T, scenario string) ProcessSpec {
	t.Helper()
	fixture, err := filepath.Abs(filepath.Join("..", "..", "..", "testdata", "appserver", "schema"))
	if err != nil {
		t.Fatal(err)
	}
	rolloutPath := filepath.Join(t.TempDir(), "rollout.jsonl")
	if err := os.WriteFile(rolloutPath, nil, 0600); err != nil {
		t.Fatal(err)
	}
	return ProcessSpec{Path: os.Args[0], Args: []string{"-test.run=TestFakeAppServerProcess", "--"}, Env: []string{"HOME=" + t.TempDir(), "LC_ALL=C", "PATH=/usr/bin:/bin", "THREADBEAR_FAKE_FIXTURE=" + fixture, "THREADBEAR_FAKE_ROLLOUT=" + rolloutPath, "THREADBEAR_FAKE_SERVER=" + scenario, "TMPDIR=" + os.TempDir()}}
}
func fakeMain() {
	separator := 0
	for i, a := range os.Args {
		if a == "--" {
			separator = i + 1
			break
		}
	}
	args := os.Args[separator:]
	if len(args) > 1 && args[1] == "generate-json-schema" {
		out := ""
		for i, a := range args {
			if a == "--out" && i+1 < len(args) {
				out = args[i+1]
			}
		}
		if err := copyTree(os.Getenv("THREADBEAR_FAKE_FIXTURE"), out); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		return
	}
	fakeServe(os.Getenv("THREADBEAR_FAKE_SERVER"))
}
func copyTree(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0700)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0600)
	})
}
func fakeServe(scenario string) {
	decoder := json.NewDecoder(bufio.NewReader(os.Stdin))
	encoder := json.NewEncoder(os.Stdout)
	for {
		var request struct {
			ID     int64           `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := decoder.Decode(&request); err != nil {
			if errors.Is(err, io.EOF) {
				return
			}
			os.Exit(2)
		}
		if request.Method == "initialized" {
			continue
		}
		if request.Method == "initialize" {
			switch scenario {
			case "malformed":
				fmt.Fprintln(os.Stdout, "{bad")
				return
			case "timeout":
				time.Sleep(5 * time.Second)
				return
			case "exit":
				return
			case "unexpected":
				encoder.Encode(map[string]any{"id": 9, "method": "tool/requestUserInput"})
				return
			case "interleaved":
				encoder.Encode(map[string]any{"method": "thread/started", "params": map[string]any{}})
			}
			if os.Getenv("THREADBEAR_SECRET") != "" {
				encoder.Encode(fakeError(request.ID, "environment leaked"))
				continue
			}
			encoder.Encode(map[string]any{"id": request.ID, "result": map[string]any{"codexHome": os.Getenv("HOME"), "platformFamily": "unix", "platformOs": "linux", "userAgent": "fake"}})
			continue
		}
		if scenario == "mutation-error" && request.Method == "thread/name/set" {
			encoder.Encode(fakeError(request.ID, "synthetic mutation failure"))
			continue
		}
		if scenario == "correlation" && request.Method == "synthetic/one" {
			var next struct {
				ID     int64           `json:"id"`
				Method string          `json:"method"`
				Params json.RawMessage `json:"params"`
			}
			if err := decoder.Decode(&next); err != nil {
				os.Exit(2)
			}
			encoder.Encode(map[string]any{"id": next.ID, "result": map[string]any{"value": "two"}})
			encoder.Encode(map[string]any{"id": request.ID, "result": map[string]any{"value": "one"}})
			continue
		}
		switch request.Method {
		case "thread/turns/list":
			encoder.Encode(map[string]any{"id": request.ID, "result": map[string]any{"data": []any{fakeTurns()[1], fakeTurns()[0]}}})
		case "thread/read":
			encoder.Encode(map[string]any{"id": request.ID, "result": map[string]any{"thread": map[string]any{"id": "task-1", "path": os.Getenv("THREADBEAR_FAKE_ROLLOUT"), "status": map[string]any{"type": "active", "activeFlags": []string{"waitingOnApproval"}}, "recencyAt": 1770000000, "turns": fakeTurns()}}})
		case "thread/inject_items":
			if err := persistInjectedItems(request.Params); err != nil {
				encoder.Encode(fakeError(request.ID, err.Error()))
				continue
			}
			encoder.Encode(map[string]any{"id": request.ID, "result": map[string]any{}})
		case "thread/name/set", "thread/archive":
			if scenario == "notification-burst" && request.Method == "thread/archive" {
				for index := 0; index < 256; index++ {
					encoder.Encode(map[string]any{"method": "thread/archived", "params": map[string]any{"threadId": fmt.Sprintf("task-%d", index)}})
				}
			}
			encoder.Encode(map[string]any{"id": request.ID, "result": map[string]any{}})
		case "thread/unarchive":
			encoder.Encode(map[string]any{"id": request.ID, "result": map[string]any{"thread": map[string]any{"id": "task-1"}}})
		case "thread/start":
			if err := validateStart(request.Params); err != nil {
				encoder.Encode(fakeError(request.ID, err.Error()))
				continue
			}
			encoder.Encode(map[string]any{"id": request.ID, "result": map[string]any{"thread": map[string]any{"id": "ephemeral-1", "ephemeral": true, "path": nil, "status": map[string]any{"type": "idle"}}}})
		case "turn/start":
			if err := validateTurn(request.Params); err != nil {
				encoder.Encode(fakeError(request.ID, err.Error()))
				continue
			}
			encoder.Encode(map[string]any{"id": request.ID, "result": map[string]any{"turn": map[string]any{"id": "turn-1", "status": "inProgress"}}})
			encoder.Encode(map[string]any{"method": "item/completed", "params": map[string]any{"threadId": "ephemeral-1", "turnId": "turn-1", "completedAtMs": 1, "item": map[string]any{"id": "item-1", "type": "agentMessage", "phase": "final_answer", "text": `{"state":"complete"}`}}})
			encoder.Encode(map[string]any{"method": "turn/completed", "params": map[string]any{"threadId": "ephemeral-1", "turn": map[string]any{"id": "turn-1", "status": "completed", "items": []any{}, "itemsView": "notLoaded"}}})
		default:
			encoder.Encode(fakeError(request.ID, "unknown"))
		}
	}
}
func persistInjectedItems(raw json.RawMessage) error {
	var params struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return err
	}
	file, err := os.OpenFile(os.Getenv("THREADBEAR_FAKE_ROLLOUT"), os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	encoder := json.NewEncoder(file)
	for _, item := range params.Items {
		if err := encoder.Encode(map[string]any{"type": "response_item", "payload": item}); err != nil {
			return err
		}
	}
	return nil
}

func fakeError(id int64, message string) map[string]any {
	return map[string]any{"id": id, "error": map[string]any{"code": -32601, "message": message}}
}
func fakeTurns() []any {
	return []any{map[string]any{"id": "previous", "status": "completed", "items": []any{map[string]any{"type": "userMessage", "content": []any{map[string]any{"type": "inputText", "text": "previous user"}}}, map[string]any{"type": "agentMessage", "phase": "final_answer", "text": "previous agent"}, map[string]any{"type": "commandExecution", "text": "secret tool output"}}}, map[string]any{"id": "latest", "status": "failed", "error": map[string]any{"message": "synthetic structured failure"}, "items": []any{map[string]any{"type": "userMessage", "content": []any{map[string]any{"type": "inputText", "text": "latest user"}}}, map[string]any{"type": "agentMessage", "phase": "final_answer", "text": strings.Repeat("x", 100000) + " completed"}, map[string]any{"type": "reasoning", "text": "secret reasoning"}}}}
}
func validateStart(raw json.RawMessage) error {
	var p map[string]any
	if err := json.Unmarshal(raw, &p); err != nil {
		return err
	}
	for _, f := range []string{"ephemeral", "model", "environments", "dynamicTools", "approvalPolicy"} {
		if _, ok := p[f]; !ok {
			return fmt.Errorf("missing %s", f)
		}
	}
	_, permissions := p["permissions"]
	_, sandbox := p["sandbox"]
	if permissions == sandbox {
		return errors.New("thread/start must choose exactly one of permissions or sandbox")
	}
	if p["ephemeral"] != true || p["approvalPolicy"] != "never" {
		return errors.New("unsafe controls")
	}
	if sandbox && p["sandbox"] != "read-only" {
		return errors.New("sandbox is not read-only")
	}
	return nil
}
func validateTurn(raw json.RawMessage) error {
	var p map[string]any
	if err := json.Unmarshal(raw, &p); err != nil {
		return err
	}
	for _, f := range []string{"threadId", "input", "model", "effort", "outputSchema", "environments", "approvalPolicy"} {
		if _, ok := p[f]; !ok {
			return fmt.Errorf("missing %s", f)
		}
	}
	_, permissions := p["permissions"]
	_, sandbox := p["sandboxPolicy"]
	if permissions == sandbox {
		return errors.New("turn/start must choose exactly one of permissions or sandboxPolicy")
	}
	if p["approvalPolicy"] != "never" {
		return errors.New("approvals enabled")
	}
	return nil
}
