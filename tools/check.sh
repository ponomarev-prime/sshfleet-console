#!/bin/sh
set -eu

script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
repo_root=$(CDPATH='' cd -- "$script_dir/.." && pwd)
manifest="$script_dir/manifest.toml"
built=false

if [ "${1:-}" = "--built" ]; then
    built=true
fi

manifest_value() {
    section=$1
    key=$2
    awk -v section="[tools.$section]" -v key="$key" '
        $0 == section { active = 1; next }
        /^\[/ { active = 0 }
        active && $1 == key { value = $3; gsub(/^"|"$/, "", value); print value; exit }
    ' "$manifest"
}

for name in lf dtop nvim bat; do
    source_path=$(manifest_value "$name" source)
    expected_revision=$(manifest_value "$name" revision)
    expected_url=$(manifest_value "$name" url)
    launcher=$(manifest_value "$name" launcher)

    test -n "$source_path"
    test -n "$expected_revision"
    test -n "$expected_url"
    test -x "$repo_root/$launcher"
    test -e "$repo_root/tools/config/$name"

    actual_url=$(git -C "$repo_root" config -f .gitmodules --get "submodule.$source_path.url")
    test "$actual_url" = "$expected_url"

    if [ ! -e "$repo_root/$source_path/.git" ]; then
        printf '%s\n' "$source_path is not initialized; run: make toolchain-sync" >&2
        exit 1
    fi

    actual_revision=$(git -C "$repo_root/$source_path" rev-parse HEAD)
    if [ "$actual_revision" != "$expected_revision" ]; then
        printf '%s: expected %s, got %s\n' "$name" "$expected_revision" "$actual_revision" >&2
        exit 1
    fi
done

test -x "$repo_root/tools/launchers/batcat"
test -x "$repo_root/tools/launchers/sshfleet-preview"
test -x "$repo_root/tools/launchers/sshfleet-open"
test -x "$repo_root/tools/launchers/sshfleet-editor"
test -x "$repo_root/tools/launchers/sshfleet"
test -x "$repo_root/tools/launchers/sshf"

if [ "$built" = true ]; then
    test -x "$repo_root/.toolchain/bin/lf"
    test -x "$repo_root/.toolchain/bin/dtop"
    test -x "$repo_root/.toolchain/bin/nvim"
    test -x "$repo_root/.toolchain/bin/bat"
    test -x "$repo_root/.toolchain/bin/batcat"

    "$repo_root/.toolchain/bin/lf" -version
    "$repo_root/.toolchain/bin/dtop" --version
    "$repo_root/.toolchain/bin/nvim" --version | sed -n '1p'
    "$repo_root/.toolchain/bin/bat" --version
fi

printf '%s\n' "toolchain check: ok"
