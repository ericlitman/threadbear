package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPublishedInstallGuideMatchesCurrentCLI(t *testing.T) {
	root := filepath.Join("..", "..")
	guide, err := os.ReadFile(filepath.Join(root, "INSTALL.md"))
	if err != nil {
		t.Fatal(err)
	}
	published, err := os.ReadFile(filepath.Join(root, "site", "install"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(guide, published) {
		t.Fatal("INSTALL.md and site/install must be byte-identical")
	}

	text := string(guide)
	for _, removed := range []string{
		"--archive-control-task",
		"--archive-after-days",
		"--auto-update",
		"--classifier-model",
		"--heartbeat-seconds",
		"--status-guidance",
		"--token-display",
		"threadbear configure",
		"threadbear disable",
		"threadbear enable",
		"threadbear inspect",
		"threadbear update",
	} {
		if strings.Contains(text, removed) {
			t.Errorf("published install guide contains removed CLI surface %q", removed)
		}
	}
	for _, required := range []string{
		"## Hi. Let's install ThreadBear.",
		"## Recommended setup",
		"--noninteractive --confirm --json",
		"~/.local/bin/threadbear inventory --json",
		"genuinely fresh Codex Desktop task",
		"two native title calls per ordinary turn",
		"~/.local/bin/threadbear uninstall --noninteractive --confirm --json",
	} {
		if !strings.Contains(text, required) {
			t.Errorf("published install guide is missing %q", required)
		}
	}
}

func TestPublishedInstallGuideKeepsFirstConsentTurnVisible(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "INSTALL.md"))
	if err != nil {
		t.Fatal(err)
	}
	guide := string(data)

	for _, required := range []string{
		"Codex collapses commentary after a turn finishes.",
		"commentary copies do not satisfy this contract",
		"Every terminal final answer in this first turn must be self-contained.",
		"If every check and the dry run succeeds, `phase: final_answer` must include the complete orientation above, the readiness sentence, the full recommendation card, and the consent question.",
		"Do not end a successful turn with only the consent question.",
		"compose one terminal final answer with no later tool call or commentary",
		"every recommendation bullet, and consent question must all be present in `phase: final_answer`",
		"do not follow it with a question-only final answer",
	} {
		if !strings.Contains(guide, required) {
			t.Errorf("published install guide is missing visible consent-turn contract %q", required)
		}
	}

	if strings.Contains(guide, "Continue in the same response with the full card") {
		t.Error("published install guide retains the ambiguous response-boundary rule")
	}
}

func TestPublishedInstallGuideDoesNotRequestConsentAfterFailedChecks(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "INSTALL.md"))
	if err != nil {
		t.Fatal(err)
	}
	guide := string(data)

	for _, required := range []string{
		"If any check fails, keep the complete orientation and truthful failure visible in `phase: final_answer`",
		"do not claim readiness, show the recommendation card, or ask for consent",
		"Only after every check and the dry run succeeds, compose one terminal final answer",
	} {
		if !strings.Contains(guide, required) {
			t.Errorf("published install guide is missing truthful failure-turn contract %q", required)
		}
	}
}

func TestHomepageDoesNotPromiseRemovedCapabilities(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "site", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	page := string(data)
	for _, removedClaim := range []string{
		"safely archived",
		"Only completed inactive tasks can be auto-archived",
		"ThreadBear can update itself by default",
		"zero-token idle",
		"Unchanged heartbeats use zero model tokens",
		"produce zero output",
		"exits silently",
		"update-check",
		"version-change",
		"LaunchAgent",
		"heartbeat",
		"control task",
	} {
		if strings.Contains(page, removedClaim) {
			t.Errorf("homepage contains removed capability claim %q", removedClaim)
		}
	}
}
