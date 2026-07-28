package launchagent

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"text/template"

	"github.com/ericlitman/threadbear/assets"
	"github.com/ericlitman/threadbear/internal/config"
)

const (
	Label         = config.LaunchAgentLabel
	DefaultLocale = "C"
)

type PlistSpec struct {
	Label         string
	BinaryPath    string
	StartInterval int
	Home          string
	CodexHome     string
	Path          string
	LCAll         string
	StdoutPath    string
	StderrPath    string
}

func RenderPlist(spec PlistSpec) ([]byte, error) {
	if spec.Label == "" {
		return nil, errors.New("LaunchAgent label is required")
	}
	if !filepath.IsAbs(spec.BinaryPath) {
		return nil, errors.New("LaunchAgent binary path must be absolute")
	}
	if spec.StartInterval <= 0 {
		return nil, errors.New("LaunchAgent interval must be positive")
	}
	for name, value := range map[string]string{"HOME": spec.Home, "CODEX_HOME": spec.CodexHome, "PATH": spec.Path, "LC_ALL": spec.LCAll, "stdout log": spec.StdoutPath, "stderr log": spec.StderrPath} {
		if value == "" {
			return nil, fmt.Errorf("LaunchAgent %s is required", name)
		}
	}
	for name, value := range map[string]string{"HOME": spec.Home, "CODEX_HOME": spec.CodexHome, "stdout log": spec.StdoutPath, "stderr log": spec.StderrPath} {
		if !filepath.IsAbs(value) {
			return nil, fmt.Errorf("LaunchAgent %s must be absolute", name)
		}
	}
	tmpl, err := template.New("launchagent").Funcs(template.FuncMap{"xml": escapeXML}).Parse(assets.LaunchAgentPlistTemplate)
	if err != nil {
		return nil, fmt.Errorf("parse LaunchAgent template: %w", err)
	}
	var rendered bytes.Buffer
	if err := tmpl.Execute(&rendered, spec); err != nil {
		return nil, fmt.Errorf("render LaunchAgent template: %w", err)
	}
	return rendered.Bytes(), nil
}

func escapeXML(value string) string {
	var escaped bytes.Buffer
	_ = xml.EscapeText(&escaped, []byte(value))
	return escaped.String()
}

func writePrivateAtomic(path string, data []byte) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create LaunchAgent directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".threadbear-plist-*")
	if err != nil {
		return fmt.Errorf("create temporary LaunchAgent plist: %w", err)
	}
	temporaryPath := temporary.Name()
	committed := false
	defer func() {
		_ = temporary.Close()
		if !committed {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return fmt.Errorf("make temporary LaunchAgent plist private: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		return fmt.Errorf("write temporary LaunchAgent plist: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sync temporary LaunchAgent plist: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary LaunchAgent plist: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace LaunchAgent plist: %w", err)
	}
	committed = true
	directoryHandle, err := os.Open(directory)
	if err != nil {
		return fmt.Errorf("open LaunchAgent directory: %w", err)
	}
	defer directoryHandle.Close()
	if err := directoryHandle.Sync(); err != nil {
		return fmt.Errorf("sync LaunchAgent directory: %w", err)
	}
	return nil
}
