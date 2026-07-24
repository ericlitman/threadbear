package appserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

var ErrCapability = errors.New("required Codex App Server capability is unavailable")

type ProcessSpec struct {
	Path string
	Args []string
	Env  []string
}

func DefaultProcessSpec(codexHome string) ProcessSpec {
	home, _ := os.UserHomeDir()
	if strings.TrimSpace(codexHome) == "" {
		codexHome = filepath.Join(home, ".codex")
	}
	return ProcessSpec{Path: "codex", Env: []string{"CODEX_HOME=" + codexHome, "HOME=" + home, "LC_ALL=C", "PATH=/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin", "TMPDIR=" + os.TempDir()}}
}
func (s ProcessSpec) command(ctx context.Context, arguments ...string) (*exec.Cmd, error) {
	if strings.TrimSpace(s.Path) == "" {
		return nil, errors.New("App Server executable is required")
	}
	if s.Env == nil {
		return nil, errors.New("App Server environment must be explicit")
	}
	executable, err := resolveExecutable(s.Path, s.Env)
	if err != nil {
		return nil, err
	}
	args := append(append([]string{}, s.Args...), arguments...)
	command := exec.CommandContext(ctx, executable, args...)
	command.Env = append([]string{}, s.Env...)
	return command, nil
}

func resolveExecutable(name string, environment []string) (string, error) {
	if filepath.IsAbs(name) || strings.ContainsRune(name, os.PathSeparator) {
		return name, nil
	}
	pathValue := ""
	for _, entry := range environment {
		if strings.HasPrefix(entry, "PATH=") {
			pathValue = strings.TrimPrefix(entry, "PATH=")
			break
		}
	}
	for _, directory := range filepath.SplitList(pathValue) {
		candidate := filepath.Join(directory, name)
		info, err := os.Stat(candidate)
		if err == nil && !info.IsDir() && info.Mode().Perm()&0111 != 0 {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("resolve App Server executable %s from explicit PATH: %w", name, exec.ErrNotFound)
}

type Capabilities struct {
	Methods           map[string]bool
	ThreadStartFields map[string]bool
	TurnStartFields   map[string]bool
}

func (c Capabilities) HasMethod(method string) bool          { return c.Methods[method] }
func (c Capabilities) HasThreadStartField(field string) bool { return c.ThreadStartFields[field] }
func (c Capabilities) HasTurnStartField(field string) bool   { return c.TurnStartFields[field] }
func (c Capabilities) RecentTurnsMethod() string {
	if c.HasMethod("thread/turns/list") {
		return "thread/turns/list"
	}
	if c.HasMethod("thread/read") {
		return "thread/read"
	}
	return ""
}
func (c Capabilities) ToolRestrictionCandidates() ToolRestriction {
	return ToolRestriction{ConfigOverride: c.HasThreadStartField("config"), PermissionProfile: c.HasThreadStartField("permissions") && c.HasTurnStartField("permissions"), EnvironmentsDisabled: c.HasThreadStartField("environments") && c.HasTurnStartField("environments"), DynamicToolsDisabled: c.HasThreadStartField("dynamicTools"), ApprovalsDisabled: c.HasThreadStartField("approvalPolicy") && c.HasTurnStartField("approvalPolicy"), ReadOnlySandbox: c.HasThreadStartField("sandbox") && c.HasTurnStartField("sandboxPolicy"), OutputConstrained: c.HasTurnStartField("outputSchema")}
}
func (c Capabilities) requireMethod(method string) error {
	if !c.HasMethod(method) {
		return fmt.Errorf("%w: method %s", ErrCapability, method)
	}
	return nil
}
func (c Capabilities) requireEphemeral(request EphemeralRequest) (ToolRestriction, error) {
	for _, method := range []string{"thread/start", "turn/start"} {
		if err := c.requireMethod(method); err != nil {
			return ToolRestriction{}, err
		}
	}
	for _, field := range []string{"ephemeral", "model"} {
		if !c.HasThreadStartField(field) {
			return ToolRestriction{}, fmt.Errorf("%w: thread/start.%s", ErrCapability, field)
		}
	}
	for _, field := range []string{"model", "effort", "outputSchema"} {
		if !c.HasTurnStartField(field) {
			return ToolRestriction{}, fmt.Errorf("%w: turn/start.%s", ErrCapability, field)
		}
	}
	restriction := c.ToolRestrictionCandidates()
	if !restriction.EnvironmentsDisabled || !restriction.DynamicToolsDisabled || !restriction.ApprovalsDisabled || !restriction.ReadOnlySandbox || !restriction.OutputConstrained {
		return ToolRestriction{}, fmt.Errorf("%w: compensating tool restriction controls", ErrCapability)
	}
	restriction.ConfigOverride = restriction.ConfigOverride && len(request.ToolConfig) > 0
	restriction.PermissionProfile = restriction.PermissionProfile && request.PermissionProfile != ""
	restriction.UnprovenToolSources = []string{"core", "mcp", "extension", "hosted"}
	return restriction, nil
}
func DiscoverCapabilities(ctx context.Context, process ProcessSpec) (Capabilities, error) {
	directory, err := os.MkdirTemp("", "threadbear-appserver-schema-")
	if err != nil {
		return Capabilities{}, fmt.Errorf("create App Server schema directory: %w", err)
	}
	defer os.RemoveAll(directory)
	command, err := process.command(ctx, "app-server", "generate-json-schema", "--out", directory, "--experimental")
	if err != nil {
		return Capabilities{}, err
	}
	output, err := command.CombinedOutput()
	if err != nil {
		return Capabilities{}, fmt.Errorf("generate App Server schema: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return LoadCapabilities(directory)
}
func LoadCapabilities(directory string) (Capabilities, error) {
	clientRequest, err := readSchema(filepath.Join(directory, "ClientRequest.json"))
	if err != nil {
		return Capabilities{}, err
	}
	threadStart, err := readSchema(filepath.Join(directory, "v2", "ThreadStartParams.json"))
	if err != nil {
		return Capabilities{}, err
	}
	turnStart, err := readSchema(filepath.Join(directory, "v2", "TurnStartParams.json"))
	if err != nil {
		return Capabilities{}, err
	}
	methods := make(map[string]bool)
	collectMethods(clientRequest, methods)
	return Capabilities{Methods: methods, ThreadStartFields: schemaProperties(threadStart), TurnStartFields: schemaProperties(turnStart)}, nil
}
func readSchema(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read App Server schema %s: %w", filepath.Base(path), err)
	}
	var schema map[string]any
	if err := json.Unmarshal(data, &schema); err != nil {
		return nil, fmt.Errorf("parse App Server schema %s: %w", filepath.Base(path), err)
	}
	return schema, nil
}
func schemaProperties(schema map[string]any) map[string]bool {
	properties, _ := schema["properties"].(map[string]any)
	result := make(map[string]bool, len(properties))
	for name := range properties {
		result[name] = true
	}
	return result
}
func collectMethods(value any, methods map[string]bool) {
	switch value := value.(type) {
	case map[string]any:
		if properties, ok := value["properties"].(map[string]any); ok {
			if method, ok := properties["method"].(map[string]any); ok {
				if constant, ok := method["const"].(string); ok {
					methods[constant] = true
				}
				if values, ok := method["enum"].([]any); ok {
					for _, candidate := range values {
						if name, ok := candidate.(string); ok {
							methods[name] = true
						}
					}
				}
			}
		}
		for _, nested := range value {
			collectMethods(nested, methods)
		}
	case []any:
		for _, nested := range value {
			collectMethods(nested, methods)
		}
	}
}
func (c Capabilities) MethodNames() []string {
	names := make([]string, 0, len(c.Methods))
	for name := range c.Methods {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
