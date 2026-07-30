package appserver

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestClassifierSessionCopiesOnlyPrivateAuthAndCleansUp(t *testing.T) {
	operatorHome := t.TempDir()
	if err := os.WriteFile(filepath.Join(operatorHome, "auth.json"), []byte(`{"token":"secret"}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(operatorHome, "config.toml"), []byte("sentinel = true"), 0600); err != nil {
		t.Fatal(err)
	}
	session, err := OpenClassifierSession(context.Background(), fakeProcess(t, "normal"), operatorHome, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root := session.CleanupToken()
	for _, entry := range session.client.command.Env {
		if entry == "HOME="+operatorHome || entry == "CODEX_HOME="+operatorHome {
			t.Fatalf("classifier inherited operator home: %v", session.client.command.Env)
		}
	}
	if !containsEnvironment(session.client.command.Env, "HOME="+root) || !containsEnvironment(session.client.command.Env, "CODEX_HOME="+root) {
		t.Fatalf("classifier environment=%v", session.client.command.Env)
	}
	info, err := os.Stat(root)
	if err != nil || info.Mode().Perm() != 0700 {
		t.Fatalf("root mode=%v err=%v", info.Mode().Perm(), err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	if !reflect.DeepEqual(names, []string{"auth.json"}) {
		t.Fatalf("isolated files=%v", names)
	}
	authInfo, err := os.Stat(filepath.Join(root, "auth.json"))
	if err != nil || authInfo.Mode().Perm() != 0600 {
		t.Fatalf("auth mode=%v err=%v", authInfo.Mode().Perm(), err)
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("classifier root remains: %v", err)
	}
}

func TestClassifierSessionRejectsSymlinkAuthWithoutLeakingPath(t *testing.T) {
	operatorHome := t.TempDir()
	target := filepath.Join(t.TempDir(), "auth.json")
	if err := os.WriteFile(target, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(operatorHome, "auth.json")); err != nil {
		t.Fatal(err)
	}
	_, err := OpenClassifierSession(context.Background(), fakeProcess(t, "normal"), operatorHome, t.TempDir())
	if !errors.Is(err, ErrClassifierAuth) {
		t.Fatalf("error=%v", err)
	}
	if strings.Contains(err.Error(), operatorHome) || strings.Contains(err.Error(), target) {
		t.Fatalf("error exposed authentication path: %v", err)
	}
}

func TestClassifierSessionRejectsMissingAndNonRegularAuth(t *testing.T) {
	for _, test := range []struct {
		name  string
		setup func(string) error
	}{
		{name: "missing", setup: func(string) error { return nil }},
		{name: "directory", setup: func(home string) error { return os.Mkdir(filepath.Join(home, "auth.json"), 0700) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			operatorHome := t.TempDir()
			if err := test.setup(operatorHome); err != nil {
				t.Fatal(err)
			}
			_, err := OpenClassifierSession(context.Background(), fakeProcess(t, "normal"), operatorHome, t.TempDir())
			if !errors.Is(err, ErrClassifierAuth) || strings.Contains(err.Error(), operatorHome) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestClassifierSessionSetupFailureRemovesRoot(t *testing.T) {
	operatorHome := t.TempDir()
	if err := os.WriteFile(filepath.Join(operatorHome, "auth.json"), []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	rootParent := t.TempDir()
	before, err := filepath.Glob(filepath.Join(rootParent, classifierRootPrefix+"*"))
	if err != nil {
		t.Fatal(err)
	}
	process := fakeProcess(t, "normal")
	process.Path = filepath.Join(t.TempDir(), "missing-codex")
	_, err = OpenClassifierSession(context.Background(), process, operatorHome, rootParent)
	if !errors.Is(err, ErrClassifierIsolation) {
		t.Fatalf("error=%v", err)
	}
	after, err := filepath.Glob(filepath.Join(rootParent, classifierRootPrefix+"*"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("classifier roots leaked: before=%v after=%v", before, after)
	}
}

func TestClassifierSessionCancellationAndProcessExitCleanUp(t *testing.T) {
	for _, test := range []struct {
		name string
		stop func(context.CancelFunc, *ClassifierSession)
	}{
		{name: "cancellation", stop: func(cancel context.CancelFunc, _ *ClassifierSession) { cancel() }},
		{name: "process exit", stop: func(_ context.CancelFunc, session *ClassifierSession) { _ = session.client.command.Process.Kill() }},
	} {
		t.Run(test.name, func(t *testing.T) {
			operatorHome := t.TempDir()
			if err := os.WriteFile(filepath.Join(operatorHome, "auth.json"), []byte("secret"), 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			session, err := OpenClassifierSession(ctx, fakeProcess(t, "normal"), operatorHome, t.TempDir())
			if err != nil {
				cancel()
				t.Fatal(err)
			}
			root := session.CleanupToken()
			test.stop(cancel, session)
			defer cancel()
			deadline := time.Now().Add(2 * time.Second)
			for {
				_, err := os.Stat(root)
				if errors.Is(err, os.ErrNotExist) {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("classifier root remains: %v", err)
				}
				time.Sleep(10 * time.Millisecond)
			}
			if err := session.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestClassifierSessionReportsCleanupFailure(t *testing.T) {
	rootParent := t.TempDir()
	root, err := os.MkdirTemp(rootParent, classifierRootPrefix)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	client := startFake(t, "normal", fixtureCaps(t))
	session := &ClassifierSession{client: client, root: root, rootParent: rootParent, removeAll: func(string) error { return errors.New("synthetic") }}
	if err := session.Close(); !errors.Is(err, ErrClassifierCleanup) || strings.Contains(err.Error(), root) {
		t.Fatalf("error=%v", err)
	}
}

func TestCleanupClassifierRootRejectsUnownedPath(t *testing.T) {
	root := t.TempDir()
	if err := CleanupClassifierRoot(t.TempDir(), root); !errors.Is(err, ErrClassifierCleanup) {
		t.Fatalf("error=%v", err)
	}
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("unowned root changed: %v", err)
	}
}

func containsEnvironment(environment []string, target string) bool {
	for _, entry := range environment {
		if entry == target {
			return true
		}
	}
	return false
}
