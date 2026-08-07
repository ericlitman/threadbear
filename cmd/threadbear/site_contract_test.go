package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readRepoFile(t *testing.T, path ...string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(append([]string{"..", ".."}, path...)...))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func requireText(t *testing.T, text string, values ...string) {
	t.Helper()
	for _, value := range values {
		if !strings.Contains(text, value) {
			t.Errorf("missing product contract %q", value)
		}
	}
}

func rejectText(t *testing.T, text string, values ...string) {
	t.Helper()
	for _, value := range values {
		if strings.Contains(text, value) {
			t.Errorf("contains removed product contract %q", value)
		}
	}
}

func TestPublishedInstallGuideMatchesCurrentProduct(t *testing.T) {
	guide := readRepoFile(t, "INSTALL.md")
	published := readRepoFile(t, "site", "install")
	if guide != published {
		t.Fatal("INSTALL.md and site/install must be byte-identical")
	}
	requireText(t, guide,
		"small local command reads and updates the title through Codex's official App Server",
		"--dry-run --json",
		"--noninteractive --confirm --json",
		"--no-onboard",
		"ThreadBear onboard",
		"onboard --dry-run --json",
		"onboard --noninteractive --confirm --json",
		"entire unarchived App Server catalog before any write",
		"handles every safe target serially with no item cap",
		"attempts each write once",
		"acknowledgement without exact readback is `unconfirmed`",
		"`updated`, `unchanged`, `skipped`, and `unconfirmed`",
		"`legacy_main_task_id` plus `legacy_automation_id`, `legacy_automation_name`, `legacy_automation_kind`, and `legacy_automation_target_thread_id`",
		"unpin the preview's exact legacy main-task ID",
		"never opens Codex SQLite",
		"binary is written last",
		"Every successful update reports `restart_required`",
		"daily update-only LaunchAgent",
		"do not run the title command",
	)
	rejectText(t, guide,
		"codex_app__set_thread_title",
		"PreToolUse",
		"PostToolUse",
		"hooks.json",
		"native title setter",
		"ThreadBear footer",
		"--control-task-id",
		"threadbear inventory",
		"threadbear migration",
		"threadbear maintenance",
		"migration_pending",
		"migration_running",
		"migration_complete",
		"migration_failed",
		"background migration-controller",
		"Luna helper",
		"uninstall --prepare",
		"state_N.sqlite",
	)
}

func TestInstalledGuidanceDefinesOneTerminalCommand(t *testing.T) {
	guidance := readRepoFile(t, "assets", "AGENTS.threadbear.md")
	requireText(t, guidance,
		"Write the substantive response first.",
		`\"$HOME/.local/bin/threadbear\" title --status STATUS --json`,
		"yield_time_ms:4000",
		"max_output_tokens:1000",
		"Make exactly one attempt at that terminal moment.",
		"do not poll, retry, reconcile, or delay the response",
		"The status controls only the visible icon.",
	)
	if count := strings.Count(guidance, "title --status STATUS --json"); count != 1 {
		t.Fatalf("managed guidance contains %d terminal title commands; want one", count)
	}
	rejectText(t, guidance,
		"codex_app__set_thread_title",
		"PreToolUse",
		"PostToolUse",
		"ThreadBear footer",
		"maintenance --cancel",
		"prepared uninstall",
	)
}

func TestInstalledSkillStaysACompactOperatingGuide(t *testing.T) {
	protocol := readRepoFile(t, "assets", "skill", "SKILL.md")
	if size := len([]byte(protocol)); size > 5*1024 {
		t.Fatalf("installed skill is %d bytes; compact-guide ceiling is 5 KiB", size)
	}
	requireText(t, protocol,
		"## Install or reset",
		"## Onboard existing tasks",
		"onboard --dry-run --json",
		"onboard --noninteractive --confirm --json",
		"enumerate and deduplicate the full unarchived App Server catalog before any write",
		"processes the complete plan serially with no item cap",
		"counted only after exact readback",
		"Never retry an unconfirmed result.",
		"## Update",
		"truthful rerunnable partial",
		"`restart_required`",
		"## Uninstall",
		"do not run the title command",
	)
	rejectText(t, protocol,
		"codex_app__set_thread_title",
		"PreToolUse",
		"PostToolUse",
		"Migration controller",
		"migration --phase",
		"maintenance --cancel",
		"uninstall --prepare",
	)
}

func TestHomepageDescribesOnlyShippedCapabilities(t *testing.T) {
	page := readRepoFile(t, "site", "index.html")
	requireText(t, page,
		"One terminal update",
		"One direct writer",
		"reads the exact title, writes at most once, and verifies exact readback",
		"App Server pagination before serial writes",
		"no arbitrary first-50 cap",
		"null or blank <code>name</code>",
		"<code>preview</code> is never adopted",
		"daily update-only LaunchAgent",
		"There is no SQLite access, daemon, proxy, cache, model, retry, fallback, queue, or repair pass.",
		"rerunnable partial",
		"title-core readiness",
	)
	rejectText(t, page,
		"codex_app__set_thread_title",
		"PreToolUse",
		"PostToolUse",
		"native setter",
		"deterministic hook",
		"automatic archive",
		"read-only SQLite lookup",
	)
}
