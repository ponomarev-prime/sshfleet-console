#!/bin/sh
set -eu

script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
repo_root=$(CDPATH='' cd -- "$script_dir/.." && pwd)
test_root=$(mktemp -d)
trap 'rm -rf -- "$test_root"' EXIT HUP INT TERM

# Recreate the public core archive shape: one application binary, the canonical
# shell-neutral launcher, and a transitional sshf launcher. There is no
# compatibility sshf-askpass binary.
package_root="$test_root/package"
mkdir -p "$package_root/bin" "$package_root/tools/bin"
cp "$repo_root/install.sh" "$package_root/install.sh"
cp "$repo_root/bin/sshfleet" "$package_root/bin/sshfleet"
cp "$repo_root/tools/launchers/sshfleet" "$package_root/tools/bin/sshfleet"
cp "$repo_root/tools/launchers/sshf" "$package_root/tools/bin/sshf"
chmod 0755 "$package_root/install.sh" "$package_root/bin/sshfleet" \
    "$package_root/tools/bin/sshfleet" "$package_root/tools/bin/sshf"

HOME="$test_root/home" "$package_root/install.sh" --prebuilt \
    --prefix "$test_root/app" --bin-dir "$test_root/bin" >"$test_root/install.log"
first_version=$(readlink "$test_root/app/current")

HOME="$test_root/home" "$package_root/install.sh" --prebuilt \
    --prefix "$test_root/app" --bin-dir "$test_root/bin" >>"$test_root/install.log"
second_version=$(readlink "$test_root/app/current")

test "$first_version" != "$second_version"
test -d "$test_root/app/$first_version"
test -d "$test_root/app/$second_version"

test -L "$test_root/app/current"
test -x "$test_root/app/current/bin/sshfleet"
test ! -e "$test_root/app/current/bin/sshf-askpass"
test -x "$test_root/app/current/tools/bin/sshfleet"
test -x "$test_root/app/current/tools/bin/sshf"
test -L "$test_root/bin/sshfleet"
test -L "$test_root/bin/sshf"
test -L "$test_root/bin/sf"
test ! -e "$test_root/app/current/tools/bin/lf"
test ! -e "$test_root/app/current/.toolchain/remote/sshfleet-tools-linux-amd64.tar.gz"

version_output=$(HOME="$test_root/home" PATH="$test_root/bin:$PATH" sshfleet -version)
case "$version_output" in
    'sshfleet '*) ;;
    *) printf 'unexpected installed launcher output: %s\n' "$version_output" >&2; exit 1 ;;
esac

HOME="$test_root/home" PATH="$test_root/bin:$PATH" \
    sshfleet healthcheck --config /dev/null >"$test_root/healthcheck.txt"
grep -E '^\[OK\][[:space:]]+credentials/askpass[[:space:]]+built-in .*self-contained second process$' \
    "$test_root/healthcheck.txt" >/dev/null

test "$(HOME="$test_root/home" PATH="$test_root/bin:$PATH" sshf --version)" = "$version_output"
test "$(HOME="$test_root/home" PATH="$test_root/bin:$PATH" sf --version)" = "$version_output"

mkdir -p "$test_root/collision-bin"
printf '%s\n' 'third-party sf' > "$test_root/collision-bin/sf"
chmod 0755 "$test_root/collision-bin/sf"
HOME="$test_root/home" "$package_root/install.sh" --prebuilt \
    --prefix "$test_root/collision-app" --bin-dir "$test_root/collision-bin" \
    >"$test_root/collision-install.log" 2>&1
test "$(cat "$test_root/collision-bin/sf")" = 'third-party sf'
test -L "$test_root/collision-bin/sshfleet"
grep -F 'sf already exists; alias was not changed' "$test_root/collision-install.log" >/dev/null

if "$repo_root/install.sh" --not-an-option >"$test_root/invalid.log" 2>&1; then
    printf '%s\n' 'installer accepted an unknown option' >&2
    exit 1
fi

if "$repo_root/install.sh" --prebuilt --full >"$test_root/invalid-full.log" 2>&1; then
    printf '%s\n' 'installer accepted incompatible --prebuilt --full modes' >&2
    exit 1
fi

printf '%s\n' 'core installer: ok'
