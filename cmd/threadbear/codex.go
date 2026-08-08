package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const minimumCodexVersion = "0.146.0"

var (
	locateCodex       = locateDesktopCodex
	codexVersionLimit = 5 * time.Second
	codexVersionRE    = regexp.MustCompile(`^codex-cli ([0-9]+)\.([0-9]+)\.([0-9]+)(?:[-+].*)?$`)
)

type codexCompatibility struct {
	Path    string
	Version string
}

func locateDesktopCodex() (string, error) {
	for _, path := range desktopCodexCandidates(homeDir()) {
		info, err := os.Stat(path)
		if err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0 {
			return path, nil
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("inspect Codex Desktop executable %s: %w", path, err)
		}
	}
	return "", errors.New("Codex Desktop command is unavailable; update or restart Codex and try again")
}

func desktopCodexCandidates(home string) []string {
	return []string{
		filepath.Join(home, "Applications", "ChatGPT.app", "Contents", "Resources", "codex"),
		filepath.Join(home, "Applications", "Codex.app", "Contents", "Resources", "codex"),
		"/Applications/ChatGPT.app/Contents/Resources/codex",
		"/Applications/Codex.app/Contents/Resources/codex",
		filepath.Join(home, ".local", "bin", "codex"),
	}
}

func requireCompatibleCodex(ctx context.Context) (codexCompatibility, error) {
	path, err := locateCodex()
	if err != nil {
		return codexCompatibility{}, err
	}
	runCtx, cancel := context.WithTimeout(ctx, codexVersionLimit)
	defer cancel()
	output, err := exec.CommandContext(runCtx, path, "--version").Output()
	if err != nil {
		if runCtx.Err() != nil {
			err = runCtx.Err()
		}
		return codexCompatibility{}, fmt.Errorf("check Codex Desktop version: %w", err)
	}
	value := strings.TrimSpace(string(output))
	match := codexVersionRE.FindStringSubmatch(value)
	if match == nil {
		return codexCompatibility{}, fmt.Errorf("unsupported Codex Desktop version response %q", value)
	}
	got := [3]int{}
	for index := range got {
		got[index], _ = strconv.Atoi(match[index+1])
	}
	want := [3]int{0, 146, 0}
	if got[0] < want[0] || got[0] == want[0] && got[1] < want[1] ||
		got[0] == want[0] && got[1] == want[1] && got[2] < want[2] {
		return codexCompatibility{}, fmt.Errorf("Codex Desktop %s is too old; ThreadBear requires %s or newer", strings.TrimPrefix(value, "codex-cli "), minimumCodexVersion)
	}
	return codexCompatibility{Path: path, Version: strings.TrimPrefix(value, "codex-cli ")}, nil
}
