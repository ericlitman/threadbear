package launchagent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/ericlitman/threadbear/internal/app"
	"github.com/ericlitman/threadbear/internal/config"
)

var _ app.LaunchAgent = (*Adapter)(nil)

type CommandRunner interface {
	Run(context.Context, string, ...string) ([]byte, error)
}

type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, name, args...)
	command.Env = []string{"PATH=/usr/bin:/bin", "LC_ALL=C"}
	return command.CombinedOutput()
}

type Options struct {
	Home            string
	CodexHome       string
	BinaryPath      string
	PlistPath       string
	StdoutPath      string
	StderrPath      string
	Path            string
	LCAll           string
	LaunchctlPath   string
	LegacyPlistPath string
	LegacyLockPath  string
	LegacyLockProbe func(string) error
	UID             int
	Runner          CommandRunner
}

type Adapter struct {
	home            string
	codexHome       string
	binaryPath      string
	plistPath       string
	stdoutPath      string
	stderrPath      string
	path            string
	lcAll           string
	launchctlPath   string
	legacyPlistPath string
	legacyLockPath  string
	legacyLockProbe func(string) error
	domain          string
	service         string
	legacyService   string
	runner          CommandRunner
}

func New(options Options) (*Adapter, error) {
	if !filepath.IsAbs(options.Home) {
		return nil, errors.New("LaunchAgent home must be absolute")
	}
	if !filepath.IsAbs(options.BinaryPath) {
		return nil, errors.New("LaunchAgent binary path must be absolute")
	}
	uid := options.UID
	if uid == 0 {
		uid = os.Getuid()
	}
	if options.CodexHome == "" {
		options.CodexHome = filepath.Join(options.Home, ".codex")
	}
	if options.PlistPath == "" {
		options.PlistPath = filepath.Join(options.Home, "Library", "LaunchAgents", Label+".plist")
	}
	logDirectory := filepath.Join(options.Home, ".local", "share", "threadbear", "logs")
	if options.StdoutPath == "" {
		options.StdoutPath = filepath.Join(logDirectory, "heartbeat.stdout.log")
	}
	if options.StderrPath == "" {
		options.StderrPath = filepath.Join(logDirectory, "heartbeat.stderr.log")
	}
	if options.Path == "" {
		options.Path = DefaultPath
	}
	if options.LCAll == "" {
		options.LCAll = DefaultLocale
	}
	if options.LaunchctlPath == "" {
		options.LaunchctlPath = "/bin/launchctl"
	}
	if options.LegacyPlistPath == "" {
		options.LegacyPlistPath = filepath.Join(options.Home, "Library", "LaunchAgents", LegacyLabel+".plist")
	}
	if options.LegacyLockPath == "" {
		options.LegacyLockPath = filepath.Join(options.Home, ".local", "share", "threadwatch", "run.lock")
	}
	if options.LegacyLockProbe == nil {
		options.LegacyLockProbe = verifyLockAvailable
	}
	if options.Runner == nil {
		options.Runner = ExecRunner{}
	}
	for name, value := range map[string]string{"CODEX_HOME": options.CodexHome, "plist": options.PlistPath, "stdout log": options.StdoutPath, "stderr log": options.StderrPath, "launchctl": options.LaunchctlPath, "legacy plist": options.LegacyPlistPath, "legacy lock": options.LegacyLockPath} {
		if !filepath.IsAbs(value) {
			return nil, fmt.Errorf("LaunchAgent %s path must be absolute", name)
		}
	}
	domain := "gui/" + strconv.Itoa(uid)
	return &Adapter{home: options.Home, codexHome: options.CodexHome, binaryPath: options.BinaryPath, plistPath: options.PlistPath, stdoutPath: options.StdoutPath, stderrPath: options.StderrPath, path: options.Path, lcAll: options.LCAll, launchctlPath: options.LaunchctlPath, legacyPlistPath: options.LegacyPlistPath, legacyLockPath: options.LegacyLockPath, legacyLockProbe: options.LegacyLockProbe, domain: domain, service: domain + "/" + Label, legacyService: domain + "/" + LegacyLabel, runner: options.Runner}, nil
}

func (a *Adapter) Healthy(ctx context.Context) (bool, error) {
	if _, err := os.Stat(a.plistPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("stat LaunchAgent plist: %w", err)
	}
	disabled, err := a.disabled(ctx, Label)
	if err != nil {
		return false, err
	}
	if disabled {
		return false, nil
	}
	return a.loaded(ctx, a.service)
}

func (a *Adapter) Apply(ctx context.Context, value config.Config) error {
	pathValue := value.CodexSpawnPath
	if pathValue == "" {
		pathValue = a.path
	}
	disabled, err := a.disabled(ctx, Label)
	if err != nil {
		return err
	}
	if !disabled {
		rendered, renderErr := RenderPlist(PlistSpec{Label: Label, BinaryPath: a.binaryPath, StartInterval: value.HeartbeatSeconds, Home: a.home, CodexHome: a.codexHome, Path: pathValue, LCAll: a.lcAll, StdoutPath: a.stdoutPath, StderrPath: a.stderrPath})
		if renderErr != nil {
			return renderErr
		}
		current, readErr := os.ReadFile(a.plistPath)
		if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
			return fmt.Errorf("read LaunchAgent plist: %w", readErr)
		}
		loaded, loadedErr := a.loaded(ctx, a.service)
		if loadedErr != nil {
			return loadedErr
		}
		if loaded && readErr == nil && bytes.Equal(current, rendered) {
			return nil
		}
	}
	if _, err := a.Stage(ctx, value); err != nil {
		return err
	}
	if disabled {
		return nil
	}
	_, err = a.Enable(ctx)
	return err
}

func (a *Adapter) Stage(ctx context.Context, value config.Config) (bool, error) {
	pathValue := value.CodexSpawnPath
	if pathValue == "" {
		pathValue = a.path
	}
	rendered, err := RenderPlist(PlistSpec{
		Label: Label, BinaryPath: a.binaryPath, StartInterval: value.HeartbeatSeconds,
		Home: a.home, CodexHome: a.codexHome, Path: pathValue, LCAll: a.lcAll,
		StdoutPath: a.stdoutPath, StderrPath: a.stderrPath,
	})
	if err != nil {
		return false, err
	}
	disabled, err := a.disabled(ctx, Label)
	if err != nil {
		return false, err
	}
	loaded, err := a.loaded(ctx, a.service)
	if err != nil {
		return false, err
	}
	current, readErr := os.ReadFile(a.plistPath)
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return false, fmt.Errorf("read LaunchAgent plist: %w", readErr)
	}
	same := readErr == nil && bytes.Equal(current, rendered)
	if same && disabled && !loaded {
		return false, nil
	}
	changed := false
	if !disabled {
		if err := a.command(ctx, "disable", a.service); err != nil {
			return false, err
		}
		changed = true
	}
	if loaded {
		if err := a.bootout(ctx, a.service); err != nil {
			return false, err
		}
		changed = true
	}
	if same {
		return changed, nil
	}
	if err := os.MkdirAll(filepath.Dir(a.stdoutPath), 0o700); err != nil {
		return false, fmt.Errorf("create LaunchAgent log directory: %w", err)
	}
	if err := writePrivateAtomic(a.plistPath, rendered); err != nil {
		return false, err
	}
	return true, nil
}

func (a *Adapter) Enable(ctx context.Context) (bool, error) {
	disabled, err := a.disabled(ctx, Label)
	if err != nil {
		return false, err
	}
	loaded, err := a.loaded(ctx, a.service)
	if err != nil {
		return false, err
	}
	if !disabled && loaded {
		return false, nil
	}
	if disabled {
		if err := a.command(ctx, "enable", a.service); err != nil {
			return false, err
		}
	}
	if !loaded {
		if _, err := os.Stat(a.plistPath); err != nil {
			return false, fmt.Errorf("load LaunchAgent plist: %w", err)
		}
		if err := a.command(ctx, "bootstrap", a.domain, a.plistPath); err != nil {
			return false, err
		}
		if err := a.command(ctx, "kickstart", "-k", a.service); err != nil {
			return false, err
		}
	}
	return true, nil
}

func (a *Adapter) Disable(ctx context.Context) (bool, error) {
	disabled, err := a.disabled(ctx, Label)
	if err != nil {
		return false, err
	}
	loaded, err := a.loaded(ctx, a.service)
	if err != nil {
		return false, err
	}
	if disabled && !loaded {
		return false, nil
	}
	if !disabled {
		if err := a.command(ctx, "disable", a.service); err != nil {
			return false, err
		}
	}
	if loaded {
		if err := a.bootout(ctx, a.service); err != nil {
			return false, err
		}
	}
	return true, nil
}

func (a *Adapter) Loaded(ctx context.Context) (bool, error) {
	return a.loaded(ctx, a.service)
}

func (a *Adapter) Remove(ctx context.Context) error {
	disabled, err := a.disabled(ctx, Label)
	if err != nil {
		return err
	}
	loaded, err := a.loaded(ctx, a.service)
	if err != nil {
		return err
	}
	if loaded {
		if err := a.bootout(ctx, a.service); err != nil {
			return err
		}
	}
	if disabled {
		if err := a.command(ctx, "enable", a.service); err != nil {
			return err
		}
	}
	if err := os.Remove(a.plistPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove LaunchAgent plist: %w", err)
	}
	return nil
}

func (a *Adapter) StopLegacy(ctx context.Context) error {
	disabled, err := a.disabled(ctx, LegacyLabel)
	if err != nil {
		return err
	}
	if !disabled {
		if err := a.command(ctx, "disable", a.legacyService); err != nil {
			return fmt.Errorf("disable legacy LaunchAgent: %w", err)
		}
	}
	loaded, err := a.loaded(ctx, a.legacyService)
	if err != nil {
		return err
	}
	if loaded {
		if err := a.bootout(ctx, a.legacyService); err != nil {
			return fmt.Errorf("stop legacy LaunchAgent: %w", err)
		}
	}
	if _, err := os.Lstat(a.legacyPlistPath); err == nil {
		disabledPath := a.legacyPlistPath + ".disabled-by-threadbear"
		if err := os.Rename(a.legacyPlistPath, disabledPath); err != nil {
			return fmt.Errorf("quarantine legacy LaunchAgent plist: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect legacy LaunchAgent plist: %w", err)
	}
	return a.VerifyLegacyStopped(ctx)
}

func (a *Adapter) VerifyLegacyStopped(ctx context.Context) error {
	disabled, err := a.disabled(ctx, LegacyLabel)
	if err != nil {
		return err
	}
	if !disabled {
		return errors.New("legacy LaunchAgent is not disabled")
	}
	loaded, err := a.loaded(ctx, a.legacyService)
	if err != nil {
		return err
	}
	if loaded {
		return errors.New("legacy LaunchAgent is still loaded")
	}
	if _, err := os.Lstat(a.legacyPlistPath); err == nil {
		return errors.New("legacy LaunchAgent plist can still reactivate")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := a.legacyLockProbe(a.legacyLockPath); err != nil {
		return fmt.Errorf("legacy run lock is held: %w", err)
	}
	return nil
}

func (a *Adapter) LegacyStopped(ctx context.Context) (bool, error) {
	loaded, err := a.loaded(ctx, a.legacyService)
	return !loaded, err
}

func (a *Adapter) DetectLegacyInterval() (int, bool, error) {
	path := a.legacyPlistPath
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		path += ".disabled-by-threadbear"
		data, err = os.ReadFile(path)
	}
	if errors.Is(err, os.ErrNotExist) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("read legacy LaunchAgent plist %s: %w", path, err)
	}
	interval, found, err := plistStartInterval(data)
	if err != nil {
		return 0, false, fmt.Errorf("detect legacy LaunchAgent interval: %w", err)
	}
	return interval, found, nil
}

func (a *Adapter) LegacyInterval() (int, bool, error) { return a.DetectLegacyInterval() }

func (a *Adapter) loaded(ctx context.Context, service string) (bool, error) {
	output, err := a.runner.Run(ctx, a.launchctlPath, "print", service)
	if err == nil {
		return true, nil
	}
	if notLoaded(output, err) {
		return false, nil
	}
	return false, commandError("inspect loaded LaunchAgent state", output, err)
}

func (a *Adapter) disabled(ctx context.Context, label string) (bool, error) {
	output, err := a.runner.Run(ctx, a.launchctlPath, "print-disabled", a.domain)
	if err != nil {
		return false, commandError("inspect disabled LaunchAgent state", output, err)
	}
	text := string(output)
	for _, separator := range []string{"=>", "="} {
		if strings.Contains(text, `"`+label+`" `+separator+` true`) || strings.Contains(text, label+" "+separator+" true") {
			return true, nil
		}
	}
	return false, nil
}

func (a *Adapter) bootout(ctx context.Context, service string) error {
	output, err := a.runner.Run(ctx, a.launchctlPath, "bootout", service)
	if err == nil || notLoaded(output, err) {
		return nil
	}
	return commandError("boot out LaunchAgent", output, err)
}

func (a *Adapter) command(ctx context.Context, args ...string) error {
	output, err := a.runner.Run(ctx, a.launchctlPath, args...)
	if err != nil {
		return commandError("run launchctl "+args[0], output, err)
	}
	return nil
}

func commandError(action string, output []byte, err error) error {
	detail := strings.TrimSpace(string(output))
	if detail == "" {
		return fmt.Errorf("%s: %w", action, err)
	}
	return fmt.Errorf("%s: %w: %s", action, err, detail)
}

func notLoaded(output []byte, err error) bool {
	text := strings.ToLower(string(output) + " " + err.Error())
	return strings.Contains(text, "could not find service") || strings.Contains(text, "no such process") || strings.Contains(text, "not found")
}

func verifyLockAvailable(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return err
	}
	return syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
}
