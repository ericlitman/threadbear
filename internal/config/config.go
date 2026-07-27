package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/ericlitman/threadbear/internal/tokens"
)

const (
	legacySchemaVersion                 = 1
	tokenDisplaySchemaVersion           = 2
	CurrentSchemaVersion                = 3
	ProductName                         = "ThreadBear"
	ControlTaskTitle                    = "🧵🐻 ThreadBear 🐻🧵"
	Website                             = "https://threadbear.sh"
	BinaryPath                          = "~/.local/bin/threadbear"
	StateDirectory                      = "~/.local/share/threadbear"
	LaunchAgentLabel                    = "org.litman.threadbear"
	DefaultHeartbeatSeconds             = 300
	DefaultArchiveAfterDays             = 14
	DefaultClassifierModel              = "gpt-5.6-luna"
	DefaultClassifierContextBudgetBytes = 250000
	AutoUpdateCheckInterval             = 30 * time.Minute
	NotifyOnlyUpdateCheckInterval       = 24 * time.Hour
)

type ClassifierEffort string

const (
	EffortLow    ClassifierEffort = "low"
	EffortMedium ClassifierEffort = "medium"
	EffortHigh   ClassifierEffort = "high"
	EffortXHigh  ClassifierEffort = "xhigh"
)

var ErrUnsupportedSchema = errors.New("unsupported config schema")

type Config struct {
	SchemaVersion                int              `json:"schema_version"`
	ControlTaskID                string           `json:"control_task_id"`
	CodexExecutable              string           `json:"codex_executable,omitempty"`
	CodexSpawnPath               string           `json:"codex_spawn_path,omitempty"`
	HeartbeatSeconds             int              `json:"heartbeat_seconds"`
	ArchiveEnabled               bool             `json:"archive_enabled"`
	ArchiveAfterDays             int              `json:"archive_after_days"`
	RenameEnabled                bool             `json:"rename_enabled"`
	AutoUpdateEnabled            bool             `json:"auto_update_enabled"`
	TokenDisplay                 tokens.Position  `json:"token_display"`
	AgentsEnabled                bool             `json:"agents_enabled"`
	ClassifierModel              string           `json:"classifier_model"`
	ClassifierEffort             ClassifierEffort `json:"classifier_effort"`
	ClassifierContextBudgetBytes int              `json:"classifier_context_budget_bytes"`
}

func Default(controlTaskID string) Config {
	return Config{
		SchemaVersion:                CurrentSchemaVersion,
		ControlTaskID:                controlTaskID,
		HeartbeatSeconds:             DefaultHeartbeatSeconds,
		ArchiveEnabled:               true,
		ArchiveAfterDays:             DefaultArchiveAfterDays,
		RenameEnabled:                true,
		AutoUpdateEnabled:            true,
		TokenDisplay:                 tokens.PositionStart,
		AgentsEnabled:                true,
		ClassifierModel:              DefaultClassifierModel,
		ClassifierEffort:             EffortMedium,
		ClassifierContextBudgetBytes: DefaultClassifierContextBudgetBytes,
	}
}

func (c Config) Validate() error { return c.validate(true, true) }

func UpdateCheckInterval(autoUpdateEnabled bool) time.Duration {
	if autoUpdateEnabled {
		return AutoUpdateCheckInterval
	}
	return NotifyOnlyUpdateCheckInterval
}

func (c Config) validate(requireBudget, requirePair bool) error {
	if c.SchemaVersion != CurrentSchemaVersion {
		return fmt.Errorf("%w: got %d, want %d", ErrUnsupportedSchema, c.SchemaVersion, CurrentSchemaVersion)
	}
	if c.ControlTaskID == "" {
		return errors.New("control_task_id is required")
	}
	if strings.TrimSpace(c.ControlTaskID) != c.ControlTaskID {
		return errors.New("control_task_id must not contain surrounding whitespace")
	}
	if c.CodexExecutable != "" && (!filepath.IsAbs(c.CodexExecutable) || strings.TrimSpace(c.CodexExecutable) != c.CodexExecutable) {
		return errors.New("codex_executable must be an absolute path")
	}
	if c.CodexSpawnPath != "" && c.CodexExecutable == "" {
		return errors.New("codex_spawn_path requires codex_executable")
	}
	if requirePair && c.CodexExecutable != "" && c.CodexSpawnPath == "" {
		return errors.New("codex_executable requires codex_spawn_path")
	}
	if c.CodexSpawnPath != "" && sanitizePath(c.CodexSpawnPath) != c.CodexSpawnPath {
		return errors.New("codex_spawn_path must be canonical, absolute-only, and deduplicated")
	}
	if c.HeartbeatSeconds <= 0 {
		return errors.New("heartbeat_seconds must be positive")
	}
	if c.ArchiveAfterDays <= 0 {
		return errors.New("archive_after_days must be positive")
	}
	if !c.TokenDisplay.Valid() {
		return fmt.Errorf("token_display %q is unsupported", c.TokenDisplay)
	}
	if c.ClassifierModel == "" {
		return errors.New("classifier_model is required")
	}
	if strings.TrimSpace(c.ClassifierModel) != c.ClassifierModel {
		return errors.New("classifier_model must not contain surrounding whitespace")
	}
	if requireBudget && c.ClassifierContextBudgetBytes <= 0 {
		return errors.New("classifier_context_budget_bytes must be positive")
	}
	switch c.ClassifierEffort {
	case EffortLow, EffortMedium, EffortHigh, EffortXHigh:
	default:
		return fmt.Errorf("classifier_effort %q is unsupported", c.ClassifierEffort)
	}
	return nil
}

func Decode(data []byte) (Config, error) {
	var envelope struct {
		SchemaVersion *int `json:"schema_version"`
	}
	if err := decodeStrict(data, &envelope, false); err != nil {
		return Config{}, fmt.Errorf("read config schema: %w", err)
	}
	if envelope.SchemaVersion == nil {
		return Config{}, fmt.Errorf("%w: got 0, want %d", ErrUnsupportedSchema, CurrentSchemaVersion)
	}
	schemaVersion := *envelope.SchemaVersion
	if schemaVersion < legacySchemaVersion || schemaVersion > CurrentSchemaVersion {
		return Config{}, fmt.Errorf("%w: got %d, want %d", ErrUnsupportedSchema, schemaVersion, CurrentSchemaVersion)
	}
	var wire struct {
		SchemaVersion                *int              `json:"schema_version"`
		ControlTaskID                *string           `json:"control_task_id"`
		CodexExecutable              *string           `json:"codex_executable"`
		CodexSpawnPath               json.RawMessage   `json:"codex_spawn_path"`
		HeartbeatSeconds             *int              `json:"heartbeat_seconds"`
		ArchiveEnabled               *bool             `json:"archive_enabled"`
		ArchiveAfterDays             *int              `json:"archive_after_days"`
		RenameEnabled                *bool             `json:"rename_enabled"`
		AutoUpdateEnabled            *bool             `json:"auto_update_enabled"`
		TokenDisplay                 *tokens.Position  `json:"token_display"`
		AgentsEnabled                *bool             `json:"agents_enabled"`
		ClassifierModel              *string           `json:"classifier_model"`
		ClassifierEffort             *ClassifierEffort `json:"classifier_effort"`
		ClassifierContextBudgetBytes *int              `json:"classifier_context_budget_bytes"`
	}
	if err := decodeStrict(data, &wire, true); err != nil {
		return Config{}, fmt.Errorf("decode config: %w", err)
	}
	if wire.ControlTaskID == nil || wire.HeartbeatSeconds == nil || wire.ArchiveEnabled == nil || wire.ArchiveAfterDays == nil || wire.RenameEnabled == nil || wire.AgentsEnabled == nil || wire.ClassifierModel == nil || wire.ClassifierEffort == nil {
		return Config{}, errors.New("config is missing a required field")
	}
	if schemaVersion == legacySchemaVersion && wire.TokenDisplay != nil {
		return Config{}, errors.New("decode config: token_display is not valid in schema version 1")
	}
	if schemaVersion >= tokenDisplaySchemaVersion && (wire.TokenDisplay == nil || wire.ClassifierContextBudgetBytes == nil) {
		return Config{}, errors.New("config is missing a required field")
	}
	if schemaVersion == CurrentSchemaVersion && wire.AutoUpdateEnabled == nil {
		return Config{}, errors.New("config is missing a required field")
	}
	spawnPath, err := decodeSpawnPath(wire.CodexSpawnPath, schemaVersion == legacySchemaVersion)
	if err != nil {
		return Config{}, fmt.Errorf("decode config: %w", err)
	}
	budget := 0
	if wire.ClassifierContextBudgetBytes != nil {
		budget = *wire.ClassifierContextBudgetBytes
	} else if *wire.ClassifierModel == DefaultClassifierModel {
		budget = DefaultClassifierContextBudgetBytes
	}
	tokenDisplay := tokens.PositionOff
	if schemaVersion >= tokenDisplaySchemaVersion {
		tokenDisplay = *wire.TokenDisplay
	}
	autoUpdateEnabled := true
	if schemaVersion == CurrentSchemaVersion {
		autoUpdateEnabled = *wire.AutoUpdateEnabled
	}
	c := Config{
		SchemaVersion:                CurrentSchemaVersion,
		ControlTaskID:                *wire.ControlTaskID,
		CodexExecutable:              optionalString(wire.CodexExecutable),
		CodexSpawnPath:               spawnPath,
		HeartbeatSeconds:             *wire.HeartbeatSeconds,
		ArchiveEnabled:               *wire.ArchiveEnabled,
		ArchiveAfterDays:             *wire.ArchiveAfterDays,
		RenameEnabled:                *wire.RenameEnabled,
		AutoUpdateEnabled:            autoUpdateEnabled,
		TokenDisplay:                 tokenDisplay,
		AgentsEnabled:                *wire.AgentsEnabled,
		ClassifierModel:              *wire.ClassifierModel,
		ClassifierEffort:             *wire.ClassifierEffort,
		ClassifierContextBudgetBytes: budget,
	}
	strict := schemaVersion >= tokenDisplaySchemaVersion
	if err := c.validate(strict, strict); err != nil {
		return Config{}, err
	}
	return c, nil
}

func decodeSpawnPath(raw json.RawMessage, allowLegacyArray bool) (string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err == nil {
		return value, nil
	}
	if !allowLegacyArray {
		return "", errors.New("codex_spawn_path must be a string")
	}
	var legacy []string
	if err := json.Unmarshal(raw, &legacy); err != nil {
		return "", errors.New("codex_spawn_path must be a string")
	}
	return sanitizePath(strings.Join(legacy, string(filepath.ListSeparator))), nil
}

func sanitizePath(value string) string {
	result := make([]string, 0)
	seen := make(map[string]bool)
	for _, entry := range filepath.SplitList(value) {
		if !filepath.IsAbs(entry) {
			continue
		}
		entry = filepath.Clean(entry)
		if seen[entry] {
			continue
		}
		seen[entry] = true
		result = append(result, entry)
	}
	return strings.Join(result, string(filepath.ListSeparator))
}

func optionalString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
func decodeStrict(data []byte, target any, disallowUnknown bool) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if disallowUnknown {
		decoder.DisallowUnknownFields()
	}
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}
