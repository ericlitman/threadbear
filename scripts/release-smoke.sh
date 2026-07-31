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
	rm -rf "$root"
}
trap cleanup EXIT HUP INT TERM

home=$root/home
codex_home=$home/.codex
rollout=$codex_home/release-smoke.jsonl
mkdir -p "$codex_home" "$home/.local/bin"
printf '%s\n' \
	'{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"release smoke"}]}}' \
	'{"type":"response_item","payload":{"type":"message","role":"assistant","phase":"final_answer","content":[{"type":"output_text","text":"Release smoke finished.\n\n🧵🐻 complete"}]}}' >"$rollout"
sqlite3 "$codex_home/state_1.sqlite" <<SQL
CREATE TABLE threads (
  id TEXT PRIMARY KEY, updated_at_ms INTEGER, title TEXT, name TEXT,
  archived INTEGER, source TEXT, thread_source TEXT, rollout_path TEXT
);
INSERT INTO threads VALUES (
  'release-smoke', 1, 'Release smoke subject', NULL,
  0, 'cli', 'cli', '$rollout'
);
SQL

env HOME="$home" CODEX_HOME="$codex_home" \
  PATH="$home/.local/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin" \
  THREADBEAR_RELEASE_BASE_URL="https://github.com/ericlitman/threadbear/releases" \
  sh -c 'curl -fsSL https://threadbear.sh/install.sh | sh -s -- \
    --version "$1" --noninteractive --confirm --json' \
  sh "$version"

binary=$home/.local/bin/threadbear
test "$("$binary" version --json | sed -n 's/.*"version":"\([^"]*\)".*/\1/p')" = "$version"
HOME="$home" CODEX_HOME="$codex_home" "$binary" self-test --candidate --json
HOME="$home" CODEX_HOME="$codex_home" "$binary" status --json
inventory=$(HOME="$home" CODEX_HOME="$codex_home" "$binary" inventory --json)
printf '%s\n' "$inventory" | grep -F '"count":1' >/dev/null
printf '%s\n' "$inventory" | grep -F '"deterministic":1' >/dev/null
printf '%s\n' "$inventory" | grep -F '"task_id":"release-smoke"' >/dev/null
printf '%s\n' "$inventory" | grep -F '"status":"complete"' >/dev/null

pre='{"hook_event_name":"PreToolUse","session_id":"release-smoke","tool_name":"codex_appset_thread_title","tool_use_id":"release-smoke-final","tool_input":{"title":"🧵🐻 complete"}}'
prepared=$(printf '%s\n' "$pre" | HOME="$home" CODEX_HOME="$codex_home" "$binary" hook)
expected_title='✅ Release smoke subject'
printf '%s\n' "$prepared" | grep -F '"permissionDecision":"allow"' >/dev/null
printf '%s\n' "$prepared" | grep -F "\"title\":\"$expected_title\"" >/dev/null

sqlite3 "$codex_home/state_1.sqlite" \
	"UPDATE threads SET title = '$expected_title' WHERE id = 'release-smoke';"
post='{"hook_event_name":"PostToolUse","session_id":"release-smoke","tool_name":"codex_appset_thread_title","tool_use_id":"release-smoke-final","tool_input":{"title":"✅ Release smoke subject"},"tool_response":"{\"threadId\":\"release-smoke\",\"title\":\"✅ Release smoke subject\"}"}'
printf '%s\n' "$post" | HOME="$home" CODEX_HOME="$codex_home" "$binary" hook

state=$home/.local/share/threadbear/native.json
grep -F '"release-smoke"' "$state" >/dev/null
grep -E '"subject"[[:space:]]*:[[:space:]]*"Release smoke subject"' "$state" >/dev/null
grep -E '"last"[[:space:]]*:[[:space:]]*"✅ Release smoke subject"' "$state" >/dev/null
if grep -F '"pending"' "$state" >/dev/null; then
	echo "release smoke title remained pending" >&2
	exit 1
fi
inventory=$(HOME="$home" CODEX_HOME="$codex_home" "$binary" inventory --json)
printf '%s\n' "$inventory" | grep -F '"applied":true' >/dev/null
HOME="$home" CODEX_HOME="$codex_home" "$binary" uninstall --noninteractive --confirm --json
test ! -e "$binary"
