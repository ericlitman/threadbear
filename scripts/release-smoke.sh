#!/bin/sh
set -eu

if [ "$#" -ne 1 ]; then
	echo "usage: $0 vN.N.N" >&2
	exit 2
fi
tag=$1
version=${tag#v}
if [ "$tag" = "$version" ] || ! printf '%s\n' "$version" | awk '/^[0-9]+\.[0-9]+\.[0-9]+$/ { ok=1 } END { exit(ok ? 0 : 1) }'; then
	echo "release smoke requires an exact vN.N.N tag" >&2
	exit 2
fi

root=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
original_home=${HOME:-}
domain="gui/$(id -u)"
service="$domain/org.litman.threadbear"
if /bin/launchctl print "$service" >/dev/null 2>&1; then
	echo "release smoke refuses to replace an already-loaded ThreadBear LaunchAgent" >&2
	exit 1
fi
disabled_services=$(/bin/launchctl print-disabled "$domain" 2>/dev/null || true)
if printf '%s\n' "$disabled_services" | grep -F 'org.litman.threadbear' | grep -Eq 'true|disabled'; then
	echo "release smoke refuses to change a pre-existing disabled ThreadBear LaunchAgent state" >&2
	exit 1
fi
temporary_root=$(mktemp -d "${TMPDIR:-/tmp}/threadbear-release-smoke.XXXXXX")
export HOME="$temporary_root/home"
export CODEX_HOME="$HOME/.codex"
export PATH="$HOME/.local/bin:$PATH"
installed=$HOME/.local/bin/threadbear

cleanup() {
	if [ -x "$installed" ]; then
		"$installed" uninstall --noninteractive --confirm >/dev/null 2>&1 || true
	fi
	/bin/launchctl bootout "$service" >/dev/null 2>&1 || true
	/bin/launchctl enable "$service" >/dev/null 2>&1 || true
	rm -rf "$temporary_root"
	if [ -n "$original_home" ]; then
		HOME=$original_home
		export HOME
	fi
}
trap cleanup EXIT HUP INT TERM

mkdir -p "$HOME/.local/bin" "$CODEX_HOME"
ln -s "$root/testdata/appserver/fake_codex.py" "$HOME/.local/bin/codex"

install_result=$temporary_root/install.json
curl -fsSL https://threadbear.sh/install.sh | sh -s -- \
	--version "$version" \
	--control-task-id release-smoke-control \
	--noninteractive --confirm --json \
	--heartbeat-seconds 300 \
	--auto-update=true \
	--archive=true \
	--archive-after-days 14 \
	--rename=true \
	--token-display=start \
	--agents=true \
	--classifier-model gpt-5.6-luna \
	--classifier-effort medium \
	--classifier-context-budget-bytes 250000 \
	>"$install_result"

python3 - "$install_result" <<'PY'
import json
import sys
result = json.load(open(sys.argv[1], encoding="utf-8"))
if result.get("command") != "install" or result.get("control_task_disposition") != "adopted":
    raise SystemExit("release smoke install did not adopt the fixture control task")
if result.get("warnings"):
    raise SystemExit("release smoke install returned warnings")
PY

version_result=$temporary_root/version.json
"$installed" version --json >"$version_result"
python3 - "$version_result" "$version" <<'PY'
import json
import sys
result = json.load(open(sys.argv[1], encoding="utf-8"))
if result.get("installed_version") != sys.argv[2]:
    raise SystemExit("installed version does not match release tag")
PY

self_test_result=$temporary_root/self-test.json
"$installed" self-test --json >"$self_test_result"
python3 - "$self_test_result" <<'PY'
import json
import sys
if json.load(open(sys.argv[1], encoding="utf-8")).get("ok") is not True:
    raise SystemExit("installed self-test failed")
PY

/bin/launchctl print "$service" >/dev/null
[ "$(stat -f '%Lp' "$installed")" = 700 ]
[ "$(stat -f '%Lp' "$HOME/Library/LaunchAgents/org.litman.threadbear.plist")" = 600 ]
[ -f "$CODEX_HOME/AGENTS.md" ]
[ -f "$CODEX_HOME/skills/threadbear/SKILL.md" ]

request_log=$CODEX_HOME/appserver-requests.log
[ "$(awk '$0 == "thread/read" { count++ } END { print count + 0 }' "$request_log")" -ge 3 ]
[ "$(awk '$0 == "thread/inject_items" { count++ } END { print count + 0 }' "$request_log")" -eq 1 ]
if awk '$0 == "thread/start" || $0 == "turn/start" { found=1 } END { exit(found ? 0 : 1) }' "$request_log"; then
	echo "fixture unexpectedly received classifier protocol calls" >&2
	exit 1
fi

"$installed" uninstall --noninteractive --confirm --json >"$temporary_root/uninstall.json"
python3 - "$temporary_root/uninstall.json" <<'PY'
import json
import sys
result = json.load(open(sys.argv[1], encoding="utf-8"))
if result.get("command") != "uninstall" or result.get("changed") is not True or result.get("deleted_state") is not True:
    raise SystemExit("release smoke uninstall result is incomplete")
PY
[ ! -e "$installed" ]
[ ! -e "$HOME/.local/share/threadbear" ]
[ ! -e "$HOME/Library/LaunchAgents/org.litman.threadbear.plist" ]
[ ! -e "$CODEX_HOME/AGENTS.md" ]
[ ! -e "$CODEX_HOME/skills/threadbear/SKILL.md" ]
if /bin/launchctl print "$service" >/dev/null 2>&1; then
	echo "LaunchAgent remains loaded after uninstall" >&2
	exit 1
fi

printf 'release smoke passed: version=%s architecture=%s install=ok self_test=ok launchagent=ok uninstall=ok\n' "$version" "$(uname -m)"
