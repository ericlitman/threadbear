package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	_ "modernc.org/sqlite"
)

type indexedTask struct {
	ID, Title, RolloutPath, ThreadSource string
	Revision                             int64
}

func inventory(ctx context.Context, control string) ([]indexedTask, error) {
	db, err := openIndex()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `SELECT id, updated_at_ms, COALESCE(name,title), rollout_path,
		COALESCE(thread_source,'') FROM threads
		WHERE archived=0 AND source='vscode' AND id<>? ORDER BY id`, control)
	if err != nil {
		return nil, fmt.Errorf("read Codex task index: %w", err)
	}
	defer rows.Close()
	var tasks []indexedTask
	for rows.Next() {
		var task indexedTask
		if err := rows.Scan(&task.ID, &task.Revision, &task.Title, &task.RolloutPath, &task.ThreadSource); err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}

func openIndex() (*sql.DB, error) {
	home := sqliteHome()
	matches, _ := filepath.Glob(filepath.Join(home, "state_*.sqlite"))
	sort.Slice(matches, func(i, j int) bool { return stateNumber(matches[i]) < stateNumber(matches[j]) })
	if len(matches) == 0 {
		return nil, errors.New("Codex state index not found")
	}
	dsn := (&url.URL{Scheme: "file", Path: matches[len(matches)-1], RawQuery: "mode=ro"}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	return db, nil
}

func sqliteHome() string {
	base := codexHome()
	if data, err := os.ReadFile(filepath.Join(base, "config.toml")); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			key, raw, ok := strings.Cut(strings.TrimSpace(line), "=")
			if ok && strings.TrimSpace(key) == "sqlite_home" {
				if value, err := strconv.Unquote(strings.TrimSpace(raw)); err == nil {
					if !filepath.IsAbs(value) {
						value = filepath.Join(base, value)
					}
					return value
				}
			}
		}
	}
	if value := os.Getenv("CODEX_SQLITE_HOME"); value != "" {
		return value
	}
	return base
}

func oneTask(ctx context.Context, id string) (indexedTask, bool, error) {
	db, err := openIndex()
	if err != nil {
		return indexedTask{}, false, err
	}
	defer db.Close()
	var task indexedTask
	err = db.QueryRowContext(ctx, `SELECT id, updated_at_ms, COALESCE(name,title), rollout_path,
		COALESCE(thread_source,'') FROM threads
		WHERE archived=0 AND source='vscode' AND id=?`, id).
		Scan(&task.ID, &task.Revision, &task.Title, &task.RolloutPath, &task.ThreadSource)
	if errors.Is(err, sql.ErrNoRows) {
		return indexedTask{}, false, nil
	}
	if err != nil {
		return indexedTask{}, false, fmt.Errorf("read Codex task index: %w", err)
	}
	return task, true, nil
}

func stateNumber(path string) int {
	base := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(path), "state_"), ".sqlite")
	n, _ := strconv.Atoi(base)
	return n
}

type analysis struct {
	Resolved       bool
	Status, Action string
	Evidence       string
	Size           int64
}

func readEvidence(path string) ([]byte, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, 0, err
	}
	end := info.Size()
	start := max(int64(0), end-512*1024)
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return nil, 0, err
	}
	data, err := io.ReadAll(io.LimitReader(f, end-start))
	if err != nil {
		return nil, 0, err
	}
	if start > 0 {
		if at := bytes.IndexByte(data, '\n'); at >= 0 {
			data = data[at+1:]
		} else {
			data = nil
		}
	}
	if len(data) > 0 && data[len(data)-1] != '\n' {
		if at := bytes.LastIndexByte(data, '\n'); at >= 0 {
			data = data[:at+1]
		} else {
			data = nil
		}
	}
	return data, end, nil
}

func latestTurnID(data []byte) string {
	var latest string
	for _, line := range bytes.Split(data, []byte{'\n'}) {
		var item struct {
			Type    string `json:"type"`
			Payload struct {
				TurnID string `json:"turn_id"`
			} `json:"payload"`
		}
		if json.Unmarshal(line, &item) == nil && item.Type == "turn_context" && item.Payload.TurnID != "" {
			latest = item.Payload.TurnID
		}
	}
	return latest
}

func latestTurnIDFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	reader := bufio.NewReaderSize(file, 64*1024)
	var latest string
	var line []byte
	skip := false
	for {
		fragment, readErr := reader.ReadSlice('\n')
		if !skip && len(line)+len(fragment) <= 64*1024 {
			line = append(line, fragment...)
		} else {
			line = line[:0]
			skip = true
		}
		if errors.Is(readErr, bufio.ErrBufferFull) {
			continue
		}
		if !skip {
			if id := latestTurnID(line); id != "" {
				latest = id
			}
		}
		line = line[:0]
		skip = false
		if errors.Is(readErr, io.EOF) {
			return latest, nil
		}
		if readErr != nil {
			return "", readErr
		}
	}
}

func analyze(data []byte) analysis {
	sum := sha256.Sum256(data)
	result := analysis{Evidence: hex.EncodeToString(sum[:]), Size: int64(len(data))}
	var latest string
	var latestRole string
	hadError := false
	for _, line := range bytes.Split(data, []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		var item struct {
			Type    string `json:"type"`
			Payload struct {
				Type, Role, Phase, Message string
				Content                    []struct{ Text string }
			} `json:"payload"`
		}
		if json.Unmarshal(line, &item) != nil {
			continue
		}
		if item.Type == "event_msg" && (item.Payload.Type == "error" || item.Payload.Type == "stream_error") {
			hadError = true
		}
		if item.Type != "response_item" || item.Payload.Type != "message" {
			continue
		}
		text := item.Payload.Message
		for _, part := range item.Payload.Content {
			text += part.Text
		}
		if item.Payload.Role == "user" {
			latest, latestRole = text, item.Payload.Role
			hadError = false
		} else if item.Payload.Role == "assistant" && item.Payload.Phase == "final_answer" {
			latest, latestRole = text, item.Payload.Role
		}
	}
	if latestRole == "assistant" {
		footer := parseFooter(latest)
		footer.Evidence, footer.Size = result.Evidence, result.Size
		if footer.Resolved {
			return footer
		}
	}
	if hadError {
		result.Resolved, result.Status = true, "blocked"
	}
	return result
}

func evidenceID(path string, boundary int64, data []byte) string {
	sum := sha256.New()
	fmt.Fprintf(sum, "%s\x00%d\x00", path, boundary)
	sum.Write(data)
	return hex.EncodeToString(sum.Sum(nil))
}

func parseFooter(message string) analysis {
	lines := strings.Split(strings.TrimRight(message, "\r\n"), "\n")
	if len(lines) == 0 {
		return analysis{}
	}
	line := strings.TrimSuffix(lines[len(lines)-1], "\r")
	if strings.HasPrefix(strings.TrimSpace(line), ">") {
		return analysis{}
	}
	for _, prior := range lines[:len(lines)-1] {
		if strings.Contains(prior, "🧵🐻 ") {
			return analysis{}
		}
	}
	if line == "🧵🐻 complete" {
		return analysis{Resolved: true, Status: "complete"}
	}
	if line == "🧵🐻 automation" {
		return analysis{Resolved: true, Status: "automation"}
	}
	remainder := strings.TrimPrefix(line, "🧵🐻 ")
	if remainder == line {
		return analysis{}
	}
	statusText, ownerAction, ok := strings.Cut(remainder, " (")
	owner, action, ok2 := strings.Cut(ownerAction, "): ")
	statuses := map[string]string{"next steps": "next_steps", "needs input": "needs_input", "blocked": "blocked"}
	status, valid := statuses[statusText]
	if !ok || !ok2 || !valid || strings.TrimSpace(action) != action || len(strings.Fields(action)) < 2 {
		return analysis{}
	}
	if status == "needs_input" && owner != "you" || status == "blocked" && owner != "external" ||
		status == "next_steps" && owner != "you" && owner != "agent" && owner != "external" {
		return analysis{}
	}
	return analysis{Resolved: true, Status: status, Action: action}
}

func semanticText(data []byte) string {
	var user, agent string
	for _, line := range bytes.Split(data, []byte{'\n'}) {
		var item struct {
			Type    string `json:"type"`
			Payload struct {
				Type, Role, Phase, Message string
				Content                    []struct{ Text string }
			} `json:"payload"`
		}
		if json.Unmarshal(line, &item) != nil || item.Type != "response_item" || item.Payload.Type != "message" {
			continue
		}
		text := item.Payload.Message
		for _, part := range item.Payload.Content {
			text += part.Text
		}
		if item.Payload.Role == "user" {
			user = text
		} else if item.Payload.Role == "assistant" && item.Payload.Phase == "final_answer" {
			agent = text
		}
	}
	trim := func(value string) string {
		if len(value) > 4*1024 {
			return value[len(value)-4*1024:]
		}
		return value
	}
	return "USER:\n" + trim(user) + "\nASSISTANT:\n" + trim(agent)
}

func sameEvidence(task indexedTask, record taskRecord) bool {
	data, size, err := readEvidence(task.RolloutPath)
	if err != nil || size != record.RolloutSize {
		return false
	}
	got := analyze(data)
	got.Evidence = evidenceID(task.RolloutPath, size, data)
	return got.Evidence == record.Evidence && task.Revision == record.Revision && task.Title == record.Title
}
