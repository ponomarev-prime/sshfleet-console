#!/bin/sh
set -eu

script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
repo_root=$(CDPATH='' cd -- "$script_dir/.." && pwd)
mkdir -p "$repo_root/.tmp"
test_root=$(mktemp -d "$repo_root/.tmp/installed-launcher.XXXXXX")
trap 'rm -rf -- "$test_root"' EXIT HUP INT TERM

mkdir -p "$test_root/bin" "$test_root/home" "$test_root/screenshots"
ln -s "$repo_root/tools/launchers/sshfleet" "$test_root/bin/sshfleet"

expected=$("$repo_root/bin/sshfleet" --version)
actual=$(HOME="$test_root/home" PATH="$test_root/bin:/usr/bin:/bin" sshfleet --version)
if [ "$actual" != "$expected" ]; then
    printf 'installed launcher is stale:\nexpected: %s\nactual:   %s\n' "$expected" "$actual" >&2
    exit 1
fi

resolved=$(HOME="$test_root/home" PATH="$test_root/bin:/usr/bin:/bin" command -v sshfleet)
if [ "$resolved" != "$test_root/bin/sshfleet" ]; then
    printf 'installed launcher resolves to unexpected path: %s\n' "$resolved" >&2
    exit 1
fi

cd "$repo_root"
SSHF_E2E_BINARY="$test_root/bin/sshfleet" \
SSHF_SCREENSHOT_DIR="${SSHF_SCREENSHOT_DIR:-$test_root/screenshots}" \
go test -v ./cmd/sshf -run 'TestTUIEndToEndScreenshotsAndActionMenuTraversal/medium' -count=1

printf '%s\n' 'installed sshfleet launcher PTY: version and terminal tabs ok'
