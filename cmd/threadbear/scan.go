package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/BurntSushi/toml"
	"io"
	_ "modernc.org/sqlite"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type indexedTask struct{ ID, Title, RolloutPath string }

func inventory(ctx context.Context) ([]indexedTask, error) {
	db, err := openIndex()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `SELECT id, COALESCE(name,title,''), COALESCE(rollout_path,'')
		FROM threads WHERE archived=0 ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("read Codex task index: %w", err)
	}
	defer rows.Close()
	var tasks []indexedTask
	for rows.Next() {
		var task indexedTask
		if err := rows.Scan(&task.ID, &task.Title, &task.RolloutPath); err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}
func oneTask(ctx context.Context, id string) (indexedTask, bool, error) {
	if strings.TrimSpace(id) == "" {
		return indexedTask{}, false, errors.New("task ID is empty")
	}
	db, err := openIndex()
	if err != nil {
		return indexedTask{}, false, err
	}
	defer db.Close()
	var task indexedTask
	err = db.QueryRowContext(ctx, `SELECT id, COALESCE(name,title,''), COALESCE(rollout_path,'')
		FROM threads WHERE id=? AND archived=0`, id).Scan(&task.ID, &task.Title, &task.RolloutPath)
	if errors.Is(err, sql.ErrNoRows) {
		return indexedTask{}, false, nil
	}
	if err != nil {
		return indexedTask{}, false, fmt.Errorf("read Codex task index: %w", err)
	}
	return task, true, nil
}
func openIndex() (*sql.DB, error) {
	home, err := sqliteHome()
	if err != nil {
		return nil, err
	}
	matches, _ := filepath.Glob(filepath.Join(home, "state_*.sqlite"))
	latest, latestNumber := "", -1
	for _, path := range matches {
		value := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(path), "state_"), ".sqlite")
		if number, _ := strconv.Atoi(value); number > latestNumber {
			latest, latestNumber = path, number
		}
	}
	if latest == "" {
		return nil, errors.New("Codex state index not found")
	}
	dsn := (&url.URL{Scheme: "file", Path: latest, RawQuery: "mode=ro"}).String()
	return sql.Open("sqlite", dsn)
}
func sqliteHome() (string, error) {
	base := codexHome()
	var config struct {
		SQLiteHome string `toml:"sqlite_home"`
	}
	if _, err := toml.DecodeFile(filepath.Join(base, "config.toml"), &config); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return base, nil
		}
		return "", fmt.Errorf("parse Codex config: %w", err)
	}
	value := strings.TrimSpace(config.SQLiteHome)
	if value == "" {
		return base, nil
	}
	if !filepath.IsAbs(value) {
		value = filepath.Join(base, value)
	}
	return value, nil
}

func rolloutFooter(path string) (footer, bool) {
	if path == "" {
		return footer{}, false
	}
	f, err := os.Open(path)
	if err != nil {
		return footer{}, false
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return footer{}, false
	}
	const limit = int64(256 << 10)
	start := max(int64(0), info.Size()-limit)
	data, err := io.ReadAll(io.NewSectionReader(f, start, info.Size()-start))
	if err != nil {
		return footer{}, false
	}
	if len(data) == 0 || bytes.IndexByte(data, '\n') < 0 {
		return footer{}, false
	}
	if start > 0 {
		data = data[bytes.IndexByte(data, '\n')+1:]
	}
	lines := bytes.Split(data, []byte{'\n'})
	for i := len(lines) - 2; i >= 0; i-- {
		var item struct {
			Type    string `json:"type"`
			Payload struct {
				Type, Role, Phase, Message string
				Content                    []struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"payload"`
		}
		if json.Unmarshal(lines[i], &item) != nil {
			continue
		}
		if item.Type == "turn_context" || item.Type == "event_msg" && (item.Payload.Type == "turn_aborted" || item.Payload.Type == "task_started") ||
			item.Type == "response_item" && item.Payload.Type == "message" && item.Payload.Role == "user" {
			return footer{}, false
		}
		if item.Type != "response_item" || item.Payload.Type != "message" || item.Payload.Role != "assistant" || item.Payload.Phase != "final_answer" {
			continue
		}
		message := item.Payload.Message
		for _, part := range item.Payload.Content {
			message += part.Text
		}
		return parseFooter(message)
	}
	return footer{}, false
}

type inventoryItem struct {
	TaskID        string `json:"task_id"`
	Title         string `json:"title"`
	Subject       string `json:"subject"`
	Status        string `json:"status"`
	Action        string `json:"action,omitempty"`
	Deterministic bool   `json:"deterministic"`
	Applied       bool   `json:"applied"`
}

func migrationInventory(ctx context.Context) ([]inventoryItem, int, state, error) {
	tasks, err := inventory(ctx)
	if err != nil {
		return nil, 0, state{}, err
	}
	known, readErr := currentStateOrEmpty()
	if readErr != nil {
		return nil, 0, state{}, readErr
	}
	items := make([]inventoryItem, 0, len(tasks))
	remaining := 0
	for _, task := range tasks {
		if task.ID == known.MainTaskID || task.ID == known.ControllerTaskID {
			continue
		}
		record := known.Tasks[task.ID]
		subject := canonicalSubject(task.Title, record)
		result, ok := rolloutFooter(task.RolloutPath)
		if !ok {
			if record.Pending == nil && record.Last == task.Title && statusIcons[record.Status] != "" {
				result = footer{Status: record.Status, Action: record.Action}
				ok = true
			} else {
				result.Status = "unknown"
			}
		}
		desired := renderTitle(result.Status, subject, result.Action)
		items = append(items, inventoryItem{TaskID: task.ID, Title: task.Title, Subject: subject, Status: result.Status, Action: result.Action, Deterministic: ok})
		last := &items[len(items)-1]
		last.Applied = record.Pending == nil && record.Last == task.Title && record.Last == desired
		if !last.Applied {
			remaining++
		}
	}
	return items, remaining, known, nil
}
