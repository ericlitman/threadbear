package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type heartbeatResult struct {
	Ready bool      `json:"ready"`
	Stats scanStats `json:"stats"`
}

type guardedResult struct {
	Task indexedTask
	Info analysis
}

type semanticCandidate struct {
	Guard guardedResult
	Text  string
}

func heartbeat(ctx context.Context, dry bool) (heartbeatResult, error) {
	started := time.Now()
	disk := newStore(stateDir())
	var lock *os.File
	var err error
	if !dry {
		lock, err = disk.lock()
		if err != nil {
			return heartbeatResult{}, err
		}
	}
	current, err := disk.load()
	if dry && errors.Is(err, os.ErrNotExist) && os.Getenv("THREADBEAR_CONTROL_TASK_ID") != "" {
		current = freshState(os.Getenv("THREADBEAR_CONTROL_TASK_ID"))
		err = nil
	}
	if err != nil {
		if lock != nil {
			unlock(lock)
		}
		return heartbeatResult{}, err
	}
	tasks, err := inventory(ctx, current.ControlTaskID)
	if err != nil {
		if lock != nil {
			unlock(lock)
		}
		return heartbeatResult{}, err
	}
	stats := scanStats{Tasks: len(tasks)}
	present := make(map[string]bool, len(tasks))
	for _, task := range tasks {
		present[task.ID] = true
	}
	for id := range current.Tasks {
		if id != current.ControlTaskID && !present[id] {
			delete(current.Tasks, id)
		}
	}
	for id, pending := range current.Plans {
		if pending.TaskID != current.ControlTaskID && !present[pending.TaskID] {
			delete(current.Plans, id)
		}
	}
	var unresolved []guardedResult
	for _, task := range tasks {
		data, boundary, readErr := readEvidence(task.RolloutPath)
		if readErr != nil {
			continue
		}
		result := analyze(data)
		result.Evidence = evidenceID(task.RolloutPath, boundary, data)
		result.Size = boundary
		old, exists := current.Tasks[task.ID]
		if exists && old.Revision == task.Revision && old.Title == task.Title && old.Evidence == result.Evidence && old.RolloutSize == boundary {
			if old.Status == "unknown" && old.LastApplied == "" {
				old.AmbiguousPasses++
				current.Tasks[task.ID] = old
				unresolved = append(unresolved, guardedResult{task, result})
				stats.Ambiguous++
			}
			continue
		}
		if exists && old.Evidence == result.Evidence && old.LastApplied != "" && task.Title == old.LastApplied {
			old.Revision, old.Title, old.RolloutSize = task.Revision, task.Title, boundary
			current.Tasks[task.ID] = old
			continue
		}
		stats.Changed++
		if result.Resolved {
			record, pending := decision(task, old, result)
			current.Tasks[task.ID] = record
			putPlan(&current, pending)
			stats.Deterministic++
		} else {
			passes := 1
			if exists && old.Evidence == result.Evidence {
				passes = old.AmbiguousPasses + 1
			}
			record := taskRecord{Revision: task.Revision, Title: task.Title, RolloutPath: task.RolloutPath, RolloutSize: boundary, Evidence: result.Evidence, Status: "unknown", Subject: chooseSubject(task.Title, old), AmbiguousPasses: passes, LastApplied: old.LastApplied}
			current.Tasks[task.ID] = record
			unresolved = append(unresolved, guardedResult{task, result})
			stats.Ambiguous++
		}
	}
	stats.ScanMillis = time.Since(started).Milliseconds()
	stats.Staged = len(current.Plans)
	current.LastScan = stats
	if dry {
		return heartbeatResult{Ready: true, Stats: stats}, nil
	}
	if err := disk.save(current); err != nil {
		unlock(lock)
		return heartbeatResult{}, err
	}
	if err := unlock(lock); err != nil {
		return heartbeatResult{}, err
	}
	if current.NativeBootstrap {
		return heartbeatResult{Ready: true, Stats: stats}, nil
	}
	if len(unresolved) == 0 {
		return heartbeatResult{Ready: true, Stats: stats}, nil
	}

	client, err := openApp(ctx, current.CodexPath)
	if err != nil {
		return heartbeatResult{Ready: false, Stats: stats}, err
	}
	defer client.close()
	var outcomes []guardedResult
	var semantic []semanticCandidate
	for _, item := range unresolved {
		thread, readErr := client.readThread(item.Task.ID)
		if readErr != nil {
			continue
		}
		if runtime := runtimeResult(thread, item.Task.ThreadSource); runtime.Resolved {
			runtime.Evidence, runtime.Size = item.Info.Evidence, item.Info.Size
			outcomes = append(outcomes, guardedResult{item.Task, runtime})
			continue
		}
		record := current.Tasks[item.Task.ID]
		if lunaEligible(record, item.Info.Evidence) {
			data, _, _ := readEvidence(item.Task.RolloutPath)
			semantic = append(semantic, semanticCandidate{item, semanticText(data)})
		}
	}
	for len(semantic) > 0 {
		end := min(16, len(semantic))
		batch := semantic[:end]
		classified, semanticErr := classifyLuna(ctx, batch)
		stats.Luna++
		if semanticErr == nil {
			for _, item := range batch {
				result := classified[item.Guard.Task.ID]
				result.Evidence, result.Size = item.Guard.Info.Evidence, item.Guard.Info.Size
				outcomes = append(outcomes, guardedResult{item.Guard.Task, result})
			}
		}
		semantic = semantic[end:]
	}
	lock, err = disk.lock()
	if err != nil {
		return heartbeatResult{}, err
	}
	latest, err := disk.load()
	if err == nil && len(outcomes) > 0 {
		tasks, indexErr := inventory(ctx, latest.ControlTaskID)
		byID := make(map[string]indexedTask, len(tasks))
		for _, task := range tasks {
			byID[task.ID] = task
		}
		if indexErr != nil {
			err = indexErr
		} else {
			for _, outcome := range outcomes {
				record, ok := latest.Tasks[outcome.Task.ID]
				task, present := byID[outcome.Task.ID]
				if !ok || !present || record.Evidence != outcome.Info.Evidence || !sameEvidence(task, record) {
					continue
				}
				updated, pending := decision(task, record, outcome.Info)
				latest.Tasks[task.ID] = updated
				putPlan(&latest, pending)
			}
		}
	}
	if err == nil {
		stats.Staged = len(latest.Plans)
		latest.LastScan = stats
		err = disk.save(latest)
	}
	err = errors.Join(err, unlock(lock))
	if err != nil {
		return heartbeatResult{}, err
	}
	return heartbeatResult{Ready: true, Stats: stats}, nil
}

func runtimeResult(thread appThread, source string) analysis {
	if thread.Status.Type == "active" {
		for _, flag := range thread.Status.ActiveFlags {
			value := strings.ToLower(flag)
			if strings.Contains(value, "wait") || strings.Contains(value, "approval") || strings.Contains(value, "input") {
				return analysis{Resolved: true, Status: "needs_input", Action: "respond to the pending prompt"}
			}
		}
		return analysis{Resolved: true, Status: "running"}
	}
	if thread.Status.Type == "idle" && strings.Contains(strings.ToLower(source), "automation") {
		return analysis{Resolved: true, Status: "automation"}
	}
	return analysis{}
}

func decision(task indexedTask, old taskRecord, result analysis) (taskRecord, plan) {
	record, pending := decide(task, result)
	record.Subject = chooseSubject(task.Title, old)
	record.LastApplied = old.LastApplied
	pending.Subject = record.Subject
	pending.DesiredTitle = title(result.Status, record.Subject, result.Action)
	pending.ID = planID(pending)
	return record, pending
}

func chooseSubject(current string, old taskRecord) string {
	if old.LastApplied != "" && current == old.LastApplied && old.Subject != "" {
		return old.Subject
	}
	return subject(current)
}

func putPlan(value *state, pending plan) {
	for id, current := range value.Plans {
		if current.TaskID == pending.TaskID {
			delete(value.Plans, id)
		}
	}
	if pending.DesiredTitle != pending.ExpectedTitle {
		value.Plans[pending.ID] = pending
	}
}

func classifyLuna(ctx context.Context, tasks []semanticCandidate) (map[string]analysis, error) {
	dir, err := os.MkdirTemp("", "threadbear-luna-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	schema := `{"type":"object","additionalProperties":false,"required":["results"],"properties":{"results":{"type":"array","items":{"type":"object","additionalProperties":false,"required":["task_id","status","action"],"properties":{"task_id":{"type":"string"},"status":{"type":"string","enum":["running","blocked","needs_input","automation","next_steps","complete","unknown"]},"action":{"type":"string"}}}}}}`
	schemaPath, outputPath := filepath.Join(dir, "schema.json"), filepath.Join(dir, "result.json")
	if err := os.WriteFile(schemaPath, []byte(schema), 0o600); err != nil {
		return nil, err
	}
	input := make([]map[string]string, 0, len(tasks))
	expected := map[string]bool{}
	for _, task := range tasks {
		input = append(input, map[string]string{"task_id": task.Guard.Task.ID, "evidence": task.Text})
		expected[task.Guard.Task.ID] = true
	}
	payload, _ := json.Marshal(input)
	prompt := "Classify each legacy Codex task using only supplied evidence. Return unknown when uncertain. Action is empty unless a concrete owner action is explicit. Return every task exactly once.\n" + string(payload)
	isolatedHome := filepath.Join(dir, "codex")
	if err := os.Mkdir(isolatedHome, 0o700); err != nil {
		return nil, err
	}
	auth, err := os.ReadFile(filepath.Join(codexHome(), "auth.json"))
	if err != nil || os.WriteFile(filepath.Join(isolatedHome, "auth.json"), auth, 0o600) != nil {
		return nil, errors.New("copy minimal Codex authentication")
	}
	args := []string{"exec", "--ephemeral", "--ignore-user-config", "-m", "gpt-5.6-luna", "-c", `model_reasoning_effort="medium"`, "-c", "features.shell_tool=false", "-c", "features.unified_exec=false", "-c", "features.apps=false", "-c", "features.plugins=false", "-c", "features.memories=false", "-c", "features.multi_agent=false", "-c", "features.computer_use=false", "-c", "features.image_generation=false", "-c", `web_search="disabled"`, "-c", "mcp_servers={}", "-s", "read-only", "--skip-git-repo-check", "-C", dir, "--output-schema", schemaPath, "-o", outputPath, prompt}
	command := exec.CommandContext(ctx, "codex", args...)
	command.Env = append(os.Environ(), "CODEX_HOME="+isolatedHome)
	if output, err := command.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("Luna classifier failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	data, err := os.ReadFile(outputPath)
	if err != nil {
		return nil, err
	}
	return decodeLuna(data, expected, len(tasks))
}

func decodeLuna(data []byte, expected map[string]bool, count int) (map[string]analysis, error) {
	var decoded struct {
		Results []struct {
			TaskID string `json:"task_id"`
			Status string `json:"status"`
			Action string `json:"action"`
		} `json:"results"`
	}
	if json.Unmarshal(data, &decoded) != nil || len(decoded.Results) != count {
		return nil, fmt.Errorf("invalid Luna result: %s", strings.TrimSpace(string(data)))
	}
	results := map[string]analysis{}
	for _, row := range decoded.Results {
		if !expected[row.TaskID] || statusEmoji[row.Status] == "" || results[row.TaskID].Resolved {
			return nil, fmt.Errorf("invalid Luna row: task=%q status=%q", row.TaskID, row.Status)
		}
		results[row.TaskID] = analysis{Resolved: true, Status: row.Status, Action: strings.TrimSpace(row.Action)}
	}
	return results, nil
}
