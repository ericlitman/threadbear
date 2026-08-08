package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

const (
	updateAgentLabel             = "sh.threadbear.update"
	launchctlServiceNotFoundExit = 113
)

var errLaunchAgentNotLoaded = errors.New("LaunchAgent is not loaded")
var launchctlRunner = func(ctx context.Context, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, "/bin/launchctl", args...).CombinedOutput()
}

type updateAgentState struct {
	Label            string   `json:"label"`
	Path             string   `json:"path"`
	Exact            bool     `json:"exact"`
	Loaded           bool     `json:"loaded"`
	ProgramArguments []string `json:"program_arguments"`
}

func updateAgentPath() string {
	return filepath.Join(homeDir(), "Library", "LaunchAgents", updateAgentLabel+".plist")
}
func updateAgentArguments(binary string) []string {
	return []string{binary, "update", "--automatic", "--json"}
}
func updateAgentDomain() string { return "gui/" + strconv.Itoa(os.Getuid()) }
func updateAgentTarget() string { return updateAgentDomain() + "/" + updateAgentLabel }

func updateAgentPlist(binary string) []byte {
	var arguments strings.Builder
	for _, argument := range updateAgentArguments(binary) {
		fmt.Fprintf(&arguments, "<string>%s</string>", html.EscapeString(argument))
	}
	return []byte(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>Label</key><string>` + updateAgentLabel + `</string>
<key>ProgramArguments</key><array>` + arguments.String() + `</array>
<key>StartCalendarInterval</key><dict><key>Hour</key><integer>12</integer><key>Minute</key><integer>0</integer></dict>
<key>EnvironmentVariables</key><dict><key>HOME</key><string>` + html.EscapeString(homeDir()) + `</string><key>CODEX_HOME</key><string>` + html.EscapeString(codexHome()) + `</string></dict>
<key>StandardOutPath</key><string>/dev/null</string>
<key>StandardErrorPath</key><string>/dev/null</string>
</dict></plist>
`)
}

func updateAgentLoaded(ctx context.Context, path, binary string) (bool, error) {
	output, err := launchctlRunner(ctx, "print", updateAgentTarget())
	if err == nil {
		if !exactLoadedUpdateAgent(output, path, binary) {
			return true, errors.New("loaded update LaunchAgent does not match the exact managed plist")
		}
		return true, nil
	}
	var exitError *exec.ExitError
	if errors.Is(err, errLaunchAgentNotLoaded) || errors.As(err, &exitError) && exitError.ExitCode() == launchctlServiceNotFoundExit {
		return false, nil
	}
	return false, fmt.Errorf("inspect %s LaunchAgent: %w: %s", updateAgentLabel, err, strings.TrimSpace(string(output)))
}

func exactLoadedUpdateAgent(output []byte, path, binary string) bool {
	lines := strings.Split(string(output), "\n")
	target, plist, program := false, false, false
	var arguments []string
	for index := 0; index < len(lines); index++ {
		line := strings.TrimSpace(lines[index])
		switch line {
		case updateAgentTarget() + " = {":
			target = true
		case "path = " + path:
			plist = true
		case "program = " + binary:
			program = true
		case "arguments = {":
			for index++; index < len(lines); index++ {
				argument := strings.TrimSpace(lines[index])
				if argument == "}" {
					break
				}
				if argument != "" {
					arguments = append(arguments, argument)
				}
			}
		}
	}
	return target && plist && program && slices.Equal(arguments, updateAgentArguments(binary))
}

func inspectUpdateAgent(ctx context.Context, path, binary string) (updateAgentState, bool, error) {
	info, statErr := os.Lstat(path)
	exists := statErr == nil
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return updateAgentState{}, false, statErr
	}
	if exists && (!info.Mode().IsRegular() || info.Mode().Perm() != 0o600) {
		return updateAgentState{}, true, errors.New("update LaunchAgent path is not a private regular file")
	}
	var data []byte
	if exists {
		var err error
		if data, err = os.ReadFile(path); err != nil {
			return updateAgentState{}, true, err
		}
	}
	loaded, err := updateAgentLoaded(ctx, path, binary)
	state := updateAgentState{
		Label: updateAgentLabel, Path: path, Exact: exists && bytes.Equal(data, updateAgentPlist(binary)),
		Loaded: loaded, ProgramArguments: updateAgentArguments(binary),
	}
	if err != nil {
		return state, exists, err
	}
	if exists && !state.Exact {
		return state, true, errors.New("update LaunchAgent path contains non-ThreadBear content")
	}
	if loaded && !state.Exact {
		return state, exists, errors.New("update LaunchAgent label is loaded without the exact managed plist")
	}
	return state, exists, nil
}

func installUpdateAgent(ctx context.Context, path, binary string) error {
	state, exists, err := inspectUpdateAgent(ctx, path, binary)
	if err != nil {
		return err
	}
	if state.Loaded {
		return nil
	}
	if !exists {
		if err := writeAtomic(path, updateAgentPlist(binary), 0o600); err != nil {
			return err
		}
	}
	output, err := launchctlRunner(ctx, "bootstrap", updateAgentDomain(), path)
	if err != nil {
		if !exists {
			_ = os.Remove(path)
		}
		return fmt.Errorf("bootstrap %s LaunchAgent: %w: %s", updateAgentLabel, err, strings.TrimSpace(string(output)))
	}
	loaded, err := updateAgentLoaded(ctx, path, binary)
	if err != nil || !loaded {
		return errors.Join(err, errors.New("update LaunchAgent did not load"))
	}
	return nil
}

func removeUpdateAgent(ctx context.Context, path, binary string) error {
	state, exists, err := inspectUpdateAgent(ctx, path, binary)
	if err != nil {
		return err
	}
	if state.Loaded {
		output, err := launchctlRunner(ctx, "bootout", updateAgentTarget())
		if err != nil {
			return fmt.Errorf("bootout %s LaunchAgent: %w: %s", updateAgentLabel, err, strings.TrimSpace(string(output)))
		}
	}
	if exists {
		return os.Remove(path)
	}
	return nil
}
