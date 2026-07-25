package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultManifestURL    = "https://github.com/ericlitman/threadbear/releases/latest/download/latest.json"
	DefaultReleaseBaseURL = "https://github.com/ericlitman/threadbear/releases/download"
	maximumMetadataBytes  = 1 << 20
	releaseRequestTimeout = 60 * time.Second
)

type Asset struct {
	URL       string `json:"url"`
	SHA256URL string `json:"sha256_url"`
}

type Manifest struct {
	Version string           `json:"version"`
	Assets  map[string]Asset `json:"assets"`
}

type Release struct {
	Version     string
	BinaryURL   string
	ChecksumURL string
}

type Status struct {
	LatestVersion string
	Newer         bool
}

type Checker struct {
	Client      *http.Client
	ManifestURL string
	GOOS        string
	GOARCH      string
}

func (c Checker) Check(ctx context.Context, installedVersion string) (Status, error) {
	release, err := resolveRelease(ctx, clientOrDefault(c.Client), c.ManifestURL, "", "", valueOrDefault(c.GOOS, runtime.GOOS), valueOrDefault(c.GOARCH, runtime.GOARCH))
	if err != nil {
		return Status{}, err
	}
	newer, err := newerVersion(release.Version, installedVersion)
	if err != nil {
		return Status{}, err
	}
	return Status{LatestVersion: release.Version, Newer: newer}, nil
}

func resolveRelease(ctx context.Context, client *http.Client, manifestURL, releaseBaseURL, requestedVersion, goos, goarch string) (Release, error) {
	if goos != "darwin" {
		return Release{}, fmt.Errorf("unsupported platform %q", goos)
	}
	architecture, err := normalizeArchitecture(goarch)
	if err != nil {
		return Release{}, err
	}
	if requestedVersion != "" {
		if !exactVersion(requestedVersion) {
			return Release{}, fmt.Errorf("version must be exact N.N.N without a leading v")
		}
		assetName := "threadbear_darwin_" + architecture
		base := strings.TrimRight(valueOrDefault(releaseBaseURL, DefaultReleaseBaseURL), "/") + "/v" + requestedVersion + "/" + assetName
		return Release{Version: requestedVersion, BinaryURL: base, ChecksumURL: base + ".sha256"}, nil
	}
	manifest, err := fetchManifest(ctx, client, valueOrDefault(manifestURL, DefaultManifestURL))
	if err != nil {
		return Release{}, err
	}
	asset, ok := manifest.Assets["darwin_"+architecture]
	if !ok || asset.URL == "" || asset.SHA256URL == "" {
		return Release{}, fmt.Errorf("latest release has no darwin/%s asset", architecture)
	}
	return Release{Version: manifest.Version, BinaryURL: asset.URL, ChecksumURL: asset.SHA256URL}, nil
}

func fetchManifest(ctx context.Context, client *http.Client, url string) (Manifest, error) {
	data, err := fetchBytes(ctx, client, url, maximumMetadataBytes)
	if err != nil {
		return Manifest{}, fmt.Errorf("fetch latest release manifest: %w", err)
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode latest release manifest: %w", err)
	}
	if !exactVersion(manifest.Version) {
		return Manifest{}, fmt.Errorf("latest release version must be stable N.N.N")
	}
	return manifest, nil
}

func fetchBytes(ctx context.Context, client *http.Client, url string, limit int64) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected HTTP status %s", response.Status)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("response exceeds %d bytes", limit)
	}
	return data, nil
}

func newerVersion(candidate, installed string) (bool, error) {
	candidateParts, err := versionParts(candidate)
	if err != nil {
		return false, fmt.Errorf("candidate version: %w", err)
	}
	installedParts, err := versionParts(installed)
	if err != nil {
		return false, fmt.Errorf("installed version: %w", err)
	}
	for index := range candidateParts {
		if candidateParts[index] != installedParts[index] {
			return candidateParts[index] > installedParts[index], nil
		}
	}
	return false, nil
}

func versionParts(value string) ([3]uint64, error) {
	var result [3]uint64
	if !exactVersion(value) {
		return result, fmt.Errorf("version must be exact N.N.N without a leading v")
	}
	for index, part := range strings.Split(value, ".") {
		parsed, err := strconv.ParseUint(part, 10, 64)
		if err != nil {
			return result, err
		}
		result[index] = parsed
	}
	return result, nil
}

func exactVersion(value string) bool {
	parts := strings.Split(value, ".")
	if len(parts) != 3 || strings.TrimSpace(value) != value {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
		for _, char := range part {
			if char < '0' || char > '9' {
				return false
			}
		}
	}
	return true
}

func normalizeArchitecture(value string) (string, error) {
	switch value {
	case "arm64":
		return "arm64", nil
	case "amd64", "x86_64":
		return "amd64", nil
	default:
		return "", fmt.Errorf("unsupported architecture %q", value)
	}
}

func clientOrDefault(client *http.Client) *http.Client {
	if client == nil {
		return &http.Client{Timeout: releaseRequestTimeout}
	}
	return client
}
func valueOrDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
