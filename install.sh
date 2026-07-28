#!/bin/sh
set -eu

umask 077

release_base=${THREADBEAR_RELEASE_BASE_URL:-https://github.com/ericlitman/threadbear/releases}
selected_version=
noninteractive=false
previous=
for argument in "$@"; do
	if [ "$previous" = version ]; then
		selected_version=$argument
		previous=
		continue
	fi
	case "$argument" in
		--noninteractive) noninteractive=true ;;
		--version) previous=version ;;
		--version=*) selected_version=${argument#--version=} ;;
	esac
done
if [ "$previous" = version ]; then
	echo "threadbear: --version requires a value" >&2
	exit 2
fi
if [ "$noninteractive" = false ] && { [ -t 0 ] || [ -t 1 ] || [ -t 2 ]; }; then
	echo "threadbear: terminal installation is deprecated; open a Codex task and follow https://threadbear.sh/install instead." >&2
fi
validate_version() {
	printf '%s\n' "$1" | awk '/^[0-9]+\.[0-9]+\.[0-9]+$/ { valid=1 } END { exit(valid ? 0 : 1) }'
}

requested_version=$selected_version
load_manifest() {
	if ! manifest=$(curl -fsSL "$manifest_url" 2>/dev/null); then
		if [ -n "$requested_version" ]; then
			echo "threadbear: version $requested_version is not published." >&2
			echo "threadbear: published versions are listed at https://github.com/ericlitman/threadbear/releases" >&2
		else
			echo "threadbear: no published release yet, so there is nothing to install." >&2
			echo "threadbear: follow https://github.com/ericlitman/threadbear for release status." >&2
		fi
		exit 1
	fi
	manifest_version=$(printf '%s\n' "$manifest" | sed -n 's/.*"version"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | sed -n '1p')
	if [ -z "$manifest_version" ]; then
		echo "threadbear: release manifest has no version" >&2
		exit 1
	fi
	if ! validate_version "$manifest_version"; then
		echo "threadbear: release manifest version must be exact N.N.N" >&2
		exit 1
	fi
	if [ -n "$requested_version" ] && [ "$manifest_version" != "$requested_version" ]; then
		echo "threadbear: release manifest version mismatch" >&2
		exit 1
	fi
	selected_version=$manifest_version
}
if [ -n "$requested_version" ]; then
	if ! validate_version "$requested_version"; then
		echo "threadbear: version must be exact N.N.N without a leading v" >&2
		exit 2
	fi
	manifest_url="$release_base/download/v$requested_version/latest.json"
else
	manifest_url="$release_base/latest/download/latest.json"
	load_manifest
fi

case $(uname -s) in
	Darwin) platform=darwin ;;
	*) echo "threadbear: only Darwin is supported" >&2; exit 1 ;;
esac
case $(uname -m) in
	arm64) architecture=arm64 ;;
	x86_64|amd64) architecture=amd64 ;;
	*) echo "threadbear: unsupported architecture" >&2; exit 1 ;;
esac
asset_key="${platform}_${architecture}"
if [ -n "$requested_version" ]; then
	load_manifest
fi

manifest_asset() {
	field=$1
	printf '%s\n' "$manifest" | awk -v asset_key="$asset_key" -v field="$field" '
		index($0, "\"" asset_key "\"") { selected=1; next }
		selected && index($0, "\"" field "\"") {
			value=$0
			sub(/^[^:]*:[[:space:]]*"/, "", value)
			sub(/".*/, "", value)
			print value
			exit
		}
		selected && index($0, "}") { exit }
	'
}
binary_url=$(manifest_asset url)
checksum_url=$(manifest_asset sha256_url)
if [ -z "$binary_url" ] || [ -z "$checksum_url" ]; then
	echo "threadbear: release manifest has no $asset_key asset" >&2
	exit 1
fi

temporary_directory=$(mktemp -d "${TMPDIR:-/tmp}/threadbear.XXXXXX")
candidate=$temporary_directory/threadbear
cleanup() {
	rm -rf "$temporary_directory"
}
trap cleanup EXIT HUP INT TERM

curl -fsSL "$binary_url" -o "$candidate"
curl -fsSL "$checksum_url" -o "$candidate.sha256"
expected=$(sed -n 's/^\([0-9A-Fa-f][0-9A-Fa-f]*\).*/\1/p' "$candidate.sha256" | sed -n '1p' | tr 'A-F' 'a-f')
if [ -z "$expected" ] || [ "${#expected}" -ne 64 ]; then
	echo "threadbear: release checksum is missing" >&2
	exit 1
fi
actual=$(shasum -a 256 "$candidate" | sed 's/[[:space:]].*//')
if [ "$actual" != "$expected" ]; then
	echo "threadbear: release checksum mismatch" >&2
	exit 1
fi
chmod 700 "$candidate"
if ! selftest_output=$("$candidate" self-test --candidate 2>&1); then
	printf '%s\n' "$selftest_output" >&2
	echo "threadbear: the downloaded candidate failed its self-test; nothing was installed." >&2
	echo "threadbear: the check named above is the reason. If it mentions installed_state, a previous install may have left partial state in ~/.local/share/threadbear." >&2
	exit 1
fi
embedded=$("$candidate" version --json | sed -n 's/.*"installed_version":"\([^"]*\)".*/\1/p')
if [ "$embedded" != "$selected_version" ]; then
	echo "threadbear: candidate version mismatch" >&2
	exit 1
fi
"$candidate" install "$@"
