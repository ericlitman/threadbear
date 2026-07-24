package codex

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var ErrExecutableNotFound = errors.New("Codex executable not found")

func ResolveExecutable(home, pathValue string) (string, error) {
	return resolveExecutable(home, pathValue, []string{
		"/opt/homebrew/bin/codex",
		"/usr/local/bin/codex",
		filepath.Join(home, ".local", "bin", "codex"),
	})
}

func resolveExecutable(home, pathValue string, standard []string) (string, error) {
	if !filepath.IsAbs(home) {
		return "", errors.New("user home must be absolute")
	}
	candidates := make([]string, 0)
	for _, directory := range filepath.SplitList(pathValue) {
		if directory == "" || !filepath.IsAbs(directory) {
			continue
		}
		candidates = append(candidates, filepath.Join(directory, "codex"))
	}
	candidates = append(candidates, standard...)
	seen := make(map[string]bool, len(candidates))
	verificationFailures := make([]string, 0)
	for _, candidate := range candidates {
		candidate = filepath.Clean(candidate)
		if seen[candidate] {
			continue
		}
		seen[candidate] = true
		info, err := os.Stat(candidate)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
			continue
		}
		if err := verifyExecutable(candidate); err != nil {
			verificationFailures = append(verificationFailures, fmt.Sprintf("%s: %v", candidate, err))
			continue
		}
		return candidate, nil
	}
	remedy := fmt.Sprintf("install the Codex CLI and ensure its absolute bin directory is in PATH, or place codex at /opt/homebrew/bin/codex, /usr/local/bin/codex, or %s", filepath.Join(home, ".local", "bin", "codex"))
	if len(verificationFailures) > 0 {
		return "", fmt.Errorf("%w; executable candidates did not run successfully (%s); %s", ErrExecutableNotFound, strings.Join(verificationFailures, "; "), remedy)
	}
	return "", fmt.Errorf("%w; %s", ErrExecutableNotFound, remedy)
}

func ValidateExecutable(path string) error {
	if strings.TrimSpace(path) != path || !filepath.IsAbs(path) {
		return errors.New("Codex executable path must be absolute")
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("inspect Codex executable: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return errors.New("Codex executable must be a regular executable file")
	}
	return nil
}

func verifyExecutable(path string) error {
	devNull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("open null device: %w", err)
	}
	defer devNull.Close()
	process, err := os.StartProcess(path, []string{path, "--version"}, &os.ProcAttr{Files: []*os.File{devNull, devNull, devNull}})
	if err != nil {
		return fmt.Errorf("start codex --version: %w", err)
	}
	type waitResult struct {
		state *os.ProcessState
		err   error
	}
	done := make(chan waitResult, 1)
	go func() { state, waitErr := process.Wait(); done <- waitResult{state: state, err: waitErr} }()
	select {
	case result := <-done:
		if result.err != nil {
			return fmt.Errorf("wait for codex --version: %w", result.err)
		}
		if !result.state.Success() {
			return fmt.Errorf("codex --version exited with %s", result.state.String())
		}
		return nil
	case <-time.After(5 * time.Second):
		_ = process.Kill()
		<-done
		return errors.New("codex --version timed out")
	}
}
