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
fake_codex=$home/Applications/ChatGPT.app/Contents/Resources/codex
app_server_log=$root/app-server.jsonl
app_server_state=$root/app-server-state.json
native_tool_log=$root/native-tool.jsonl
simulate_mounted=$root/simulate-mounted.py
current_id=00000000-0000-4000-8000-000000000001
raw_id=00000000-0000-4000-8000-000000000002
delegated_id=00000000-0000-4000-8000-000000000003
blank_id=00000000-0000-4000-8000-000000000004
mounted_drift_id=10000000-0000-4000-8000-000000000002
unconfirmed_id=10000000-0000-4000-8000-000000000003
mounted_wrong_id=10000000-0000-4000-8000-000000000004

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

mkdir -p "$codex_home/skills/threadbear" "$home/.local/bin" "$home/Library/LaunchAgents" "$(dirname "$fake_codex")"

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

if sys.argv[1:] == ["--version"]:
    print("codex-cli 0.146.0")
    raise SystemExit(0)
if sys.argv[1:] != ["app-server", "--stdio"]:
    raise SystemExit("fixture accepts only --version or app-server --stdio")

mode = os.environ.get("THREADBEAR_SMOKE_APP_SERVER_MODE", "normal")
log_path = os.environ["THREADBEAR_SMOKE_APP_SERVER_LOG"]
state_path = os.environ["THREADBEAR_SMOKE_APP_SERVER_STATE"]
current_id = "00000000-0000-4000-8000-000000000001"
raw_id = "00000000-0000-4000-8000-000000000002"
delegated_id = "00000000-0000-4000-8000-000000000003"
blank_id = "00000000-0000-4000-8000-000000000004"

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
            "limit": 100,
            "sortKey": "recency_at",
            "sortDirection": "desc",
        }:
            send({"method": "fixture/notification", "params": {"stage": "current-page-1"}})
            send({"id": request_id, "result": {"data": threads[:100], "nextCursor": "current-page-2"}})
        elif params == {
            "archived": False,
            "limit": 100,
            "sortKey": "recency_at",
            "sortDirection": "desc",
            "cursor": "current-page-2",
        }:
            send({"method": "fixture/notification", "params": {"stage": "current-page-2"}})
            send({"id": request_id, "result": {"data": threads[100:], "nextCursor": None}})
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
            send({"id": request_id, "result": {"thread": thread}})
    elif method == "thread/name/set":
        send({"id": request_id, "error": {"code": -32099, "message": "production binary must not call thread/name/set"}})
    else:
        send({"id": request_id, "error": {"code": -32601, "message": "unexpected method"}})
PY
chmod 700 "$fake_codex"
THREADBEAR_SMOKE_APP_SERVER_LOG="$app_server_log" \
	THREADBEAR_SMOKE_APP_SERVER_STATE="$app_server_state" \
	"$fake_codex" app-server --stdio </dev/null

cat >"$simulate_mounted" <<'PY'
#!/usr/bin/env python3
import json
import sys

mode, plan_path, state_path, log_path, output_path, fail_id, drift_id, wrong_id = sys.argv[1:]
plan = json.load(open(plan_path, encoding="utf-8"))
state = json.load(open(state_path, encoding="utf-8"))
by_id = {thread["id"]: thread for thread in state["threads"]}

def save():
    with open(state_path, "w", encoding="utf-8") as target:
        json.dump(state, target, separators=(",", ":"))
        target.write("\n")

def decode_tool_result(value):
    if isinstance(value, str):
        try:
            value = json.loads(value)
        except json.JSONDecodeError:
            return None
    return value if isinstance(value, dict) else None

def set_title(task_id, title, explicit):
    params = {"title": title}
    if explicit:
        params["threadId"] = task_id
    record = {"method": "codex_app__set_thread_title", "params": params}
    if task_id == fail_id:
        record["error"] = "injected mounted setter failure"
        with open(log_path, "a", encoding="utf-8") as target:
            target.write(json.dumps(record, sort_keys=True) + "\n")
        return None
    if task_id not in by_id or not isinstance(title, str):
        raise SystemExit("invalid simulated native setter input")
    by_id[task_id]["name"] = title
    save()
    response = json.dumps({"threadId": task_id, "title": title}, separators=(",", ":"))
    record["response"] = response
    with open(log_path, "a", encoding="utf-8") as target:
        target.write(json.dumps(record, sort_keys=True) + "\n")
    return response

def read_title(task_id):
    if task_id not in by_id:
        return None
    if task_id == drift_id:
        by_id[task_id]["name"] = "User rename at mounted revalidation"
        save()
    returned_id = "90000000-0000-4000-8000-000000000999" if task_id == wrong_id else task_id
    response = json.dumps({"thread": {"id": returned_id, "title": by_id[task_id].get("name")}}, separators=(",", ":"))
    record = {
        "method": "codex_app__read_thread",
        "params": {
            "threadId": task_id,
            "includeOutputs": False,
            "turnLimit": 1,
            "maxOutputCharsPerItem": 1,
        },
        "response": response,
    }
    with open(log_path, "a", encoding="utf-8") as target:
        target.write(json.dumps(record, sort_keys=True) + "\n")
    return response

if mode == "current":
    task_id = plan.get("task_id")
    if (plan.get("ready") is not True or not isinstance(task_id, str) or
        not isinstance(plan.get("icon"), str) or
        not isinstance(plan.get("owned_prefixes"), list) or
        not isinstance(plan.get("blocked_prefixes"), list) or
        not isinstance(plan.get("internal_markers"), list) or
        not isinstance(plan.get("max_title_units"), int)):
        raise SystemExit("invalid current title policy")
    current = decode_tool_result(read_title(task_id))
    if (current is None or current.get("thread", {}).get("id") != task_id or
        not isinstance(current.get("thread", {}).get("title"), str)):
        result = {"ready": False, "reason": "Codex title read was not confirmed exactly"}
    else:
        previous = current["thread"]["title"]
        if any(previous.startswith(prefix) for prefix in plan["blocked_prefixes"]):
            result = {"ready": False, "reason": "The current title has an ambiguous old ThreadBear prefix"}
        else:
            subject = previous
            for prefix in plan["owned_prefixes"]:
                if subject.startswith(prefix):
                    subject = subject[len(prefix):]
                    break
            lower = subject.lower()
            units = len((plan["icon"] + " " + subject).encode("utf-16-le")) // 2
            unsafe = (not subject.strip() or any(ord(char) < 32 or 127 <= ord(char) <= 159 or
                      char in "\u2028\u2029" for char in subject) or
                      any(marker in lower for marker in plan["internal_markers"]) or
                      units > plan["max_title_units"])
            if unsafe:
                result = {"ready": False, "reason": "The current title is not safe to decorate"}
            else:
                desired = plan["icon"] + " " + subject
                if desired == previous:
                    result = {"ready": True, "task_id": task_id, "title": previous, "updated": False}
                else:
                    raw_response = set_title(task_id, desired, False)
                    response = decode_tool_result(raw_response)
                    if raw_response is None:
                        result = {"ready": False, "reason": "Codex title write failed"}
                    elif response is None or response.get("threadId") != task_id or response.get("title") != desired:
                        result = {"ready": False, "reason": "Codex title write was not confirmed exactly"}
                    else:
                        result = {"ready": True, "task_id": task_id, "title": response["title"], "updated": True}
elif mode == "onboard":
    if plan.get("ready") is not True or plan.get("plan_complete") is not True or plan.get("read_only") is not False or not isinstance(plan.get("items"), list):
        raise SystemExit("invalid onboarding plan")
    prepared = [item for item in plan["items"] if item.get("outcome") == "prepared"]
    if any(not isinstance(item.get("task_id"), str) or not isinstance(item.get("title"), str) or not isinstance(item.get("desired_title"), str) for item in prepared):
        raise SystemExit("invalid prepared onboarding item")
    updated = 0
    skipped = 0
    unconfirmed = 0
    for item in prepared:
        current = decode_tool_result(read_title(item["task_id"]))
        if current is None or current.get("thread", {}).get("id") != item["task_id"] or current.get("thread", {}).get("title") != item["title"]:
            skipped += 1
            continue
        response = decode_tool_result(set_title(item["task_id"], item["desired_title"], True))
        if response is not None and response.get("threadId") == item["task_id"] and response.get("title") == item["desired_title"]:
            updated += 1
        else:
            unconfirmed += 1
    total = plan["total"] if isinstance(plan.get("total"), int) else len(plan["items"])
    accounted = updated + skipped + unconfirmed == len(prepared)
    result = {
        "ready": accounted and unconfirmed == 0,
        "plan_complete": True,
        "onboarding_complete": accounted and unconfirmed == 0,
        "total": total,
        "updated": updated,
        "skipped": skipped,
        "unchanged": total - updated - unconfirmed,
        "unconfirmed": unconfirmed,
    }
else:
    raise SystemExit("unknown mounted simulation mode")

with open(output_path, "w", encoding="utf-8") as target:
    json.dump(result, target, separators=(",", ":"))
    target.write("\n")
PY
chmod 700 "$simulate_mounted"

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
reset_codex=$reset_home/Applications/ChatGPT.app/Contents/Resources/codex
reset_main_id=20000000-0000-4000-8000-000000000001
reset_hooks=$reset_codex_home/hooks.json
mkdir -p "$reset_codex_home" "$reset_state" "$reset_home/.local/bin" "$reset_home/Library/LaunchAgents" "$(dirname "$reset_codex")"
cp "$fake_codex" "$reset_codex"
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
assert self_test["ready"] is True and self_test["version"] == version, self_test
assert isinstance(self_test["codex_version"], str) and self_test["codex_version"], self_test
assert status_value["ready"] is True and status_value["installed"] is True, status_value
assert status_value["version"] == version and status_value["automatic_updates_enabled"] is True, status_value
assert status_value["codex_version"] == self_test["codex_version"], status_value
assert status_value["artifacts"] == {
    "agents": True,
    "binary": True,
    "codex": True,
    "legacy_state_absent": True,
    "skill": True,
    "state": True,
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
assert text.count("tools.codex_app__set_thread_title") == 1, text
assert text.count("tools.codex_app__read_thread") == 1, text
assert "const decodeNative = value =>" in text, text
assert "decodeNative(await tools.codex_app__read_thread" in text, text
assert "decodeNative(await tools.codex_app__set_thread_title" in text, text
assert "plan.owned_prefixes" in text and "plan.blocked_prefixes" in text, text
assert "thread/name/set" not in text, text
assert "PreToolUse" not in text and "PostToolUse" not in text, text
PY
python3 - "$codex_home/skills/threadbear/SKILL.md" <<'PY'
import sys

text = open(sys.argv[1], encoding="utf-8").read()
assert text.count("tools.codex_app__set_thread_title") == 1, text
assert text.count("tools.codex_app__read_thread") == 1, text
assert text.count("tools.write_stdin") == 1, text
assert 'sandbox_permissions:"require_escalated"' in text, text
assert "Allow ThreadBear to read the full Codex task list" in text, text
assert 'item.outcome === "prepared"' in text, text
assert "const parseNative = value =>" in text, text
assert 'if (typeof value !== "string") return value;' in text, text
assert "try { return JSON.parse(value); } catch { return null; }" in text, text
assert "current = parseNative(await tools.codex_app__read_thread" in text, text
assert "renamed = parseNative(await tools.codex_app__set_thread_title" in text, text
assert "current?.thread?.id !== item.task_id" in text, text
assert "current.thread.title !== item.title" in text, text
assert "updated + skipped + unconfirmed === prepared.length" in text, text
assert "thread/name/set" not in text, text
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

# The production helper returns only fixed policy under the caller's ordinary
# workspace sandbox. It starts no App Server and writes no title state. The
# mounted simulation performs the exact read and sole possible title write.
: >"$app_server_log"
: >"$native_tool_log"
run_threadbear title --status complete --json >"$root/title-complete-plan.json"
python3 - "$root/title-complete-plan.json" "$current_id" <<'PY'
import json
import sys

value = json.load(open(sys.argv[1], encoding="utf-8"))
task_id = sys.argv[2]
assert value["ready"] is True and value["task_id"] == task_id, value
assert value["status"] == "complete" and value["icon"] == "✅", value
assert value["owned_prefixes"] == ["✅ ", "➡️ ", "🙋 ", "🚨 ", "🤖 ", "🐻 "], value
assert value["blocked_prefixes"] == ["➡ ", "⏳ ", "❔ ", "🧵🐻"], value
assert "<codex_delegation>" in value["internal_markers"], value
assert value["max_title_units"] == 60, value
PY
test ! -s "$app_server_log" || fail "ordinary title helper started App Server"
"$simulate_mounted" current "$root/title-complete-plan.json" "$app_server_state" "$native_tool_log" "$root/title-complete.json" "" "" ""
python3 - "$root/title-complete.json" "$native_tool_log" "$current_id" <<'PY'
import json
import sys

value = json.load(open(sys.argv[1], encoding="utf-8"))
calls = [json.loads(line) for line in open(sys.argv[2], encoding="utf-8")]
task_id = sys.argv[3]
assert value == {"ready": True, "task_id": task_id, "title": "✅ Release smoke exact subject", "updated": True}, value
assert [call["method"] for call in calls] == ["codex_app__read_thread", "codex_app__set_thread_title"], calls
assert calls[0]["params"] == {
    "threadId": task_id, "includeOutputs": False, "turnLimit": 1, "maxOutputCharsPerItem": 1,
}, calls
assert isinstance(calls[0]["response"], str), calls
assert calls[1] == {
    "method": "codex_app__set_thread_title",
    "params": {"title": "✅ Release smoke exact subject"},
    "response": json.dumps({"threadId": task_id, "title": "✅ Release smoke exact subject"}, separators=(",", ":")),
}, calls
PY
test ! -e "$state_dir/subjects" || fail "ordinary title helper created subject state"

: >"$app_server_log"
if run_threadbear_without_caller title --status complete --json >"$root/title-no-caller.json"; then
	fail "title command accepted a missing CODEX_THREAD_ID"
fi
python3 - "$root/title-no-caller.json" <<'PY'
import json
import sys

value = json.load(open(sys.argv[1], encoding="utf-8"))
assert value["ready"] is False and "CODEX_THREAD_ID" in value["error"], value
assert value["task_id"] == "" and value["icon"] == "", value
PY
test ! -s "$app_server_log" || fail "missing caller started the App Server"

# A mounted setter failure is local to the managed cell. It gets one native
# attempt, no detached fallback, and no reconciliation or retry.
: >"$app_server_log"
: >"$native_tool_log"
run_threadbear title --status next_steps --json >"$root/title-next-plan.json"
"$simulate_mounted" current "$root/title-next-plan.json" "$app_server_state" "$native_tool_log" "$root/title-next-failed.json" "$current_id" "" ""
python3 - "$root/title-next-plan.json" "$root/title-next-failed.json" "$native_tool_log" <<'PY'
import json
import sys

plan = json.load(open(sys.argv[1], encoding="utf-8"))
result = json.load(open(sys.argv[2], encoding="utf-8"))
calls = [json.loads(line) for line in open(sys.argv[3], encoding="utf-8")]
assert plan["ready"] is True and plan["status"] == "next_steps", plan
assert plan["icon"] == "➡️", plan
assert result == {"ready": False, "reason": "Codex title write failed"}, result
assert [call["method"] for call in calls] == ["codex_app__read_thread", "codex_app__set_thread_title"], calls
assert calls[1]["params"] == {"title": "➡️ Release smoke exact subject"}, calls
assert calls[1]["error"] == "injected mounted setter failure", calls
PY
test ! -s "$app_server_log" || fail "failed ordinary title update started App Server"

: >"$app_server_log"
: >"$native_tool_log"
run_threadbear title --status automation --json >"$root/title-automation-plan.json"
"$simulate_mounted" current "$root/title-automation-plan.json" "$app_server_state" "$native_tool_log" "$root/title-automation.json" "" "" ""
python3 - "$root/title-automation-plan.json" "$root/title-automation.json" "$native_tool_log" <<'PY'
import json
import sys

plan = json.load(open(sys.argv[1], encoding="utf-8"))
value = json.load(open(sys.argv[2], encoding="utf-8"))
calls = [json.loads(line) for line in open(sys.argv[3], encoding="utf-8")]
assert plan["status"] == "automation" and plan["icon"] == "🤖", plan
assert value["ready"] is True and value["updated"] is True, value
assert value["title"] == "🤖 Release smoke exact subject", value
assert [call["method"] for call in calls] == ["codex_app__read_thread", "codex_app__set_thread_title"], calls
assert isinstance(calls[0].get("response"), str) and isinstance(calls[1].get("response"), str), calls
PY
test ! -s "$app_server_log" || fail "ordinary automation title update started App Server"
cmp "$codex_home/hooks.json" "$hooks_before" >/dev/null ||
	fail "title planning or mounted setter simulation changed hooks.json"

# Full enumeration must finish before any historical write.
test ! -e "$state_dir/subjects" || fail "title flow created subject state"
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
assert value["items"] is None and value["prepared"] == 0 and value["needs_update"] == 0, value
PY
test ! -e "$state_dir/subjects" || fail "failed catalog enumeration created subject state"
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
assert value["prepared"] == 0 and value["unchanged"] == 1 and value["skipped"] == 2, value
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

# One confirmed production pass prepares actions from the complete snapshot
# without per-target RPCs. The mounted-native simulation then rereads every
# prepared title immediately before one possible setter; drift and a same-title
# response for the wrong task both skip the write, while one injected setter
# failure proves exact accounting and no retry.
: >"$app_server_log"
: >"$native_tool_log"
app_state_before_preparation=$(shasum -a 256 "$app_server_state" | awk '{print $1}')
run_threadbear_with_caller "$delegated_id" onboard --noninteractive --confirm --json >"$root/onboard-prepared-edge.json"
python3 - "$root/onboard-prepared-edge.json" "$delegated_id" <<'PY'
import json
import sys

value = json.load(open(sys.argv[1], encoding="utf-8"))
delegated_id = sys.argv[2]
assert value["ready"] is True and value["plan_complete"] is True, value
assert value["read_only"] is False and value["onboarding_complete"] is False, value
assert value["total"] == len(value["items"]) == 109 and value["safe"] == 107, value
assert value["needs_update"] == 105 and value["prepared"] == 105, value
assert value["unchanged"] == 2 and value["skipped"] == 2, value
assert value["prepared"] + value["unchanged"] + value["skipped"] == value["total"], value
by_id = {item["task_id"]: item for item in value["items"]}
assert by_id[delegated_id]["outcome"] == "unchanged", by_id[delegated_id]
assert by_id[delegated_id]["reason"] == "active task is handled by the terminal title writer", by_id[delegated_id]
assert all(item["outcome"] != "updated" and item["outcome"] != "unconfirmed" for item in value["items"]), value
PY
python3 - "$app_server_log" <<'PY'
import json
import sys

messages = [json.loads(line) for line in open(sys.argv[1], encoding="utf-8")]
lists = [message for message in messages if message.get("method") == "thread/list"]
assert [message["params"] for message in lists] == [
    {"archived": False, "limit": 100},
    {"archived": False, "limit": 100, "cursor": "page-2"},
], lists
assert not any(message.get("method") in {"thread/read", "thread/name/set"} for message in messages), messages
PY
test "$(shasum -a 256 "$app_server_state" | awk '{print $1}')" = "$app_state_before_preparation" ||
	fail "onboarding preparation mutated native task state"
test ! -e "$state_dir/subjects" || fail "onboarding preparation created subject state"
"$simulate_mounted" onboard "$root/onboard-prepared-edge.json" "$app_server_state" "$native_tool_log" "$root/onboard-edge.json" "$unconfirmed_id" "$mounted_drift_id" "$mounted_wrong_id"
python3 - "$root/onboard-edge.json" "$native_tool_log" "$root/onboard-prepared-edge.json" "$unconfirmed_id" "$mounted_drift_id" "$mounted_wrong_id" <<'PY'
import json
import sys

value = json.load(open(sys.argv[1], encoding="utf-8"))
calls = [json.loads(line) for line in open(sys.argv[2], encoding="utf-8")]
plan = json.load(open(sys.argv[3], encoding="utf-8"))
failed_id, drift_id, wrong_id = sys.argv[4:]
assert value == {
    "ready": False,
    "plan_complete": True,
    "onboarding_complete": False,
    "total": 109,
    "updated": 102,
    "skipped": 2,
    "unchanged": 6,
    "unconfirmed": 1,
}, value
reads = [call for call in calls if call["method"] == "codex_app__read_thread"]
sets = [call for call in calls if call["method"] == "codex_app__set_thread_title"]
assert len(reads) == 105 and len(sets) == 103 and len(calls) == 208, len(calls)
read_ids = [call["params"]["threadId"] for call in reads]
set_ids = [call["params"]["threadId"] for call in sets]
prepared = {item["task_id"]: item for item in plan["items"] if item["outcome"] == "prepared"}
assert len(read_ids) == len(set(read_ids)), read_ids
assert len(set_ids) == len(set(set_ids)), set_ids
assert set(read_ids) == set(prepared), (read_ids, prepared)
assert all(call["params"]["title"] == prepared[call["params"]["threadId"]]["desired_title"] for call in sets), sets
assert read_ids.count(drift_id) == 1 and drift_id not in set_ids, (read_ids, set_ids)
drift_response = next(call for call in reads if call["params"]["threadId"] == drift_id)["response"]
assert isinstance(drift_response, str), drift_response
assert json.loads(drift_response)["thread"]["title"] == "User rename at mounted revalidation", reads
assert read_ids.count(wrong_id) == 1 and wrong_id not in set_ids, (read_ids, set_ids)
wrong_response = json.loads(next(call for call in reads if call["params"]["threadId"] == wrong_id)["response"])
assert wrong_response["thread"]["id"] != wrong_id, wrong_response
assert wrong_response["thread"]["title"] == prepared[wrong_id]["title"], wrong_response
assert set_ids.count(failed_id) == 1, set_ids
assert all(call["params"] == {
    "threadId": call["params"]["threadId"],
    "includeOutputs": False,
    "turnLimit": 1,
    "maxOutputCharsPerItem": 1,
} for call in reads), reads
assert all(calls[index - 1]["method"] == "codex_app__read_thread" and
           calls[index - 1]["params"]["threadId"] == call["params"]["threadId"]
           for index, call in enumerate(calls) if call["method"] == "codex_app__set_thread_title"), calls
assert all(isinstance(call.get("response"), str) for call in reads), reads
assert all(isinstance(call.get("response"), str) for call in sets if "error" not in call), sets
assert sum("error" in call for call in sets) == 1, sets
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
assert value["needs_update"] == 4 and value["prepared"] == 0, value
assert value["unchanged"] == 103 and value["skipped"] == 2, value
PY

: >"$app_server_log"
: >"$native_tool_log"
run_threadbear onboard --noninteractive --confirm --json >"$root/onboard-final-plan.json"
python3 - "$root/onboard-final-plan.json" "$app_server_log" <<'PY'
import json
import sys

value = json.load(open(sys.argv[1], encoding="utf-8"))
assert value["ready"] is True and value["plan_complete"] is True, value
assert value["read_only"] is False and value["onboarding_complete"] is False, value
assert value["total"] == 109 and value["safe"] == 107, value
assert value["needs_update"] == 4 and value["prepared"] == 4, value
assert value["unchanged"] == 103 and value["skipped"] == 2, value
messages = [json.loads(line) for line in open(sys.argv[2], encoding="utf-8")]
assert not any(message.get("method") in {"thread/read", "thread/name/set"} for message in messages), messages
PY
"$simulate_mounted" onboard "$root/onboard-final-plan.json" "$app_server_state" "$native_tool_log" "$root/onboard-converged.json" "" "" ""
python3 - "$root/onboard-converged.json" "$native_tool_log" "$root/onboard-final-plan.json" <<'PY'
import json
import sys

value = json.load(open(sys.argv[1], encoding="utf-8"))
calls = [json.loads(line) for line in open(sys.argv[2], encoding="utf-8")]
plan = json.load(open(sys.argv[3], encoding="utf-8"))
assert value == {
    "ready": True,
    "plan_complete": True,
    "onboarding_complete": True,
    "total": 109,
    "updated": 4,
    "skipped": 0,
    "unchanged": 105,
    "unconfirmed": 0,
}, value
reads = [call for call in calls if call["method"] == "codex_app__read_thread"]
sets = [call for call in calls if call["method"] == "codex_app__set_thread_title"]
prepared = {item["task_id"]: item for item in plan["items"] if item["outcome"] == "prepared"}
assert len(reads) == 4 and len(sets) == 4 and len(calls) == 8, calls
assert len({call["params"]["threadId"] for call in reads}) == 4, reads
assert len({call["params"]["threadId"] for call in sets}) == 4, sets
assert {call["params"]["threadId"] for call in reads} == set(prepared), (reads, prepared)
assert all(call["params"]["title"] == prepared[call["params"]["threadId"]]["desired_title"] for call in sets), sets
assert all(isinstance(call.get("response"), str) and "error" not in call for call in calls), calls
PY

: >"$app_server_log"
run_threadbear onboard --dry-run --json >"$root/onboard-final-preview.json"
python3 - "$root/onboard-final-preview.json" "$app_server_log" <<'PY'
import json
import sys

value = json.load(open(sys.argv[1], encoding="utf-8"))
assert value["ready"] is True and value["onboarding_complete"] is True, value
assert value["needs_update"] == 0 and value["prepared"] == 0, value
assert value["unchanged"] == 107 and value["skipped"] == 2, value
messages = [json.loads(line) for line in open(sys.argv[2], encoding="utf-8")]
assert not any(message.get("method") in {"thread/read", "thread/name/set"} for message in messages), messages
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
    f"remove private ThreadBear state under {state_dir}",
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
