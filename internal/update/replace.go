package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

type Result struct {
	PreviousVersion  string
	InstalledVersion string
	Changed          bool
}

type CommandRunner func(context.Context, string, ...string) ([]byte, error)

type Replacer struct {
	Client           *http.Client
	ManifestURL      string
	ReleaseBaseURL   string
	ExecutablePath   string
	InstalledVersion string
	GOOS             string
	GOARCH           string
	RunCommand       CommandRunner
	Rename           func(string, string) error
}

func (r Replacer) Update(ctx context.Context, requestedVersion string) (Result, error) {
	client := clientOrDefault(r.Client)
	release, err := resolveRelease(ctx, client, r.ManifestURL, r.ReleaseBaseURL, requestedVersion, valueOrDefault(r.GOOS, runtime.GOOS), valueOrDefault(r.GOARCH, runtime.GOARCH))
	if err != nil {
		return Result{}, err
	}
	if requestedVersion == "" {
		newer, compareErr := newerVersion(release.Version, r.InstalledVersion)
		if compareErr != nil {
			return Result{}, compareErr
		}
		if !newer {
			return Result{PreviousVersion: r.InstalledVersion, InstalledVersion: r.InstalledVersion}, nil
		}
	} else if release.Version == r.InstalledVersion {
		return Result{PreviousVersion: r.InstalledVersion, InstalledVersion: r.InstalledVersion}, nil
	}
	target, err := filepath.Abs(r.ExecutablePath)
	if err != nil {
		return Result{}, fmt.Errorf("resolve installed binary: %w", err)
	}
	candidate, err := os.CreateTemp(filepath.Dir(target), ".threadbear-update-*")
	if err != nil {
		return Result{}, fmt.Errorf("create private candidate: %w", err)
	}
	candidatePath := candidate.Name()
	defer os.Remove(candidatePath)
	hash := sha256.New()
	if err := downloadTo(ctx, client, release.BinaryURL, io.MultiWriter(candidate, hash)); err != nil {
		candidate.Close()
		return Result{}, fmt.Errorf("download candidate: %w", err)
	}
	if err := candidate.Close(); err != nil {
		return Result{}, fmt.Errorf("close candidate: %w", err)
	}
	checksum, err := fetchBytes(ctx, client, release.ChecksumURL, maximumMetadataBytes)
	if err != nil {
		return Result{}, fmt.Errorf("fetch candidate checksum: %w", err)
	}
	expected, err := parseChecksum(checksum)
	if err != nil {
		return Result{}, err
	}
	if hex.EncodeToString(hash.Sum(nil)) != expected {
		return Result{}, fmt.Errorf("candidate checksum mismatch")
	}
	if err := os.Chmod(candidatePath, 0o700); err != nil {
		return Result{}, fmt.Errorf("make candidate executable: %w", err)
	}
	run := r.RunCommand
	if run == nil {
		run = runCandidate
	}
	versionOutput, err := run(ctx, candidatePath, "version", "--json")
	if err != nil {
		return Result{}, fmt.Errorf("read candidate version: %w", err)
	}
	var versionResult struct {
		InstalledVersion string `json:"installed_version"`
	}
	if err := json.Unmarshal(versionOutput, &versionResult); err != nil {
		return Result{}, fmt.Errorf("decode candidate version: %w", err)
	}
	if versionResult.InstalledVersion != release.Version {
		return Result{}, fmt.Errorf("candidate embedded version %q does not match %q", versionResult.InstalledVersion, release.Version)
	}
	selfTestOutput, err := run(ctx, candidatePath, "self-test", "--candidate", "--json")
	if err != nil {
		return Result{}, fmt.Errorf("candidate self-test: %w", err)
	}
	var selfTest struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(selfTestOutput, &selfTest); err != nil {
		return Result{}, fmt.Errorf("decode candidate self-test: %w", err)
	}
	if !selfTest.OK {
		return Result{}, fmt.Errorf("candidate self-test failed")
	}
	rename := r.Rename
	if rename == nil {
		rename = os.Rename
	}
	if err := rename(candidatePath, target); err != nil {
		return Result{}, fmt.Errorf("replace installed binary: %w", err)
	}
	return Result{PreviousVersion: r.InstalledVersion, InstalledVersion: release.Version, Changed: true}, nil
}

func downloadTo(ctx context.Context, client *http.Client, url string, writer io.Writer) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected HTTP status %s", response.Status)
	}
	_, err = io.Copy(writer, response.Body)
	return err
}

func parseChecksum(data []byte) (string, error) {
	fields := strings.Fields(string(data))
	if len(fields) == 0 || len(fields[0]) != sha256.Size*2 {
		return "", fmt.Errorf("candidate checksum is missing")
	}
	checksum := strings.ToLower(fields[0])
	if _, err := hex.DecodeString(checksum); err != nil {
		return "", fmt.Errorf("candidate checksum is invalid")
	}
	return checksum, nil
}

func runCandidate(ctx context.Context, path string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, path, args...)
	command.Env = []string{"HOME=" + os.Getenv("HOME"), "PATH=" + os.Getenv("PATH"), "LC_ALL=C"}
	if codexHome := os.Getenv("CODEX_HOME"); codexHome != "" {
		command.Env = append(command.Env, "CODEX_HOME="+codexHome)
	}
	return command.Output()
}
