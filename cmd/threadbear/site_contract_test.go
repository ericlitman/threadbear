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
		"The loader runs `title-script --status <complete|next_steps|needs_input|blocked|automation>` exactly once.",
		"reviewed JavaScript program embedded in the verified binary",
		"Codex evaluates it inside the current in-app tool context",
		"The check prints every fixed Codex Desktop command it finds",
		"For every lifecycle action, write the lasting summary after all tool calls.",
		"Nothing changes in this step.",
		"Existing task titles will not change until those checks finish.",
		"The background first read may still be adding icons to existing tasks.",
		"Never leave that recap only in commentary, progress notices, notifications, or raw tool output",
		"do not copy raw fields or list internal files",
		"Group safe skips as “left unchanged” unless the user needs to act.",
		"I couldn't confirm whether this title changed",
		"## Here's what will happen",
		"## ThreadBear recap 🐻",
		"Other Codex settings and files stay untouched.",
		"existing tasks with enough evidence will gain best-guess status icons",
		"Codex asks once before that complete-catalog read.",
		`sandbox_permissions:"require_escalated"`,
		"icons progress only if permission is granted",
		"A small sparkle marks a first read and disappears after that task's next turn",
		"yield_control();",
		`"$HOME/.local/bin/threadbear\" onboard-stream`,
		"gpt-5.6-luna",
		"Unknown tasks are reread but not renamed.",
		"Every semantic candidate gets one immediate mounted reread",
		"installs only verified official releases",
		"Updates never read tasks or change titles.",
		"ThreadBear and its automatic updates were removed after cleaning X task titles.",
		"--dry-run --json",
		"--noninteractive --confirm --json",
		"uninstall --prepare --noninteractive --confirm --json",
		"uninstall --commit --noninteractive --confirm --json",
		"A bare confirmed uninstall is refused.",
		"one fresh complete plan",
		"with the initiating task last",
		"Any missing, drifted, malformed, wrong-target, wrong-title, or thrown result blocks teardown",
		"There is no final catalog scan, marker, queue, controller, or resume state.",
		"one small terminal JavaScript loader",
		"wait only for that same cell",
		"yield does not cancel a slow native call",
		"Exact returned task ID/title is required.",
		"never opens Codex SQLite",
		"binary is written last",
		"Every successful update reports `restart_required`",
		"daily update-only LaunchAgent",
		"do not run the title command",
	)
	rejectText(t, guide,
		"quiet daily update check",
		"this title stayed as-is",
		"the result is uncertain, I'll leave it alone",
		"makes at most one App Server name update",
		"acknowledgement without exact readback",
		"only task read/write authority",
		"thread/name/set",
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
		"ThreadBear onboard",
		"--no-onboard",
		"Existing title icons may remain",
		"state_N.sqlite",
		"rereads every candidate",
		`"$codex_path" --version; break`,
		"serially exactly once for every prepared item",
		"do not poll, retry, reconcile, or delay the response",
	)
}

func TestReleaseDocsKeepTheEstablishedImmediateRepaintGate(t *testing.T) {
	architecture := readRepoFile(t, "docs", "architecture.md")
	checklist := readRepoFile(t, "docs", "release-checklist.md")
	requireText(t, architecture,
		"release acceptance still verifies the mounted header and sidebar",
		"If live canaries show practical corruption or response blocking, rewriting is disabled",
	)
	requireText(t, checklist,
		"Require immediate mounted repaint for the active header and one controlled historical row",
		"verify both titles after restart",
	)
	rejectText(t, checklist, "if Codex keeps it cached, reopen the project once")
}

func TestInstalledGuidanceDefinesOneTerminalLoaderAndNativeProgram(t *testing.T) {
	guidance := readRepoFile(t, "assets", "AGENTS.threadbear.md")
	program := readRepoFile(t, "assets", "ordinary-title.js")
	requireText(t, guidance,
		"Write the substantive response first.",
		`\"$HOME/.local/bin/threadbear\" title-script --status STATUS`,
		"Replace only `STATUS` with the exact enum",
		"if (source.exit_code !== 0) { text(source); exit(); }",
		"await (0,eval)(source.output)({tools,text,exit});",
		"verified local binary emits its embedded title program",
		"mounted app reads the exact current title and is the sole writer",
		"If the outer cell yields, wait only for that same cell",
		"yield does not cancel a slow native call",
		"Never start another cell, poll the title, retry, or reconcile.",
		"A returned failure is local to this turn.",
		"The status controls only the visible icon.",
		"It also recognizes the five `✦` first-read prefixes and the obsolete neutral bear prefix",
	)
	requireText(t, program,
		"tools.codex_app__read_thread({",
		"tools.codex_app__set_thread_title({title: desired})",
		"const decodeNative = value =>",
		"renamed.threadId !== plan.task_id",
		"renamed.title !== desired",
	)
	if count := strings.Count(guidance, "```js"); count != 1 {
		t.Fatalf("managed guidance contains %d JavaScript cells; want one", count)
	}
	if count := strings.Count(guidance, "title-script --status STATUS"); count != 1 {
		t.Fatalf("managed guidance contains %d terminal loaders; want one", count)
	}
	if len([]byte(extractJavaScriptCell(t, guidance))) > 250 {
		t.Fatalf("managed title loader is too large: %d bytes", len([]byte(extractJavaScriptCell(t, guidance))))
	}
	if count := strings.Count(program, "tools.codex_app__set_thread_title("); count != 1 {
		t.Fatalf("embedded title program contains %d native title calls; want one", count)
	}
	rejectText(t, guidance,
		"title --status STATUS --json",
		"tools.codex_app__read_thread",
		"tools.codex_app__set_thread_title",
		"thread/name/set",
		"Promise.race",
		"setTimeout",
		"delay the response",
		"PreToolUse",
		"PostToolUse",
		"ThreadBear footer",
		"maintenance --cancel",
		"prepared uninstall",
		"six exact current icon prefixes",
	)
}

func TestInstalledSkillStaysCompactAndRunsOneSerialNativePass(t *testing.T) {
	protocol := readRepoFile(t, "assets", "skill", "SKILL.md")
	if size := len([]byte(protocol)); size > 7*1024 {
		t.Fatalf("installed skill is %d bytes; want a compact one-page guide", size)
	}
	requireText(t, protocol,
		"Be upbeat and plain.",
		"Before consent, end with **Here's what will happen**",
		"After tools, end with **ThreadBear recap 🐻**",
		"Put the recap in the final answer",
		"Call safe skips “left unchanged.”",
		"## Install or reset",
		"helper, instructions, skill, daily updates, and the one ephemeral existing-task first read",
		"Titles do not change before installation succeeds.",
		"one ephemeral existing-task first read",
		"Unknown tasks stay untouched.",
		"Never replace that cell with a controller, task, queue, retry pass, or other writer.",
		"## Uninstall",
		"uninstall --dry-run --json",
		`\"$HOME/.local/bin/threadbear\" uninstall --prepare --noninteractive --confirm --json`,
		`sandbox_permissions:"require_escalated"`,
		"This preview changes nothing.",
		"If Codex says approval requests are disabled, stop.",
		"Never change settings or bypass permission.",
		`item.outcome === "prepared"`,
		`typeof item.title !== "string"`,
		"for (const item of prepared)",
		"tools.write_stdin({",
		"session_id:call.session_id",
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
		"notify(`ThreadBear uninstall: titles ${done}/${prepared.length}`)",
		"const accounted = updated + drifted + unconfirmed === prepared.length",
		"if (!accounted || drifted !== 0 || unconfirmed !== 0)",
		"cleanup_complete:false",
		"safe_rerun:\"threadbear uninstall --dry-run --json\"",
		"## Update",
		"Preview download, checks, replacement, and restart.",
		"uninstall --commit --noninteractive --confirm --json",
		"Only `uninstalled:true` means removed",
		"Never retry a drifted or unconfirmed title in the same pass.",
		"After artifact commit, make no title call.",
		"ThreadBear was removed. X task titles were cleaned",
	)
	rejectText(t, protocol, "this title stayed as-is")
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
		"onboard --dry-run",
		"icons may remain",
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

	if count := strings.Count(guide, "## Here's what will happen"); count < 3 {
		t.Fatalf("install guide has %d lifecycle previews; want install, update, and uninstall guidance", count)
	}
	if count := strings.Count(guide, "## ThreadBear recap 🐻"); count < 3 {
		t.Fatalf("install guide has %d durable recaps; want install, update, and uninstall results", count)
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
		"result, uncertainty, next action",
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
	} {
		text := readRepoFile(t, path...)
		requireText(t, text, "mounted")
		rejectText(t, text,
			"only task read/write authority",
			"makes at most one `thread/name/set`",
			"immediate read/write/readback",
			"exact readback",
		)
	}
	status := readRepoFile(t, "docs", "status-convention.md")
	requireText(t, status, "mounted app")
	rejectText(t, status, "thread/name/set", "exact readback")
}

func TestHomepageDescribesOnlyShippedCapabilities(t *testing.T) {
	page := readRepoFile(t, "site", "index.html")
	requireText(t, page,
		"One terminal update",
		"Codex reads the title, then makes at most one native title write",
		"The mounted app owns titles",
		"A clean goodbye",
		"Uninstall fully plans owned-prefix cleanup before serial app-native writes",
		"immediately rereads each prepared task through the mounted app before one possible prefix removal",
		"Drift or an unconfirmed result stops before artifact removal.",
		"null or blank <code>name</code>",
		"<code>preview</code> is never adopted",
		"daily update-only LaunchAgent",
		"A quiet ✦ marks a conservative first read",
		"one ephemeral first read progressively adds exact or best-guess icons",
		"Ordinary turns work with Codex's default workspace permissions and start no App Server or model.",
		"sequential Luna-medium batches",
		"There is no SQLite access, title database, daemon, proxy, cache, queue, or repair pass.",
		"rerunnable partial",
		"title-core readiness",
	)
	rejectText(t, page,
		"Five outcomes, plus a welcome bear",
		"aria-label=\"blocked, needs input, automation, next steps, complete, onboarded\"",
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
