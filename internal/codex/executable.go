package codex

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var ErrExecutableNotFound = errors.New("Codex executable not found")

var fixedSystemPath = []string{"/usr/local/bin", "/usr/bin", "/bin", "/usr/sbin", "/sbin"}

type ExecutableSpec struct {
	Path      string
	SpawnPath []string
}

func ResolveExecutable(home, pathValue string) (string, error) {
	spec, err := ResolveExecutableSpec(home, pathValue)
	return spec.Path, err
}

func ResolveExecutableSpec(home, pathValue string) (ExecutableSpec, error) {
	return resolveExecutableSpec(home, pathValue, []string{
		"/opt/homebrew/bin/codex",
		"/usr/local/bin/codex",
		filepath.Join(home, ".local", "bin", "codex"),
	})
}

func resolveExecutable(home, pathValue string, standard []string) (string, error) {
	spec, err := resolveExecutableSpec(home, pathValue, standard)
	return spec.Path, err
}

func resolveExecutableSpec(home, pathValue string, standard []string) (ExecutableSpec, error) {
	if !filepath.IsAbs(home) {
		return ExecutableSpec{}, errors.New("user home must be absolute")
	}
	pathDirectories := absolutePathDirectories(pathValue)
	candidates := make([]string, 0, len(pathDirectories)+len(standard))
	for _, directory := range pathDirectories {
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
		if err := ValidateExecutable(candidate); err != nil {
			continue
		}
		spec, err := DeriveExecutableSpec(home, candidate, pathValue)
		if err != nil {
			verificationFailures = append(verificationFailures, fmt.Sprintf("%s: %v", candidate, err))
			continue
		}
		if err := VerifyExecutableSpec(spec); err != nil {
			verificationFailures = append(verificationFailures, fmt.Sprintf("%s: %v", candidate, err))
			continue
		}
		return spec, nil
	}
	remedy := fmt.Sprintf("install the Codex CLI and ensure its absolute bin directory is in PATH, or place codex at /opt/homebrew/bin/codex, /usr/local/bin/codex, or %s", filepath.Join(home, ".local", "bin", "codex"))
	if len(verificationFailures) > 0 {
		return ExecutableSpec{}, fmt.Errorf("%w; executable candidates did not run successfully (%s); %s", ErrExecutableNotFound, strings.Join(verificationFailures, "; "), remedy)
	}
	return ExecutableSpec{}, fmt.Errorf("%w; %s", ErrExecutableNotFound, remedy)
}

func DeriveExecutableSpec(home, executable, pathValue string) (ExecutableSpec, error) {
	if !filepath.IsAbs(home) {
		return ExecutableSpec{}, errors.New("user home must be absolute")
	}
	if err := ValidateExecutable(executable); err != nil {
		return ExecutableSpec{}, err
	}
	directories := append([]string{}, absolutePathDirectories(pathValue)...)
	directories = append(directories, filepath.Dir(executable), "/opt/homebrew/bin", "/usr/local/bin", filepath.Join(home, ".local", "bin"), "/usr/bin", "/bin", "/usr/sbin", "/sbin")
	spec := ExecutableSpec{Path: executable, SpawnPath: uniqueCanonicalDirectories(directories)}
	if err := ValidateExecutableSpec(spec); err != nil {
		return ExecutableSpec{}, err
	}
	interpreter, envShebang, err := envShebangInterpreter(executable)
	if err != nil {
		return ExecutableSpec{}, err
	}
	if envShebang {
		if _, err := resolveNamedExecutable(interpreter, spec.SpawnPath); err != nil {
			return ExecutableSpec{}, fmt.Errorf("resolve Codex shebang interpreter %s: %w", interpreter, err)
		}
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
	if len(spec.SpawnPath) == 0 {
		return errors.New("Codex spawn PATH must not be empty")
	}
	for _, directory := range spec.SpawnPath {
		if directory == "" || strings.TrimSpace(directory) != directory || !filepath.IsAbs(directory) {
			return errors.New("Codex spawn PATH entries must be nonempty absolute directories")
		}
	}
	return nil
}

func VerifyExecutableSpec(spec ExecutableSpec) error {
	if err := ValidateExecutableSpec(spec); err != nil {
		return err
	}
	devNull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("open null device: %w", err)
	}
	defer devNull.Close()
	process, err := os.StartProcess(spec.Path, []string{spec.Path, "--version"}, &os.ProcAttr{
		Env:   []string{"PATH=" + strings.Join(spec.SpawnPath, string(os.PathListSeparator))},
		Files: []*os.File{devNull, devNull, devNull},
	})
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

func envShebangInterpreter(path string) (string, bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", false, fmt.Errorf("inspect Codex shebang: %w", err)
	}
	defer file.Close()
	line, err := bufio.NewReader(file).ReadString('\n')
	if err != nil && len(line) == 0 {
		return "", false, nil
	}
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "#!") {
		return "", false, nil
	}
	fields := strings.Fields(strings.TrimSpace(strings.TrimPrefix(line, "#!")))
	if len(fields) == 0 || fields[0] != "/usr/bin/env" {
		return "", false, nil
	}
	if len(fields) != 2 || fields[1] == "" || strings.ContainsRune(fields[1], os.PathSeparator) {
		return "", false, errors.New("Codex /usr/bin/env shebang must name one interpreter")
	}
	return fields[1], true, nil
}

func absolutePathDirectories(pathValue string) []string {
	result := make([]string, 0)
	for _, directory := range filepath.SplitList(pathValue) {
		if directory == "" || !filepath.IsAbs(directory) {
			continue
		}
		result = append(result, filepath.Clean(directory))
	}
	return uniqueCanonicalDirectories(result)
}

func uniqueCanonicalDirectories(directories []string) []string {
	result := make([]string, 0, len(directories))
	seen := make(map[string]bool, len(directories))
	for _, directory := range directories {
		directory = filepath.Clean(directory)
		if !filepath.IsAbs(directory) || seen[directory] {
			continue
		}
		seen[directory] = true
		result = append(result, directory)
	}
	return result
}

func resolveNamedExecutable(name string, directories []string) (string, error) {
	for _, directory := range uniqueCanonicalDirectories(directories) {
		candidate := filepath.Join(directory, name)
		info, err := os.Stat(candidate)
		if err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0 {
			return candidate, nil
		}
	}
	return "", os.ErrNotExist
}
