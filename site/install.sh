#!/bin/sh
set -eu

umask 077

release_base=${THREADBEAR_RELEASE_BASE_URL:-https://threadbear.dev/releases}
selected_version=
previous=
for argument in "$@"; do
	if [ "$previous" = version ]; then
		selected_version=$argument
		previous=
		continue
	fi
	case "$argument" in
		--version) previous=version ;;
		--version=*) selected_version=${argument#--version=} ;;
	esac
done
if [ "$previous" = version ]; then
	echo "threadbear: --version requires a value" >&2
	exit 2
fi
validate_version() {
	printf '%s\n' "$1" | awk '/^[0-9]+\.[0-9]+\.[0-9]+$/ { valid=1 } END { exit(valid ? 0 : 1) }'
}

if [ -n "$selected_version" ]; then
	if ! validate_version "$selected_version"; then
		echo "threadbear: version must be exact N.N.N without a leading v" >&2
		exit 2
	fi
else
	manifest=$(curl -fsSL "$release_base/latest.json")
	selected_version=$(printf '%s\n' "$manifest" | sed -n 's/.*"version"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | sed -n '1p')
	if [ -z "$selected_version" ]; then
		echo "threadbear: latest release manifest has no version" >&2
		exit 1
	fi
	if ! validate_version "$selected_version"; then
		echo "threadbear: latest release manifest version must be exact N.N.N" >&2
		exit 1
	fi
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

temporary_directory=$(mktemp -d "${TMPDIR:-/tmp}/threadbear.XXXXXX")
candidate=$temporary_directory/threadbear
cleanup() {
	rm -rf "$temporary_directory"
}
trap cleanup EXIT HUP INT TERM

asset="threadbear_${platform}_${architecture}"
curl -fsSL "$release_base/$selected_version/$asset" -o "$candidate"
curl -fsSL "$release_base/$selected_version/$asset.sha256" -o "$candidate.sha256"
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
"$candidate" self-test --candidate >/dev/null
embedded=$("$candidate" version --json | sed -n 's/.*"installed_version":"\([^"]*\)".*/\1/p')
if [ "$embedded" != "$selected_version" ]; then
	echo "threadbear: candidate version mismatch" >&2
	exit 1
fi
"$candidate" install "$@"
