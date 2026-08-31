#!/bin/sh
set -eu

if [ "$#" -ne 2 ]; then
    printf '%s\n' 'usage: release-preflight.sh vMAJOR.MINOR.PATCH EXPECTED_COMMIT' >&2
    exit 2
fi

version=$1
expected_commit=$2
printf '%s\n' "$version" | grep -Eq '^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$' || {
    printf 'release version must be stable SemVer vMAJOR.MINOR.PATCH: %s\n' "$version" >&2
    exit 1
}

head_commit=$(git rev-parse HEAD)
[ "$head_commit" = "$expected_commit" ] || {
    printf 'release commit mismatch: HEAD=%s expected=%s\n' "$head_commit" "$expected_commit" >&2
    exit 1
}

branch=${GITHUB_REF_NAME:-}
if [ -z "$branch" ]; then
    branch=$(git symbolic-ref --quiet --short HEAD 2>/dev/null || printf detached)
fi
[ "$branch" = main ] || {
    printf 'stable releases are allowed only from main, got: %s\n' "$branch" >&2
    exit 1
}

if [ -n "$(git status --porcelain --untracked-files=normal)" ]; then
    printf '%s\n' 'release worktree is not clean' >&2
    exit 1
fi

if git show-ref --verify --quiet "refs/remotes/origin/main"; then
    origin_main=$(git rev-parse refs/remotes/origin/main)
    [ "$origin_main" = "$head_commit" ] || {
        printf 'release commit is not current origin/main: %s != %s\n' "$head_commit" "$origin_main" >&2
        exit 1
    }
fi

if git show-ref --verify --quiet "refs/tags/$version"; then
    printf 'release tag already exists: %s\n' "$version" >&2
    exit 1
fi

latest=$(git tag --list 'v[0-9]*.[0-9]*.[0-9]*' | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' | sort -V | tail -n 1 || true)
if [ -n "$latest" ]; then
    highest=$(printf '%s\n%s\n' "$latest" "$version" | sort -V | tail -n 1)
    [ "$highest" = "$version" ] && [ "$latest" != "$version" ] || {
        printf 'release version must be greater than %s: %s\n' "$latest" "$version" >&2
        exit 1
    }
fi

printf 'release preflight: %s at %s on main\n' "$version" "$head_commit"
