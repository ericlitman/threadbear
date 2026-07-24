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
	DerivedTitle string
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

func ResolveSQLiteHome(codexHome string) (string, error) {
	// Codex 0.145 resolves an explicit sqlite_home config value first and uses
	// CODEX_SQLITE_HOME only as the fallback (core/src/config/mod.rs: cfg.sqlite_home
	// .or_else(resolve_sqlite_home_env)).
	data, err := os.ReadFile(filepath.Join(codexHome, "config.toml"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("read Codex config: %w", err)
	}
	if err == nil {
		value, found, err := sqliteHomeFromConfig(data)
		if err != nil {
			return "", fmt.Errorf("read Codex sqlite_home: %w", err)
		}
		if found {
			if !filepath.IsAbs(value) {
				value = filepath.Join(codexHome, value)
			}
			return filepath.Abs(value)
		}
	}
	if value := strings.TrimSpace(os.Getenv("CODEX_SQLITE_HOME")); value != "" {
		return filepath.Abs(value)
	}
	return filepath.Abs(codexHome)
}

func OpenDefaultIndex() (*Index, error) {
	codexHome, err := ResolveCodexHome()
	if err != nil {
		// A Codex home is required to read config-first precedence; without one,
		// the environment override still names a database home on its own.
		if value := strings.TrimSpace(os.Getenv("CODEX_SQLITE_HOME")); value != "" {
			sqliteHome, absErr := filepath.Abs(value)
			if absErr != nil {
				return nil, absErr
			}
			return OpenIndex(sqliteHome)
		}
		return nil, err
	}
	sqliteHome, err := ResolveSQLiteHome(codexHome)
	if err != nil {
		return nil, err
	}
	return OpenIndex(sqliteHome)
}

func OpenIndex(sqliteHome string) (*Index, error) {
	path, err := locateStateDatabase(sqliteHome)
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
SELECT id, updated_at_ms, title, name, archived, source, thread_source, rollout_path
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
		var updatedAtMS int64
		var name sql.NullString
		var archived int
		var threadSource sql.NullString
		var rolloutPath sql.NullString
		if err := rows.Scan(&task.TaskID, &updatedAtMS, &task.DerivedTitle, &name, &archived, &task.Source, &threadSource, &rolloutPath); err != nil {
			return Inventory{}, fmt.Errorf("read Codex task: %w", err)
		}
		task.Revision = strconv.FormatInt(updatedAtMS, 10)
		task.Title = task.DerivedTitle
		if name.Valid {
			task.Title = name.String
		}
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
		if !ok || previous.CapturedRevision != task.Revision || previous.CapturedTitle != task.Title || previous.Retry != nil {
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

func sqliteHomeFromConfig(data []byte) (string, bool, error) {
	inTable := false
	multiline := ""
	for _, line := range strings.Split(string(data), "\n") {
		if multiline != "" {
			if strings.Contains(line, multiline) {
				multiline = ""
			}
			continue
		}
		if delimiter := openedMultilineTOMLString(line); delimiter != "" {
			multiline = delimiter
			continue
		}
		line = strings.TrimSpace(trimTOMLComment(line))
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") {
			inTable = true
			continue
		}
		if inTable {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found || strings.TrimSpace(key) != "sqlite_home" {
			continue
		}
		parsed, err := parseTOMLString(strings.TrimSpace(value))
		if err != nil {
			return "", false, err
		}
		if strings.TrimSpace(parsed) == "" {
			return "", false, errors.New("sqlite_home must not be empty")
		}
		return parsed, true, nil
	}
	return "", false, nil
}

func openedMultilineTOMLString(value string) string {
	for index := 0; index < len(value); {
		switch value[index] {
		case '#':
			return ""
		case '"':
			delimiter := `"""`
			if strings.HasPrefix(value[index:], delimiter) {
				remainder := value[index+len(delimiter):]
				closing := strings.Index(remainder, delimiter)
				if closing < 0 {
					return delimiter
				}
				index += len(delimiter) + closing + len(delimiter)
				continue
			}
			index++
			escaped := false
			for index < len(value) {
				char := value[index]
				index++
				if escaped {
					escaped = false
					continue
				}
				if char == '\\' {
					escaped = true
					continue
				}
				if char == '"' {
					break
				}
			}
		case '\'':
			delimiter := "'''"
			if strings.HasPrefix(value[index:], delimiter) {
				remainder := value[index+len(delimiter):]
				closing := strings.Index(remainder, delimiter)
				if closing < 0 {
					return delimiter
				}
				index += len(delimiter) + closing + len(delimiter)
				continue
			}
			index++
			for index < len(value) && value[index] != '\'' {
				index++
			}
			if index < len(value) {
				index++
			}
		default:
			index++
		}
	}
	return ""
}

func trimTOMLComment(value string) string {
	var quote byte
	escaped := false
	for index := 0; index < len(value); index++ {
		char := value[index]
		if quote == '"' && escaped {
			escaped = false
			continue
		}
		if quote == '"' && char == '\\' {
			escaped = true
			continue
		}
		if quote != 0 {
			if char == quote {
				quote = 0
			}
			continue
		}
		if char == '"' || char == '\'' {
			quote = char
			continue
		}
		if char == '#' {
			return value[:index]
		}
	}
	return value
}

func parseTOMLString(value string) (string, error) {
	if len(value) < 2 {
		return "", errors.New("sqlite_home must be a TOML string")
	}
	if value[0] == '\'' && value[len(value)-1] == '\'' {
		return value[1 : len(value)-1], nil
	}
	if value[0] != '"' || value[len(value)-1] != '"' {
		return "", errors.New("sqlite_home must be a TOML string")
	}
	parsed, err := strconv.Unquote(value)
	if err != nil {
		return "", fmt.Errorf("parse sqlite_home: %w", err)
	}
	return parsed, nil
}

func readOnlyURI(path string) string {
	value := url.URL{Scheme: "file", Path: filepath.ToSlash(path)}
	query := value.Query()
	query.Set("mode", "ro")
	value.RawQuery = query.Encode()
	return value.String()
}
