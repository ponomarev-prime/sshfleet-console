#!/bin/sh
set -eu

script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
repo_root=$(CDPATH='' cd -- "$script_dir/.." && pwd)
if [ -z "${TMPDIR:-}" ]; then
    TMPDIR=$repo_root/.tmp/versioning
    export TMPDIR
fi
mkdir -p "$TMPDIR"
test_root=$(mktemp -d)
trap 'rm -rf -- "$test_root"' EXIT HUP INT TERM

fail() {
    printf 'versioning: %s\n' "$1" >&2
    exit 1
}

expect_failure() {
    name=$1
    shift
    if "$@" >"$test_root/$name.log" 2>&1; then
        fail "$name unexpectedly succeeded"
    fi
}

require_json() {
    json=$1
    field=$2
    value=$3
    printf '%s\n' "$json" | grep -F '"'"$field"'": '"$value" >/dev/null || \
        fail "JSON field $field did not contain $value"
}

preflight() {
    candidate=$1
    expected=$2
    branch=$3
    (cd "$gate_repo" && GITHUB_REF_NAME=$branch "$repo_root/tools/release-preflight.sh" "$candidate" "$expected")
}

printf '%s\n' 'versioning: binary provenance'
"$repo_root/tools/build-sshf.sh" "$repo_root/bin/sshfleet"
commit=$(git -C "$repo_root" rev-parse HEAD)
short_commit=$(printf '%s' "$commit" | cut -c1-12)
source_date=$(git -C "$repo_root" show -s --format=%cI "$commit")
version_json=$("$repo_root/bin/sshfleet" version --json)
require_json "$version_json" commit '"'"$commit"'"'
require_json "$version_json" source_date '"'"$source_date"'"'
printf '%s\n' "$version_json" | grep -E '"source_state": "(clean|dirty)"' >/dev/null || fail "current source state missing"
printf '%s\n' "$version_json" | grep -E '"dirty": (true|false)' >/dev/null || fail "current dirty boolean missing"
"$repo_root/bin/sshfleet" --version | grep -E '^sshfleet [A-Za-z0-9._+-]+ \(' >/dev/null || fail "one-line version is malformed"
"$repo_root/bin/sshfleet" version | grep -F "commit:      $commit" >/dev/null || fail "human version omits full commit"
expect_failure version-positional "$repo_root/bin/sshfleet" version unexpected
expect_failure version-unknown-flag "$repo_root/bin/sshfleet" version --unknown

printf '%s\n' 'versioning: injected build metadata'
stable_binary=$test_root/sshf-stable
SSHF_BUILD_VERSION=v0.1.0 \
SSHF_BUILD_CHANNEL=stable \
SSHF_BUILD_BRANCH=main \
SSHF_BUILD_COMMIT="$commit" \
SSHF_BUILD_DATE="$source_date" \
SSHF_BUILD_DIRTY=false \
    "$repo_root/tools/build-sshf.sh" "$stable_binary"
stable_json=$("$stable_binary" version --json)
require_json "$stable_json" version '"v0.1.0"'
require_json "$stable_json" channel '"stable"'
require_json "$stable_json" branch '"main"'
require_json "$stable_json" commit '"'"$commit"'"'
require_json "$stable_json" source_date '"'"$source_date"'"'
require_json "$stable_json" dirty false
require_json "$stable_json" source_state '"clean"'

dirty_binary=$test_root/sshf-dirty
SSHF_BUILD_VERSION=dev-fixture+dirty \
SSHF_BUILD_CHANNEL=development \
SSHF_BUILD_BRANCH=dev \
SSHF_BUILD_COMMIT="$commit" \
SSHF_BUILD_DATE="$source_date" \
SSHF_BUILD_DIRTY=true \
    "$repo_root/tools/build-sshf.sh" "$dirty_binary"
dirty_json=$("$dirty_binary" version --json)
require_json "$dirty_json" dirty true
require_json "$dirty_json" source_state '"dirty"'

sanitized_binary=$test_root/sshf-sanitized-branch
SSHF_BUILD_BRANCH='feature/version test' \
SSHF_BUILD_COMMIT="$commit" \
SSHF_BUILD_DATE="$source_date" \
SSHF_BUILD_DIRTY=false \
    "$repo_root/tools/build-sshf.sh" "$sanitized_binary"
sanitized_json=$("$sanitized_binary" version --json)
require_json "$sanitized_json" branch '"feature-version-test"'
require_json "$sanitized_json" version '"feature-version-test-'"$short_commit"'"'

expect_failure build-arity "$repo_root/tools/build-sshf.sh"
expect_failure invalid-dirty env SSHF_BUILD_DIRTY=maybe "$repo_root/tools/build-sshf.sh" "$test_root/invalid-dirty"
expect_failure unsafe-version env SSHF_BUILD_VERSION='v1.0.0 bad' "$repo_root/tools/build-sshf.sh" "$test_root/unsafe-version"
expect_failure unsafe-channel env SSHF_BUILD_CHANNEL='stable bad' "$repo_root/tools/build-sshf.sh" "$test_root/unsafe-channel"
expect_failure unsafe-commit env SSHF_BUILD_COMMIT='commit bad' "$repo_root/tools/build-sshf.sh" "$test_root/unsafe-commit"
expect_failure unsafe-date env SSHF_BUILD_DATE='2026-08-30 bad' "$repo_root/tools/build-sshf.sh" "$test_root/unsafe-date"

printf '%s\n' 'versioning: stable release preflight'
gate_repo=$test_root/gate
git init -q -b main "$gate_repo"
git -C "$gate_repo" config user.name 'SSH Fleet Console Tests'
git -C "$gate_repo" config user.email 'sshfleet-tests@example.invalid'
printf '%s\n' fixture > "$gate_repo/file"
git -C "$gate_repo" add file
git -C "$gate_repo" commit -q -m initial
first_commit=$(git -C "$gate_repo" rev-parse HEAD)

for invalid in 0.1.0 v0.1 v0.1.0-rc1 v0.1.0+build v01.1.0 v0.01.0 v0.1.00; do
    expect_failure "semver-$(printf '%s' "$invalid" | tr -c 'A-Za-z0-9' '-')" preflight "$invalid" "$first_commit" main
done
expect_failure commit-mismatch preflight v0.1.0 0000000000000000000000000000000000000000 main
expect_failure wrong-branch preflight v0.1.0 "$first_commit" dev
preflight v0.1.0 "$first_commit" main >/dev/null

printf '%s\n' second >> "$gate_repo/file"
git -C "$gate_repo" add file
git -C "$gate_repo" commit -q -m second
gate_commit=$(git -C "$gate_repo" rev-parse HEAD)
git -C "$gate_repo" update-ref refs/remotes/origin/main "$first_commit"
expect_failure stale-origin-main preflight v0.1.0 "$gate_commit" main
git -C "$gate_repo" update-ref refs/remotes/origin/main "$gate_commit"
preflight v0.1.0 "$gate_commit" main >/dev/null

git -C "$gate_repo" tag v0.1.0
expect_failure duplicate-tag preflight v0.1.0 "$gate_commit" main
expect_failure version-rollback preflight v0.0.9 "$gate_commit" main
preflight v0.1.1 "$gate_commit" main >/dev/null

git -C "$gate_repo" tag v0.10.0
git -C "$gate_repo" tag v99.0.0-rc1
expect_failure numeric-version-rollback preflight v0.9.99 "$gate_commit" main
preflight v0.10.1 "$gate_commit" main >/dev/null

printf '%s\n' modified > "$gate_repo/file"
expect_failure dirty-tracked preflight v0.10.1 "$gate_commit" main
git -C "$gate_repo" restore file
printf '%s\n' untracked > "$gate_repo/untracked"
expect_failure dirty-untracked preflight v0.10.1 "$gate_commit" main
rm -f "$gate_repo/untracked"
preflight v0.10.1 "$gate_commit" main >/dev/null

printf '%s\n' 'versioning: reproducible release archives'
version_id=$("$repo_root/bin/sshfleet" --version | awk '{print $2}')
native_arch=$(go env GOARCH)
case "$native_arch" in
    amd64|arm64) ;;
    *) fail "unsupported test architecture: $native_arch" ;;
esac
mkdir -p "$test_root/dist" "$test_root/dist-second" "$test_root/unpack"
"$repo_root/tools/build-release.sh" "$version_id" linux "$native_arch" "$test_root/dist" >/dev/null
archive_name=sshfleet-console-$version_id-linux-$native_arch
archive=$test_root/dist/$archive_name.tar.gz
"$repo_root/tools/build-release.sh" "$version_id" linux "$native_arch" "$test_root/dist-second" >/dev/null
second_archive=$test_root/dist-second/$archive_name.tar.gz
cmp "$archive" "$second_archive" >/dev/null || fail "same source produced different archives"

expect_failure unsafe-release-name "$repo_root/tools/build-release.sh" 'bad/name' linux amd64 "$test_root/rejected"
expect_failure unsupported-release-os "$repo_root/tools/build-release.sh" "$version_id" darwin amd64 "$test_root/rejected"
expect_failure unsupported-release-arch "$repo_root/tools/build-release.sh" "$version_id" linux 386 "$test_root/rejected"

tar -xzf "$archive" -C "$test_root/unpack"
stage=$test_root/unpack/$archive_name
cat > "$test_root/expected-files" <<EOF
/
/README.md
/SECURITY.md
/LICENSE
/NOTICE
/THIRD_PARTY_NOTICES.md
/VERSION
/bin/
/bin/sshfleet
/docs/
/docs/assets/
/docs/assets/screenshots/
/docs/assets/screenshots/fleet-overview.png
/docs/assets/screenshots/groups.png
/docs/assets/screenshots/host-actions.png
/docs/assets/screenshots/terminal-tabs.png
/docs/configuration.md
/docs/features.md
/docs/glossary.md
/docs/manual-isolated-config.md
/docs/manual-toolchain-checks.md
/docs/project-goals-and-scenarios.md
/docs/publishing.md
/docs/releasing.md
/docs/repositories.md
/docs/security-backlog.md
/docs/security-model.md
/docs/user-guide.md
/install.sh
/sshfleet.example.toml
/third_party/
/third_party/licenses/
/third_party/licenses/charm.land_bubbletea_v2.txt
/third_party/licenses/charm.land_lipgloss_v2.txt
/third_party/licenses/github.com_charmbracelet_colorprofile.txt
/third_party/licenses/github.com_charmbracelet_ultraviolet.txt
/third_party/licenses/github.com_charmbracelet_x_ansi.txt
/third_party/licenses/github.com_charmbracelet_x_exp_ordered.txt
/third_party/licenses/github.com_charmbracelet_x_term.txt
/third_party/licenses/github.com_charmbracelet_x_termios.txt
/third_party/licenses/github.com_charmbracelet_x_vt.txt
/third_party/licenses/github.com_charmbracelet_x_windows.txt
/third_party/licenses/github.com_clipperhouse_displaywidth.txt
/third_party/licenses/github.com_clipperhouse_uax29_v2.txt
/third_party/licenses/github.com_creack_pty.txt
/third_party/licenses/github.com_lucasb-eyer_go-colorful.txt
/third_party/licenses/github.com_mattn_go-runewidth.txt
/third_party/licenses/github.com_muesli_cancelreader.txt
/third_party/licenses/github.com_pelletier_go-toml_v2.txt
/third_party/licenses/github.com_rivo_uniseg.txt
/third_party/licenses/github.com_xo_terminfo.txt
/third_party/licenses/golang.org_x_sync.txt
/third_party/licenses/golang.org_x_sys.txt
/tools/
/tools/README.md
/tools/bin/
/tools/bin/sshfleet
/tools/bin/sshf
/tools/manifest.toml
EOF
tar -tzf "$archive" | sed "s#^$archive_name##" | sort > "$test_root/archive-files"
sort "$test_root/expected-files" > "$test_root/expected-files.sorted"
cmp "$test_root/expected-files.sorted" "$test_root/archive-files" >/dev/null || fail "release archive contents changed"

grep -F "version=$version_id" "$stage/VERSION" >/dev/null || fail "VERSION version mismatch"
grep -F "commit=$commit" "$stage/VERSION" >/dev/null || fail "VERSION commit mismatch"
grep -F "source_date=$source_date" "$stage/VERSION" >/dev/null || fail "VERSION source date mismatch"
grep -F "platform=linux/$native_arch" "$stage/VERSION" >/dev/null || fail "VERSION platform mismatch"
manifest_sha=$(sed -n 's/^binary_sha256=//p' "$stage/VERSION")
binary_sha=$(sha256sum "$stage/bin/sshfleet" | awk '{print $1}')
test "$manifest_sha" = "$binary_sha" || fail "VERSION binary checksum mismatch"
test "$(stat -c %a "$stage/bin/sshfleet")" = 755 || fail "binary mode is not 0755"
test "$(stat -c %a "$stage/README.md")" = 644 || fail "documentation mode is not 0644"
test "$(stat -c %a "$stage/LICENSE")" = 644 || fail "LICENSE mode is not 0644"
test "$(stat -c %a "$stage/NOTICE")" = 644 || fail "NOTICE mode is not 0644"
test "$(stat -c %a "$stage/THIRD_PARTY_NOTICES.md")" = 644 || fail "third-party notices mode is not 0644"
test "$(stat -c %a "$stage/docs/user-guide.md")" = 644 || fail "user guide mode is not 0644"
cmp "$repo_root/LICENSE" "$stage/LICENSE" >/dev/null || fail "packaged LICENSE differs"
cmp "$repo_root/NOTICE" "$stage/NOTICE" >/dev/null || fail "packaged NOTICE differs"
cmp "$repo_root/THIRD_PARTY_NOTICES.md" "$stage/THIRD_PARTY_NOTICES.md" >/dev/null || fail "packaged third-party notices differ"
cmp "$repo_root/third_party/licenses/github.com_creack_pty.txt" "$stage/third_party/licenses/github.com_creack_pty.txt" >/dev/null || fail "packaged dependency license differs"
grep -F 'docs/user-guide.md' "$stage/README.md" >/dev/null || fail "packaged README does not link the packaged user guide"

packaged_json=$("$stage/bin/sshfleet" version --json)
require_json "$packaged_json" version '"'"$version_id"'"'
require_json "$packaged_json" commit '"'"$commit"'"'
manifest_state=$(sed -n 's/^source_state=//p' "$stage/VERSION")
require_json "$packaged_json" source_state '"'"$manifest_state"'"'

printf '%s\n' 'versioning: Linux arm64 artifact'
mkdir -p "$test_root/dist-arm64" "$test_root/unpack-arm64"
"$repo_root/tools/build-release.sh" "$version_id" linux arm64 "$test_root/dist-arm64" >/dev/null
arm64_name=sshfleet-console-$version_id-linux-arm64
tar -xzf "$test_root/dist-arm64/$arm64_name.tar.gz" -C "$test_root/unpack-arm64"
grep -F 'platform=linux/arm64' "$test_root/unpack-arm64/$arm64_name/VERSION" >/dev/null || fail "arm64 manifest mismatch"
file "$test_root/unpack-arm64/$arm64_name/bin/sshfleet" | grep -F 'ARM aarch64' >/dev/null || fail "arm64 archive does not contain an arm64 binary"

printf '%s\n' 'versioning: ok'
