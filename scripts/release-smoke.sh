#!/bin/sh
set -eu
umask 077

fail() {
	printf 'release smoke: %s\n' "$1" >&2
	exit 1
}

tag=${1:?usage: release-smoke.sh vN.N.N}
if ! printf '%s\n' "$tag" | awk '/^v[0-9]+\.[0-9]+\.[0-9]+$/ { valid=1 } END { exit(valid ? 0 : 1) }'; then
	fail "release tag must be vN.N.N"
fi
version=${tag#v}

test "$(uname -s)" = Darwin || fail "this smoke requires macOS"
test -x /bin/launchctl || fail "/bin/launchctl is unavailable"
command -v python3 >/dev/null 2>&1 || fail "python3 is unavailable"
unset PYTHONOPTIMIZE

root=$(mktemp -d "${TMPDIR:-/tmp}/threadbear-smoke.XXXXXX")
root=$(cd "$root" && pwd -P)
home=$root/home
codex_home=$home/.codex
binary=$home/.local/bin/threadbear
state_dir=$home/.local/share/threadbear
agent_label=sh.threadbear.update
agent_target="gui/$(id -u)/$agent_label"
agent_path=$home/Library/LaunchAgents/$agent_label.plist
fake_codex=$home/.local/bin/codex
app_server_log=$root/app-server.jsonl
app_server_state=$root/app-server-state.json
current_id=00000000-0000-4000-8000-000000000001
raw_id=00000000-0000-4000-8000-000000000002
delegated_id=00000000-0000-4000-8000-000000000003
blank_id=00000000-0000-4000-8000-000000000004
drift_id=10000000-0000-4000-8000-000000000001
failed_id=10000000-0000-4000-8000-000000000002
unconfirmed_id=10000000-0000-4000-8000-000000000003

cleanup() {
	set +e
	if launch_output=$(/bin/launchctl print "$agent_target" 2>/dev/null) &&
		{ printf '%s\n' "$launch_output" | grep -F "$binary" >/dev/null 2>&1 ||
			{ [ -n "${reset_binary:-}" ] && printf '%s\n' "$launch_output" | grep -F "$reset_binary" >/dev/null 2>&1; }; }; then
		if ! /bin/launchctl bootout "$agent_target" >/dev/null 2>&1 &&
			/bin/launchctl print "$agent_target" >/dev/null 2>&1; then
			printf 'release smoke: could not unload owned %s; retained %s\n' "$agent_target" "$root" >&2
			return
		fi
	fi
	rm -rf "$root"
}
trap cleanup EXIT HUP INT TERM

if /bin/launchctl print "$agent_target" >/dev/null 2>&1; then
	fail "$agent_target is already loaded; refusing to disturb it"
fi
case $(date '+%H:%M') in
	11:58|11:59|12:00|12:01|12:02)
		fail "refusing to load the daily updater near its 12:00 calendar firing"
		;;
esac

mkdir -p "$codex_home/skills/threadbear" "$home/.local/bin" "$home/Library/LaunchAgents"

agents_before=$root/AGENTS.before.md
hooks_before=$root/hooks.before.json
skill_neighbor_before=$root/skill-neighbor.before
agent_neighbor_before=$root/agent-neighbor.before.plist

cat >"$codex_home/AGENTS.md" <<'EOF'
# Unrelated local guidance

Keep this exact user-owned AGENTS content.
EOF
cp "$codex_home/AGENTS.md" "$agents_before"

cat >"$codex_home/hooks.json" <<'EOF'
{
  "user_setting": "keep",
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Bash",
        "hooks": [
          {"type": "command", "command": "foreign-pre", "timeout": 7}
        ]
      }
    ],
    "PostToolUse": [
      {
        "matcher": "foreign-title-tool",
        "hooks": [
          {"type": "command", "command": "foreign-post", "timeout": 9}
        ]
      }
    ]
  }
}
EOF
cp "$codex_home/hooks.json" "$hooks_before"

printf '%s\n' 'user-owned skill neighbor' >"$codex_home/skills/threadbear/NOTES.md"
cp "$codex_home/skills/threadbear/NOTES.md" "$skill_neighbor_before"

cat >"$home/Library/LaunchAgents/com.example.threadbear-smoke-neighbor.plist" <<'EOF'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>com.example.threadbear-smoke-neighbor</string>
  <key>ProgramArguments</key>
  <array><string>/usr/bin/true</string></array>
</dict>
</plist>
EOF
cp "$home/Library/LaunchAgents/com.example.threadbear-smoke-neighbor.plist" "$agent_neighbor_before"

cat >"$fake_codex" <<'PY'
#!/usr/bin/env python3
import json
import os
import sys

if sys.argv[1:] != ["app-server", "--stdio"]:
    raise SystemExit("fixture accepts only: codex app-server --stdio")

mode = os.environ.get("THREADBEAR_SMOKE_APP_SERVER_MODE", "normal")
log_path = os.environ["THREADBEAR_SMOKE_APP_SERVER_LOG"]
state_path = os.environ["THREADBEAR_SMOKE_APP_SERVER_STATE"]
current_id = "00000000-0000-4000-8000-000000000001"
raw_id = "00000000-0000-4000-8000-000000000002"
delegated_id = "00000000-0000-4000-8000-000000000003"
blank_id = "00000000-0000-4000-8000-000000000004"
drift_id = "10000000-0000-4000-8000-000000000001"
failed_id = "10000000-0000-4000-8000-000000000002"
unconfirmed_id = "10000000-0000-4000-8000-000000000003"

def initial_threads():
    value = [
        {
            "id": f"10000000-0000-4000-8000-{number:012d}",
            "name": f"Existing task {number:03d}",
            "preview": f"First message {number:03d}",
            "source": "cli",
        }
        for number in range(1, 106)
    ]
    value.extend([
        {
            "id": current_id,
            "name": "Release smoke exact subject",
            "preview": "<codex_internal_context>" + ("x" * 752) + "</codex_internal_context>",
            "source": "cli",
        },
        {
            "id": raw_id,
            "name": None,
            "preview": "<environment_context>private release smoke</environment_context>",
            "source": "vscode",
        },
        {
            "id": delegated_id,
            "name": "Visible delegated task",
            "preview": "Delegated task with a safe visible name",
            "source": "subagent",
        },
        {
            "id": blank_id,
            "name": "   ",
            "preview": "Plausible safe preview that must never become a title",
            "source": "cli",
        },
    ])
    assert len(value[-4]["preview"]) == 801
    return value

if os.path.exists(state_path):
    threads = json.load(open(state_path, encoding="utf-8"))["threads"]
else:
    threads = initial_threads()
    with open(state_path, "w", encoding="utf-8") as target:
        json.dump({"threads": threads}, target, separators=(",", ":"))
        target.write("\n")

by_id = {thread["id"]: thread for thread in threads}
current_page_count = 0

def save():
    with open(state_path, "w", encoding="utf-8") as target:
        json.dump({"threads": threads}, target, separators=(",", ":"))
        target.write("\n")

def send(value):
    print(json.dumps(value, separators=(",", ":")), flush=True)

for encoded in sys.stdin:
    message = json.loads(encoded)
    with open(log_path, "a", encoding="utf-8") as target:
        target.write(json.dumps(message, sort_keys=True) + "\n")
    method = message.get("method")
    request_id = message.get("id")
    params = message.get("params", {})

    if method == "initialize":
        send({"method": "fixture/notification", "params": {"stage": "initialize"}})
        send({"id": request_id, "result": {"serverInfo": {"name": "release-smoke"}}})
    elif method == "initialized":
        continue
    elif method == "thread/list":
        if params == {
            "archived": False,
            "limit": 25,
            "sortKey": "recency_at",
            "sortDirection": "desc",
        }:
            current_page_count += 1
            current_page = [
                dict(by_id[blank_id]),
                dict(by_id[delegated_id]),
                dict(by_id[current_id]),
                dict(by_id[raw_id]),
            ]
            send({"method": "fixture/notification", "params": {"stage": "current-page"}})
            send({"id": request_id, "result": {"data": current_page, "nextCursor": "must-not-follow"}})
            if mode == "current-rename-race" and current_page_count == 1:
                by_id[current_id]["name"] = "User rename during the no-CAS window"
                save()
                with open(log_path, "a", encoding="utf-8") as target:
                    target.write(json.dumps({"fixture": "external-rename", "name": by_id[current_id]["name"]}) + "\n")
        elif params == {"archived": False, "limit": 100}:
            send({"method": "fixture/notification", "params": {"stage": "page-1"}})
            send({"id": request_id, "result": {"data": threads[:100], "nextCursor": "page-2"}})
        elif params == {"archived": False, "limit": 100, "cursor": "page-2"}:
            if mode == "fail-page-2":
                send({"id": request_id, "error": {"code": -32000, "message": "injected page failure"}})
            else:
                send({"id": request_id, "result": {"data": threads[100:] + [threads[0]], "nextCursor": None}})
        else:
            send({"id": request_id, "error": {"code": -32602, "message": "unexpected list request"}})
    elif method == "thread/read":
        thread_id = params.get("threadId")
        if params != {"threadId": thread_id, "includeTurns": False} or thread_id not in by_id:
            send({"id": request_id, "error": {"code": -32602, "message": "unexpected read request"}})
        else:
            thread = dict(by_id[thread_id])
            if mode == "onboarding-edge" and thread_id == drift_id:
                thread["name"] = "Renamed while onboarding"
            send({"id": request_id, "result": {"thread": thread}})
    elif method == "thread/name/set":
        thread_id = params.get("threadId")
        name = params.get("name")
        if thread_id not in by_id or not isinstance(name, str):
            send({"id": request_id, "error": {"code": -32602, "message": "unexpected set request"}})
        elif mode == "onboarding-edge" and thread_id == failed_id:
            send({"id": request_id, "error": {"code": -32001, "message": "injected set failure"}})
        elif (
            mode == "current-unconfirmed" and thread_id == current_id
        ) or (
            mode == "onboarding-edge" and thread_id == unconfirmed_id
        ):
            send({"id": request_id, "result": {}})
        else:
            by_id[thread_id]["name"] = name
            save()
            send({"id": request_id, "result": {}})
    else:
        send({"id": request_id, "error": {"code": -32601, "message": "unexpected method"}})
PY
chmod 700 "$fake_codex"

run_threadbear_with_caller() {
	caller=$1
	shift
	HOME="$home" \
		CODEX_HOME="$codex_home" \
		CODEX_THREAD_ID="$caller" \
		PATH="$home/.local/bin:/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin" \
		THREADBEAR_RELEASE_BASE_URL="https://github.com/ericlitman/threadbear/releases" \
		THREADBEAR_SMOKE_APP_SERVER_LOG="$app_server_log" \
		THREADBEAR_SMOKE_APP_SERVER_STATE="$app_server_state" \
		THREADBEAR_SMOKE_APP_SERVER_MODE="${THREADBEAR_SMOKE_APP_SERVER_MODE:-normal}" \
		"$binary" "$@"
}

run_threadbear() {
	run_threadbear_with_caller "$current_id" "$@"
}

run_threadbear_without_caller() {
	env -u CODEX_THREAD_ID \
		HOME="$home" \
		CODEX_HOME="$codex_home" \
		PATH="$home/.local/bin:/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin" \
		THREADBEAR_RELEASE_BASE_URL="https://github.com/ericlitman/threadbear/releases" \
		THREADBEAR_SMOKE_APP_SERVER_LOG="$app_server_log" \
		THREADBEAR_SMOKE_APP_SERVER_STATE="$app_server_state" \
		"$binary" "$@"
}

candidate_override=${THREADBEAR_SMOKE_CANDIDATE:-}
published_installer=$root/published-install.sh
if [ -n "$candidate_override" ]; then
	test -x "$candidate_override" || fail "THREADBEAR_SMOKE_CANDIDATE is not executable"
	test "$("$candidate_override" version --json | sed -n 's/.*"version":"\([^"]*\)".*/\1/p')" = "$version" ||
		fail "candidate version does not match $version"
else
	curl -fsSL https://threadbear.sh/install.sh -o "$published_installer"
fi
run_published_installer() {
	if [ -n "$candidate_override" ]; then
		env HOME="$home" \
			CODEX_HOME="$codex_home" \
			PATH="$home/.local/bin:/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin" \
			THREADBEAR_RELEASE_BASE_URL="https://github.com/ericlitman/threadbear/releases" \
			"$candidate_override" install "$@"
		return
	fi
	env HOME="$home" \
		CODEX_HOME="$codex_home" \
		PATH="$home/.local/bin:/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin" \
		THREADBEAR_RELEASE_BASE_URL="https://github.com/ericlitman/threadbear/releases" \
		sh "$published_installer" "$@"
}

# Prove the one supported legacy reset removes only exact obsolete interception
# entries. Current-format paths below must leave hooks.json byte-identical.
reset_home=$root/reset-home
reset_codex_home=$reset_home/.codex
reset_state=$reset_home/.local/share/threadbear
reset_binary=$reset_home/.local/bin/threadbear
reset_main_id=20000000-0000-4000-8000-000000000001
reset_hooks=$reset_codex_home/hooks.json
mkdir -p "$reset_codex_home" "$reset_state" "$reset_home/.local/bin" "$reset_home/Library/LaunchAgents"
printf '{"format":4,"main_task_id":"%s","phase":"migration_complete","tasks":{}}\n' \
	"$reset_main_id" >"$reset_state/native.json"
chmod 700 "$reset_state"
chmod 600 "$reset_state/native.json"
python3 - "$reset_hooks" "$reset_binary" <<'PY'
import json
import sys

path, binary = sys.argv[1:]
owned = {
    "matcher": "codex_appset_thread_title",
    "hooks": [{"type": "command", "command": "'" + binary + "' hook", "timeout": 17}],
}
value = {
    "user_setting": "keep",
    "hooks": {
        "PreToolUse": [
            {"matcher": "Bash", "hooks": [{"type": "command", "command": "foreign-pre"}]},
            owned,
        ],
        "PostToolUse": [
            owned,
            {"matcher": "foreign-title-tool", "hooks": [{"type": "command", "command": "foreign-post"}]},
        ],
    },
}
with open(path, "w", encoding="utf-8") as target:
    json.dump(value, target, indent=2)
    target.write("\n")
PY

run_reset_installer() {
	if [ -n "$candidate_override" ]; then
		env HOME="$reset_home" \
			CODEX_HOME="$reset_codex_home" \
			PATH="$reset_home/.local/bin:/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin" \
			THREADBEAR_RELEASE_BASE_URL="https://github.com/ericlitman/threadbear/releases" \
			"$candidate_override" install "$@"
		return
	fi
	env HOME="$reset_home" \
		CODEX_HOME="$reset_codex_home" \
		PATH="$reset_home/.local/bin:/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin" \
		THREADBEAR_RELEASE_BASE_URL="https://github.com/ericlitman/threadbear/releases" \
		sh "$published_installer" "$@"
}
run_reset_threadbear() {
	HOME="$reset_home" \
		CODEX_HOME="$reset_codex_home" \
		PATH="$reset_home/.local/bin:/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin" \
		"$reset_binary" "$@"
}

run_reset_installer --version "$version" --dry-run --json >"$root/reset-preview.json"
python3 - "$root/reset-preview.json" "$version" "$reset_main_id" <<'PY'
import json
import sys

value = json.load(open(sys.argv[1], encoding="utf-8"))
version, main_id = sys.argv[2:]
assert value["ready"] is True and value["dry_run"] is True, value
assert value["version"] == version and value["legacy_reset_required"] is True, value
assert value["legacy_main_task_id"] == main_id, value
assert value["legacy_automation_id"] == "threadbear-maintenance", value
assert value["legacy_automation_target_thread_id"] == main_id, value
assert any("legacy ThreadBear title hooks" in change for change in value["planned_changes"]), value
PY
if run_reset_installer --version "$version" --noninteractive --confirm --json >"$root/reset-refused.json"; then
	fail "legacy install crossed the reset gate without --reset"
fi
test -e "$reset_state/native.json" || fail "refused reset deleted legacy state"
test ! -e "$reset_binary" || fail "refused reset wrote the binary"

# The guide owns the consented automation deletion and exact-task unpin. This
# isolated CLI begins after those native controls report success.
run_reset_installer --version "$version" --reset --noninteractive --confirm --json >"$root/reset-install.json"
python3 - "$root/reset-install.json" "$version" <<'PY'
import json
import sys

value = json.load(open(sys.argv[1], encoding="utf-8"))
assert value["ready"] is True and value["installed"] is True, value
assert value["version"] == sys.argv[2] and value["reset"] is True, value
assert value["legacy_reset_required"] is False and value["restart_required"] is True, value
PY
python3 - "$reset_hooks" <<'PY'
import json
import sys

value = json.load(open(sys.argv[1], encoding="utf-8"))
assert value["user_setting"] == "keep", value
assert [group["matcher"] for group in value["hooks"]["PreToolUse"]] == ["Bash"], value
assert [group["matcher"] for group in value["hooks"]["PostToolUse"]] == ["foreign-title-tool"], value
PY
test ! -e "$reset_state/native.json" || fail "completed reset retained legacy state"
run_reset_threadbear uninstall --dry-run --json >"$root/reset-uninstall-preview.json"
run_reset_threadbear uninstall --noninteractive --confirm --json >"$root/reset-uninstall.json"
python3 - "$root/reset-uninstall.json" <<'PY'
import json
import sys

value = json.load(open(sys.argv[1], encoding="utf-8"))
assert value["ready"] is True and value["uninstalled"] is True, value
assert value["restart_required"] is True and value["icons_may_remain"] is True, value
PY

# A foreign LaunchAgent collision must stop before every current-format surface.
printf '%s\n' 'foreign updater collision' >"$agent_path"
cp "$codex_home/hooks.json" "$root/hooks.before-collision.json"
if run_published_installer --version "$version" --noninteractive --confirm --json >"$root/install-collision.json"; then
	fail "install accepted a foreign LaunchAgent collision"
fi
python3 - "$root/install-collision.json" <<'PY'
import json
import sys

value = json.load(open(sys.argv[1], encoding="utf-8"))
assert value["ready"] is False and value["installed"] is False, value
assert "LaunchAgent" in value["error"], value
PY
cmp "$codex_home/hooks.json" "$root/hooks.before-collision.json" >/dev/null ||
	fail "failed current install changed hooks.json"
test ! -e "$binary" || fail "failed install preflight wrote the binary"
rm "$agent_path"

run_published_installer --version "$version" --dry-run --json >"$root/install-preview.json"
python3 - "$root/install-preview.json" "$version" <<'PY'
import json
import sys

value = json.load(open(sys.argv[1], encoding="utf-8"))
assert value["ready"] is True and value["dry_run"] is True, value
assert value["version"] == sys.argv[2] and value["installed"] is False, value
assert value["legacy_reset_required"] is False and value["partial"] is False, value
assert value["onboarding_requested"] is True, value
assert value["next_request"] == "threadbear onboard --dry-run --json", value
assert not any("hook" in change.lower() for change in value["planned_changes"]), value
PY
cmp "$codex_home/hooks.json" "$hooks_before" >/dev/null ||
	fail "current install preview changed hooks.json"
test ! -e "$binary" || fail "install preview wrote the binary"

run_published_installer --version "$version" --noninteractive --confirm --json >"$root/install.json"
python3 - "$root/install.json" "$version" <<'PY'
import json
import sys

value = json.load(open(sys.argv[1], encoding="utf-8"))
assert value["ready"] is True and value["installed"] is True, value
assert value["version"] == sys.argv[2] and value["dry_run"] is False, value
assert value["legacy_reset_required"] is False and value["partial"] is False, value
assert value["onboarding_requested"] is True, value
assert value["automatic_updates_enabled"] is True and value["restart_required"] is True, value
assert value["next_request"] == "threadbear onboard --dry-run --json", value
PY
cmp "$codex_home/hooks.json" "$hooks_before" >/dev/null ||
	fail "current install changed hooks.json"

test -x "$binary" || fail "published installer did not install an executable"
run_threadbear version --json >"$root/version.json"
run_threadbear self-test --candidate --json >"$root/self-test.json"
run_threadbear status --json >"$root/status.json"
python3 - "$root/version.json" "$root/self-test.json" "$root/status.json" "$version" "$binary" "$agent_path" <<'PY'
import json
import sys

version_value = json.load(open(sys.argv[1], encoding="utf-8"))
self_test = json.load(open(sys.argv[2], encoding="utf-8"))
status_value = json.load(open(sys.argv[3], encoding="utf-8"))
version, binary, agent_path = sys.argv[4:]
assert version_value == {"version": version}, version_value
assert self_test == {"ready": True, "version": version}, self_test
assert status_value["ready"] is True and status_value["installed"] is True, status_value
assert status_value["version"] == version and status_value["automatic_updates_enabled"] is True, status_value
assert status_value["artifacts"] == {
    "agents": True,
    "binary": True,
    "legacy_state_absent": True,
    "skill": True,
    "subjects": True,
}, status_value
assert status_value["updater"] == {
    "label": "sh.threadbear.update",
    "path": agent_path,
    "exact": True,
    "loaded": True,
    "program_arguments": [binary, "update", "--automatic", "--json"],
}, status_value
PY

python3 - "$codex_home/AGENTS.md" <<'PY'
import sys

text = open(sys.argv[1], encoding="utf-8").read()
assert text.count("title --status STATUS --json") == 1, text
assert "codex_app__set_thread_title" not in text, text
assert "PreToolUse" not in text and "PostToolUse" not in text, text
PY
cmp "$codex_home/hooks.json" "$hooks_before" >/dev/null ||
	fail "status or verification changed hooks.json"

/bin/launchctl print "$agent_target" >"$root/launchctl.txt"
grep -F "$binary" "$root/launchctl.txt" >/dev/null || fail "loaded updater does not name the smoke binary"
python3 - "$agent_path" "$binary" "$home" "$codex_home" <<'PY'
import os
import plistlib
import stat
import sys

path, binary, home, codex_home = sys.argv[1:]
with open(path, "rb") as source:
    value = plistlib.load(source)
assert value == {
    "Label": "sh.threadbear.update",
    "ProgramArguments": [binary, "update", "--automatic", "--json"],
    "StartCalendarInterval": {"Hour": 12, "Minute": 0},
    "EnvironmentVariables": {"HOME": home, "CODEX_HOME": codex_home},
    "StandardOutPath": "/dev/null",
    "StandardErrorPath": "/dev/null",
}, value
assert stat.S_IMODE(os.stat(path).st_mode) == 0o600
PY

/bin/launchctl bootout "$agent_target"
rm "$agent_path"
run_threadbear status --json >"$root/status-without-updater.json"
python3 - "$root/status-without-updater.json" <<'PY'
import json
import sys

value = json.load(open(sys.argv[1], encoding="utf-8"))
assert value["ready"] is True and value["installed"] is True, value
assert value["automatic_updates_enabled"] is False, value
assert value["updater"]["exact"] is False and value["updater"]["loaded"] is False, value
PY
run_threadbear install --no-onboard --noninteractive --confirm --json >"$root/reinstall-updater.json"
python3 - "$root/reinstall-updater.json" <<'PY'
import json
import sys

value = json.load(open(sys.argv[1], encoding="utf-8"))
assert value["ready"] is True and value["installed"] is True, value
assert value["automatic_updates_enabled"] is True and value["restart_required"] is True, value
assert value["onboarding_requested"] is False and "next_request" not in value, value
PY
/bin/launchctl print "$agent_target" >/dev/null 2>&1 || fail "reinstall did not restore the updater"
cmp "$codex_home/hooks.json" "$hooks_before" >/dev/null ||
	fail "current reinstall changed hooks.json"

# The terminal writer performs one exact current read, one name set, and exact
# readback. It persists only the safe subject.
: >"$app_server_log"
run_threadbear title --status complete --json >"$root/title-complete.json"
python3 - "$root/title-complete.json" "$current_id" <<'PY'
import json
import sys

value = json.load(open(sys.argv[1], encoding="utf-8"))
task_id = sys.argv[2]
assert value == {
    "ready": True,
    "task_id": task_id,
    "status": "complete",
    "previous_title": "Release smoke exact subject",
    "desired_title": "✅ Release smoke exact subject",
    "title": "✅ Release smoke exact subject",
    "updated": True,
    "unchanged": False,
    "unconfirmed": False,
}, value
PY
python3 - "$app_server_log" "$current_id" <<'PY'
import json
import sys

messages = [json.loads(line) for line in open(sys.argv[1], encoding="utf-8")]
current_id = sys.argv[2]
methods = [message["method"] for message in messages]
assert methods == ["initialize", "initialized", "thread/list", "thread/name/set", "thread/list"], messages
lists = [message for message in messages if message["method"] == "thread/list"]
expected = {"archived": False, "limit": 25, "sortKey": "recency_at", "sortDirection": "desc"}
assert [message["params"] for message in lists] == [expected, expected], lists
assert [message["id"] for message in lists] == [2, 4], lists
setter = next(message for message in messages if message["method"] == "thread/name/set")
assert setter["id"] == 3, setter
assert setter["params"] == {"threadId": current_id, "name": "✅ Release smoke exact subject"}, setter
assert all("cursor" not in message["params"] and "searchTerm" not in message["params"] for message in lists)
PY
python3 - "$state_dir/subjects/$current_id.json" <<'PY'
import json
import os
import stat
import sys

path = sys.argv[1]
assert json.load(open(path, encoding="utf-8")) == {"subject": "Release smoke exact subject"}
assert stat.S_IMODE(os.stat(path).st_mode) == 0o600
PY

: >"$app_server_log"
if run_threadbear_without_caller title --status complete --json >"$root/title-no-caller.json"; then
	fail "title command accepted a missing CODEX_THREAD_ID"
fi
python3 - "$root/title-no-caller.json" <<'PY'
import json
import sys

value = json.load(open(sys.argv[1], encoding="utf-8"))
assert value["ready"] is False and "CODEX_THREAD_ID" in value["error"], value
assert value["updated"] is False and value["unconfirmed"] is False, value
PY
test ! -s "$app_server_log" || fail "missing caller started the App Server"

: >"$app_server_log"
if THREADBEAR_SMOKE_APP_SERVER_MODE=current-unconfirmed \
	run_threadbear title --status next_steps --json >"$root/title-unconfirmed.json"; then
	fail "title command accepted acknowledgement without exact readback"
fi
unset THREADBEAR_SMOKE_APP_SERVER_MODE
python3 - "$root/title-unconfirmed.json" <<'PY'
import json
import sys

value = json.load(open(sys.argv[1], encoding="utf-8"))
assert value["ready"] is False and value["unconfirmed"] is True, value
assert value["previous_title"] == "✅ Release smoke exact subject", value
assert value["desired_title"] == "➡️ Release smoke exact subject", value
assert "not confirmed by exact readback" in value["reason"], value
assert value["updated"] is False and value["unchanged"] is False, value
PY
python3 - "$app_server_log" <<'PY'
import json
import sys

messages = [json.loads(line) for line in open(sys.argv[1], encoding="utf-8")]
assert [message["method"] for message in messages].count("thread/name/set") == 1, messages
PY

run_threadbear title --status automation --json >"$root/title-automation.json"
python3 - "$root/title-automation.json" <<'PY'
import json
import sys

value = json.load(open(sys.argv[1], encoding="utf-8"))
assert value["ready"] is True and value["updated"] is True, value
assert value["title"] == "🤖 Release smoke exact subject", value
PY

# Codex has no compare-and-set between the immediate read and the one write.
# Force that accepted ordering and prove it remains one bounded write with no
# retry or reconciliation. The ordinary real-Desktop canary is the practical
# kill-switch gate; this fixture records the unavoidable protocol semantics.
: >"$app_server_log"
THREADBEAR_SMOKE_APP_SERVER_MODE=current-rename-race \
	run_threadbear title --status blocked --json >"$root/title-rename-race.json"
unset THREADBEAR_SMOKE_APP_SERVER_MODE
python3 - "$root/title-rename-race.json" <<'PY'
import json
import sys

value = json.load(open(sys.argv[1], encoding="utf-8"))
assert value["ready"] is True and value["updated"] is True, value
assert value["previous_title"] == "🤖 Release smoke exact subject", value
assert value["desired_title"] == "🚨 Release smoke exact subject", value
assert value["title"] == "🚨 Release smoke exact subject", value
assert value["unconfirmed"] is False, value
PY
python3 - "$app_server_log" <<'PY'
import json
import sys

messages = [json.loads(line) for line in open(sys.argv[1], encoding="utf-8")]
assert [message for message in messages if message.get("fixture") == "external-rename"] == [
    {"fixture": "external-rename", "name": "User rename during the no-CAS window"}
], messages
assert [message.get("method") for message in messages].count("thread/name/set") == 1, messages
assert [message.get("method") for message in messages].count("thread/list") == 2, messages
PY
cmp "$codex_home/hooks.json" "$hooks_before" >/dev/null ||
	fail "direct title writer changed hooks.json"

# Full enumeration must finish before any historical write.
find "$state_dir/subjects" -type f -exec shasum -a 256 {} \; | LC_ALL=C sort >"$root/subjects.before-failed-page"
app_state_before=$(shasum -a 256 "$app_server_state" | awk '{print $1}')
: >"$app_server_log"
if THREADBEAR_SMOKE_APP_SERVER_MODE=fail-page-2 \
	run_threadbear onboard --dry-run --json >"$root/onboard-failed-page.json"; then
	fail "onboard accepted an App Server page failure"
fi
unset THREADBEAR_SMOKE_APP_SERVER_MODE
python3 - "$root/onboard-failed-page.json" <<'PY'
import json
import sys

value = json.load(open(sys.argv[1], encoding="utf-8"))
assert value["ready"] is False and "thread/list page 2" in value["error"], value
assert value["plan_complete"] is False and value["total"] == 0, value
assert value["items"] is None and value["updated"] == 0 and value["unconfirmed"] == 0, value
PY
find "$state_dir/subjects" -type f -exec shasum -a 256 {} \; | LC_ALL=C sort >"$root/subjects.after-failed-page"
cmp "$root/subjects.before-failed-page" "$root/subjects.after-failed-page" >/dev/null ||
	fail "failed catalog enumeration changed subject state"
test "$(shasum -a 256 "$app_server_state" | awk '{print $1}')" = "$app_state_before" ||
	fail "failed catalog enumeration changed task state"
python3 - "$app_server_log" <<'PY'
import json
import sys

messages = [json.loads(line) for line in open(sys.argv[1], encoding="utf-8")]
lists = [message for message in messages if message.get("method") == "thread/list"]
assert [message["params"] for message in lists] == [
    {"archived": False, "limit": 100},
    {"archived": False, "limit": 100, "cursor": "page-2"},
], lists
assert not any(message.get("method") == "thread/name/set" for message in messages), messages
PY

: >"$app_server_log"
run_threadbear onboard --dry-run --json >"$root/onboard-preview.json"
python3 - "$root/onboard-preview.json" "$current_id" "$raw_id" "$blank_id" <<'PY'
import json
import sys

value = json.load(open(sys.argv[1], encoding="utf-8"))
current_id, raw_id, blank_id = sys.argv[2:]
assert value["ready"] is True and value["plan_complete"] is True and value["read_only"] is True, value
assert value["onboarding_complete"] is False, value
assert value["total"] == len(value["items"]) == 109, value
assert value["safe"] == 107 and value["needs_update"] == 106, value
assert value["updated"] == 0 and value["unchanged"] == 1, value
assert value["skipped"] == 2 and value["unconfirmed"] == 0, value
assert [item["task_id"] for item in value["items"]] == sorted(item["task_id"] for item in value["items"])
by_id = {item["task_id"]: item for item in value["items"]}
assert by_id[current_id]["outcome"] == "unchanged", by_id[current_id]
for task_id in (raw_id, blank_id):
    item = by_id[task_id]
    assert item["safe"] is False and item["outcome"] == "skipped", item
    assert "title" not in item and "subject" not in item and "desired_title" not in item, item
PY
python3 - "$app_server_log" <<'PY'
import json
import sys

messages = [json.loads(line) for line in open(sys.argv[1], encoding="utf-8")]
assert not any(message.get("method") in {"thread/read", "thread/name/set"} for message in messages), messages
PY

# One confirmed pass handles the entire safe set serially. Synthetic drift,
# setter failure, and acknowledgement-without-readback stay local and appear in
# the aggregate receipt. The active caller is never neutralized.
: >"$app_server_log"
if THREADBEAR_SMOKE_APP_SERVER_MODE=onboarding-edge \
	run_threadbear_with_caller "$delegated_id" onboard --noninteractive --confirm --json >"$root/onboard-edge.json"; then
	fail "edge onboarding reported complete despite unconfirmed targets"
fi
unset THREADBEAR_SMOKE_APP_SERVER_MODE
python3 - "$root/onboard-edge.json" "$delegated_id" "$drift_id" "$failed_id" "$unconfirmed_id" <<'PY'
import json
import sys

value = json.load(open(sys.argv[1], encoding="utf-8"))
delegated_id, drift_id, failed_id, unconfirmed_id = sys.argv[2:]
assert value["ready"] is False and value["plan_complete"] is True, value
assert value["read_only"] is False and value["onboarding_complete"] is False, value
assert value["total"] == len(value["items"]) == 109 and value["safe"] == 107, value
assert value["needs_update"] == 0, value
assert value["updated"] == 102 and value["unchanged"] == 2, value
assert value["skipped"] == 3 and value["unconfirmed"] == 2, value
assert value["updated"] + value["unchanged"] + value["skipped"] + value["unconfirmed"] == value["total"], value
by_id = {item["task_id"]: item for item in value["items"]}
assert by_id[delegated_id]["outcome"] == "unchanged", by_id[delegated_id]
assert by_id[delegated_id]["reason"] == "active task is handled by the terminal title writer", by_id[delegated_id]
assert by_id[drift_id]["outcome"] == "skipped", by_id[drift_id]
assert by_id[failed_id]["outcome"] == "unconfirmed", by_id[failed_id]
assert by_id[unconfirmed_id]["outcome"] == "unconfirmed", by_id[unconfirmed_id]
PY
python3 - "$app_server_log" "$delegated_id" "$drift_id" <<'PY'
import json
import sys

messages = [json.loads(line) for line in open(sys.argv[1], encoding="utf-8")]
delegated_id, drift_id = sys.argv[2:]
sets = [message for message in messages if message.get("method") == "thread/name/set"]
assert len(sets) == 104, len(sets)
ids = [message["params"]["threadId"] for message in sets]
assert len(ids) == len(set(ids)), ids
assert delegated_id not in ids and drift_id not in ids, ids
reads = [message for message in messages if message.get("method") == "thread/read"]
assert all(message["params"]["includeTurns"] is False for message in reads), reads
PY
cmp "$codex_home/hooks.json" "$hooks_before" >/dev/null ||
	fail "confirmed onboarding changed hooks.json"

run_threadbear onboard --dry-run --json >"$root/onboard-after-edge.json"
python3 - "$root/onboard-after-edge.json" <<'PY'
import json
import sys

value = json.load(open(sys.argv[1], encoding="utf-8"))
assert value["ready"] is True and value["plan_complete"] is True, value
assert value["total"] == 109 and value["safe"] == 107, value
assert value["needs_update"] == 4 and value["unchanged"] == 103, value
assert value["skipped"] == 2 and value["updated"] == 0 and value["unconfirmed"] == 0, value
PY

run_threadbear onboard --noninteractive --confirm --json >"$root/onboard-converged.json"
python3 - "$root/onboard-converged.json" <<'PY'
import json
import sys

value = json.load(open(sys.argv[1], encoding="utf-8"))
assert value["ready"] is True and value["plan_complete"] is True, value
assert value["read_only"] is False and value["onboarding_complete"] is True, value
assert value["total"] == 109 and value["safe"] == 107, value
assert value["needs_update"] == 0 and value["updated"] == 4, value
assert value["unchanged"] == 103 and value["skipped"] == 2 and value["unconfirmed"] == 0, value
assert value["updated"] + value["unchanged"] + value["skipped"] == value["total"], value
PY

# Exercise the real daily updater once, then the direct current-version command.
binary_before_update=$(shasum -a 256 "$binary" | awk '{print $1}')
agent_before_update=$(shasum -a 256 "$agent_path" | awk '{print $1}')
test ! -e "$state_dir/update.json" || fail "install ran the update-only LaunchAgent unexpectedly"
/bin/launchctl kickstart -k "$agent_target"
update_wait=0
while [ "$update_wait" -lt 30 ]; do
	/bin/launchctl print "$agent_target" >"$root/launchctl-after-kickstart.txt"
	if grep -F 'last exit code =' "$root/launchctl-after-kickstart.txt" |
		grep -vF '(never exited)' >/dev/null; then
		if grep -F 'state = not running' "$root/launchctl-after-kickstart.txt" >/dev/null &&
			grep -F 'last exit code = 0' "$root/launchctl-after-kickstart.txt" >/dev/null; then
			break
		fi
		fail "the real update-only LaunchAgent exited unsuccessfully"
	fi
	sleep 1
	update_wait=$((update_wait + 1))
done
test "$update_wait" -lt 30 || fail "the real update-only LaunchAgent did not finish"

python3 - "$state_dir/update.json" "$version" "${candidate_override:+candidate}" <<'PY'
import datetime
import json
import sys

value = json.load(open(sys.argv[1], encoding="utf-8"))
current = tuple(map(int, sys.argv[2].split(".")))
latest = tuple(map(int, value["version"].split(".")))
assert value["from"] == sys.argv[2], value
assert (latest <= current if sys.argv[3] == "candidate" else latest == current), value
assert value["outcome"] == "current" and value["automatic"] is True, value
assert value["restart_required"] is False, value
datetime.datetime.fromisoformat(value["checked_at"].replace("Z", "+00:00"))
PY
test "$(shasum -a 256 "$binary" | awk '{print $1}')" = "$binary_before_update" ||
	fail "automatic current update replaced the binary"
test "$(shasum -a 256 "$agent_path" | awk '{print $1}')" = "$agent_before_update" ||
	fail "automatic current update changed the LaunchAgent"

run_threadbear update --json >"$root/update.json"
python3 - "$root/update.json" "$version" "${candidate_override:+candidate}" <<'PY'
import json
import sys

value = json.load(open(sys.argv[1], encoding="utf-8"))
assert value["ready"] is True and value["current"] is True, value
assert value["version"] == sys.argv[2] and value["automatic"] is False, value
assert value["restart_required"] is False, value
current = tuple(map(int, sys.argv[2].split(".")))
latest = tuple(map(int, value["latest"].split(".")))
assert (latest <= current if sys.argv[3] == "candidate" else latest == current), value
assert set(value) == {"ready", "current", "version", "latest", "automatic", "restart_required"}, value
PY
cmp "$codex_home/hooks.json" "$hooks_before" >/dev/null ||
	fail "update changed hooks.json"

run_threadbear uninstall --dry-run --json >"$root/uninstall-preview.json"
python3 - "$root/uninstall-preview.json" "$binary" "$state_dir" "$codex_home" "$agent_path" <<'PY'
import json
import sys

path, binary, state_dir, codex_home, agent_path = sys.argv[1:]
changes = [
    f"boot out and remove sh.threadbear.update LaunchAgent {agent_path}",
    f"remove managed AGENTS block from {codex_home}/AGENTS.md",
    f"remove skill {codex_home}/skills/threadbear/SKILL.md",
    f"remove owned subject records under {state_dir}/subjects",
    f"remove update receipt {state_dir}/update.json",
    f"remove binary last {binary}",
]
value = json.load(open(path, encoding="utf-8"))
assert value == {
    "ready": True,
    "dry_run": True,
    "uninstalled": False,
    "icons_may_remain": True,
    "restart_required": False,
    "partial": False,
    "warning": "Existing ThreadBear title icons may remain until renamed.",
    "planned_changes": changes,
}, value
assert not any("hook" in change.lower() for change in changes), changes
PY
cmp "$codex_home/hooks.json" "$hooks_before" >/dev/null ||
	fail "uninstall preview changed hooks.json"
test -x "$binary" || fail "uninstall preview removed the binary"
test -e "$agent_path" || fail "uninstall preview removed the LaunchAgent"

/bin/launchctl kickstart -k "$agent_target"
run_threadbear uninstall --noninteractive --confirm --json >"$root/uninstall.json"
python3 - "$root/uninstall.json" "$root/uninstall-preview.json" <<'PY'
import json
import sys

value = json.load(open(sys.argv[1], encoding="utf-8"))
preview = json.load(open(sys.argv[2], encoding="utf-8"))
assert value == {
    "ready": True,
    "dry_run": False,
    "uninstalled": True,
    "icons_may_remain": True,
    "restart_required": True,
    "partial": False,
    "warning": preview["warning"],
    "planned_changes": preview["planned_changes"],
}, value
PY

test ! -e "$binary" || fail "uninstall left the binary"
test -x "$fake_codex" || fail "uninstall removed the neighboring Codex fixture"
test ! -e "$state_dir" || fail "uninstall left ThreadBear state"
test ! -e "$agent_path" || fail "uninstall left the LaunchAgent"
if /bin/launchctl print "$agent_target" >/dev/null 2>&1; then
	fail "uninstall left the updater loaded"
fi
cmp "$codex_home/AGENTS.md" "$agents_before" >/dev/null || fail "uninstall changed unrelated AGENTS content"
cmp "$codex_home/skills/threadbear/NOTES.md" "$skill_neighbor_before" >/dev/null ||
	fail "uninstall changed neighboring skill content"
test ! -e "$codex_home/skills/threadbear/SKILL.md" || fail "uninstall left the managed skill"
cmp "$home/Library/LaunchAgents/com.example.threadbear-smoke-neighbor.plist" "$agent_neighbor_before" >/dev/null ||
	fail "uninstall changed a neighboring LaunchAgent"
cmp "$codex_home/hooks.json" "$hooks_before" >/dev/null ||
	fail "current-format uninstall changed hooks.json"

if [ -n "$candidate_override" ]; then
	printf 'ThreadBear %s exact-candidate release smoke passed.\n' "$version"
else
	printf 'ThreadBear %s published release smoke passed.\n' "$version"
fi
