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
		"Status icon in each thread's title.",
		"Next action added to the thread title → like this.",
		"Codex limits title length limited to 60 UTF-16 units, so I'll truncate as needed.",
		"Small local footprint: one binary in ~/.local/bin, a skill, and two hooks.",
		"One persistent thread, 🧵🐻 ThreadBear 🐻🧵, for changing config and uninstalling.",
		"Deterministic classification first; Luna medium only for ambiguity.",
		"--control-task-id",
		"--noninteractive --confirm --json",
		"~/.local/bin/threadbear inventory --json",
		"migration_pending",
		"migration_running",
		"migration_complete",
		"migration_failed",
		"exactly one background migration-controller task",
		"codex_app__set_thread_title",
		"codex_app__set_thread_pinned",
		"Do not open, select, or navigate to it.",
		"adaptive waves of fresh read-only Luna-medium workers",
		"Every successful worker handle is recorded and awaited",
		"results may arrive out of order",
		"eight-minute deadline",
		"active or unaccounted for",
		"status still says `migration_pending` or `migration_running`",
		"migration stopped and is not still working",
		"two native title calls per ordinary turn",
		"~/.local/bin/threadbear uninstall --noninteractive --confirm --json",
		"Want me to uninstall ThreadBear?",
	} {
		if !strings.Contains(text, required) {
			t.Errorf("published install guide is missing %q", required)
		}
	}
	if strings.Contains(text, "foreground migration") {
		t.Error("published install guide still assigns migration to the persistent task")
	}
	for _, debugOnly := range []string{"canary", "--debug-canaries", "genuinely fresh Codex Desktop task"} {
		if strings.Contains(strings.ToLower(text), strings.ToLower(debugOnly)) {
			t.Errorf("published install guide discloses debug-only verification %q", debugOnly)
		}
	}
}

func TestInstalledSkillDefinesAdaptiveMigrationWaves(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "assets", "skill", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	protocol := string(data)

	for _, required := range []string{
		"stable batches of at most 20 tasks",
		"adaptive waves of fresh, read-only Luna-medium workers",
		"After every successful spawn, immediately record its exact handle and assigned task IDs",
		"At the first agent-capacity error, stop launching that wave.",
		"Never reinterpret that error as zero workers when earlier spawns succeeded.",
		"even when results arrive out of order",
		"give each worker eight minutes from spawn",
		"discard only that batch's uncommitted classifications",
		"retry the timed-out batch once in the next wave",
		"Do not start another wave or return while a retained worker is still active or unaccounted for.",
		"If zero workers can start, wait 30 seconds and retry for at most two minutes",
		"Record `migration_failed` with the same controller ID before every non-successful return",
	} {
		if !strings.Contains(protocol, required) {
			t.Errorf("installed skill is missing adaptive migration invariant %q", required)
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

func TestManagedCleanupContractIsShipped(t *testing.T) {
	root := filepath.Join("..", "..", "assets")
	for path, required := range map[string][]string{
		filepath.Join(root, "skill", "SKILL.md"):    {"## Title cleanup", "🧵🐻 strip title icons", "For uninstall, target the control task last"},
		filepath.Join(root, "AGENTS.threadbear.md"): {"A confirmed uninstall turn is the sole exception", "respond without another title call or ThreadBear footer"},
	} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, text := range required {
			if !strings.Contains(string(data), text) {
				t.Errorf("%s is missing %q", path, text)
			}
		}
	}
}

func TestShippedLogicStaysBelowAbsoluteLineCeiling(t *testing.T) {
	root := filepath.Join("..", "..")
	paths, err := filepath.Glob(filepath.Join(root, "cmd", "threadbear", "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	paths = append(paths, filepath.Join(root, "install.sh"), filepath.Join(root, "site", "install.sh"))
	count := 0
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		count += bytes.Count(data, []byte{'\n'})
		if len(data) > 0 && data[len(data)-1] != '\n' {
			count++
		}
	}
	t.Logf("shipped executable logic: %d lines (target 1000, absolute ceiling 1500)", count)
	if count > 1500 {
		t.Fatalf("shipped executable logic is %d lines; absolute ceiling is 1500", count)
	}
}
