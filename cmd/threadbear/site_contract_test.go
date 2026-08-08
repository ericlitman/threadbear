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
		"It prepares one safe title, then Codex applies it once.",
		"For every lifecycle action, write the lasting summary after all tool calls.",
		"Nothing changes in this step.",
		"Existing task titles will not change in this step.",
		"onboarding stays a separate previewed choice.",
		"Never leave that recap only in commentary, progress notices, notifications, or raw tool output",
		"do not copy raw fields or list internal files and components",
		"Group safe skips as “left unchanged” unless the user needs to act.",
		"## Here's what will happen",
		"## ThreadBear recap 🐻",
		"Other Codex settings and files stay untouched.",
		"Existing tasks have not been changed yet",
		"Checked N existing tasks: updated X, left Y unchanged, and could not confirm Z.",
		"The update check does not read tasks or change titles.",
		"ThreadBear and its daily update check were removed.",
		"--dry-run --json",
		"--noninteractive --confirm --json",
		"--no-onboard",
		"ThreadBear onboard",
		"entire unarchived App Server catalog before any preparation or title write",
		"fresh complete catalog snapshot",
		"returns one `prepared` action containing the snapshot title and desired title",
		"tools.write_stdin",
		"tools.codex_app__read_thread({threadId:item.task_id,includeOutputs:false,turnLimit:1,maxOutputCharsPerItem:1})",
		"A missing, unreadable, wrong-ID, or changed-title response is skipped.",
		"tools.codex_app__set_thread_title({threadId:item.task_id,title:item.desired_title})",
		"Every prepared item must reach exactly one outcome.",
		"Checked N existing tasks: updated X, left Y unchanged, and could not confirm Z.",
		"tools.codex_app__set_thread_title({title:plan.desired_title})",
		"one injection-safe terminal JavaScript cell",
		"never re-embedded by the model",
		"wait only for that same cell",
		"yield does not cancel a slow native call",
		"exact returned planned task ID and title",
		"never opens Codex SQLite",
		"binary is written last",
		"Every successful update reports `restart_required`",
		"daily update-only LaunchAgent",
		"do not run the title command",
	)
	rejectText(t, guide,
		"makes at most one App Server name update",
		"acknowledgement without exact readback",
		"only task read/write authority",
		"thread/name/set",
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
		"rereads every candidate",
		"serially exactly once for every prepared item",
		"do not poll, retry, reconcile, or delay the response",
	)
}

func TestInstalledGuidanceDefinesOneTerminalPlannerAndNativeWrite(t *testing.T) {
	guidance := readRepoFile(t, "assets", "AGENTS.threadbear.md")
	requireText(t, guidance,
		"Write the substantive response first.",
		`\"$HOME/.local/bin/threadbear\" title --status STATUS --json`,
		`// @exec: {"yield_time_ms": 30000, "max_output_tokens": 1000}`,
		"Replace only `STATUS` with the exact enum",
		"if (local.exit_code !== 0) { text(local); exit(); }",
		"plan = JSON.parse(local.output)",
		`typeof plan.write_required !== "boolean"`,
		"if (!plan.write_required) { text(local); exit(); }",
		"tools.codex_app__set_thread_title({title:plan.desired_title})",
		"const decodeNative = value =>",
		`typeof value !== "string"`,
		"renamed = decodeNative(await tools.codex_app__set_thread_title",
		"renamed.threadId !== plan.task_id",
		"renamed.title !== plan.desired_title",
		"mounted Codex app is the sole writer",
		"If the outer cell yields, wait only for that same cell",
		"yield does not cancel a slow native call",
		"Never start another cell, poll the title, retry, or reconcile.",
		"A returned failure is local to this turn.",
		"The status controls only the visible icon.",
	)
	if count := strings.Count(guidance, "```js"); count != 1 {
		t.Fatalf("managed guidance contains %d JavaScript cells; want one", count)
	}
	if count := strings.Count(guidance, "title --status STATUS --json"); count != 1 {
		t.Fatalf("managed guidance contains %d terminal planners; want one", count)
	}
	if count := strings.Count(guidance, "tools.codex_app__set_thread_title("); count != 1 {
		t.Fatalf("managed guidance contains %d native title calls; want one", count)
	}
	rejectText(t, guidance,
		"threadId:plan.task_id",
		"thread/name/set",
		"Promise.race",
		"setTimeout",
		"delay the response",
		"PreToolUse",
		"PostToolUse",
		"ThreadBear footer",
		"maintenance --cancel",
		"prepared uninstall",
	)
}

func TestInstalledSkillStaysCompactAndRunsOneSerialNativePass(t *testing.T) {
	protocol := readRepoFile(t, "assets", "skill", "SKILL.md")
	if size := len([]byte(protocol)); size > 5*1024 {
		t.Fatalf("installed skill is %d bytes; compact-guide ceiling is 5 KiB", size)
	}
	requireText(t, protocol,
		"Be upbeat/plain.",
		"Before consent, end with **Here's what will happen**",
		"After tools, end with **ThreadBear recap 🐻**",
		"Never leave it in commentary/tool output.",
		"Recap visible facts",
		"no JSON/self-test/state/files/planners/records/booleans",
		"Safe skips are “left unchanged”",
		"title failure is “this title stayed as-is.”",
		"## Install or reset",
		"leave tasks/settings/titles",
		"## Onboard existing tasks",
		"onboard --dry-run --json",
		`\"$HOME/.local/bin/threadbear\" onboard --noninteractive --confirm --json`,
		"full catalog",
		`item.outcome === "prepared"`,
		`typeof item.title !== "string"`,
		"for (const item of prepared)",
		"tools.write_stdin({",
		"session_id:local.session_id",
		"tools.codex_app__read_thread({threadId:item.task_id",
		"includeOutputs:false,turnLimit:1,maxOutputCharsPerItem:1",
		"const parseNative = value =>",
		`typeof value !== "string"`,
		"current = parseNative(await tools.codex_app__read_thread",
		"current?.thread?.id !== item.task_id || current.thread.title !== item.title",
		"tools.codex_app__set_thread_title({",
		"renamed = parseNative(await tools.codex_app__set_thread_title",
		"threadId:item.task_id",
		"title:item.desired_title",
		"renamed.threadId === item.task_id",
		"renamed.title === item.desired_title",
		"notify(`ThreadBear onboarding: ${done}/${prepared.length}`)",
		"const accounted = updated + skipped + unconfirmed === prepared.length",
		"ready:accounted && unconfirmed === 0",
		"onboarding_complete:accounted && unconfirmed === 0",
		"unchanged:plan.total - updated - unconfirmed",
		"Updated X of N existing tasks; Y were left unchanged; Z could not be confirmed.",
		"No cap, controller, worker, queue, or persistent task.",
		"## Update",
		"Preview download, verification, replacement, restart.",
		"## Uninstall",
		"keep tasks/settings/files; icons may remain.",
		"no title cell.",
		"Recap exactly: “ThreadBear was removed.",
	)
	if count := strings.Count(protocol, "tools.codex_app__set_thread_title("); count != 1 {
		t.Fatalf("installed skill contains %d native title call sites; want one", count)
	}
	if count := strings.Count(protocol, "tools.codex_app__read_thread("); count != 1 {
		t.Fatalf("installed skill contains %d mounted title read call sites; want one", count)
	}
	if count := strings.Count(protocol, "tools.exec_command("); count != 1 {
		t.Fatalf("installed skill contains %d preparation process call sites; want one", count)
	}
	rejectText(t, protocol,
		"thread/name/set",
		"exact readback",
		"Promise.all",
		"Promise.race",
		"setTimeout",
		"ready:unconfirmed === 0",
		"rereads every candidate",
		"PreToolUse",
		"PostToolUse",
		"Migration controller",
		"migration --phase",
		"maintenance --cancel",
		"uninstall --prepare",
	)
}

func TestLifecycleCopyLeavesADurableFriendlyRecap(t *testing.T) {
	guide := readRepoFile(t, "INSTALL.md")
	protocol := readRepoFile(t, "assets", "skill", "SKILL.md")
	help := readRepoFile(t, "assets", "help.txt")
	var userFacing strings.Builder
	for _, line := range strings.Split(guide, "\n") {
		if strings.HasPrefix(line, "> ") {
			userFacing.WriteString(strings.TrimPrefix(line, "> "))
			userFacing.WriteByte('\n')
		}
	}

	if count := strings.Count(guide, "## Here's what will happen"); count < 4 {
		t.Fatalf("install guide has %d lifecycle previews; want install, onboarding, update, and uninstall guidance", count)
	}
	if count := strings.Count(guide, "## ThreadBear recap 🐻"); count < 4 {
		t.Fatalf("install guide has %d durable recaps; want universal plus lifecycle results", count)
	}
	requireText(t, guide,
		"end the final response",
		"after all tool calls",
		"what stayed untouched",
		"the next action",
		"those can disappear when Codex summarizes the turn",
	)
	requireText(t, protocol,
		"Before consent, end with",
		"After tools, end with",
		"result/counts, uncertainty, next action",
	)
	requireText(t, help,
		"Show what will change, then install ThreadBear",
		"Show what will be removed, then remove ThreadBear",
		"the final response must recap the result, uncertainty, and next action",
	)
	rejectText(t, userFacing.String(),
		"binary",
		"LaunchAgent",
		"App Server",
		"JSON",
		"subject record",
		"native setter",
	)
}

func TestCurrentDocsNameThePlannerAndSoleMountedWriter(t *testing.T) {
	for _, path := range [][]string{
		{"README.md"},
		{"docs", "architecture.md"},
		{"docs", "compatibility.md"},
		{"docs", "status-convention.md"},
	} {
		text := readRepoFile(t, path...)
		requireText(t, text, "mounted Codex app")
		rejectText(t, text,
			"only task read/write authority",
			"makes at most one `thread/name/set`",
			"immediate read/write/readback",
			"exact readback",
		)
	}
}

func TestHomepageDescribesOnlyShippedCapabilities(t *testing.T) {
	page := readRepoFile(t, "site", "index.html")
	requireText(t, page,
		"One terminal update",
		"The mounted app writes",
		"App Server client prepares the safe title; Codex's native setter applies it",
		"App Server pagination before serial app-native writes",
		"no arbitrary first-50 cap",
		"read/planning authority only",
		"native setter is the sole title writer",
		"immediately rereads each prepared task through the mounted app",
		"skips drift",
		"null or blank <code>name</code>",
		"<code>preview</code> is never adopted",
		"daily update-only LaunchAgent",
		"There is no SQLite access, daemon, proxy, cache, model, retry, fallback, queue, or repair pass.",
		"rerunnable partial",
		"title-core readiness",
	)
	rejectText(t, page,
		"One direct writer",
		"writes at most once, and verifies exact readback",
		"only task read/write authority",
		"thread/name/set",
		"PreToolUse",
		"PostToolUse",
		"deterministic hook",
		"automatic archive",
		"read-only SQLite lookup",
	)
}
