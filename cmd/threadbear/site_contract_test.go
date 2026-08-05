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
	} {
		if strings.Contains(text, removed) {
			t.Errorf("published install guide contains removed CLI surface %q", removed)
		}
	}
	for _, required := range []string{
		"## Hi. Let's install ThreadBear.",
		"## Recommended setup",
		"Status icon in each native-addressable local Codex task title.",
		"Next action added to the thread title → like this.",
		"Codex limits title length limited to 60 UTF-16 units, so I'll truncate as needed.",
		"Small local footprint: one binary in ~/.local/bin, a skill, and two hooks.",
		"One persistent thread, 🧵🐻 ThreadBear 🐻🧵, for changing config and uninstalling.",
		"Deterministic classification and Luna-low ambiguity checks run in parallel.",
		"A small Luna helper checks in hourly, then stays quiet when there is nothing to do.",
		"Finished tasks can curl up in the archive after 14 quiet days—and come back whenever you need them.",
		"ThreadBear keeps itself fresh from verified releases and tells you when it has a new coat.",
		"run the verified update check last",
		"threadbear-maintenance",
		"native automation control",
		"On creation omit `id`",
		"returned `automationId` must equal `threadbear-maintenance`",
		"Never treat the create request's status as proof",
		"immediately update that exact returned ID",
		"status:\"PAUSED\"",
		"paused hourly heartbeat",
		"activate the exact owned heartbeat",
		"--control-task-id",
		"--noninteractive --confirm --json",
		"~/.local/bin/threadbear inventory --json",
		"~/.local/bin/threadbear update --json",
		"migration_pending",
		"migration_running",
		"migration_complete",
		"migration_failed",
		"exactly one projectless background migration-controller task",
		"one exact untagged home-title call",
		"Do not add a nonce or make a second title call.",
		"do not use visual inspection, computer control, screenshots, or Codex `/hooks`",
		"Older signed-in ChatGPT chat-history rows are outside Codex's current task-title API and will stay unchanged.",
		"never describe zero local inventory rows as proof that every visible sidebar row changed",
		"dispatch it within 60 seconds of consent",
		"using `model:\"gpt-5.6-terra\"`, `thinking:\"medium\"`",
		"first title mutation issued within 60 seconds of controller start and within 15 seconds of the inventory result",
		"Worker creation uses the fixed `codex_app__create_thread` surface with `model:\"gpt-5.6-luna\"` and `thinking:\"low\"`",
		"❔ ThreadBear could not classify",
		"Do not open, select, or navigate to it.",
		"one bounded concurrent spawn wave of fresh read-only Luna-low workers",
		"Every successful worker handle is recorded and awaited",
		"results may arrive out of order",
		"bounded concurrent waves of at most eight distinct task IDs",
		"without a client-created `Promise.race` or other synthetic timeout",
		"Only an explicit timeout from the native tool is a timeout",
		"eight-minute deadline",
		"active or unaccounted for",
		"status still says `migration_pending` or `migration_running`",
		"migration stopped and is not still working",
		"two native title calls per ordinary turn",
		"~/.local/bin/threadbear uninstall --prepare --initiator-task-id",
		"~/.local/bin/threadbear uninstall --initiator-task-id",
		"You can uninstall from any active native Codex task—even when the ThreadBear home is archived.",
		"Do not ask the user to open, select, navigate to, or unarchive the ThreadBear home.",
		"same initiating task",
		"Want me to uninstall ThreadBear?",
	} {
		if !strings.Contains(text, required) {
			t.Errorf("published install guide is missing %q", required)
		}
	}
	if strings.Contains(text, "foreground migration") {
		t.Error("published install guide still assigns migration to the persistent task")
	}
	if strings.Contains(text, "Use Codex `/hooks` to inspect") {
		t.Error("published install guide still asks end users to inspect Codex hooks")
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
		"never use visual inspection, computer control, screenshots, or Codex `/hooks`",
		"dispatch it within 60 seconds of consent",
		"first stable batch of at most 25",
		"start the initial bounded worker-spawn wave concurrently with the first deterministic activation-and-setter wave",
		"within 15 seconds of the inventory result",
		"`codex_app__create_thread` using `model:\"gpt-5.6-luna\"`, `thinking:\"low\"`",
		"Do not inspect or compare alternative agent surfaces at runtime.",
		"stable batches of at most 10 tasks",
		"Derive each assigned list mechanically from the parsed inventory `task_id` fields in stable order",
		"never retype, transform, or synthesize an ID",
		"one JSON array with every assigned ID exactly once and no other ID",
		"Validate only that final-answer item; separate worker commentary is not part of the result grammar.",
		"followed only by the one terminal ThreadBear status line required by the managed block",
		"A `wait_threads` snapshot may normalize the one separator newline before the footer into a space",
		"accept one or more whitespace characters followed by the exact required footer",
		"do not require a physical newline",
		"Ignore that required footer only while parsing the array",
		"one bounded concurrent wave of fresh, read-only Luna-low workers",
		"archive all currently validated workers together",
		"one concurrent `Promise.all` call",
		"never serialize those archives",
		"never wait for every worker before applying an already validated and archived batch",
		"A `wait_threads` response with `timedOut:true` is only a normal polling snapshot and is never a worker timeout",
		"Immediately record every successful handle with its assigned task IDs",
		"Retained classifier worker IDs are installation internals: exclude them from every migration title wave.",
		"A missing, mismatched, or unconfirmed archive result fails closed",
		"At an agent-capacity error, account for every earlier success",
		"Never reinterpret that error as zero workers when earlier spawns succeeded.",
		"even when results arrive out of order",
		"give each worker eight minutes from spawn",
		"discard only that batch's uncommitted classifications",
		"retry that read-only batch once in the next wave",
		"A second invalid result or actual worker deadline reports failure and complete accounting to the home",
		"bounded waves of at most eight distinct targets",
		"call `codex_app__read_thread` concurrently for every target as a bounded read-only activation gate",
		"require each response's exact task ID and the inventory-planned title",
		"begin the setter wave within 15 seconds",
		"never include the same target twice in a wave",
		"without a client-created `Promise.race` or other synthetic timeout",
		"Only an explicit timeout from the native tool is a timeout.",
		"never compare the native return title with the compact input",
		"use fresh inventory as the authoritative applied result",
		"Account for the whole wave before reconciling it with inventory",
		"Execute every ready stable queue in one orchestrated script loop",
		"do not add commentary, model deliberation, or a separate outer tool round trip between settled waves",
		"one narrow stale-snapshot exception",
		"explicitly says a target is inactive or not found",
		"its exact ID is absent from the refreshed inventory",
		"the task naturally left the addressable catalog and is not counted as applied",
		"status `complete` requires `title:\"🧵🐻 complete\"`",
		"status `needs_input` requires `title:\"🧵🐻 needs input (you): ACTION\"`",
		"status `next_steps` requires `title:\"🧵🐻 next steps (agent): ACTION\"`",
		"bare inputs such as `complete`, `blocked`, or `next_steps` are invalid",
		"Never prepend the visible status icon, insert the word `ThreadBear`, include the task subject, or pre-render the visible title",
		"the Pre hook alone expands the compact input around the authoritative subject",
		"Do not start another wave or return while a retained worker is still active or unaccounted for.",
		"If zero workers can start, wait 30 seconds and retry for at most two minutes",
		"ThreadBear controller registration.",
		"retain the create result only as a supervision handle",
		"report the complete accounting to the home so it can record `migration_failed` without `--settled`",
		"The failed phase denies every new title proposal",
		"Unapplied proposals remain pending for manual fail-closed recovery",
	} {
		if !strings.Contains(protocol, required) {
			t.Errorf("installed skill is missing adaptive migration invariant %q", required)
		}
	}
	if !strings.Contains(protocol, "`"+unknownMarker+"`") {
		t.Errorf("installed skill does not name the hook-accepted unknown marker %q", unknownMarker)
	}
	if strings.Contains(protocol, "Use Codex `/hooks` to inspect") {
		t.Error("installed skill still asks end users to inspect Codex hooks")
	}
	firstBatch := strings.Index(protocol, "first stable batch of at most 25")
	workerSurface := strings.LastIndex(protocol, "`codex_app__create_thread` using")
	if firstBatch < 0 || workerSurface < 0 || firstBatch > workerSurface {
		t.Error("installed skill does not put prompt deterministic progress before Luna worker creation")
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

func TestHomepageMatchesNativeMaintenanceCapabilities(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "site", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	page := string(data)
	for _, requiredClaim := range []string{
		"Create a new session in ChatGPT Desktop using Luna on medium effort, and paste this in:",
		"14 quiet days",
		"native task control",
		"never archives active work",
		"installs no LaunchAgent",
		"verified official release",
		"update check last",
	} {
		if !strings.Contains(page, requiredClaim) {
			t.Errorf("homepage is missing maintenance capability claim %q", requiredClaim)
		}
	}
	for _, removedClaim := range []string{
		"zero-token idle",
		"Unchanged heartbeats use zero model tokens",
		"produce zero output",
		"exits silently",
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
		filepath.Join(root, "skill", "SKILL.md"):    {"## Title cleanup", "same controller ID", "A stopped `migration_failed` installation is uninstallable", "prepared uninstall task", "For uninstall, target the control task last", "no attempt suffix"},
		filepath.Join(root, "AGENTS.threadbear.md"): {"A prepared uninstall suspends this turn protocol", "respond without another title call or ThreadBear footer", "THREADBEAR_TITLE_ATTEMPT='${attempt}'"},
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
	paths = append(paths, filepath.Join(root, "assets", "embed.go"), filepath.Join(root, "install.sh"))
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
	t.Logf("shipped executable logic: %d lines (target 1500, absolute ceiling 2000)", count)
	if count > 2000 {
		t.Fatalf("shipped executable logic is %d lines; absolute ceiling is 2000", count)
	}
}
