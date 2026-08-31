#!/bin/sh
set -eu

script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
repo_root=$(CDPATH='' cd -- "$script_dir/.." && pwd)
mkdir -p "$repo_root/.tmp"
test_root=$(mktemp -d "$repo_root/.tmp/full-install.XXXXXX")
trap 'rm -rf -- "$test_root"' EXIT HUP INT TERM

"$repo_root/install.sh" --full \
    --prefix "$test_root/app" --bin-dir "$test_root/bin" >"$test_root/install.log"

for tool in lf dtop nvim bat; do
    test -x "$test_root/app/current/.toolchain/bin/$tool"
    test -x "$test_root/app/current/tools/bin/$tool"
done
test -f "$test_root/app/current/.toolchain/remote/sshfleet-tools-linux-amd64.tar.gz"
test -f "$test_root/app/current/.toolchain/remote/sshfleet-tools-linux-amd64.tar.gz.sha256"

PATH="$test_root/bin:$PATH" \
    sshfleet healthcheck --config /dev/null >"$test_root/healthcheck.txt"
for tool in nvim lf dtop bat; do
    grep -E "^\[OK\][[:space:]]+optional/${tool}[[:space:]]+sshfleet[[:space:]]" "$test_root/healthcheck.txt" >/dev/null
done

printf '%s\n' 'full installer: ok'
