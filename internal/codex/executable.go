package codex

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	for _, candidate := range candidates {
		candidate = filepath.Clean(candidate)
		if seen[candidate] {
			continue
		}
		seen[candidate] = true
		info, err := os.Stat(candidate)
		if err != nil {
			continue
		}
		if info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0 {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("%w; install the Codex CLI and ensure its absolute bin directory is in PATH, or place codex at /opt/homebrew/bin/codex, /usr/local/bin/codex, or %s", ErrExecutableNotFound, filepath.Join(home, ".local", "bin", "codex"))
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
