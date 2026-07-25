package install

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const (
	ManagedBlockStart = "<!-- BEGIN THREADBEAR MANAGED BLOCK -->"
	ManagedBlockEnd   = "<!-- END THREADBEAR MANAGED BLOCK -->"
)

var (
	ErrMalformedManagedBlock = errors.New("malformed ThreadBear managed block")
	ErrUnsafeManagedPath     = errors.New("unsafe managed path")
)

func ManagedBlock(content []byte) []byte {
	content = bytes.TrimRight(content, "\r\n")
	block := make([]byte, 0, len(content)+len(ManagedBlockStart)+len(ManagedBlockEnd)+3)
	block = append(block, ManagedBlockStart...)
	block = append(block, '\n')
	block = append(block, content...)
	block = append(block, '\n')
	block = append(block, ManagedBlockEnd...)
	block = append(block, '\n')
	return block
}

func UpdateManagedBlock(original, content []byte) ([]byte, error) {
	start, end, found, err := managedBlockBounds(original)
	if err != nil {
		return nil, err
	}
	block := ManagedBlock(content)
	if found {
		updated := make([]byte, 0, len(original)-(end-start)+len(block))
		updated = append(updated, original[:start]...)
		updated = append(updated, block...)
		updated = append(updated, original[end:]...)
		return updated, nil
	}
	if len(original) == 0 {
		return block, nil
	}
	updated := make([]byte, 0, len(original)+len(block)+1)
	updated = append(updated, original...)
	updated = append(updated, '\n')
	updated = append(updated, block...)
	return updated, nil
}

func RemoveManagedBlock(original []byte) ([]byte, error) {
	start, end, found, err := managedBlockBounds(original)
	if err != nil || !found {
		return append([]byte(nil), original...), err
	}
	if start > 0 && original[start-1] == '\n' {
		start--
	}
	updated := make([]byte, 0, len(original)-(end-start))
	updated = append(updated, original[:start]...)
	updated = append(updated, original[end:]...)
	return updated, nil
}

func managedBlockBounds(data []byte) (int, int, bool, error) {
	startMarker := []byte(ManagedBlockStart)
	endMarker := []byte(ManagedBlockEnd)
	starts := bytes.Count(data, startMarker)
	ends := bytes.Count(data, endMarker)
	if starts == 0 && ends == 0 {
		return 0, 0, false, nil
	}
	if starts != 1 || ends != 1 {
		return 0, 0, false, ErrMalformedManagedBlock
	}
	start := bytes.Index(data, startMarker)
	endStart := bytes.Index(data, endMarker)
	if endStart < start+len(startMarker) {
		return 0, 0, false, ErrMalformedManagedBlock
	}
	between := data[start+len(startMarker) : endStart]
	if len(between) == 0 || between[0] != '\n' {
		return 0, 0, false, ErrMalformedManagedBlock
	}
	end := endStart + len(endMarker)
	if end < len(data) {
		if data[end] != '\n' {
			return 0, 0, false, ErrMalformedManagedBlock
		}
		end++
	}
	return start, end, true, nil
}

func ValidateManagedFile(path string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("%w: path must be absolute", ErrUnsafeManagedPath)
	}
	if err := rejectSymlinkComponents(path); err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	_, _, _, err = managedBlockBounds(data)
	return err
}

func WriteManagedBlock(path string, content []byte) error {
	return mutateManagedFile(path, func(original []byte) ([]byte, error) {
		return UpdateManagedBlock(original, content)
	})
}

func DeleteManagedBlock(path string) error {
	return mutateManagedFile(path, RemoveManagedBlock)
}

func mutateManagedFile(path string, mutate func([]byte) ([]byte, error)) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("%w: path must be absolute", ErrUnsafeManagedPath)
	}
	if err := rejectSymlinkComponents(path); err != nil {
		return err
	}
	original, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if errors.Is(err, fs.ErrNotExist) {
		original = nil
	}
	updated, err := mutate(original)
	if err != nil {
		return err
	}
	if bytes.Equal(original, updated) {
		return nil
	}
	if len(updated) == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return syncDirectory(filepath.Dir(path))
	}
	return writeAtomic(path, updated, 0o600)
}

func rejectSymlinkComponents(path string) error {
	clean := filepath.Clean(path)
	for current := clean; current != string(filepath.Separator); current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			temporaryRoot := filepath.Clean(os.TempDir())
			if strings.HasPrefix(clean+string(filepath.Separator), temporaryRoot+string(filepath.Separator)) && strings.HasPrefix(temporaryRoot+string(filepath.Separator), current+string(filepath.Separator)) {
				continue
			}
			return fmt.Errorf("%w: %s is a symlink", ErrUnsafeManagedPath, current)
		}
	}
	return nil
}

func writeAtomic(path string, data []byte, mode fs.FileMode) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	if err := rejectSymlinkComponents(path); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".threadbear-*")
	if err != nil {
		return err
	}
	name := temporary.Name()
	committed := false
	defer func() {
		_ = temporary.Close()
		if !committed {
			_ = os.Remove(name)
		}
	}()
	if err := temporary.Chmod(mode); err != nil {
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	committed = true
	return syncDirectory(directory)
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	return errors.Join(directory.Sync(), directory.Close())
}
