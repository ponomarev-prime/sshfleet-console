#!/bin/sh
set -eu

script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
repo_root=$(CDPATH='' cd -- "$script_dir/.." && pwd)
bundle=${1:-"$repo_root/.toolchain/remote/sshfleet-tools-linux-amd64.tar.gz"}

if [ ! -f "$bundle" ]; then
    printf 'missing remote bundle: %s\n' "$bundle" >&2
    exit 1
fi

docker run --rm --network none -v "$bundle:/bundle.tar.gz:ro" ubuntu:22.04 \
    sh -c 'set -eu; root=$(mktemp -d); tar -xzf /bundle.tar.gz -C "$root"; "$root/run" self-test keep; script -qec "$root/bin/nvim +q" /dev/null; rm -rf "$root"'
