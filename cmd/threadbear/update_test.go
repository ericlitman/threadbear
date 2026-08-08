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
	StructuredInstallFailure         bool
	AssetDelay                       time.Duration
}

type updateFixture struct {
	server   *httptest.Server
	base     string
	requests map[string]int
	mu       sync.Mutex
}

func TestUpdateNewerCurrentAndAutomaticReceipt(t *testing.T) {
	p := prepareUpdate(t, "1.2.3", true)
	fixture := startUpdateFixture(t, updateFixtureOptions{ReleaseVersion: "1.2.4"})
	result, err := update(context.Background(), true)
	if err != nil || result.(map[string]any)["updated"] != true || result.(map[string]any)["automatic"] != true || result.(map[string]any)["version"] != "1.2.4" || result.(map[string]any)["restart_required"] != true {
		t.Fatalf("newer update = %#v, %v", result, err)
	}
	var receipt updateReceipt
	data, readErr := os.ReadFile(p.updateReceipt)
	if readErr != nil || json.Unmarshal(data, &receipt) != nil || receipt.Outcome != "updated" || receipt.Version != "1.2.4" || !receipt.Automatic || !receipt.RestartRequired || receipt.CheckedAt == "" {
		t.Fatalf("update receipt = %#v, %v", receipt, readErr)
	}
	entries, err := os.ReadDir(newStore(stateDir()).subjectDir())
	if err != nil || len(entries) != 0 {
		t.Fatalf("update touched subject records: %#v, %v", entries, err)
	}

	version = "1.2.4"
	before := fixture.count("asset")
	result, err = update(context.Background(), false)
	if err != nil || result.(map[string]any)["current"] != true || result.(map[string]any)["automatic"] != false || result.(map[string]any)["restart_required"] != false || fixture.count("asset") != before {
		t.Fatalf("same-version no-op = %#v, %v", result, err)
	}
	data, _ = os.ReadFile(p.updateReceipt)
	if json.Unmarshal(data, &receipt) != nil || receipt.Outcome != "current" || receipt.Automatic || receipt.RestartRequired {
		t.Fatalf("current receipt = %#v", receipt)
	}
}

func TestUpdateSameVersionReportsUnhealthyButNewerCandidateRepairs(t *testing.T) {
	p := prepareUpdate(t, "2.0.0", false)
	fixture := startUpdateFixture(t, updateFixtureOptions{ReleaseVersion: "2.0.0"})
	result, err := update(context.Background(), false)
	requireUpdateStage(t, err, "installation")
	value := result.(map[string]any)
	if value["ready"] != false || value["installed"] != true || value["current"] != nil || fixture.count("asset") != 0 {
		t.Fatalf("same-version unhealthy result = %#v, %v", result, err)
	}
	if data, _ := os.ReadFile(p.skill); string(data) == assets.SkillManagedContent {
		t.Fatal("same-version update silently repaired the unhealthy skill")
	}
	startUpdateFixture(t, updateFixtureOptions{ReleaseVersion: "2.0.1"})
	result, err = update(context.Background(), false)
	if err != nil || result.(map[string]any)["updated"] != true || result.(map[string]any)["ready"] != true {
		t.Fatalf("newer repair update = %#v, %v", result, err)
	}
	if data, _ := os.ReadFile(p.skill); string(data) != assets.SkillManagedContent {
		t.Fatal("newer candidate did not repair the unhealthy skill")
	}
}

func TestUpdateRecordsFailureWithoutRecreatingUninstalledState(t *testing.T) {
	p := prepareUpdate(t, "2.0.0", true)
	startUpdateFixture(t, updateFixtureOptions{ReleaseVersion: "2.0.1", ChecksumBody: []byte(strings.Repeat("0", 64) + "\n")})
	_, err := update(context.Background(), true)
	requireUpdateStage(t, err, "checksum")
	receipt, readErr := readUpdateReceipt(p.updateReceipt)
	if readErr != nil || receipt.Outcome != "failed" || receipt.Error == "" || !receipt.Automatic || receipt.Version != "2.0.1" || receipt.RestartRequired {
		t.Fatalf("failure receipt = %#v, %v", receipt, readErr)
	}
}

func TestUpdateRefusesLegacyAndMissingInstallBeforeNetwork(t *testing.T) {
	p := prepareUpdate(t, "2.0.0", true)
	fixture := startUpdateFixture(t, updateFixtureOptions{ReleaseVersion: "2.0.1"})
	mustWrite(t, filepath.Join(stateDir(), "native.json"), `{"format":4}`)
	_, err := update(context.Background(), false)
	requireUpdateStage(t, err, "installation")
	if fixture.count("manifest") != 0 {
		t.Fatal("legacy state allowed update network access")
	}
	if err := os.Remove(filepath.Join(stateDir(), "native.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(newStore(stateDir()).subjectDir()); err != nil {
		t.Fatal(err)
	}
	_, err = update(context.Background(), false)
	requireUpdateStage(t, err, "installation")
	if fixture.count("manifest") != 0 {
		t.Fatal("missing installation allowed update network access")
	}
	if _, err := os.Stat(p.binary); err != nil {
		t.Fatalf("refused update changed binary: %v", err)
	}
}

func TestUpdateUsesOnlyUpdaterAndLifecycleSurfaces(t *testing.T) {
	prepareUpdate(t, "2.0.0", true)
	startUpdateFixture(t, updateFixtureOptions{ReleaseVersion: "2.0.0"})
	matches, _ := filepath.Glob(filepath.Join(codexHome(), "state_*.sqlite"))
	for _, match := range matches {
		if err := os.Remove(match); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := update(context.Background(), true); err != nil {
		t.Fatalf("update read task catalog: %v", err)
	}
}

func TestUpdateSerializesConcurrentChecks(t *testing.T) {
	prepareUpdate(t, "2.1.2", true)
	fixture := startUpdateFixture(t, updateFixtureOptions{ReleaseVersion: "2.1.3"})
	lock, err := lifecycleLock("update.lock")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, updateErr := update(context.Background(), false); done <- updateErr }()
	select {
	case err := <-done:
		unlock(lock)
		t.Fatalf("update bypassed update lock: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	if fixture.count("manifest") != 0 {
		unlock(lock)
		t.Fatal("update fetched manifest while lock was held")
	}
	unlock(lock)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestUpdateCheckWaitsForLifecycleOnlyAtReceipt(t *testing.T) {
	prepareUpdate(t, "2.1.2", true)
	fixture := startUpdateFixture(t, updateFixtureOptions{ReleaseVersion: "2.1.2"})
	lock, err := lifecycleLock("lifecycle.lock")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, updateErr := update(context.Background(), false); done <- updateErr }()
	deadline := time.Now().Add(time.Second)
	for fixture.count("manifest") == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if fixture.count("manifest") != 1 {
		unlock(lock)
		t.Fatal("update check waited for lifecycle.lock before network verification")
	}
	select {
	case err := <-done:
		unlock(lock)
		t.Fatalf("update receipt bypassed lifecycle.lock: %v", err)
	default:
	}
	unlock(lock)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestUpdateRejectsManifestAndCandidateFailuresWithoutReplacement(t *testing.T) {
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
		{name: "candidate timeout", stage: "candidate_self_test", options: updateFixtureOptions{ReleaseVersion: "2.0.1", SelfTestMode: "sleep"}, configure: func() { updateCandidateTimeout = 100 * time.Millisecond }},
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
			_, err := update(context.Background(), false)
			requireUpdateStage(t, err, test.stage)
			after, _ := os.ReadFile(p.binary)
			if !bytes.Equal(before, after) {
				t.Fatal("failed update changed installed binary")
			}
			receipt, receiptErr := readUpdateReceipt(p.updateReceipt)
			wantRestart := test.stage == "candidate_install"
			if receiptErr != nil || receipt.Outcome != "failed" || receipt.RestartRequired != wantRestart {
				t.Fatalf("failed update receipt = %#v, %v; restart_required want %t", receipt, receiptErr, wantRestart)
			}
		})
	}
}

func TestUpdateCommandReturnsTypedFailureStage(t *testing.T) {
	prepareUpdate(t, "2.0.0", true)
	mustWrite(t, filepath.Join(stateDir(), "native.json"), `{"format":4}`)
	var output bytes.Buffer
	if code := run(context.Background(), []string{"update", "--json"}, strings.NewReader(""), &output, &bytes.Buffer{}); code != 1 {
		t.Fatalf("update exit = %d", code)
	}
	var result map[string]any
	if json.Unmarshal(output.Bytes(), &result) != nil || result["stage"] != "installation" || result["ready"] != false {
		t.Fatalf("typed failure = %s", output.String())
	}
}

func TestUpdateCommandPreservesStructuredCandidateInstallFailure(t *testing.T) {
	prepareUpdate(t, "2.0.0", true)
	startUpdateFixture(t, updateFixtureOptions{ReleaseVersion: "2.0.1", StructuredInstallFailure: true})
	var output bytes.Buffer
	if code := run(context.Background(), []string{"update", "--json"}, strings.NewReader(""), &output, &bytes.Buffer{}); code != 1 {
		t.Fatalf("update exit = %d", code)
	}
	var result map[string]any
	if json.Unmarshal(output.Bytes(), &result) != nil || result["stage"] != "candidate_install" || result["install_stage"] != "managed_guidance" || result["partial"] != true || result["restart_required"] != true || result["safe_rerun"] != "threadbear update --json" {
		t.Fatalf("structured candidate failure = %s", output.String())
	}
}

func prepareUpdate(t *testing.T, current string, healthy bool) lifecyclePaths {
	t.Helper()
	p := isolatedLifecycle(t)
	oldVersion := version
	version = current
	t.Cleanup(func() { version = oldVersion })
	if _, err := install(context.Background(), installOptions{Confirmed: true}); err != nil {
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
		options.AssetData = candidateScript(options.CandidateVersion, options.SelfTestMode, options.InstallFailure, options.StructuredInstallFailure)
	}
	if options.ChecksumBody == nil {
		digest := sha256.Sum256(options.AssetData)
		options.ChecksumBody = []byte(hex.EncodeToString(digest[:]) + "\n")
	}
	fixture := &updateFixture{requests: map[string]int{}}
	fixture.server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		kind := "asset"
		if strings.HasSuffix(request.URL.Path, "latest.json") {
			kind = "manifest"
		} else if strings.HasSuffix(request.URL.Path, ".sha256") {
			kind = "checksum"
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
			_ = json.NewEncoder(writer).Encode(releaseManifest{Version: options.ReleaseVersion, Assets: map[string]releaseAsset{
				options.AssetKey: {URL: assetURL, SHA256URL: fixture.base + "/download/v" + options.ReleaseVersion + "/threadbear_darwin_arm64.sha256"},
			}})
		case "checksum":
			_, _ = writer.Write(options.ChecksumBody)
		default:
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

func candidateScript(candidateVersion, selfTestMode string, installFailure, structuredInstallFailure bool) []byte {
	selfTest := fmt.Sprintf(`printf '{"ready":true,"version":"%s"}\n'`, candidateVersion)
	if selfTestMode == "fail" {
		selfTest = "echo self-test-failed >&2; exit 9"
	} else if selfTestMode == "sleep" {
		selfTest = "sleep 1"
	}
	install := `automatic=false
no_onboard=false
for argument in "$@"; do
  [ "$argument" = "--automatic" ] && automatic=true
  [ "$argument" = "--no-onboard" ] && no_onboard=true
done
[ "$automatic" = true ] && [ "$no_onboard" = true ] || { echo missing-automatic-core-only-flags >&2; exit 10; }
cp "$TB_UPDATE_TEST_SKILL_SOURCE" "$TB_UPDATE_TEST_SKILL_TARGET"
printf '{"ready":true,"installed":true}\n'`
	if installFailure {
		install = "echo install-failed >&2; exit 8"
	} else if structuredInstallFailure {
		install = `printf '{"ready":false,"partial":true,"stage":"managed_guidance","restart_required":true,"safe_rerun":"threadbear update --json"}\n'; exit 8`
	}
	return []byte(`#!/bin/sh
case "$1" in
version) printf '{"version":"` + candidateVersion + `"}\n' ;;
self-test) ` + selfTest + ` ;;
install) ` + install + ` ;;
status)
  if cmp -s "$TB_UPDATE_TEST_SKILL_SOURCE" "$TB_UPDATE_TEST_SKILL_TARGET"; then
    printf '{"ready":true,"version":"` + candidateVersion + `"}\n'
  else
    printf '{"ready":false,"version":"` + candidateVersion + `"}\n'
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
