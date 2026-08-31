#!/bin/sh
set -eu

usage() {
    printf '%s\n' 'usage: tools/create-public-snapshot.sh DESTINATION' >&2
}

if [ "$#" -ne 1 ]; then
    usage
    exit 2
fi

script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
repo_root=$(CDPATH='' cd -- "$script_dir/.." && pwd)
destination=$1
source_ref=${SSHF_PUBLIC_SOURCE_REF:-dev}
public_name=${SSHF_PUBLIC_GIT_NAME:-ponomarev-prime}
public_email=${SSHF_PUBLIC_GIT_EMAIL:-ponomarev-prime@users.noreply.github.com}

case "$destination" in
    ''|/|.|..) printf '%s\n' 'public snapshot: unsafe destination' >&2; exit 2 ;;
esac
if [ -e "$destination" ]; then
    printf 'public snapshot: destination already exists: %s\n' "$destination" >&2
    exit 2
fi
if [ -n "$(git -C "$repo_root" status --porcelain --untracked-files=normal)" ]; then
    printf '%s\n' 'public snapshot: source worktree must be clean' >&2
    exit 1
fi

source_commit=$(git -C "$repo_root" rev-parse "$source_ref^{commit}")
head_commit=$(git -C "$repo_root" rev-parse HEAD)
if [ "$source_commit" != "$head_commit" ]; then
    printf 'public snapshot: %s is not current HEAD\n' "$source_ref" >&2
    exit 1
fi

"$repo_root/tools/public-audit.sh"
mkdir -p "$(dirname -- "$destination")"
mkdir "$destination"
git -C "$repo_root" archive --format=tar "$source_ref" | tar -xf - -C "$destination"

git -C "$destination" init -b dev >/dev/null
git -C "$destination" config user.name "$public_name"
git -C "$destination" config user.email "$public_email"
git -C "$destination" add -A

# git archive preserves files but not gitlink index entries. Restore the exact
# pinned submodule objects without importing any private parent history.
git -C "$repo_root" ls-tree -r "$source_ref" |
while read -r mode object_type object_id path; do
    if [ "$mode" = 160000 ] && [ "$object_type" = commit ]; then
        git -C "$destination" update-index --add --cacheinfo "$mode,$object_id,$path"
    fi
done

source_tree=$(git -C "$repo_root" rev-parse "$source_ref^{tree}")
snapshot_tree=$(git -C "$destination" write-tree)
if [ "$snapshot_tree" != "$source_tree" ]; then
    printf '%s\n' 'public snapshot: staged tree differs from verified source tree' >&2
    exit 1
fi

git -C "$destination" commit --no-gpg-sign -m 'Initial public source release' >/dev/null
git -C "$destination" branch main
"$destination/tools/public-audit.sh"

printf 'Public snapshot: %s\n' "$destination"
printf 'Source commit:   %s\n' "$source_commit"
printf 'Source tree:     %s\n' "$source_tree"
printf 'Public root:     %s\n' "$(git -C "$destination" rev-parse HEAD)"
printf '%s\n' 'No remote or tags were created.'
