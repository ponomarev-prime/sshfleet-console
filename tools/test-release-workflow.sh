#!/bin/sh
set -eu

script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
repo_root=$(CDPATH='' cd -- "$script_dir/.." && pwd)
workflow=$repo_root/.github/workflows/release.yml

fail() {
    printf 'release workflow contract: %s\n' "$1" >&2
    exit 1
}

require_line() {
    grep -F -- "$1" "$workflow" >/dev/null || fail "missing: $1"
}

test -f "$workflow" || fail "missing workflow"

# Verification owns no write token and must produce an immutable candidate from
# the exact commit that passed the complete gate.
require_line "permissions:"
require_line "contents: read"
# shellcheck disable=SC2016 # literal workflow expression
require_line 'tools/release-preflight.sh "$RELEASE_VERSION" "$GITHUB_SHA"'
require_line "run: tools/prepare-ci-podman.sh"
require_line "run: make regression"
require_line "run: make audit-public"
# shellcheck disable=SC2016 # literal workflow expression
require_line 'SSHF_RELEASE_EXPECTED_SHA="$GITHUB_SHA" tools/build-release.sh "$RELEASE_VERSION" linux amd64 dist'
# shellcheck disable=SC2016 # literal workflow expression
require_line 'SSHF_RELEASE_EXPECTED_SHA="$GITHUB_SHA" tools/build-release.sh "$RELEASE_VERSION" linux arm64 dist'
require_line "sha256sum -c checksums.txt"
require_line "full-regression-evidence-"

# Publication is a separate protected job. It can only consume the verified
# bytes and must revalidate main/tag state before creating a release.
require_line "needs: verify"
require_line "environment: release"
require_line "contents: write"
# shellcheck disable=SC2016 # literal workflow expression
require_line 'test "$current_main" = "$GITHUB_SHA"'
# shellcheck disable=SC2016 # literal workflow expression
require_line 'git/ref/tags/$RELEASE_VERSION'
require_line 'candidate/dist/sshfleet-console-*.tar.gz'
# shellcheck disable=SC2016 # literal workflow expression
require_line '--target "$GITHUB_SHA"'
require_line "--draft"
# shellcheck disable=SC2016 # literal workflow expression
require_line 'gh release edit "$RELEASE_VERSION" --draft=false'

printf '%s\n' 'release workflow contract: ok'
