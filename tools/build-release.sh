#!/bin/sh
set -eu

if [ "$#" -ne 4 ]; then
    printf '%s\n' 'usage: build-release.sh VERSION GOOS GOARCH OUTPUT_DIR' >&2
    exit 2
fi

version=$1
target_os=$2
target_arch=$3
output_dir=$4
case "$version" in *[!A-Za-z0-9._+-]*) printf '%s\n' 'unsafe release version' >&2; exit 2 ;; esac
case "$target_os/$target_arch" in linux/amd64|linux/arm64) ;; *) printf '%s\n' 'supported targets: linux/amd64, linux/arm64' >&2; exit 2 ;; esac

script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
repo_root=$(CDPATH='' cd -- "$script_dir/.." && pwd)
head_commit=$(git -C "$repo_root" rev-parse HEAD)
channel=development
branch=$(git -C "$repo_root" symbolic-ref --quiet --short HEAD 2>/dev/null || printf detached)
if printf '%s\n' "$version" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+$'; then
    expected=${SSHF_RELEASE_EXPECTED_SHA:-$head_commit}
    (cd "$repo_root" && "$script_dir/release-preflight.sh" "$version" "$expected")
    channel=stable
    branch=main
fi
mkdir -p "$output_dir"
output_dir=$(CDPATH='' cd -- "$output_dir" && pwd)
stage_root=$(mktemp -d)
trap 'rm -rf -- "$stage_root"' EXIT HUP INT TERM
archive_name=sshfleet-console-$version-$target_os-$target_arch
stage=$stage_root/$archive_name

mkdir -p "$stage/bin" "$stage/docs/assets/screenshots" "$stage/third_party/licenses" "$stage/tools/bin"
cd "$repo_root"
SSHF_BUILD_VERSION=$version SSHF_BUILD_CHANNEL=$channel SSHF_BUILD_BRANCH=$branch \
    SSHF_BUILD_COMMIT=$head_commit CGO_ENABLED=0 GOOS=$target_os GOARCH=$target_arch \
    "$script_dir/build-sshf.sh" "$stage/bin/sshfleet"
install -m 0755 "$repo_root/tools/launchers/sshfleet" "$stage/tools/bin/sshfleet"
install -m 0755 "$repo_root/tools/launchers/sshf" "$stage/tools/bin/sshf"
install -m 0755 "$repo_root/install.sh" "$stage/install.sh"
install -m 0644 "$repo_root/README.md" "$stage/README.md"
install -m 0644 "$repo_root/SECURITY.md" "$stage/SECURITY.md"
install -m 0644 "$repo_root/LICENSE" "$stage/LICENSE"
install -m 0644 "$repo_root/NOTICE" "$stage/NOTICE"
install -m 0644 "$repo_root/THIRD_PARTY_NOTICES.md" "$stage/THIRD_PARTY_NOTICES.md"
install -m 0644 "$repo_root/sshfleet.example.toml" "$stage/sshfleet.example.toml"
install -m 0644 "$repo_root"/docs/*.md "$stage/docs/"
install -m 0644 "$repo_root"/docs/assets/screenshots/*.png "$stage/docs/assets/screenshots/"
install -m 0644 "$repo_root"/third_party/licenses/*.txt "$stage/third_party/licenses/"
install -m 0644 "$repo_root/tools/README.md" "$stage/tools/README.md"
install -m 0644 "$repo_root/tools/manifest.toml" "$stage/tools/manifest.toml"

source_date=$(git show -s --format=%cI "$head_commit")
binary_sha=$(sha256sum "$stage/bin/sshfleet" | awk '{print $1}')
source_state=clean
if [ -n "$(git status --porcelain --untracked-files=normal)" ]; then
    source_state=dirty
fi
cat > "$stage/VERSION" <<EOF
version=$version
channel=$channel
branch=$branch
commit=$head_commit
source_date=$source_date
source_state=$source_state
platform=$target_os/$target_arch
binary_sha256=$binary_sha
EOF

if [ "$target_os" = "$(go env GOOS)" ] && [ "$target_arch" = "$(go env GOARCH)" ]; then
    version_json=$("$stage/bin/sshfleet" version --json)
    printf '%s\n' "$version_json" | grep -F '"version": "'"$version"'"' >/dev/null
    printf '%s\n' "$version_json" | grep -F '"commit": "'"$head_commit"'"' >/dev/null
    printf '%s\n' "$version_json" | grep -F '"channel": "'"$channel"'"' >/dev/null
fi

source_epoch=$(git show -s --format=%ct "$head_commit")
tar --sort=name --mtime="@$source_epoch" --owner=0 --group=0 --numeric-owner \
    -C "$stage_root" -czf "$output_dir/$archive_name.tar.gz" "$archive_name"
printf '%s\n' "$output_dir/$archive_name.tar.gz"
