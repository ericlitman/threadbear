package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"
)

const updateManifestLimit = int64(1 << 20)

var updateReleaseBase, updateManifestURL = "https://github.com/ericlitman/threadbear/releases", "https://github.com/ericlitman/threadbear/releases/latest/download/latest.json"
var updateClient, updateBinaryLimit = &http.Client{Timeout: 30 * time.Second}, int64(64 << 20)
var updateGOOS, updateGOARCH, updateVersionTimeout, updateCandidateTimeout, updateInstallTimeout = runtime.GOOS, runtime.GOARCH, 30 * time.Second, 30 * time.Second, 2 * time.Minute
var updateExecutableDigest, updateExecutableDigestErr = runningExecutableDigest()

type updateError struct {
	Stage string
	Err   error
}

func (e *updateError) Error() string              { return e.Stage + ": " + e.Err.Error() }
func (e *updateError) Unwrap() error              { return e.Err }
func updateFailure(stage string, err error) error { return &updateError{Stage: stage, Err: err} }

type releaseAsset struct {
	URL       string `json:"url"`
	SHA256URL string `json:"sha256_url"`
}
type releaseManifest struct {
	Version string                  `json:"version"`
	Assets  map[string]releaseAsset `json:"assets"`
}

type updateReceipt struct {
	CheckedAt       string `json:"checked_at"`
	From            string `json:"from"`
	Version         string `json:"version"`
	Outcome         string `json:"outcome"`
	Automatic       bool   `json:"automatic"`
	RestartRequired bool   `json:"restart_required"`
	Error           string `json:"error,omitempty"`
}

func update(ctx context.Context, automatic bool) (result any, returnErr error) {
	p := installPaths()
	if err := requireRunningUpdateBinary(p.binary); err != nil {
		return nil, updateFailure("installation", err)
	}
	if err := requireCurrentFormatInstall(p); err != nil {
		return nil, updateFailure("installation", err)
	}
	updateLock, err := updateCheckLock()
	if err != nil {
		return nil, updateFailure("busy", err)
	}
	defer unlock(updateLock)
	if err := requireRunningUpdateBinary(p.binary); err != nil {
		return nil, updateFailure("installation", err)
	}
	if err := requireCurrentFormatInstall(p); err != nil {
		return nil, updateFailure("installation", err)
	}
	assetKey, assetName, err := updatePlatform()
	if err != nil {
		return nil, updateFailure("platform", err)
	}
	current, err := exactVersion(version)
	if err != nil {
		return nil, updateFailure("installed_version", err)
	}
	receipt := updateReceipt{From: version, Version: version, Automatic: automatic}
	defer func() {
		if returnErr == nil {
			return
		}
		receipt.Outcome, receipt.Error = "failed", returnErr.Error()
		_ = writeUpdateReceiptForCurrentInstall(p, receipt)
	}()
	manifestData, err := fetchUpdate(ctx, updateManifestURL, updateManifestLimit)
	if err != nil {
		return nil, updateFailure("manifest_download", err)
	}
	var manifest releaseManifest
	if json.Unmarshal(manifestData, &manifest) != nil || manifest.Assets == nil {
		return nil, updateFailure("manifest", errors.New("release manifest is invalid"))
	}
	latest, err := exactVersion(manifest.Version)
	if err != nil {
		return nil, updateFailure("manifest_version", err)
	}
	receipt.Version = manifest.Version
	comparison := slices.Compare(current[:], latest[:])
	if comparison >= 0 {
		health, healthErr := status(ctx)
		if healthErr != nil {
			return health, updateFailure("installation", healthErr)
		}
		result := map[string]any{"ready": true, "current": true, "version": version, "latest": manifest.Version, "automatic": automatic, "restart_required": false}
		receipt.Outcome = "current"
		if err := writeUpdateReceiptForCurrentInstall(p, receipt); err != nil {
			return result, updateFailure("receipt", err)
		}
		return result, nil
	}
	asset, ok := manifest.Assets[assetKey]
	if !ok {
		return nil, updateFailure("manifest_asset", fmt.Errorf("release manifest has no %s asset", assetKey))
	}
	if err := validateUpdateURL(asset.URL, manifest.Version, assetName); err != nil {
		return nil, updateFailure("asset_url", err)
	}
	if err := validateUpdateURL(asset.SHA256URL, manifest.Version, assetName+".sha256"); err != nil {
		return nil, updateFailure("checksum_url", err)
	}
	checksumData, err := fetchUpdate(ctx, asset.SHA256URL, 4096)
	if err != nil {
		return nil, updateFailure("checksum_download", err)
	}
	expected, err := parseChecksum(checksumData)
	if err != nil {
		return nil, updateFailure("checksum", err)
	}
	binary, err := fetchUpdate(ctx, asset.URL, updateBinaryLimit)
	if err != nil {
		return nil, updateFailure("candidate_download", err)
	}
	actual := sha256.Sum256(binary)
	if !bytes.Equal(actual[:], expected) {
		return nil, updateFailure("checksum", errors.New("release checksum mismatch"))
	}
	dir, err := os.MkdirTemp("", "threadbear-update-*")
	if err != nil {
		return nil, updateFailure("candidate_write", err)
	}
	defer os.RemoveAll(dir)
	candidate := dir + "/threadbear"
	if err := writeAtomic(candidate, binary, 0o700); err != nil {
		return nil, updateFailure("candidate_write", err)
	}
	if err := requireCandidate(ctx, candidate, updateVersionTimeout, "version", manifest.Version, "version", "--json"); err != nil {
		return nil, updateFailure("candidate_version", err)
	}
	if err := requireCandidate(ctx, candidate, updateCandidateTimeout, "self-test", manifest.Version, "self-test", "--candidate", "--json"); err != nil {
		return nil, updateFailure("candidate_self_test", err)
	}
	receipt.RestartRequired = true
	candidateInstall, err := candidateResult(ctx, candidate, updateInstallTimeout, "install", "", "install", "--automatic", "--no-onboard", "--noninteractive", "--confirm", "--json")
	if err != nil {
		if candidateInstall != nil {
			candidateInstall["install_stage"] = candidateInstall["stage"]
			return candidateInstall, updateFailure("candidate_install", err)
		}
		return nil, updateFailure("candidate_install", err)
	}
	if err := requireCandidate(ctx, candidate, updateCandidateTimeout, "status", manifest.Version, "status", "--json"); err != nil {
		return nil, updateFailure("installed_status", err)
	}
	result = map[string]any{"ready": true, "from": version, "version": manifest.Version, "automatic": automatic, "updated": true, "restart_required": true}
	receipt.Outcome = "updated"
	if err := writeUpdateReceiptForCurrentInstall(p, receipt); err != nil {
		return result, updateFailure("receipt", err)
	}
	return result, nil
}

func runningExecutableDigest() ([sha256.Size]byte, error) {
	path, err := os.Executable()
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	return sha256.Sum256(data), nil
}

func requireRunningUpdateBinary(path string) error {
	if updateExecutableDigestErr != nil {
		return updateExecutableDigestErr
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if sha256.Sum256(data) != updateExecutableDigest {
		return errors.New("installed binary changed after this updater process started")
	}
	return nil
}

func writeUpdateReceiptForCurrentInstall(p lifecyclePaths, receipt updateReceipt) error {
	lock, err := existingLifecycleLock("lifecycle.lock")
	if err != nil {
		return err
	}
	defer unlock(lock)
	if err := requireCurrentFormatInstall(p); err != nil {
		return err
	}
	receipt.CheckedAt = time.Now().UTC().Format(time.RFC3339)
	data, err := json.Marshal(receipt)
	if err != nil {
		return err
	}
	return writeAtomic(p.updateReceipt, append(data, '\n'), 0o600)
}

func readUpdateReceipt(path string) (updateReceipt, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return updateReceipt{}, err
	}
	var receipt updateReceipt
	if json.Unmarshal(data, &receipt) != nil || receipt.CheckedAt == "" || receipt.From == "" || receipt.Version == "" || receipt.Outcome == "" {
		return updateReceipt{}, errors.New("update receipt is invalid")
	}
	return receipt, nil
}
func updatePlatform() (string, string, error) {
	if updateGOOS != "darwin" {
		return "", "", errors.New("only Darwin is supported")
	}
	switch updateGOARCH {
	case "arm64":
		return "darwin_arm64", "threadbear_darwin_arm64", nil
	case "amd64":
		return "darwin_amd64", "threadbear_darwin_amd64", nil
	default:
		return "", "", errors.New("unsupported Darwin architecture")
	}
}
func exactVersion(value string) ([3]int, error) {
	var parsed [3]int
	parts := strings.Split(value, ".")
	if len(parts) != 3 {
		return parsed, fmt.Errorf("version %q must be exact N.N.N", value)
	}
	for index, part := range parts {
		number, err := strconv.Atoi(part)
		if err != nil || number < 0 || strconv.Itoa(number) != part {
			return parsed, fmt.Errorf("version %q must be exact N.N.N", value)
		}
		parsed[index] = number
	}
	return parsed, nil
}
func validateUpdateURL(raw, releaseVersion, filename string) error {
	base, baseErr := url.Parse(updateReleaseBase)
	value, err := url.Parse(raw)
	if baseErr != nil || err != nil || !value.IsAbs() || value.Scheme != base.Scheme || value.Host != base.Host || value.User != nil || value.RawQuery != "" || value.Fragment != "" || value.RawPath != "" {
		return errors.New("release URL is not an exact official URL")
	}
	want := strings.TrimSuffix(base.Path, "/") + "/download/v" + releaseVersion + "/" + filename
	if value.Path != want {
		return errors.New("release URL does not match the selected version and architecture")
	}
	return nil
}
func fetchUpdate(ctx context.Context, raw string, limit int64) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, err
	}
	response, err := updateClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP status %d", response.StatusCode)
	}
	if response.ContentLength > limit {
		return nil, errors.New("response exceeds the size limit")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("response exceeds the size limit")
	}
	return data, nil
}
func parseChecksum(data []byte) ([]byte, error) {
	fields := strings.Fields(string(data))
	if len(fields) == 0 || len(fields[0]) != 64 {
		return nil, errors.New("release checksum is missing")
	}
	value, err := hex.DecodeString(fields[0])
	if err != nil || len(value) != sha256.Size {
		return nil, errors.New("release checksum is invalid")
	}
	return value, nil
}
func requireCandidate(parent context.Context, candidate string, timeout time.Duration, operation, expectedVersion string, args ...string) error {
	_, err := candidateResult(parent, candidate, timeout, operation, expectedVersion, args...)
	return err
}

func candidateResult(parent context.Context, candidate string, timeout time.Duration, operation, expectedVersion string, args ...string) (map[string]any, error) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	var stdout, stderr bytes.Buffer
	command := exec.CommandContext(ctx, candidate, args...)
	command.Stdout, command.Stderr = &stdout, &stderr
	err := command.Run()
	if ctx.Err() != nil {
		return nil, errors.New("candidate timed out")
	}
	var result map[string]any
	decodeErr := json.Unmarshal(stdout.Bytes(), &result)
	if err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = strings.TrimSpace(stdout.String())
		}
		return result, fmt.Errorf("candidate failed: %w: %s", err, detail)
	}
	if decodeErr != nil {
		return nil, errors.New("candidate returned an invalid result")
	}
	if expectedVersion != "" && result["version"] != expectedVersion {
		return result, errors.New("candidate version mismatch")
	}
	if operation != "version" && result["ready"] != true {
		return result, errors.New("candidate returned an unhealthy result")
	}
	if operation == "install" && result["installed"] != true {
		return result, errors.New("candidate did not confirm installation")
	}
	return result, nil
}
