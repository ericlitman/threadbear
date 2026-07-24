package codex

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/ericlitman/threadbear/internal/state"
	_ "modernc.org/sqlite"
)

var (
	ErrIndexNotFound = errors.New("Codex state index not found")
	ErrSchema        = errors.New("unsupported Codex index schema")
)

var stateDatabaseName = regexp.MustCompile(`^state_([0-9]+)\.sqlite$`)

type Task struct {
	TaskID       string
	Revision     string
	Title        string
	Archived     bool
	Source       string
	ThreadSource string
	RolloutPath  string
}

type Inventory struct {
	Tasks []Task
}

type Comparison struct {
	Changed    []Task
	RemovedIDs []string
}

func (c Comparison) Unchanged() bool {
	return len(c.Changed) == 0 && len(c.RemovedIDs) == 0
}

type Index struct {
	path string
	uri  string
	db   *sql.DB
}

func ResolveCodexHome() (string, error) {
	if value := os.Getenv("CODEX_HOME"); value != "" {
		return filepath.Abs(value)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home: %w", err)
	}
	return filepath.Join(home, ".codex"), nil
}

func OpenDefaultIndex() (*Index, error) {
	home, err := ResolveCodexHome()
	if err != nil {
		return nil, err
	}
	return OpenIndex(home)
}

func OpenIndex(codexHome string) (*Index, error) {
	path, err := locateStateDatabase(codexHome)
	if err != nil {
		return nil, err
	}
	uri := readOnlyURI(path)
	db, err := sql.Open("sqlite", uri)
	if err != nil {
		return nil, fmt.Errorf("open Codex state index: %w", err)
	}
	db.SetMaxOpenConns(1)
	index := &Index{path: path, uri: uri, db: db}
	if err := index.validateSchema(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return index, nil
}

func (i *Index) Path() string {
	return i.path
}

func (i *Index) Close() error {
	return i.db.Close()
}

func (i *Index) Inventory(ctx context.Context, controlTaskID string) (Inventory, error) {
	if controlTaskID == "" {
		return Inventory{}, errors.New("control task ID is required")
	}
	if err := i.validateSchema(ctx); err != nil {
		return Inventory{}, err
	}
	rows, err := i.db.QueryContext(ctx, `
SELECT id, updated_at, title, archived, source, thread_source, rollout_path
FROM threads
WHERE archived = 0 AND source = 'vscode' AND id <> ?
ORDER BY id`, controlTaskID)
	if err != nil {
		return Inventory{}, fmt.Errorf("scan Codex state index: %w", err)
	}
	defer rows.Close()

	tasks := make([]Task, 0)
	for rows.Next() {
		var task Task
		var updatedAt int64
		var archived int
		var threadSource sql.NullString
		var rolloutPath sql.NullString
		if err := rows.Scan(&task.TaskID, &updatedAt, &task.Title, &archived, &task.Source, &threadSource, &rolloutPath); err != nil {
			return Inventory{}, fmt.Errorf("read Codex task: %w", err)
		}
		task.Revision = strconv.FormatInt(updatedAt, 10)
		task.Archived = archived != 0
		task.ThreadSource = threadSource.String
		task.RolloutPath = rolloutPath.String
		tasks = append(tasks, task)
	}
	if err := rows.Err(); err != nil {
		return Inventory{}, fmt.Errorf("scan Codex tasks: %w", err)
	}
	return Inventory{Tasks: tasks}, nil
}

func CompareInventory(captured Inventory, committed state.State) Comparison {
	current := make(map[string]struct{}, len(captured.Tasks))
	changed := make([]Task, 0)
	for _, task := range captured.Tasks {
		current[task.TaskID] = struct{}{}
		previous, ok := committed.Tasks[task.TaskID]
		if !ok || previous.CapturedRevision != task.Revision || previous.LastAppliedTitle != task.Title || previous.Retry != nil {
			changed = append(changed, task)
		}
	}
	removed := make([]string, 0)
	for taskID := range committed.Tasks {
		if _, ok := current[taskID]; !ok {
			removed = append(removed, taskID)
		}
	}
	sort.Slice(changed, func(left, right int) bool { return changed[left].TaskID < changed[right].TaskID })
	slices.Sort(removed)
	return Comparison{Changed: changed, RemovedIDs: removed}
}

func locateStateDatabase(codexHome string) (string, error) {
	entries, err := os.ReadDir(codexHome)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("%w: %s", ErrIndexNotFound, codexHome)
		}
		return "", fmt.Errorf("read Codex home: %w", err)
	}
	type candidate struct {
		version string
		name    string
	}
	candidates := make([]candidate, 0)
	for _, entry := range entries {
		match := stateDatabaseName.FindStringSubmatch(entry.Name())
		if match == nil {
			continue
		}
		candidates = append(candidates, candidate{version: match[1], name: entry.Name()})
	}
	if len(candidates) == 0 {
		return "", fmt.Errorf("%w: %s", ErrIndexNotFound, codexHome)
	}
	sort.Slice(candidates, func(left, right int) bool {
		comparison := compareDatabaseVersions(candidates[left].version, candidates[right].version)
		if comparison == 0 {
			return candidates[left].name > candidates[right].name
		}
		return comparison > 0
	})
	return filepath.Join(codexHome, candidates[0].name), nil
}

func compareDatabaseVersions(left, right string) int {
	left = strings.TrimLeft(left, "0")
	right = strings.TrimLeft(right, "0")
	if left == "" {
		left = "0"
	}
	if right == "" {
		right = "0"
	}
	if len(left) != len(right) {
		if len(left) < len(right) {
			return -1
		}
		return 1
	}
	return strings.Compare(left, right)
}

func readOnlyURI(path string) string {
	value := url.URL{Scheme: "file", Path: filepath.ToSlash(path)}
	query := value.Query()
	query.Set("mode", "ro")
	value.RawQuery = query.Encode()
	return value.String()
}
