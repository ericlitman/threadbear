#!/bin/sh
set -eu

usage() {
	echo "usage: $0 --version N.N.N --control-task-id ID --replica /absolute/copied-codex-home --codex /absolute/codex" >&2
	exit 2
}

version=
control_task_id=
replica=
codex=
while [ "$#" -gt 0 ]; do
	case "$1" in
		--version) [ "$#" -ge 2 ] || usage; version=$2; shift 2 ;;
		--control-task-id) [ "$#" -ge 2 ] || usage; control_task_id=$2; shift 2 ;;
		--replica) [ "$#" -ge 2 ] || usage; replica=$2; shift 2 ;;
		--codex) [ "$#" -ge 2 ] || usage; codex=$2; shift 2 ;;
		*) usage ;;
	esac
done
if [ -z "$version" ] || [ -z "$control_task_id" ] || [ -z "$replica" ] || [ -z "$codex" ]; then
	usage
fi
printf '%s\n' "$version" | awk '/^[0-9]+\.[0-9]+\.[0-9]+$/ { ok=1 } END { exit(ok ? 0 : 1) }' || usage
[ "$(uname -s)" = Darwin ] || { echo "replica rehearsal requires macOS" >&2; exit 1; }
case $(uname -m) in
	arm64) goarch=arm64 ;;
	x86_64|amd64) goarch=amd64 ;;
	*) echo "replica rehearsal requires arm64 or x86_64" >&2; exit 1 ;;
esac
if [ "${replica#/}" = "$replica" ] || [ ! -d "$replica" ]; then
	usage
fi
if [ "${codex#/}" = "$codex" ] || [ ! -x "$codex" ]; then
	usage
fi

root=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
if [ -n "$(git -C "$root" status --porcelain --untracked-files=normal)" ]; then
	echo "replica rehearsal requires a clean release checkout" >&2
	exit 1
fi
source_commit=$(git -C "$root" rev-parse HEAD)
original_home=${HOME:-}
genuine_codex_home=${CODEX_HOME:-$original_home/.codex}
replica=$(python3 -c 'import os,sys; print(os.path.realpath(sys.argv[1]))' "$replica")
genuine_codex_home=$(python3 -c 'import os,sys; print(os.path.realpath(sys.argv[1]))' "$genuine_codex_home")
if [ "$replica" = "$genuine_codex_home" ] || [ "${replica#"$genuine_codex_home"/}" != "$replica" ] || [ "${genuine_codex_home#"$replica"/}" != "$genuine_codex_home" ]; then
	echo "replica rehearsal refuses the genuine live Codex home or any overlapping path" >&2
	exit 1
fi

domain="gui/$(id -u)"
service="$domain/org.litman.threadbear"
if /bin/launchctl print "$service" >/dev/null 2>&1; then
	echo "replica rehearsal refuses to replace an already-loaded ThreadBear LaunchAgent" >&2
	exit 1
fi
disabled_services=$(/bin/launchctl print-disabled "$domain")
if printf '%s\n' "$disabled_services" | grep -F 'org.litman.threadbear' | grep -Eq 'true|disabled'; then
	echo "replica rehearsal refuses to change a pre-existing disabled ThreadBear LaunchAgent state" >&2
	exit 1
fi

temporary_root=$(mktemp -d "${TMPDIR:-/tmp}/threadbear-replica-rehearsal.XXXXXX")
# Reuse the host module cache: with HOME redirected, Go would otherwise download
# every dependency into the temporary home as read-only files that rm cannot remove.
export GOMODCACHE="$(go env GOMODCACHE)"
export HOME="$temporary_root/home"
export CODEX_HOME="$HOME/.codex"
export CODEX_SQLITE_HOME="$CODEX_HOME"
export GOCACHE="$temporary_root/go-cache"
export PATH="$HOME/.local/bin:$PATH"
installed=$HOME/.local/bin/threadbear

cleanup() {
	chmod -R u+w "$temporary_root" 2>/dev/null || true
	if [ -x "$installed" ]; then
		"$installed" uninstall --noninteractive --confirm >/dev/null 2>&1 || true
	fi
	/bin/launchctl bootout "$service" >/dev/null 2>&1 || true
	/bin/launchctl enable "$service" >/dev/null 2>&1 || true
	rm -rf "$temporary_root"
}
trap cleanup EXIT HUP INT TERM

mkdir -p "$HOME/.local/bin"
ditto "$replica" "$CODEX_HOME"
if ! python3 - "$CODEX_HOME" 2>"$temporary_root/symlink-check.stderr" <<'PY'
import sys
from pathlib import Path

copy_root = Path(sys.argv[1]).resolve()
for candidate in copy_root.rglob("*"):
    if not candidate.is_symlink():
        continue
    resolved = candidate.resolve(strict=False)
    try:
        resolved.relative_to(copy_root)
    except ValueError:
        raise SystemExit(1)
PY
then
	echo "replica contains a link outside the isolated copy" >&2
	exit 1
fi
ln -s "$codex" "$HOME/.local/bin/codex"
if [ -f "$CODEX_HOME/config.toml" ]; then
	mv "$CODEX_HOME/config.toml" "$CODEX_HOME/config.toml.replica-source"
fi

state_database=$(python3 - "$CODEX_HOME" "$replica" "$genuine_codex_home" "$temporary_root/replica-counts" 2>"$temporary_root/replica-prepare.stderr" <<'PY'
import re
import sqlite3
import sys
from pathlib import Path

copy_root = Path(sys.argv[1]).resolve()
source_roots = [Path(sys.argv[2]).resolve(), Path(sys.argv[3]).resolve()]
candidates = []
for candidate in copy_root.glob("state_*.sqlite"):
    match = re.fullmatch(r"state_([0-9]+)\.sqlite", candidate.name)
    if match and candidate.is_file() and not candidate.is_symlink():
        try:
            candidate.resolve().relative_to(copy_root)
        except ValueError:
            continue
        candidates.append((int(match.group(1)), candidate))
if not candidates:
    raise SystemExit(1)
database = max(candidates)[1]
connection = sqlite3.connect(database)
titles = connection.execute("SELECT title, name FROM threads WHERE source = 'vscode'").fetchall()
effective_titles = [(name or title or "").strip() for title, name in titles]
status_only = {"⏳", "🚨", "🙋", "🤖", "➡️", "✅", "❔"}
emoji_only_titles = sum(title in status_only for title in effective_titles)
if len(titles) < 50:
    connection.close()
    raise SystemExit(1)
Path(sys.argv[4]).write_text("replica_tasks=%d emoji_only_titles=%d" % (len(titles), emoji_only_titles), encoding="utf-8")
rows = connection.execute("SELECT id, rollout_path FROM threads WHERE rollout_path IS NOT NULL AND rollout_path <> ''").fetchall()
for task_id, rollout_path in rows:
    path = Path(rollout_path)
    if not path.is_absolute():
        connection.close()
        raise SystemExit(1)
    replacement = None
    for source_root in source_roots:
        try:
            relative = path.resolve(strict=False).relative_to(source_root)
        except ValueError:
            continue
        replacement = copy_root / relative
        try:
            replacement.resolve(strict=False).relative_to(copy_root)
        except ValueError:
            replacement = None
            continue
        break
    if replacement is None:
        connection.close()
        raise SystemExit(1)
    connection.execute("UPDATE threads SET rollout_path = ? WHERE id = ?", (str(replacement), task_id))
connection.commit()
connection.close()
print(database)
PY
) || { echo "replica must contain 50+ real-shape tasks and self-contained rollout paths" >&2; exit 1; }
[ -n "$state_database" ]

release_directory=$temporary_root/releases/download/v$version
mkdir -p "$release_directory"
asset=$release_directory/threadbear_darwin_$goarch
CGO_ENABLED=0 GOOS=darwin GOARCH=$goarch go -C "$root" build -trimpath -ldflags "-s -w -X main.version=$version" -o "$asset" ./cmd/threadbear
shasum -a 256 "$asset" >"$asset.sha256"
asset_url=$(python3 -c 'from pathlib import Path; import sys; print(Path(sys.argv[1]).resolve().as_uri())' "$asset")
checksum_url=$(python3 -c 'from pathlib import Path; import sys; print(Path(sys.argv[1]).resolve().as_uri())' "$asset.sha256")
cat >"$release_directory/latest.json" <<EOF
{
  "version": "$version",
  "assets": {
    "darwin_$goarch": {
      "url": "$asset_url",
      "sha256_url": "$checksum_url"
    }
  }
}
EOF
release_base_url=$(python3 -c 'from pathlib import Path; import sys; print(Path(sys.argv[1]).resolve().as_uri())' "$temporary_root/releases")
bootstrap_url=$(python3 -c 'from pathlib import Path; import sys; print(Path(sys.argv[1]).resolve().as_uri())' "$root/install.sh")
export THREADBEAR_RELEASE_BASE_URL="$release_base_url"

install_result=$temporary_root/install.json
curl -fsSL "$bootstrap_url" | sh -s -- \
	--version "$version" \
	--control-task-id "$control_task_id" \
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
	>"$install_result" 2>"$temporary_root/install.stderr"

python3 - "$install_result" <<'PY'
import json
import sys
result = json.load(open(sys.argv[1], encoding="utf-8"))
if result.get("command") != "install" or result.get("control_task_disposition") != "adopted" or result.get("warnings"):
    raise SystemExit(1)
PY

"$installed" version --json >"$temporary_root/version.json" 2>"$temporary_root/version.stderr"
python3 - "$temporary_root/version.json" "$version" <<'PY'
import json
import sys
if json.load(open(sys.argv[1], encoding="utf-8")).get("installed_version") != sys.argv[2]:
    raise SystemExit(1)
PY
"$installed" self-test --json >"$temporary_root/self-test.json" 2>"$temporary_root/self-test.stderr"
python3 - "$temporary_root/self-test.json" <<'PY'
import json
import sys
if json.load(open(sys.argv[1], encoding="utf-8")).get("ok") is not True:
    raise SystemExit(1)
PY
/bin/launchctl print "$service" >/dev/null

"$installed" status --json >"$temporary_root/status-before.json" 2>"$temporary_root/status-before.stderr"
heartbeat_required=$(python3 - "$temporary_root/status-before.json" <<'PY'
import json
import sys
result = json.load(open(sys.argv[1], encoding="utf-8"))
print("yes" if result.get("last_completed_heartbeat") is None else "no")
PY
)
if [ "$heartbeat_required" = yes ]; then
	"$installed" heartbeat --json >"$temporary_root/heartbeat.json" 2>"$temporary_root/heartbeat.stderr"
else
	: >"$temporary_root/heartbeat.json"
fi
"$installed" status --json >"$temporary_root/status-after.json" 2>"$temporary_root/status-after.stderr"
heartbeat_counts=$(python3 - "$temporary_root/heartbeat.json" <<'PY'
import json
import sys
text = open(sys.argv[1], encoding="utf-8").read().strip()
if not text:
    print("changed=0 archived=0 retries=0")
else:
    result = json.loads(text)
    if result.get("error_code"):
        raise SystemExit(1)
    print("changed=%d archived=%d retries=%d" % (len(result.get("changed", [])), len(result.get("archived_ids", [])), len(result.get("retries", []))))
PY
)
status_counts=$(python3 - "$temporary_root/status-after.json" <<'PY'
import json
import sys
result = json.load(open(sys.argv[1], encoding="utf-8"))
if result.get("last_completed_heartbeat") is None or result.get("last_reconcile_failure") is not None:
    raise SystemExit(1)
print("pending_retries=%d" % result.get("pending_retries", 0))
PY
)

"$installed" uninstall --noninteractive --confirm --json >"$temporary_root/uninstall.json" 2>"$temporary_root/uninstall.stderr"
python3 - "$temporary_root/uninstall.json" <<'PY'
import json
import sys
result = json.load(open(sys.argv[1], encoding="utf-8"))
if result.get("command") != "uninstall" or result.get("changed") is not True or result.get("deleted_state") is not True:
    raise SystemExit(1)
PY
[ ! -e "$installed" ]
[ ! -e "$HOME/.local/share/threadbear" ]
[ ! -e "$HOME/Library/LaunchAgents/org.litman.threadbear.plist" ]
if /bin/launchctl print "$service" >/dev/null 2>&1; then
	echo "replica rehearsal left the LaunchAgent loaded" >&2
	exit 1
fi
if grep -F '<!-- BEGIN THREADBEAR MANAGED BLOCK -->' "$CODEX_HOME/AGENTS.md" "$CODEX_HOME/skills/threadbear/SKILL.md" >/dev/null 2>&1; then
	echo "replica rehearsal left a managed block behind" >&2
	exit 1
fi

codex_version=$("$codex" --version 2>/dev/null | sed -n '1p')
replica_counts=$(sed -n '1p' "$temporary_root/replica-counts")
printf 'replica rehearsal passed: commit=%s version=%s architecture=%s macos=%s codex=%s %s install=ok self_test=ok launchagent=ok heartbeat_%s %s %s uninstall=ok isolation=temporary_copy\n' \
	"$source_commit" "$version" "$(uname -m)" "$(sw_vers -productVersion)" "$codex_version" "$replica_counts" "$heartbeat_required" "$heartbeat_counts" "$status_counts"
