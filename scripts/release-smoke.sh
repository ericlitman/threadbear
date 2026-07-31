#!/bin/sh
set -eu

tag=${1:?usage: release-smoke.sh vN.N.N}
version=${tag#v}
case "$tag" in
	v[0-9]*.[0-9]*.[0-9]*) ;;
	*) echo "release tag must be vN.N.N" >&2; exit 2 ;;
esac

root=$(mktemp -d "${TMPDIR:-/tmp}/threadbear-smoke.XXXXXX")
cleanup() {
	HOME="$root/home" launchctl bootout "gui/$(id -u)/org.litman.threadbear" >/dev/null 2>&1 || true
	rm -rf "$root"
}
trap cleanup EXIT HUP INT TERM

home=$root/home
codex_home=$home/.codex
mkdir -p "$codex_home" "$home/.local/bin"
rollout=$root/control.jsonl
printf '%s\n' '{"type":"response_item","payload":{"type":"message","role":"assistant","phase":"final_answer","content":[{"type":"output_text","text":"Ready.\\n\\n🧵🐻 complete"}]}}' > "$rollout"
sqlite3 "$codex_home/state_1.sqlite" <<SQL
CREATE TABLE threads (
  id TEXT PRIMARY KEY, updated_at_ms INTEGER, title TEXT, name TEXT,
  archived INTEGER, source TEXT, thread_source TEXT, rollout_path TEXT
);
INSERT INTO threads VALUES ('control',1,'ThreadBear',NULL,0,'vscode','','$rollout');
SQL
printf '#!/bin/sh\nexit 1\n' > "$home/.local/bin/codex"
chmod 700 "$home/.local/bin/codex"

env HOME="$home" CODEX_HOME="$codex_home" CODEX_THREAD_ID=control \
  PATH="$home/.local/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin" \
  THREADBEAR_RELEASE_BASE_URL="https://github.com/ericlitman/threadbear/releases" \
  sh -c 'curl -fsSL https://threadbear.sh/install.sh | sh -s -- \
    --version "$1" --control-task-id control --noninteractive --confirm --json' \
  sh "$version"

binary=$home/.local/bin/threadbear
test "$("$binary" version --json | sed -n 's/.*"version":"\([^"]*\)".*/\1/p')" = "$version"
HOME="$home" CODEX_HOME="$codex_home" "$binary" self-test --candidate --json
HOME="$home" CODEX_HOME="$codex_home" "$binary" status --json
HOME="$home" CODEX_HOME="$codex_home" "$binary" uninstall --noninteractive --confirm --json
test ! -e "$binary"
