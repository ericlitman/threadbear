package appserver

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/sys/unix"
)

var (
	ErrClassifierAuth      = errors.New("classifier authentication is unavailable")
	ErrClassifierIsolation = errors.New("classifier isolation is unavailable")
	ErrClassifierCleanup   = errors.New("classifier cleanup could not be confirmed")
)

const classifierRootPrefix = "threadbear-classifier-"

type ClassifierSessionHandle interface {
	RunEphemeral(context.Context, EphemeralRequest) (EphemeralResult, error)
	CleanupToken() string
	Close() error
}

type ClassifierSessionFactory struct {
	Process           func() (ProcessSpec, error)
	OperatorCodexHome string
	RootParent        string
}

func (f ClassifierSessionFactory) Open(ctx context.Context) (ClassifierSessionHandle, error) {
	if f.Process == nil {
		return nil, ErrClassifierIsolation
	}
	process, err := f.Process()
	if err != nil {
		return nil, ErrClassifierIsolation
	}
	return OpenClassifierSession(ctx, process, f.OperatorCodexHome, f.RootParent)
}

func (f ClassifierSessionFactory) Cleanup(token string) error {
	return CleanupClassifierRoot(f.RootParent, token)
}

func (f ClassifierSessionFactory) CleanupOrphans() error {
	return CleanupClassifierRoots(f.RootParent)
}

type ClassifierSession struct {
	client      *Client
	root        string
	rootParent  string
	removeAll   func(string) error
	cleanupOnce sync.Once
	cleanupErr  error
}

func OpenClassifierSession(ctx context.Context, base ProcessSpec, operatorCodexHome, rootParent string) (session *ClassifierSession, err error) {
	if !filepath.IsAbs(rootParent) {
		return nil, ErrClassifierIsolation
	}
	root, err := os.MkdirTemp(rootParent, classifierRootPrefix)
	if err != nil {
		return nil, ErrClassifierIsolation
	}
	owned := true
	defer func() {
		if !owned {
			return
		}
		if cleanupErr := removeClassifierRoot(root, rootParent, os.RemoveAll); cleanupErr != nil {
			session = nil
			err = cleanupErr
		}
	}()
	if err := os.Chmod(root, 0700); err != nil {
		return nil, ErrClassifierIsolation
	}
	if err := copyClassifierAuth(operatorCodexHome, root); err != nil {
		return nil, err
	}
	process := isolatedProcess(base, root)
	capabilities, err := DiscoverCapabilities(ctx, process)
	if err != nil {
		return nil, ErrClassifierIsolation
	}
	if err := capabilities.RequireClassifier(); err != nil {
		return nil, err
	}
	client, err := Start(ctx, process, capabilities)
	if err != nil {
		return nil, ErrClassifierIsolation
	}
	session = &ClassifierSession{client: client, root: root, rootParent: rootParent, removeAll: os.RemoveAll}
	owned = false
	go func() {
		<-client.Exited()
		session.cleanup()
	}()
	return session, nil
}

func (s *ClassifierSession) RunEphemeral(ctx context.Context, request EphemeralRequest) (EphemeralResult, error) {
	return s.client.RunEphemeral(ctx, request)
}

func (s *ClassifierSession) CleanupToken() string { return s.root }

func (s *ClassifierSession) Close() error {
	closeErr := s.client.Close()
	if closeErr != nil {
		closeErr = ErrClassifierIsolation
	}
	s.cleanup()
	return errors.Join(closeErr, s.cleanupErr)
}

func (s *ClassifierSession) cleanup() {
	s.cleanupOnce.Do(func() {
		s.cleanupErr = removeClassifierRoot(s.root, s.rootParent, s.removeAll)
	})
}

func CleanupClassifierRoot(rootParent, token string) error {
	return removeClassifierRoot(token, rootParent, os.RemoveAll)
}

func CleanupClassifierRoots(rootParent string) error {
	if !filepath.IsAbs(rootParent) {
		return ErrClassifierCleanup
	}
	entries, err := os.ReadDir(rootParent)
	if err != nil {
		return ErrClassifierCleanup
	}
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), classifierRootPrefix) {
			continue
		}
		if err := removeClassifierRoot(filepath.Join(rootParent, entry.Name()), rootParent, os.RemoveAll); err != nil {
			return err
		}
	}
	return nil
}

func removeClassifierRoot(root, rootParent string, removeAll func(string) error) error {
	clean := filepath.Clean(root)
	if filepath.Dir(clean) != filepath.Clean(rootParent) || !strings.HasPrefix(filepath.Base(clean), classifierRootPrefix) {
		return ErrClassifierCleanup
	}
	if err := removeAll(clean); err != nil {
		return ErrClassifierCleanup
	}
	if _, err := os.Lstat(clean); !errors.Is(err, os.ErrNotExist) {
		return ErrClassifierCleanup
	}
	return nil
}

func isolatedProcess(base ProcessSpec, root string) ProcessSpec {
	environment := make([]string, 0, len(base.Env)+2)
	for _, entry := range base.Env {
		if strings.HasPrefix(entry, "HOME=") || strings.HasPrefix(entry, "CODEX_HOME=") {
			continue
		}
		environment = append(environment, entry)
	}
	environment = append(environment, "HOME="+root, "CODEX_HOME="+root)
	return ProcessSpec{Path: base.Path, Args: append([]string{}, base.Args...), Env: environment}
}

func copyClassifierAuth(operatorCodexHome, root string) error {
	sourcePath := filepath.Join(operatorCodexHome, "auth.json")
	info, err := os.Lstat(sourcePath)
	if err != nil || !info.Mode().IsRegular() {
		return ErrClassifierAuth
	}
	fd, err := unix.Open(sourcePath, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return ErrClassifierAuth
	}
	source := os.NewFile(uintptr(fd), "classifier-auth")
	if source == nil {
		unix.Close(fd)
		return ErrClassifierAuth
	}
	defer source.Close()
	openedInfo, err := source.Stat()
	if err != nil || !openedInfo.Mode().IsRegular() || !os.SameFile(info, openedInfo) {
		return ErrClassifierAuth
	}
	destination, err := os.OpenFile(filepath.Join(root, "auth.json"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return ErrClassifierAuth
	}
	if _, err = io.Copy(destination, source); err != nil {
		destination.Close()
		return ErrClassifierAuth
	}
	if err = destination.Chmod(0600); err != nil {
		destination.Close()
		return ErrClassifierAuth
	}
	if err = destination.Close(); err != nil {
		return ErrClassifierAuth
	}
	return nil
}
