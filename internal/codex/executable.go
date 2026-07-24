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

type ExecutableSpec struct {
	Path      string
	SpawnPath string
}

func SanitizePath(pathValue string) string {
	result := make([]string, 0)
	seen := make(map[string]bool)
	for _, directory := range filepath.SplitList(pathValue) {
		if !filepath.IsAbs(directory) {
			continue
		}
		directory = filepath.Clean(directory)
		if seen[directory] {
			continue
		}
		seen[directory] = true
		result = append(result, directory)
	}
	return strings.Join(result, string(filepath.ListSeparator))
}

func ResolveExecutable(home, pathValue string) (string, error) {
	spec, err := ResolveExecutableSpec(home, pathValue)
	return spec.Path, err
}

func ResolveExecutableSpec(home, pathValue string) (ExecutableSpec, error) {
	if !filepath.IsAbs(home) {
		return ExecutableSpec{}, errors.New("user home must be absolute")
	}
	spawnPath := SanitizePath(pathValue)
	if spawnPath == "" {
		return ExecutableSpec{}, fmt.Errorf("%w; invoking PATH contains no absolute directories; install the Codex CLI and invoke ThreadBear with the Codex bin directory in PATH", ErrExecutableNotFound)
	}
	verificationFailures := make([]string, 0)
	for _, directory := range filepath.SplitList(spawnPath) {
		candidate := filepath.Join(directory, "codex")
		if err := ValidateExecutable(candidate); err != nil {
			continue
		}
		spec := ExecutableSpec{Path: candidate, SpawnPath: spawnPath}
		if err := VerifyExecutableSpec(home, spec); err != nil {
			verificationFailures = append(verificationFailures, fmt.Sprintf("%s: %v", candidate, err))
			continue
		}
		return spec, nil
	}
	remedy := "install the Codex CLI and invoke ThreadBear with its absolute bin directory in PATH"
	if len(verificationFailures) > 0 {
		return ExecutableSpec{}, fmt.Errorf("%w; candidates from the invoking PATH did not run successfully (%s); %s", ErrExecutableNotFound, strings.Join(verificationFailures, "; "), remedy)
	}
	return ExecutableSpec{}, fmt.Errorf("%w in the invoking PATH; %s", ErrExecutableNotFound, remedy)
}

func DeriveExecutableSpec(home, executable, pathValue string) (ExecutableSpec, error) {
	if !filepath.IsAbs(home) {
		return ExecutableSpec{}, errors.New("user home must be absolute")
	}
	spec := ExecutableSpec{Path: executable, SpawnPath: SanitizePath(pathValue)}
	if err := ValidateExecutableSpec(spec); err != nil {
		return ExecutableSpec{}, err
	}
	return spec, nil
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

func ValidateExecutableSpec(spec ExecutableSpec) error {
	if err := ValidateExecutable(spec.Path); err != nil {
		return err
	}
	if spec.SpawnPath == "" {
		return errors.New("Codex spawn PATH must not be empty")
	}
	if SanitizePath(spec.SpawnPath) != spec.SpawnPath {
		return errors.New("Codex spawn PATH must be canonical, absolute-only, and deduplicated")
	}
	return nil
}

func VerifyExecutableSpec(home string, spec ExecutableSpec) error {
	if !filepath.IsAbs(home) {
		return errors.New("user home must be absolute")
	}
	if err := ValidateExecutableSpec(spec); err != nil {
		return err
	}
	devNull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("open null device: %w", err)
	}
	defer devNull.Close()
	process, err := os.StartProcess(spec.Path, []string{spec.Path, "--version"}, &os.ProcAttr{
		Env:   []string{"HOME=" + home, "PATH=" + spec.SpawnPath, "LC_ALL=C"},
		Files: []*os.File{devNull, devNull, devNull},
	})
	if err != nil {
		return fmt.Errorf("start codex --version with captured PATH: %w", err)
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
		return errors.New("codex --version timed out after 5s")
	}
}
