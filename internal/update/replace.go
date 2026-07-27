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

	"github.com/ericlitman/threadbear/internal/install"
)

type ManagedRefreshError struct {
	Err error
}

func (e *ManagedRefreshError) Error() string {
	return fmt.Sprintf("new binary installed but managed surface refresh failed: %v", e.Err)
}

func (e *ManagedRefreshError) Unwrap() error {
	return e.Err
}

type Result struct {
	PreviousVersion  string
	InstalledVersion string
	Changed          bool
	Resources        []string
	Warnings         []string
}

type CommandRunner func(context.Context, string, ...string) ([]byte, error)

type ReplacementPreview struct {
	Resources []string
	Details   []string
}

type PreviewCallback func(ReplacementPreview) error

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
	Preview          PreviewCallback
	ManagedSurfaces  install.ManagedSurfaceSet
	AgentsEnabled    func() (bool, error)
	CurrentAssets    install.ManagedAssets
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
			return r.reconcileCurrentSurfaces()
		}
	} else if release.Version == r.InstalledVersion {
		return r.reconcileCurrentSurfaces()
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
	preview := ReplacementPreview{Resources: []string{"binary"}, Details: []string{"binary: replace installed executable"}}
	result := Result{PreviousVersion: r.InstalledVersion, InstalledVersion: release.Version}
	var managedAssets install.ManagedAssets
	var agentsEnabled bool
	managedEnabled := r.managesSurfaces()
	if managedEnabled {
		agentsEnabled, err = r.AgentsEnabled()
		if err != nil {
			return Result{}, fmt.Errorf("read managed surface preference: %w", err)
		}
		export, exportErr := run(ctx, candidatePath, "managed-assets", "--candidate", "--json")
		if exportErr != nil {
			downgrade, compareErr := newerVersion(r.InstalledVersion, release.Version)
			if compareErr != nil {
				return Result{}, compareErr
			}
			if !downgrade || !candidateReportedUnknownCommand(export) {
				return Result{}, fmt.Errorf("export candidate managed assets: %w", exportErr)
			}
			managedEnabled = false
			result.Warnings = append(result.Warnings, "AGENTS.md and the threadbear skill were not refreshed because the downgrade candidate reported that managed asset export is an unknown command")
		} else {
			if err := json.Unmarshal(export, &managedAssets); err != nil {
				return Result{}, fmt.Errorf("decode candidate managed assets: %w", err)
			}
			if err := validateManagedAssets(managedAssets); err != nil {
				return Result{}, err
			}
			mutations, previewErr := r.ManagedSurfaces.Preview(agentsEnabled, managedAssets)
			if previewErr != nil {
				return Result{}, fmt.Errorf("preview managed surface refresh: %w", previewErr)
			}
			appendChangedMutations(&preview, mutations)
		}
	}
	if r.Preview != nil {
		if err := r.Preview(ReplacementPreview{Resources: append([]string(nil), preview.Resources...), Details: append([]string(nil), preview.Details...)}); err != nil {
			return Result{}, fmt.Errorf("emit update preview: %w", err)
		}
	}
	rename := r.Rename
	if rename == nil {
		rename = os.Rename
	}
	if err := rename(candidatePath, target); err != nil {
		return Result{}, fmt.Errorf("replace installed binary: %w", err)
	}
	result.Changed = true
	result.Resources = append(result.Resources, "binary")
	if managedEnabled {
		managedResult, applyErr := r.ManagedSurfaces.Reconcile(agentsEnabled, managedAssets)
		if applyErr != nil {
			return Result{}, &ManagedRefreshError{Err: applyErr}
		}
		result.Resources = append(result.Resources, managedResult.Resources...)
	}
	return result, nil
}

func (r Replacer) reconcileCurrentSurfaces() (Result, error) {
	result := Result{PreviousVersion: r.InstalledVersion, InstalledVersion: r.InstalledVersion}
	if !r.managesSurfaces() {
		return result, nil
	}
	if err := validateManagedAssets(r.CurrentAssets); err != nil {
		return Result{}, err
	}
	agentsEnabled, err := r.AgentsEnabled()
	if err != nil {
		return Result{}, fmt.Errorf("read managed surface preference: %w", err)
	}
	mutations, err := r.ManagedSurfaces.Preview(agentsEnabled, r.CurrentAssets)
	if err != nil {
		return Result{}, fmt.Errorf("preview managed surface refresh: %w", err)
	}
	preview := ReplacementPreview{}
	appendChangedMutations(&preview, mutations)
	if len(preview.Resources) == 0 {
		if _, err := r.ManagedSurfaces.Reconcile(agentsEnabled, r.CurrentAssets); err != nil {
			return Result{}, fmt.Errorf("verify managed surfaces: %w", err)
		}
		return result, nil
	}
	if r.Preview != nil {
		if err := r.Preview(preview); err != nil {
			return Result{}, fmt.Errorf("emit update preview: %w", err)
		}
	}
	managedResult, err := r.ManagedSurfaces.Reconcile(agentsEnabled, r.CurrentAssets)
	if err != nil {
		return Result{}, fmt.Errorf("reconcile current managed surfaces: %w", err)
	}
	result.Changed = managedResult.Changed
	result.Resources = append(result.Resources, managedResult.Resources...)
	return result, nil
}

func (r Replacer) managesSurfaces() bool {
	return r.AgentsEnabled != nil && r.ManagedSurfaces.AgentsPath != "" && r.ManagedSurfaces.SkillPath != ""
}

func appendChangedMutations(preview *ReplacementPreview, mutations []install.ManagedMutation) {
	for _, mutation := range mutations {
		if mutation.Changed {
			preview.Resources = append(preview.Resources, mutation.Resource)
			preview.Details = append(preview.Details, mutation.Detail)
		}
	}
}

func validateManagedAssets(assets install.ManagedAssets) error {
	if assets.Agents == "" || assets.Skill == "" {
		return fmt.Errorf("candidate managed asset export is incomplete")
	}
	return nil
}

func candidateReportedUnknownCommand(data []byte) bool {
	var result struct {
		Operation string `json:"operation"`
		ErrorCode string `json:"error_code"`
	}
	return json.Unmarshal(data, &result) == nil && result.Operation == "dispatch" && result.ErrorCode == "unknown_command"
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
