#!/bin/sh
set -eu

if [ "$#" -ne 1 ]; then
    printf '%s\n' 'usage: build-sshf.sh OUTPUT' >&2
    exit 2
fi

output=$1
script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
repo_root=$(CDPATH='' cd -- "$script_dir/.." && pwd)

commit=${SSHF_BUILD_COMMIT:-}
if [ -z "$commit" ]; then
    commit=$(git -C "$repo_root" rev-parse HEAD 2>/dev/null || printf unknown)
fi
short_commit=$(printf '%s' "$commit" | cut -c1-12)

branch=${SSHF_BUILD_BRANCH:-${GITHUB_REF_NAME:-}}
if [ -z "$branch" ]; then
    branch=$(git -C "$repo_root" symbolic-ref --quiet --short HEAD 2>/dev/null || printf detached)
fi
branch=$(printf '%s' "$branch" | sed 's#[^A-Za-z0-9._-]#-#g; s/^-*//; s/-*$//')
[ -n "$branch" ] || branch=unknown

dirty=${SSHF_BUILD_DIRTY:-}
if [ -z "$dirty" ]; then
    if [ -n "$(git -C "$repo_root" status --porcelain --untracked-files=normal 2>/dev/null || true)" ]; then
        dirty=true
    else
        dirty=false
    fi
fi
case "$dirty" in true|false) ;; *) printf '%s\n' 'SSHF_BUILD_DIRTY must be true or false' >&2; exit 2 ;; esac

version=${SSHF_BUILD_VERSION:-}
if [ -z "$version" ]; then
    version=$branch-$short_commit
    if [ "$dirty" = true ]; then
        version=$version+dirty
    fi
fi
channel=${SSHF_BUILD_CHANNEL:-development}
source_date=${SSHF_BUILD_DATE:-}
if [ -z "$source_date" ]; then
    source_date=$(git -C "$repo_root" show -s --format=%cI "$commit" 2>/dev/null || printf unknown)
fi

for value in "$version" "$channel" "$branch" "$commit" "$source_date"; do
    case "$value" in
        ''|*[!A-Za-z0-9._:+@/-]*) printf 'unsafe build metadata: %s\n' "$value" >&2; exit 2 ;;
    esac
done

mkdir -p "$(dirname -- "$output")"
ldflags="-s -w -X main.version=$version -X main.buildChannel=$channel -X main.buildBranch=$branch -X main.buildCommit=$commit -X main.buildDate=$source_date -X main.buildDirty=$dirty"
cd "$repo_root"
CGO_ENABLED=${CGO_ENABLED:-0} go build -buildvcs=false -trimpath -ldflags "$ldflags" -o "$output" ./cmd/sshf
