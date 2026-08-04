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
  archived INTEGER, source TEXT, thread_source TEXT, rollout_path TEXT,
  first_user_message TEXT, preview TEXT
);
INSERT INTO threads VALUES (
  'release-smoke', 1, 'Release smoke raw first message with extra instructions', NULL,
  0, 'cli', 'cli', '$rollout', 'Release smoke raw first message with extra instructions',
  'Release smoke raw first message with extra instructions'
);
INSERT INTO threads VALUES (
  'release-smoke-home', 1, '⏳ Release smoke home', NULL,
  1, 'cli', '', '$rollout', 'Release smoke home', 'Release smoke home'
);
INSERT INTO threads VALUES (
  'release-smoke-controller', 1, '⏳ Completed controller sentinel', NULL,
  1, 'cli', '', '$rollout', 'Completed controller sentinel', 'Completed controller sentinel'
);
SQL

env HOME="$home" CODEX_HOME="$codex_home" \
  PATH="$home/.local/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin" \
  THREADBEAR_RELEASE_BASE_URL="https://github.com/ericlitman/threadbear/releases" \
  sh -c 'curl -fsSL https://threadbear.sh/install.sh | sh -s -- \
    --version "$1" --control-task-id release-smoke-home --noninteractive --confirm --json' \
  sh "$version"

binary=$home/.local/bin/threadbear
test "$("$binary" version --json | sed -n 's/.*"version":"\([^"]*\)".*/\1/p')" = "$version"
HOME="$home" CODEX_HOME="$codex_home" "$binary" self-test --candidate --json
HOME="$home" CODEX_HOME="$codex_home" "$binary" migration \
	--phase migration_running --controller-task-id release-smoke-controller --json
HOME="$home" CODEX_HOME="$codex_home" "$binary" status --json
inventory=$(HOME="$home" CODEX_HOME="$codex_home" "$binary" inventory --json)
printf '%s\n' "$inventory" | grep -F '"count":1' >/dev/null
printf '%s\n' "$inventory" | grep -F '"deterministic":1' >/dev/null
printf '%s\n' "$inventory" | grep -F '"task_id":"release-smoke"' >/dev/null
printf '%s\n' "$inventory" | grep -F '"status":"complete"' >/dev/null

pre='{"hook_event_name":"PreToolUse","session_id":"release-smoke","tool_name":"codex_appset_thread_title","tool_use_id":"release-smoke-running","tool_input":{"title":"⏳ ThreadBear is working: Release smoke seeded subject"}}'
prepared=$(printf '%s\n' "$pre" | HOME="$home" CODEX_HOME="$codex_home" "$binary" hook)
expected_title='⏳ Release smoke seeded subject'
printf '%s\n' "$prepared" | grep -F '"permissionDecision":"allow"' >/dev/null
printf '%s\n' "$prepared" | grep -F "\"title\":\"$expected_title\"" >/dev/null
sqlite3 "$codex_home/state_1.sqlite" "UPDATE threads SET title = '$expected_title' WHERE id = 'release-smoke';"
post='{"hook_event_name":"PostToolUse","session_id":"release-smoke","tool_name":"codex_appset_thread_title","tool_use_id":"release-smoke-running","tool_input":{"title":"⏳ Release smoke seeded subject"},"tool_response":"{\"threadId\":\"release-smoke\",\"title\":\"⏳ Release smoke seeded subject\"}"}'
printf '%s\n' "$post" | HOME="$home" CODEX_HOME="$codex_home" "$binary" hook

pre='{"hook_event_name":"PreToolUse","session_id":"release-smoke","tool_name":"codex_appset_thread_title","tool_use_id":"release-smoke-final","tool_input":{"title":"🧵🐻 complete"}}'
prepared=$(printf '%s\n' "$pre" | HOME="$home" CODEX_HOME="$codex_home" "$binary" hook)
expected_title='✅ Release smoke seeded subject'
printf '%s\n' "$prepared" | grep -F '"permissionDecision":"allow"' >/dev/null
printf '%s\n' "$prepared" | grep -F "\"title\":\"$expected_title\"" >/dev/null

sqlite3 "$codex_home/state_1.sqlite" \
	"UPDATE threads SET title = '$expected_title' WHERE id = 'release-smoke';"
post='{"hook_event_name":"PostToolUse","session_id":"release-smoke","tool_name":"codex_appset_thread_title","tool_use_id":"release-smoke-final","tool_input":{"title":"✅ Release smoke seeded subject"},"tool_response":"{\"threadId\":\"release-smoke\",\"title\":\"✅ Release smoke seeded subject\"}"}'
printf '%s\n' "$post" | HOME="$home" CODEX_HOME="$codex_home" "$binary" hook

state=$home/.local/share/threadbear/native.json
grep -F '"release-smoke"' "$state" >/dev/null
grep -E '"subject"[[:space:]]*:[[:space:]]*"Release smoke seeded subject"' "$state" >/dev/null
grep -E '"last"[[:space:]]*:[[:space:]]*"✅ Release smoke seeded subject"' "$state" >/dev/null
if grep -F '"pending"' "$state" >/dev/null; then
	echo "release smoke title remained pending" >&2
	exit 1
fi
inventory=$(HOME="$home" CODEX_HOME="$codex_home" "$binary" inventory --json)
printf '%s\n' "$inventory" | grep -F '"applied":true' >/dev/null
printf '%s\n' "$inventory" | grep -F '"remaining":0' >/dev/null
HOME="$home" CODEX_HOME="$codex_home" "$binary" migration \
	--phase migration_complete --controller-task-id release-smoke-controller --json
status=$(HOME="$home" CODEX_HOME="$codex_home" "$binary" status --json)
printf '%s\n' "$status" | grep -F '"ready":true' >/dev/null
printf '%s\n' "$status" | grep -F '"phase":"migration_complete"' >/dev/null
sqlite3 "$codex_home/state_1.sqlite" "INSERT INTO threads VALUES ('release-smoke-owner',1,'Uninstall owner',NULL,0,'cli','','${rollout}','Uninstall owner','Uninstall owner');"

prepared=$(HOME="$home" CODEX_HOME="$codex_home" "$binary" uninstall \
	--prepare --initiator-task-id release-smoke-owner --json)
printf '%s\n' "$prepared" | grep -F '"prepared":true' >/dev/null
printf '%s\n' "$prepared" | grep -F '"main_task_id":"release-smoke-home"' >/dev/null
printf '%s\n' "$prepared" | grep -F '"main_archived":true' >/dev/null
sqlite3 "$codex_home/state_1.sqlite" "UPDATE threads SET archived=0 WHERE id='release-smoke-home';"

pre='{"hook_event_name":"PreToolUse","session_id":"release-smoke-owner","tool_name":"codex_appset_thread_title","tool_use_id":"release-smoke-cleanup","tool_input":{"threadId":"release-smoke","title":"🧵🐻 strip title icons"}}'
prepared=$(printf '%s\n' "$pre" | HOME="$home" CODEX_HOME="$codex_home" "$binary" hook)
expected_title='Release smoke seeded subject'
printf '%s\n' "$prepared" | grep -F '"permissionDecision":"allow"' >/dev/null
printf '%s\n' "$prepared" | grep -F "\"title\":\"$expected_title\"" >/dev/null
sqlite3 "$codex_home/state_1.sqlite" "UPDATE threads SET title = '$expected_title' WHERE id = 'release-smoke';"
post='{"hook_event_name":"PostToolUse","session_id":"release-smoke-owner","tool_name":"codex_appset_thread_title","tool_use_id":"release-smoke-cleanup","tool_input":{"threadId":"release-smoke","title":"Release smoke seeded subject"},"tool_response":"{\"threadId\":\"release-smoke\",\"title\":\"Release smoke seeded subject\"}"}'
printf '%s\n' "$post" | HOME="$home" CODEX_HOME="$codex_home" "$binary" hook
grep -E '"subject"[[:space:]]*:[[:space:]]*"Release smoke seeded subject"' "$state" >/dev/null
grep -E '"last"[[:space:]]*:[[:space:]]*"Release smoke seeded subject"' "$state" >/dev/null

pre='{"hook_event_name":"PreToolUse","session_id":"release-smoke-owner","tool_name":"codex_appset_thread_title","tool_use_id":"release-smoke-home-cleanup","tool_input":{"threadId":"release-smoke-home","title":"🧵🐻 strip title icons"}}'
prepared=$(printf '%s\n' "$pre" | HOME="$home" CODEX_HOME="$codex_home" "$binary" hook)
expected_title='Release smoke home'
printf '%s\n' "$prepared" | grep -F '"permissionDecision":"allow"' >/dev/null
printf '%s\n' "$prepared" | grep -F "\"title\":\"$expected_title\"" >/dev/null
sqlite3 "$codex_home/state_1.sqlite" "UPDATE threads SET title = '$expected_title' WHERE id = 'release-smoke-home';"
post='{"hook_event_name":"PostToolUse","session_id":"release-smoke-owner","tool_name":"codex_appset_thread_title","tool_use_id":"release-smoke-home-cleanup","tool_input":{"threadId":"release-smoke-home","title":"Release smoke home"},"tool_response":"{\"threadId\":\"release-smoke-home\",\"title\":\"Release smoke home\"}"}'
printf '%s\n' "$post" | HOME="$home" CODEX_HOME="$codex_home" "$binary" hook
sqlite3 "$codex_home/state_1.sqlite" "UPDATE threads SET archived=1 WHERE id='release-smoke-home';"
HOME="$home" CODEX_HOME="$codex_home" "$binary" uninstall --initiator-task-id release-smoke-owner --noninteractive --confirm --json
test ! -e "$binary"
test ! -d "$home/.local/share/threadbear"
test "$(sqlite3 "$codex_home/state_1.sqlite" "SELECT title || ':' || archived FROM threads WHERE id='release-smoke-home';")" = 'Release smoke home:1'
test "$(sqlite3 "$codex_home/state_1.sqlite" "SELECT title || ':' || archived FROM threads WHERE id='release-smoke-controller';")" = '⏳ Completed controller sentinel:1'
! grep -F 'THREADBEAR MANAGED BLOCK' "$codex_home/AGENTS.md"
! grep -F "threadbear' hook" "$codex_home/hooks.json"
