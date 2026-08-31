#!/bin/sh
set -eu

script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
repo_root=$(CDPATH='' cd -- "$script_dir/.." && pwd)
test_root=$(mktemp -d)
trap 'rm -rf -- "$test_root"' EXIT HUP INT TERM
destination=$test_root/public

SSHF_PUBLIC_GIT_NAME=fixture-owner \
SSHF_PUBLIC_GIT_EMAIL=fixture-owner@users.noreply.github.com \
    "$repo_root/tools/create-public-snapshot.sh" "$destination" >/dev/null

test "$(git -C "$destination" rev-list --count --all)" = 1
test "$(git -C "$destination" branch --show-current)" = dev
test "$(git -C "$destination" rev-parse dev)" = "$(git -C "$destination" rev-parse main)"
test "$(git -C "$destination" rev-parse HEAD^{tree})" = "$(git -C "$repo_root" rev-parse HEAD^{tree})"
test -z "$(git -C "$destination" status --short)"
test -z "$(git -C "$destination" tag -l)"
test -z "$(git -C "$destination" remote)"

for path in tools/src/bat tools/src/dtop tools/src/lf tools/src/nvim; do
    test "$(git -C "$destination" ls-tree HEAD "$path")" = "$(git -C "$repo_root" ls-tree HEAD "$path")"
done

# A normal core clone has gitlinks but no initialized optional tool sources.
# The license gate must still validate the core module graph successfully.
make -C "$destination" test-licenses >/dev/null

printf '%s\n' 'clean public snapshot: one root commit, exact tree and gitlinks ok'
