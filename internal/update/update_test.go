package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDefaultClientHasBoundedTimeout(t *testing.T) {
	if timeout := clientOrDefault(nil).Timeout; timeout != 60*time.Second {
		t.Fatalf("timeout=%s", timeout)
	}
}

func TestCheckerUsesLatestStableVersion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"version":"1.10.0","assets":{"darwin_arm64":{"url":"https://example.test/arm64","sha256_url":"https://example.test/arm64.sha256"}}}`)
	}))
	defer server.Close()
	status, err := (Checker{ManifestURL: server.URL, GOOS: "darwin", GOARCH: "arm64"}).Check(context.Background(), "1.9.9")
	if err != nil || !status.Newer || status.LatestVersion != "1.10.0" {
		t.Fatalf("status=%+v err=%v", status, err)
	}
}

func TestCheckerRejectsPrerelease(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, `{"version":"1.2.0-rc.1"}`) }))
	defer server.Close()
	if _, err := (Checker{ManifestURL: server.URL, GOOS: "darwin", GOARCH: "arm64"}).Check(context.Background(), "1.1.0"); err == nil {
		t.Fatal("prerelease accepted")
	}
}

func TestCheckerRequiresCurrentArchitectureAsset(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"version":"1.2.0","assets":{"darwin_arm64":{"url":"https://example.test/arm64","sha256_url":"https://example.test/arm64.sha256"}}}`)
	}))
	defer server.Close()
	if _, err := (Checker{ManifestURL: server.URL, GOOS: "darwin", GOARCH: "amd64"}).Check(context.Background(), "1.1.0"); err == nil {
		t.Fatal("missing amd64 asset accepted")
	}
	status, err := (Checker{ManifestURL: server.URL, GOOS: "darwin", GOARCH: "arm64"}).Check(context.Background(), "1.2.0")
	if err != nil || status.Newer || status.LatestVersion != "1.2.0" {
		t.Fatalf("status=%+v err=%v", status, err)
	}
}

func TestResolveReleaseRequiresExactVersionAndSupportedDarwinArchitecture(t *testing.T) {
	if _, err := resolveRelease(context.Background(), http.DefaultClient, "", "https://example.test", "v1.2.3", "darwin", "arm64"); err == nil {
		t.Fatal("leading v accepted")
	}
	if _, err := resolveRelease(context.Background(), http.DefaultClient, "", "https://example.test", "1.2.3", "darwin", "386"); err == nil {
		t.Fatal("unsupported architecture accepted")
	}
	if _, err := resolveRelease(context.Background(), http.DefaultClient, "", "https://example.test", "1.2.3", "linux", "amd64"); err == nil {
		t.Fatal("unsupported platform accepted")
	}
}

func TestReplacerLatestDoesNotDowngradeOrReinstallCurrent(t *testing.T) {
	for _, latest := range []string{"1.1.0", "1.0.0"} {
		t.Run(latest, func(t *testing.T) {
			server := newReleaseServer(t, latest, []byte("candidate"), digest([]byte("candidate")))
			defer server.Close()
			target := filepath.Join(t.TempDir(), "threadbear")
			if err := os.WriteFile(target, []byte("old"), 0o700); err != nil {
				t.Fatal(err)
			}
			replacer := testReplacer(server.URL, target)
			replacer.RunCommand = func(context.Context, string, ...string) ([]byte, error) {
				t.Fatal("candidate executed for non-newer latest")
				return nil, nil
			}
			replacer.Rename = func(string, string) error {
				t.Fatal("candidate renamed for non-newer latest")
				return nil
			}
			result, err := replacer.Update(context.Background(), "")
			if err != nil || result.Changed || result.InstalledVersion != "1.1.0" {
				t.Fatalf("result=%+v err=%v", result, err)
			}
		})
	}
}

func TestReplacerExplicitVersionAllowsDowngrade(t *testing.T) {
	candidate := []byte("candidate")
	server := newReleaseServer(t, "9.9.9", candidate, digest(candidate))
	defer server.Close()
	target := filepath.Join(t.TempDir(), "threadbear")
	if err := os.WriteFile(target, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	replacer := testReplacer(server.URL, target)
	replacer.InstalledVersion = "1.1.0"
	replacer.RunCommand = candidateRunner("1.0.0", true)
	result, err := replacer.Update(context.Background(), "1.0.0")
	if err != nil || !result.Changed || result.InstalledVersion != "1.0.0" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestReplacerValidatesBeforeOneSameDirectoryRename(t *testing.T) {
	candidate := []byte("candidate")
	server := newReleaseServer(t, "1.2.0", candidate, digest(candidate))
	defer server.Close()
	directory := t.TempDir()
	target := filepath.Join(directory, "threadbear")
	if err := os.WriteFile(target, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	renames := 0
	replacer := testReplacer(server.URL, target)
	replacer.RunCommand = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		switch strings.Join(args, " ") {
		case "version --json":
			return []byte(`{"installed_version":"1.2.0"}`), nil
		case "self-test --candidate --json":
			return []byte(`{"ok":true}`), nil
		default:
			return nil, fmt.Errorf("unexpected candidate command %q", args)
		}
	}
	replacer.Rename = func(oldPath, newPath string) error {
		renames++
		if filepath.Dir(oldPath) != filepath.Dir(newPath) {
			t.Fatalf("rename not same-directory: %q -> %q", oldPath, newPath)
		}
		return os.Rename(oldPath, newPath)
	}
	result, err := replacer.Update(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || result.PreviousVersion != "1.1.0" || result.InstalledVersion != "1.2.0" || renames != 1 {
		t.Fatalf("result=%+v renames=%d", result, renames)
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != string(candidate) {
		t.Fatalf("binary=%q err=%v", data, err)
	}
	assertNoStagingFiles(t, directory)
}

func TestReplacerExactVersionUsesGitHubStyleVersionedAsset(t *testing.T) {
	candidate := []byte("candidate")
	server := newReleaseServer(t, "9.9.9", candidate, digest(candidate))
	defer server.Close()
	target := filepath.Join(t.TempDir(), "threadbear")
	if err := os.WriteFile(target, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	replacer := testReplacer(server.URL, target)
	replacer.GOARCH = "amd64"
	result, err := replacer.Update(context.Background(), "1.2.0")
	if err != nil || !result.Changed || result.InstalledVersion != "1.2.0" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestReplacerLeavesWorkingBinaryUntouchedOnFailure(t *testing.T) {
	candidate := []byte("candidate")
	tests := []struct {
		name, checksum, embedded string
		selfOK, renameFail       bool
	}{
		{name: "missing checksum", embedded: "1.2.0", selfOK: true},
		{name: "checksum mismatch", checksum: strings.Repeat("0", 64), embedded: "1.2.0", selfOK: true},
		{name: "wrong embedded version", checksum: digest(candidate), embedded: "1.3.0", selfOK: true},
		{name: "self-test failure", checksum: digest(candidate), embedded: "1.2.0"},
		{name: "replacement failure", checksum: digest(candidate), embedded: "1.2.0", selfOK: true, renameFail: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := newReleaseServer(t, "1.2.0", candidate, test.checksum)
			defer server.Close()
			directory := t.TempDir()
			target := filepath.Join(directory, "threadbear")
			if err := os.WriteFile(target, []byte("old"), 0o700); err != nil {
				t.Fatal(err)
			}
			replacer := testReplacer(server.URL, target)
			replacer.RunCommand = candidateRunner(test.embedded, test.selfOK)
			renames := 0
			replacer.Rename = func(string, string) error { renames++; return errors.New("rename failed") }
			if _, err := replacer.Update(context.Background(), ""); err == nil {
				t.Fatal("update succeeded")
			}
			wantRenames := 0
			if test.renameFail {
				wantRenames = 1
			}
			if renames != wantRenames {
				t.Fatalf("renames=%d want=%d", renames, wantRenames)
			}
			data, err := os.ReadFile(target)
			if err != nil || string(data) != "old" {
				t.Fatalf("binary=%q err=%v", data, err)
			}
			assertNoStagingFiles(t, directory)
		})
	}
}

func TestReplacerInterruptedDownloadAndChecksumHTTPFailureLeaveBinaryUntouched(t *testing.T) {
	for _, failurePath := range []string{"/threadbear_darwin_arm64", "/threadbear_darwin_arm64.sha256"} {
		t.Run(failurePath, func(t *testing.T) {
			var server *httptest.Server
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				switch request.URL.Path {
				case "/latest.json":
					fmt.Fprintf(w, `{"version":"1.2.0","assets":{"darwin_arm64":{"url":%q,"sha256_url":%q}}}`, server.URL+"/threadbear_darwin_arm64", server.URL+"/threadbear_darwin_arm64.sha256")
				case failurePath:
					if failurePath == "/threadbear_darwin_arm64" {
						w.Header().Set("Content-Length", "100")
						_, _ = w.Write([]byte("partial"))
						return
					}
					http.Error(w, "missing", http.StatusNotFound)
				case "/threadbear_darwin_arm64":
					_, _ = w.Write([]byte("candidate"))
				case "/threadbear_darwin_arm64.sha256":
					fmt.Fprint(w, digest([]byte("candidate")))
				}
			}))
			defer server.Close()
			directory := t.TempDir()
			target := filepath.Join(directory, "threadbear")
			if err := os.WriteFile(target, []byte("old"), 0o700); err != nil {
				t.Fatal(err)
			}
			replacer := testReplacer(server.URL, target)
			if _, err := replacer.Update(context.Background(), ""); err == nil {
				t.Fatal("update succeeded")
			}
			data, err := os.ReadFile(target)
			if err != nil || string(data) != "old" {
				t.Fatalf("binary=%q err=%v", data, err)
			}
			assertNoStagingFiles(t, directory)
		})
	}
}

func testReplacer(baseURL, target string) Replacer {
	return Replacer{ManifestURL: baseURL + "/latest.json", ReleaseBaseURL: baseURL, ExecutablePath: target, InstalledVersion: "1.1.0", GOOS: "darwin", GOARCH: "arm64", RunCommand: candidateRunner("1.2.0", true)}
}

func candidateRunner(version string, selfOK bool) CommandRunner {
	return func(_ context.Context, _ string, args ...string) ([]byte, error) {
		switch args[0] {
		case "version":
			return []byte(fmt.Sprintf(`{"installed_version":%q}`, version)), nil
		case "self-test":
			return []byte(fmt.Sprintf(`{"ok":%t}`, selfOK)), nil
		default:
			return nil, errors.New("unexpected candidate command")
		}
	}
}

func newReleaseServer(t *testing.T, version string, candidate []byte, checksum string) *httptest.Server {
	t.Helper()
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest.json":
			fmt.Fprintf(w, `{"version":%q,"assets":{"darwin_arm64":{"url":%q,"sha256_url":%q},"darwin_amd64":{"url":%q,"sha256_url":%q}}}`, version, server.URL+"/threadbear_darwin_arm64", server.URL+"/threadbear_darwin_arm64.sha256", server.URL+"/threadbear_darwin_amd64", server.URL+"/threadbear_darwin_amd64.sha256")
		case "/threadbear_darwin_arm64", "/threadbear_darwin_amd64", "/v1.2.0/threadbear_darwin_amd64", "/v1.0.0/threadbear_darwin_arm64":
			_, _ = w.Write(candidate)
		case "/threadbear_darwin_arm64.sha256", "/threadbear_darwin_amd64.sha256", "/v1.2.0/threadbear_darwin_amd64.sha256", "/v1.0.0/threadbear_darwin_arm64.sha256":
			fmt.Fprint(w, checksum)
		default:
			http.NotFound(w, r)
		}
	}))
	return server
}

func digest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func assertNoStagingFiles(t *testing.T, directory string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(directory, ".threadbear-update-*"))
	if err != nil || len(matches) != 0 {
		t.Fatalf("staging=%v err=%v", matches, err)
	}
}
