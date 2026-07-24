package state

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"syscall"

	"github.com/ericlitman/threadbear/internal/config"
)

const (
	configFileName = "config.json"
	stateFileName  = "state.json"
	cycleFileName  = "cycle.json"
	lockFileName   = "threadbear.lock"
)

var (
	ErrLocked     = errors.New("threadbear is already busy")
	ErrUnsafePath = errors.New("unsafe state path")
)

type fileOps struct {
	syncFile      func(*os.File) error
	rename        func(string, string) error
	syncDirectory func(string) error
}

type Store struct {
	dir string
	ops fileOps
}

type Lock struct {
	mu   sync.Mutex
	file *os.File
}

func NewStore(dir string) *Store {
	return &Store{
		dir: dir,
		ops: fileOps{
			syncFile:      func(file *os.File) error { return file.Sync() },
			rename:        os.Rename,
			syncDirectory: syncDirectory,
		},
	}
}

func (s *Store) Directory() string {
	return s.dir
}

func (s *Store) LoadConfig() (config.Config, error) {
	data, err := s.read(configFileName)
	if err != nil {
		return config.Config{}, err
	}
	return config.Decode(data)
}

func (s *Store) SaveConfig(value config.Config) error {
	if err := value.Validate(); err != nil {
		return err
	}
	return s.writeJSON(configFileName, value)
}

func (s *Store) LoadState() (State, error) {
	data, err := s.read(stateFileName)
	if err != nil {
		return State{}, err
	}
	var value State
	if err := decodeVersioned(data, CurrentStateSchemaVersion, &value); err != nil {
		return State{}, fmt.Errorf("decode state: %w", err)
	}
	if err := value.Validate(); err != nil {
		return State{}, err
	}
	return value, nil
}

func (s *Store) SaveState(value State) error {
	if err := value.Validate(); err != nil {
		return err
	}
	return s.writeJSON(stateFileName, value)
}

func (s *Store) LoadCycle() (CycleCheckpoint, error) {
	data, err := s.read(cycleFileName)
	if err != nil {
		return CycleCheckpoint{}, err
	}
	var value CycleCheckpoint
	if err := decodeVersioned(data, CurrentCycleSchemaVersion, &value); err != nil {
		return CycleCheckpoint{}, fmt.Errorf("decode cycle: %w", err)
	}
	if err := value.Validate(); err != nil {
		return CycleCheckpoint{}, err
	}
	return value, nil
}

func (s *Store) SaveCycle(value CycleCheckpoint) error {
	if err := value.Validate(); err != nil {
		return err
	}
	return s.writeJSON(cycleFileName, value)
}

func (s *Store) RemoveCycle() error {
	if err := s.ensureDirectory(); err != nil {
		return err
	}
	path := filepath.Join(s.dir, cycleFileName)
	if err := rejectSymlink(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	return s.ops.syncDirectory(s.dir)
}

func (s *Store) AcquireLock() (*Lock, error) {
	if err := s.ensureDirectory(); err != nil {
		return nil, err
	}
	path := filepath.Join(s.dir, lockFileName)
	if err := rejectSymlink(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := file.Chmod(0600); err != nil {
		file.Close()
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, ErrLocked
		}
		return nil, err
	}
	return &Lock{file: file}, nil
}

func (l *Lock) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return nil
	}
	unlockErr := syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
	closeErr := l.file.Close()
	l.file = nil
	return errors.Join(unlockErr, closeErr)
}

func (s *Store) read(name string) ([]byte, error) {
	if err := validatePrivateDirectory(s.dir); err != nil {
		return nil, err
	}
	path := filepath.Join(s.dir, name)
	if err := validatePrivateFile(path); err != nil {
		return nil, err
	}
	return os.ReadFile(path)
}

func (s *Store) writeJSON(name string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return s.writeAtomic(name, data)
}

func (s *Store) writeAtomic(name string, data []byte) (err error) {
	if err := s.ensureDirectory(); err != nil {
		return err
	}
	destination := filepath.Join(s.dir, name)
	if err := rejectSymlink(destination); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	temporary, err := os.CreateTemp(s.dir, "."+name+".tmp-")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	renamed := false
	defer func() {
		temporary.Close()
		if !renamed {
			os.Remove(temporaryName)
		}
	}()
	if err := temporary.Chmod(0600); err != nil {
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		return err
	}
	if err := s.ops.syncFile(temporary); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := s.ops.rename(temporaryName, destination); err != nil {
		return err
	}
	renamed = true
	return s.ops.syncDirectory(s.dir)
}

func (s *Store) ensureDirectory() error {
	info, err := os.Lstat(s.dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		if err := os.MkdirAll(s.dir, 0700); err != nil {
			return err
		}
	case err != nil:
		return err
	case info.Mode()&os.ModeSymlink != 0 || !info.IsDir():
		return fmt.Errorf("%w: %s is not a real directory", ErrUnsafePath, s.dir)
	}
	return os.Chmod(s.dir, 0700)
}

func validatePrivateDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("%w: %s is not a real directory", ErrUnsafePath, path)
	}
	if info.Mode().Perm() != 0700 {
		return fmt.Errorf("%w: %s mode is %04o, want 0700", ErrUnsafePath, path, info.Mode().Perm())
	}
	return nil
}

func validatePrivateFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("%w: %s is not a regular file", ErrUnsafePath, path)
	}
	if info.Mode().Perm() != 0600 {
		return fmt.Errorf("%w: %s mode is %04o, want 0600", ErrUnsafePath, path, info.Mode().Perm())
	}
	return nil
}

func rejectSymlink(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: %s is a symlink", ErrUnsafePath, path)
	}
	return nil
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	return errors.Join(directory.Sync(), directory.Close())
}

func decodeVersioned(data []byte, current int, target any) error {
	var envelope struct {
		SchemaVersion *int `json:"schema_version"`
	}
	if err := decodeJSON(data, &envelope, false); err != nil {
		return err
	}
	found := 0
	if envelope.SchemaVersion != nil {
		found = *envelope.SchemaVersion
	}
	if found != current {
		return fmt.Errorf("%w: got %d, want %d", ErrUnsupportedSchema, found, current)
	}
	return decodeJSON(data, target, true)
}

func decodeJSON(data []byte, target any, disallowUnknown bool) error {
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
