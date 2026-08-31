#!/bin/sh
set -eu

script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
repo_root=$(CDPATH='' cd -- "$script_dir/.." && pwd)
version=v0.12.5
archive_name=nvim-linux-x86_64.tar.gz
expected=bce0f56eda1f1b1db6eee8f4133d7a38813ea07933837dd1777411ca384c6875
cache_dir="$repo_root/.toolchain/remote-cache"
archive="$cache_dir/$archive_name"
install_dir="$cache_dir/nvim-linux-x86_64"
url="https://github.com/neovim/neovim/releases/download/$version/$archive_name"

if [ -x "$install_dir/bin/nvim" ]; then
    exit 0
fi

mkdir -p "$cache_dir"
if [ ! -f "$archive" ]; then
    curl --fail --location --proto '=https' --tlsv1.2 --output "$archive.tmp" "$url"
    mv "$archive.tmp" "$archive"
fi

actual=$(sha256sum "$archive" | awk '{ print $1 }')
if [ "$actual" != "$expected" ]; then
    printf 'Neovim archive checksum mismatch: got %s, want %s\n' "$actual" "$expected" >&2
    exit 1
fi

mkdir -p "$repo_root/.toolchain/tmp"
extract=$(mktemp -d "$repo_root/.toolchain/tmp/remote-nvim.XXXXXXXX")
trap 'rm -rf "$extract"' EXIT HUP INT TERM
tar -xzf "$archive" -C "$extract"
test -x "$extract/nvim-linux-x86_64/bin/nvim"
rm -rf "$install_dir"
mv "$extract/nvim-linux-x86_64" "$install_dir"
