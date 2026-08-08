package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeCodexVersionFixture(t *testing.T, path, response string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nprintf '%s\\n' " + quoteArgument(response) + "\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestLocateCompatibleDesktopCodexUsesFixedDesktopPathNotPATH(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	malicious := filepath.Join(t.TempDir(), "codex")
	writeCodexVersionFixture(t, malicious, "codex-cli 99.0.0")
	t.Setenv("PATH", filepath.Dir(malicious))

	path := filepath.Join(home, "Applications", "ChatGPT.app", "Contents", "Resources", "codex")
	writeCodexVersionFixture(t, path, "codex-cli 0.146.0")
	candidates := desktopCodexCandidates(home)
	if candidates[0] != path {
		t.Fatalf("per-user Desktop candidate = %q; want %q", candidates[0], path)
	}
	for _, candidate := range candidates {
		if candidate == malicious {
			t.Fatal("ambient PATH executable entered the fixed candidate list")
		}
	}
	got, err := locateCompatibleDesktopCodex(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.Path == malicious {
		t.Fatalf("located ambient PATH executable %q", got.Path)
	}
	if got.Path != path {
		t.Fatalf("located %q; want per-user Desktop executable %q", got.Path, path)
	}
	found := false
	for _, candidate := range candidates {
		found = found || got.Path == candidate
	}
	if !found {
		t.Fatalf("located %q outside fixed Desktop candidates", got.Path)
	}
}

func TestLocateCompatibleDesktopCodexSkipsStaleEarlierBundle(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	stale := filepath.Join(home, "Applications", "ChatGPT.app", "Contents", "Resources", "codex")
	supported := filepath.Join(home, "Applications", "Codex.app", "Contents", "Resources", "codex")
	writeCodexVersionFixture(t, stale, "codex-cli 0.145.9")
	writeCodexVersionFixture(t, supported, "codex-cli 0.147.0")

	got, err := locateCompatibleDesktopCodex(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.Path != supported || got.Version != "0.147.0" {
		t.Fatalf("compatibility = %#v; want later supported bundle", got)
	}
}

func TestRequireCompatibleCodexVersions(t *testing.T) {
	for _, test := range []struct {
		name, response, wantVersion, wantError string
	}{
		{name: "minimum", response: "codex-cli 0.146.0", wantVersion: "0.146.0"},
		{name: "desktop prerelease", response: "codex-cli 0.147.0-alpha.6.5", wantVersion: "0.147.0-alpha.6.5"},
		{name: "new major", response: "codex-cli 1.0.0", wantVersion: "1.0.0"},
		{name: "too old", response: "codex-cli 0.145.9", wantError: "too old"},
		{name: "malformed", response: "Codex 0.147", wantError: "unsupported Codex Desktop version response"},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "codex")
			writeCodexVersionFixture(t, path, test.response)
			previous := locateCodex
			locateCodex = func(ctx context.Context) (codexCompatibility, error) {
				return inspectCodexVersion(ctx, path)
			}
			t.Cleanup(func() { locateCodex = previous })

			got, err := requireCompatibleCodex(context.Background())
			if test.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantError) {
					t.Fatalf("error = %v; want %q", err, test.wantError)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.Path != path || got.Version != test.wantVersion {
				t.Fatalf("compatibility = %#v", got)
			}
		})
	}
}

func TestRequireCompatibleCodexTimesOut(t *testing.T) {
	path := filepath.Join(t.TempDir(), "codex")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nsleep 2\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	previousLocate, previousLimit := locateCodex, codexVersionLimit
	locateCodex = func(ctx context.Context) (codexCompatibility, error) {
		return inspectCodexVersion(ctx, path)
	}
	codexVersionLimit = 20 * time.Millisecond
	t.Cleanup(func() { locateCodex, codexVersionLimit = previousLocate, previousLimit })
	_, err := requireCompatibleCodex(context.Background())
	if err == nil || !strings.Contains(err.Error(), "deadline exceeded") {
		t.Fatalf("error = %v; want bounded timeout", err)
	}
}
