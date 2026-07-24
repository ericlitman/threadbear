package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
)

const (
	CurrentSchemaVersion                = 1
	ProductName                         = "ThreadBear"
	ControlTaskTitle                    = "🧵🐻 ThreadBear 🐻🧵"
	Website                             = "https://threadbear.dev"
	BinaryPath                          = "~/.local/bin/threadbear"
	StateDirectory                      = "~/.local/share/threadbear"
	LaunchAgentLabel                    = "org.litman.threadbear"
	DefaultHeartbeatSeconds             = 300
	DefaultArchiveAfterDays             = 14
	DefaultClassifierModel              = "gpt-5.6-luna"
	DefaultClassifierContextBudgetBytes = 250000
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
	HeartbeatSeconds             int              `json:"heartbeat_seconds"`
	ArchiveEnabled               bool             `json:"archive_enabled"`
	ArchiveAfterDays             int              `json:"archive_after_days"`
	RenameEnabled                bool             `json:"rename_enabled"`
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
		AgentsEnabled:                true,
		ClassifierModel:              DefaultClassifierModel,
		ClassifierEffort:             EffortMedium,
		ClassifierContextBudgetBytes: DefaultClassifierContextBudgetBytes,
	}
}

func (c Config) Validate() error {
	return c.validate(true)
}

func (c Config) validate(requireBudget bool) error {
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
	if c.HeartbeatSeconds <= 0 {
		return errors.New("heartbeat_seconds must be positive")
	}
	if c.ArchiveAfterDays <= 0 {
		return errors.New("archive_after_days must be positive")
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
	if envelope.SchemaVersion == nil || *envelope.SchemaVersion != CurrentSchemaVersion {
		found := 0
		if envelope.SchemaVersion != nil {
			found = *envelope.SchemaVersion
		}
		return Config{}, fmt.Errorf("%w: got %d, want %d", ErrUnsupportedSchema, found, CurrentSchemaVersion)
	}

	var wire struct {
		SchemaVersion                *int              `json:"schema_version"`
		ControlTaskID                *string           `json:"control_task_id"`
		CodexExecutable              *string           `json:"codex_executable"`
		HeartbeatSeconds             *int              `json:"heartbeat_seconds"`
		ArchiveEnabled               *bool             `json:"archive_enabled"`
		ArchiveAfterDays             *int              `json:"archive_after_days"`
		RenameEnabled                *bool             `json:"rename_enabled"`
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
	budget := 0
	if wire.ClassifierContextBudgetBytes != nil {
		budget = *wire.ClassifierContextBudgetBytes
	} else if *wire.ClassifierModel == DefaultClassifierModel {
		budget = DefaultClassifierContextBudgetBytes
	}
	c := Config{
		SchemaVersion:                *wire.SchemaVersion,
		ControlTaskID:                *wire.ControlTaskID,
		CodexExecutable:              optionalString(wire.CodexExecutable),
		HeartbeatSeconds:             *wire.HeartbeatSeconds,
		ArchiveEnabled:               *wire.ArchiveEnabled,
		ArchiveAfterDays:             *wire.ArchiveAfterDays,
		RenameEnabled:                *wire.RenameEnabled,
		AgentsEnabled:                *wire.AgentsEnabled,
		ClassifierModel:              *wire.ClassifierModel,
		ClassifierEffort:             *wire.ClassifierEffort,
		ClassifierContextBudgetBytes: budget,
	}
	if err := c.validate(false); err != nil {
		return Config{}, err
	}
	return c, nil
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
