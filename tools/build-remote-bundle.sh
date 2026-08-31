#!/bin/sh
set -eu

script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
repo_root=$(CDPATH='' cd -- "$script_dir/.." && pwd)
output=${1:-"$repo_root/.toolchain/remote/sshfleet-tools-linux-amd64.tar.gz"}

if [ "$(uname -s)" != Linux ] || [ "$(uname -m)" != x86_64 ]; then
    printf 'remote bundle build requires Linux x86_64\n' >&2
    exit 1
fi

for path in \
    "$repo_root/.toolchain/bin/lf" \
    "$repo_root/.toolchain/bin/dtop" \
    "$repo_root/.toolchain/bin/bat" \
    "$repo_root/.toolchain/remote-cache/nvim-linux-x86_64/bin/nvim"; do
    if [ ! -x "$path" ]; then
        printf 'missing companion binary: %s; run make toolchain-build\n' "$path" >&2
        exit 1
    fi
done

mkdir -p "$repo_root/.toolchain/tmp"
stage=$(mktemp -d "$repo_root/.toolchain/tmp/remote-bundle.XXXXXXXX")
trap 'rm -rf "$stage"' EXIT HUP INT TERM
mkdir -p "$stage/bin" "$stage/lib" "$stage/libexec" "$stage/config" "$(dirname -- "$output")"

install -m 0755 "$repo_root/tools/remote/run" "$stage/run"
install -m 0755 "$repo_root/tools/remote/shell" "$stage/shell"
for name in lf nvim dtop bat batcat; do
    install -m 0755 "$repo_root/tools/remote/bin/$name" "$stage/bin/$name"
done
for name in sshfleet-open sshfleet-editor; do
    install -m 0755 "$repo_root/tools/bin/$name" "$stage/bin/$name"
done
install -m 0755 "$repo_root/.toolchain/bin/lf" "$stage/libexec/lf"
install -m 0755 "$repo_root/.toolchain/bin/dtop" "$stage/libexec/dtop"
install -m 0755 "$repo_root/.toolchain/bin/bat" "$stage/libexec/bat"
cp -a "$repo_root/.toolchain/remote-cache/nvim-linux-x86_64" "$stage/nvim"
cp -a "$repo_root/tools/config/." "$stage/config/"
install -m 0644 "$repo_root/tools/manifest.toml" "$stage/manifest.toml"

dependencies() {
    ldd "$1" 2>/dev/null | awk '
        /=> \/[^ ]+/ { print $3 }
        /^[[:space:]]*\/[^ ]*ld-linux[^ ]*/ { print $1 }
    '
}

for binary in "$repo_root/.toolchain/bin/dtop" "$repo_root/.toolchain/bin/bat"; do
    dependencies "$binary"
done | sort -u | while IFS= read -r dependency; do
    [ -n "$dependency" ] || continue
    install -m 0755 "$dependency" "$stage/lib/$(basename -- "$dependency")"
done

loader=$(find "$stage/lib" -maxdepth 1 -type f -name '*ld-linux*x86-64*.so*' | head -n 1)
if [ -z "$loader" ]; then
    printf 'cannot locate bundled dynamic loader\n' >&2
    exit 1
fi
if [ "$(basename -- "$loader")" != ld-linux-x86-64.so.2 ]; then
    cp "$loader" "$stage/lib/ld-linux-x86-64.so.2"
fi

archive_tmp="$output.tmp"
tar --sort=name --mtime='UTC 1970-01-01' --owner=0 --group=0 --numeric-owner \
    -C "$stage" -czf "$archive_tmp" .
mv "$archive_tmp" "$output"
sha256sum "$output" >"$output.sha256"
printf '%s\n' "$output"
