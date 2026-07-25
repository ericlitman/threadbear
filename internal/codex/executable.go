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
var executableProbeTimeout = 5 * time.Second

const DefaultSpawnPath = "/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"

type ExecutableSpec struct {
	Path      string
	SpawnPath string
}

func SanitizePath(value string) string {
	seen := map[string]bool{}
	result := []string{}
	for _, directory := range filepath.SplitList(value) {
		if directory == "" || !filepath.IsAbs(directory) {
			continue
		}
		directory = filepath.Clean(directory)
		if seen[directory] {
			continue
		}
		seen[directory] = true
		result = append(result, directory)
	}
	return strings.Join(result, string(os.PathListSeparator))
}
func ValidateSpawnPath(value string) error {
	if value == "" {
		return errors.New("Codex spawn path is required")
	}
	if SanitizePath(value) != value {
		return errors.New("Codex spawn path must be canonical, absolute-only, and deduplicated")
	}
	return nil
}
func ComposeSpawnPath(pinned string) (string, error) {
	if err := ValidateSpawnPath(pinned); err != nil {
		return "", err
	}
	return SanitizePath(pinned + string(os.PathListSeparator) + DefaultSpawnPath), nil
}
func ResolveExecutable(home, pathValue string) (string, error) {
	spec, err := ResolveExecutableSpec(home, pathValue)
	return spec.Path, err
}
func resolveExecutable(home, pathValue string, standard []string) (string, error) {
	spec, err := resolveExecutableSpec(home, pathValue, standard)
	return spec.Path, err
}
func ResolveExecutableSpec(home, pathValue string) (ExecutableSpec, error) {
	return resolveExecutableSpec(home, pathValue, []string{"/opt/homebrew/bin/codex", "/usr/local/bin/codex", filepath.Join(home, ".local", "bin", "codex")})
}
func resolveExecutableSpec(home, pathValue string, standard []string) (ExecutableSpec, error) {
	if !filepath.IsAbs(home) {
		return ExecutableSpec{}, errors.New("user home must be absolute")
	}
	captured := SanitizePath(pathValue)
	candidates := []string{}
	for _, directory := range filepath.SplitList(captured) {
		candidates = append(candidates, filepath.Join(directory, "codex"))
	}
	candidates = append(candidates, standard...)
	seen := map[string]bool{}
	failures := []string{}
	for _, candidate := range candidates {
		candidate = filepath.Clean(candidate)
		if seen[candidate] {
			continue
		}
		seen[candidate] = true
		if ValidateExecutable(candidate) != nil {
			continue
		}
		spec, err := DeriveExecutableSpec(home, candidate, captured)
		if err == nil {
			err = ProbeExecutable(home, spec)
		}
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", candidate, err))
			continue
		}
		return spec, nil
	}
	remedy := fmt.Sprintf("install the Codex CLI and ensure its absolute bin directory is in PATH, or place codex at /opt/homebrew/bin/codex, /usr/local/bin/codex, or %s", filepath.Join(home, ".local", "bin", "codex"))
	if len(failures) > 0 {
		return ExecutableSpec{}, fmt.Errorf("%w; executable candidates did not run successfully (%s); %s", ErrExecutableNotFound, strings.Join(failures, "; "), remedy)
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
	pinned := []string{filepath.Dir(executable)}
	interpreter, usesEnv, err := envShebangInterpreter(executable)
	if err != nil {
		return ExecutableSpec{}, err
	}
	if usesEnv {
		search := SanitizePath(pathValue + string(os.PathListSeparator) + "/opt/homebrew/bin:/usr/local/bin:" + filepath.Join(home, ".local", "bin") + ":/usr/bin:/bin:/usr/sbin:/sbin")
		interpreterPath, resolveErr := resolveNamedExecutable(interpreter, filepath.SplitList(search))
		if resolveErr != nil {
			return ExecutableSpec{}, fmt.Errorf("resolve Codex shebang interpreter %s from invoking PATH or standard locations: %w; install %s or add its absolute bin directory to PATH, then rerun install", interpreter, resolveErr, interpreter)
		}
		pinned = append(pinned, filepath.Dir(interpreterPath))
	}
	spec := ExecutableSpec{Path: executable, SpawnPath: SanitizePath(strings.Join(pinned, string(os.PathListSeparator)))}
	if err := ValidateSpawnPath(spec.SpawnPath); err != nil {
		return ExecutableSpec{}, err
	}
	return spec, nil
}
func ValidateExecutable(path string) error {
	if strings.TrimSpace(path) != path || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return errors.New("Codex executable path must be canonical and absolute")
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
	return ValidateSpawnPath(spec.SpawnPath)
}

func ProbeExecutable(home string, spec ExecutableSpec) error {
	if !filepath.IsAbs(home) {
		return errors.New("user home must be absolute")
	}
	if err := ValidateExecutableSpec(spec); err != nil {
		return err
	}
	spawnPath, err := ComposeSpawnPath(spec.SpawnPath)
	if err != nil {
		return err
	}
	devNull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("open null device: %w", err)
	}
	defer devNull.Close()
	process, err := os.StartProcess(spec.Path, []string{spec.Path, "--version"}, &os.ProcAttr{Env: []string{"HOME=" + home, "PATH=" + spawnPath, "LC_ALL=C"}, Files: []*os.File{devNull, devNull, devNull}})
	if err != nil {
		return fmt.Errorf("start codex --version: %w", err)
	}
	type waitResult struct {
		state *os.ProcessState
		err   error
	}
	done := make(chan waitResult, 1)
	go func() { state, waitErr := process.Wait(); done <- waitResult{state, waitErr} }()
	select {
	case result := <-done:
		if result.err != nil {
			return fmt.Errorf("wait for codex --version: %w", result.err)
		}
		if !result.state.Success() {
			return fmt.Errorf("codex --version exited with %s", result.state.String())
		}
		return nil
	case <-time.After(executableProbeTimeout):
		_ = process.Kill()
		<-done
		return errors.New("codex --version timed out")
	}
}
func VerifyExecutableSpec(home string, spec ExecutableSpec) error {
	return ProbeExecutable(home, spec)
}
func ParseSpawnPath(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	if err := ValidateSpawnPath(value); err != nil {
		return "", err
	}
	return value, nil
}
func FormatSpawnPath(value string) (string, error) { return ParseSpawnPath(value) }
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
	if len(fields) != 2 || strings.ContainsRune(fields[1], os.PathSeparator) {
		return "", false, errors.New("Codex /usr/bin/env shebang must name one interpreter")
	}
	return fields[1], true, nil
}
func resolveNamedExecutable(name string, directories []string) (string, error) {
	for _, directory := range filepath.SplitList(SanitizePath(strings.Join(directories, string(os.PathListSeparator)))) {
		candidate := filepath.Join(directory, name)
		info, err := os.Stat(candidate)
		if err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0 {
			return candidate, nil
		}
	}
	return "", os.ErrNotExist
}
