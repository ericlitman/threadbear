package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ericlitman/threadbear/assets"
)

type updateFixtureOptions struct {
	ReleaseVersion, CandidateVersion string
	ManifestBody                     []byte
	AssetKey, AssetURL               string
	AssetData, ChecksumBody          []byte
	SelfTestMode                     string
	InstallFailure                   bool
	AssetDelay                       time.Duration
}

type updateFixture struct {
	server   *httptest.Server
	base     string
	requests map[string]int
	mu       sync.Mutex
}

func TestUpdateNewerOlderAndHealthyNoop(t *testing.T) {
	prepareUpdate(t, "1.2.3", true)
	fixture := startUpdateFixture(t, updateFixtureOptions{ReleaseVersion: "1.2.4"})
	result, err := update(context.Background())
	if err != nil || result.(map[string]any)["updated"] != true || result.(map[string]any)["version"] != "1.2.4" {
		t.Fatalf("newer update = %#v, %v", result, err)
	}
	version = "1.2.4"
	before := fixture.count("asset")
	result, err = update(context.Background())
	if err != nil || result.(map[string]any)["current"] != true || fixture.count("asset") != before {
		t.Fatalf("same-version no-op = %#v, %v, asset requests %d", result, err, fixture.count("asset"))
	}
	version = "1.2.5"
	result, err = update(context.Background())
	if err != nil || result.(map[string]any)["current"] != true || fixture.count("asset") != before {
		t.Fatalf("older-release no-op = %#v, %v, asset requests %d", result, err, fixture.count("asset"))
	}
}

func TestUpdateSameVersionRepairsManagedSurfaces(t *testing.T) {
	p := prepareUpdate(t, "2.0.0", false)
	startUpdateFixture(t, updateFixtureOptions{ReleaseVersion: "2.0.0"})
	result, err := update(context.Background())
	if err != nil || result.(map[string]any)["repaired"] != true {
		t.Fatalf("repair = %#v, %v", result, err)
	}
	data, _ := os.ReadFile(p.skill)
	if string(data) != assets.SkillManagedContent {
		t.Fatal("same-version repair did not restore the managed skill")
	}
}

func TestUpdateRefusesPendingArchiveBeforeNetwork(t *testing.T) {
	prepareUpdate(t, "2.0.0", true)
	fixture := startUpdateFixture(t, updateFixtureOptions{ReleaseVersion: "2.0.1"})
	if err := newStore(stateDir()).update(func(value *state) (bool, error) {
		value.ArchivePending = &archiveOperation{TaskID: "target", Action: "archive"}
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	_, err := update(context.Background())
	requireUpdateStage(t, err, "archive_pending")
	if fixture.count("manifest") != 0 {
		t.Fatal("pending archive allowed update network access")
	}
}

func TestUpdateRefusesPreparedUninstallBeforeNetwork(t *testing.T) {
	prepareUpdate(t, "2.0.0", true)
	fixture := startUpdateFixture(t, updateFixtureOptions{ReleaseVersion: "2.0.1"})
	if err := newStore(stateDir()).update(func(value *state) (bool, error) {
		value.UninstallPending = &uninstallOperation{InitiatorTaskID: "owner"}
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	_, err := update(context.Background())
	requireUpdateStage(t, err, "uninstall_pending")
	if fixture.count("manifest") != 0 {
		t.Fatal("prepared uninstall allowed update network access")
	}
}

func TestUpdateRefusesLegacyPendingTitleBeforeNetwork(t *testing.T) {
	prepareUpdate(t, "2.0.0", true)
	fixture := startUpdateFixture(t, updateFixtureOptions{ReleaseVersion: "2.0.1"})
	if err := newStore(stateDir()).update(func(value *state) (bool, error) {
		value.Tasks["legacy"] = taskState{Pending: &pendingProposal{Prior: "Old", Proposed: "New"}}
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	_, err := update(context.Background())
	requireUpdateStage(t, err, "title_pending")
	if fixture.count("manifest") != 0 {
		t.Fatal("pending title allowed update network access")
	}
}

func TestUpdateWaitsForConcurrentMaintenanceBeforeNetwork(t *testing.T) {
	prepareUpdate(t, "2.1.2", true)
	fixture := startUpdateFixture(t, updateFixtureOptions{ReleaseVersion: "2.1.3"})
	lock, err := newStore(stateDir()).operationLock()
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, updateErr := update(context.Background()); done <- updateErr }()
	select {
	case err := <-done:
		unlock(lock)
		t.Fatalf("update returned while maintenance held the operation lock: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if fixture.count("manifest") != 0 {
		unlock(lock)
		t.Fatal("update fetched the manifest while maintenance held the operation lock")
	}
	unlock(lock)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestUpdateRejectsManifestAndCandidateFailures(t *testing.T) {
	tests := []struct {
		name, stage string
		options     updateFixtureOptions
		configure   func()
	}{
		{name: "malformed manifest", stage: "manifest", options: updateFixtureOptions{ReleaseVersion: "2.0.1", ManifestBody: []byte(`{"version":`)}},
		{name: "wrong architecture", stage: "manifest_asset", options: updateFixtureOptions{ReleaseVersion: "2.0.1", AssetKey: "darwin_amd64"}},
		{name: "off origin asset", stage: "asset_url", options: updateFixtureOptions{ReleaseVersion: "2.0.1", AssetURL: "https://example.com/threadbear"}},
		{name: "missing checksum", stage: "checksum", options: updateFixtureOptions{ReleaseVersion: "2.0.1", ChecksumBody: []byte("missing\n")}},
		{name: "checksum mismatch", stage: "checksum", options: updateFixtureOptions{ReleaseVersion: "2.0.1", ChecksumBody: []byte(strings.Repeat("0", 64) + "\n")}},
		{name: "embedded version mismatch", stage: "candidate_version", options: updateFixtureOptions{ReleaseVersion: "2.0.1", CandidateVersion: "2.0.2"}},
		{name: "candidate self test failure", stage: "candidate_self_test", options: updateFixtureOptions{ReleaseVersion: "2.0.1", SelfTestMode: "fail"}},
		{name: "candidate timeout", stage: "candidate_self_test", options: updateFixtureOptions{ReleaseVersion: "2.0.1", SelfTestMode: "sleep"}, configure: func() { updateCandidateTimeout = 200 * time.Millisecond }},
		{name: "candidate install failure", stage: "candidate_install", options: updateFixtureOptions{ReleaseVersion: "2.0.1", InstallFailure: true}},
		{name: "oversized download", stage: "candidate_download", options: updateFixtureOptions{ReleaseVersion: "2.0.1", AssetData: bytes.Repeat([]byte("x"), 1024)}, configure: func() { updateBinaryLimit = 32 }},
		{name: "interrupted download", stage: "candidate_download", options: updateFixtureOptions{ReleaseVersion: "2.0.1", AssetDelay: 100 * time.Millisecond}, configure: func() { updateClient.Timeout = 20 * time.Millisecond }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			p := prepareUpdate(t, "2.0.0", true)
			before, _ := os.ReadFile(p.binary)
			startUpdateFixture(t, test.options)
			if test.configure != nil {
				test.configure()
			}
			_, err := update(context.Background())
			requireUpdateStage(t, err, test.stage)
			after, _ := os.ReadFile(p.binary)
			if !bytes.Equal(before, after) {
				t.Fatal("pre-install failure changed the installed binary")
			}
		})
	}
}

func TestUpdateCommandReturnsTypedFailureStage(t *testing.T) {
	prepareUpdate(t, "2.0.0", true)
	if err := newStore(stateDir()).update(func(value *state) (bool, error) {
		value.ArchivePending = &archiveOperation{TaskID: "target", Action: "archive"}
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if code := run(context.Background(), []string{"update", "--json"}, strings.NewReader(""), &output, &bytes.Buffer{}); code != 1 {
		t.Fatalf("update exit = %d", code)
	}
	var result map[string]any
	if json.Unmarshal(output.Bytes(), &result) != nil || result["stage"] != "archive_pending" || result["ready"] != false {
		t.Fatalf("typed failure = %s", output.String())
	}
}

func prepareUpdate(t *testing.T, current string, healthy bool) lifecyclePaths {
	t.Helper()
	p := isolatedLifecycle(t)
	oldVersion := version
	version = current
	t.Cleanup(func() { version = oldVersion })
	if _, err := install("main", false, true, false); err != nil {
		t.Fatal(err)
	}
	if err := newStore(stateDir()).update(func(value *state) (bool, error) {
		value.Phase = phaseMigrationComplete
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "managed-skill")
	mustWrite(t, source, assets.SkillManagedContent)
	t.Setenv("TB_UPDATE_TEST_SKILL_SOURCE", source)
	t.Setenv("TB_UPDATE_TEST_SKILL_TARGET", p.skill)
	if !healthy {
		mustWrite(t, p.skill, "modified\n")
	}
	return p
}

func startUpdateFixture(t *testing.T, options updateFixtureOptions) *updateFixture {
	t.Helper()
	if options.ReleaseVersion == "" {
		options.ReleaseVersion = "2.0.1"
	}
	if options.CandidateVersion == "" {
		options.CandidateVersion = options.ReleaseVersion
	}
	if options.AssetKey == "" {
		options.AssetKey = "darwin_arm64"
	}
	if options.AssetData == nil {
		options.AssetData = candidateScript(options.CandidateVersion, options.SelfTestMode, options.InstallFailure)
	}
	if options.ChecksumBody == nil {
		digest := sha256.Sum256(options.AssetData)
		options.ChecksumBody = []byte(hex.EncodeToString(digest[:]) + "\n")
	}
	fixture := &updateFixture{requests: map[string]int{}}
	fixture.server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		kind := "other"
		switch {
		case strings.HasSuffix(request.URL.Path, "latest.json"):
			kind = "manifest"
		case strings.HasSuffix(request.URL.Path, ".sha256"):
			kind = "checksum"
		default:
			kind = "asset"
		}
		fixture.mu.Lock()
		fixture.requests[kind]++
		fixture.mu.Unlock()
		switch kind {
		case "manifest":
			if options.ManifestBody != nil {
				_, _ = writer.Write(options.ManifestBody)
				return
			}
			assetURL := options.AssetURL
			if assetURL == "" {
				assetURL = fixture.base + "/download/v" + options.ReleaseVersion + "/threadbear_darwin_arm64"
			}
			manifest := releaseManifest{Version: options.ReleaseVersion, Assets: map[string]releaseAsset{
				options.AssetKey: {URL: assetURL, SHA256URL: fixture.base + "/download/v" + options.ReleaseVersion + "/threadbear_darwin_arm64.sha256"},
			}}
			_ = json.NewEncoder(writer).Encode(manifest)
		case "checksum":
			_, _ = writer.Write(options.ChecksumBody)
		case "asset":
			if options.AssetDelay > 0 {
				time.Sleep(options.AssetDelay)
			}
			_, _ = writer.Write(options.AssetData)
		}
	}))
	fixture.base = fixture.server.URL + "/ericlitman/threadbear/releases"
	t.Cleanup(fixture.server.Close)
	oldBase, oldManifest, oldClient := updateReleaseBase, updateManifestURL, updateClient
	oldGOOS, oldGOARCH := updateGOOS, updateGOARCH
	oldLimit, oldVersionTimeout, oldCandidateTimeout, oldInstallTimeout := updateBinaryLimit, updateVersionTimeout, updateCandidateTimeout, updateInstallTimeout
	updateReleaseBase, updateManifestURL, updateClient = fixture.base, fixture.base+"/latest/download/latest.json", fixture.server.Client()
	updateGOOS, updateGOARCH = "darwin", "arm64"
	updateBinaryLimit, updateVersionTimeout, updateCandidateTimeout, updateInstallTimeout = 64<<20, 2*time.Second, 2*time.Second, 2*time.Second
	t.Cleanup(func() {
		updateReleaseBase, updateManifestURL, updateClient = oldBase, oldManifest, oldClient
		updateGOOS, updateGOARCH = oldGOOS, oldGOARCH
		updateBinaryLimit, updateVersionTimeout, updateCandidateTimeout, updateInstallTimeout = oldLimit, oldVersionTimeout, oldCandidateTimeout, oldInstallTimeout
	})
	return fixture
}

func (fixture *updateFixture) count(kind string) int {
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	return fixture.requests[kind]
}

func candidateScript(candidateVersion, selfTestMode string, installFailure bool) []byte {
	selfTest := fmt.Sprintf(`printf '{"ready":true,"version":"%s"}\n'`, candidateVersion)
	if selfTestMode == "fail" {
		selfTest = "echo self-test-failed >&2; exit 9"
	} else if selfTestMode == "sleep" {
		selfTest = "sleep 1"
	}
	install := `cp "$TB_UPDATE_TEST_SKILL_SOURCE" "$TB_UPDATE_TEST_SKILL_TARGET"
printf '{"ready":true,"installed":true}\n'`
	if installFailure {
		install = "echo install-failed >&2; exit 8"
	}
	versionResult := fmt.Sprintf(`printf '{"version":"%s"}\n'`, candidateVersion)
	unhealthy := fmt.Sprintf(`printf '{"ready":false,"version":"%s"}\n'`, candidateVersion)
	healthy := fmt.Sprintf(`printf '{"ready":true,"version":"%s"}\n'`, candidateVersion)
	return []byte(`#!/bin/sh
case "$1" in
version) ` + versionResult + ` ;;
self-test) ` + selfTest + ` ;;
install) ` + install + ` ;;
status)
  if ! cmp -s "$TB_UPDATE_TEST_SKILL_SOURCE" "$TB_UPDATE_TEST_SKILL_TARGET"; then
    ` + unhealthy + `
  else
    ` + healthy + `
  fi ;;
*) exit 7 ;;
esac
`)
}

func requireUpdateStage(t *testing.T, err error, stage string) {
	t.Helper()
	value, ok := err.(*updateError)
	if !ok || value.Stage != stage {
		t.Fatalf("update error = %#v, want stage %q", err, stage)
	}
}
