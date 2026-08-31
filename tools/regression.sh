#!/usr/bin/env bash
set -uo pipefail

script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
repo_root=$(CDPATH='' cd -- "$script_dir/.." && pwd)

# The PTY shell-entrypoint test invokes the real Make target from sh, bash, zsh
# and fish. Stop before recursively starting the complete suite four times.
if [ "${SSHF_REGRESSION_ENTRYPOINT_PROBE:-0}" = "1" ]; then
    cd "$repo_root" || exit 1
    printf 'SSH Fleet regression entrypoint: ok\n'
    exit 0
fi

timestamp=$(date -u +%Y%m%dT%H%M%SZ)
artifact_dir=${SSHF_REGRESSION_ARTIFACT_DIR:-"$repo_root/.artifacts/regression-$timestamp"}
if [ -z "${TMPDIR:-}" ]; then
    TMPDIR=$repo_root/.tmp/regression-$timestamp-$$
    export TMPDIR
fi
mkdir -p "$TMPDIR"
mkdir -p "$artifact_dir/logs" "$artifact_dir/tui-screenshots" "$artifact_dir/bin"

status=0
run_step() {
    name=$1
    shift
    printf '\n==> %s\n' "$name"
    if "$@" > >(tee "$artifact_dir/logs/$name.log") 2>&1; then
        printf '%s\n' "ok" > "$artifact_dir/logs/$name.status"
    else
        code=$?
        printf 'failed:%s\n' "$code" > "$artifact_dir/logs/$name.status"
        status=1
    fi
}

cd "$repo_root" || exit 1
{
    printf 'started_utc=%s\n' "$timestamp"
    printf 'git_revision=%s\n' "$(git rev-parse HEAD 2>/dev/null || printf unknown)"
    printf 'git_status=%s\n' "$(git status --porcelain 2>/dev/null | wc -l)"
    printf 'go=%s\n' "$(go version 2>/dev/null || printf missing)"
    printf 'os=%s\n' "$(uname -srm 2>/dev/null || printf unknown)"
} > "$artifact_dir/manifest.txt"

run_step unit go test -timeout=2m ./...
run_step shell-entrypoints env SSHF_REQUIRE_ALL_SHELLS=1 make test-shell-entrypoints
run_step platform make test-platform
run_step cross-build make cross-build
run_step docs make test-docs
run_step licenses make test-licenses
run_step race go test -race -timeout=4m ./...
run_step vet go vet ./...
run_step build "$repo_root/tools/build-sshf.sh" "$artifact_dir/bin/sshfleet"
run_step versioning make test-version
run_step build-askpass go build -buildvcs=false -trimpath -o "$artifact_dir/bin/sshf-askpass" ./cmd/sshf-askpass
run_step install tools/test-install.sh
run_step installed-launcher env SSHF_SCREENSHOT_DIR="$artifact_dir/installed-launcher-screenshots" tools/test-installed-launcher.sh
run_step healthcheck "$artifact_dir/bin/sshfleet" healthcheck --config "$repo_root/sshfleet.example.toml"
run_step coverage go test -timeout=2m -coverprofile="$artifact_dir/coverage.out" ./...
if [ -f "$artifact_dir/coverage.out" ]; then
    run_step coverage-summary go tool cover -func="$artifact_dir/coverage.out"
fi
run_step menu go test -timeout=2m -v ./internal/ui -run 'TestHostActionMenu|TestGroup|TestTerminalTab' -count=1
run_step sources go test -timeout=2m -v ./internal/sourcebundle ./internal/inventory ./internal/config -count=1
run_step pty-e2e go test -timeout=2m -v ./cmd/sshf -run TestTUIEndToEndProbeFilterOverlayGitAndQuit -count=1
run_step pty-screenshots env SSHF_SCREENSHOT_DIR="$artifact_dir/tui-screenshots" go test -timeout=2m -v ./cmd/sshf -run 'TestTUIEndToEnd(ScreenshotsAndActionMenuTraversal|GroupsCRUDAndMembership|LocalContainerMenuPreviewAndLogs|TrustedLocalConfigMenuAndPreview)' -count=1
run_step editor-pty env SSHF_SCREENSHOT_DIR="$artifact_dir/tui-screenshots" go test -timeout=2m -v ./cmd/sshf -run TestTUIRealNeovimReceivesArrowKeys -count=1
run_step toolchain "$repo_root/tools/check.sh"
run_step install-full "$repo_root/tools/test-install-full.sh"

if [ "${SSHF_REGRESSION_DOCKER:-1}" = "1" ]; then
    if docker info >/dev/null 2>&1; then
        run_step docker-host-key make test-docker
        run_step docker-container-pty env SSHF_SCREENSHOT_DIR="$artifact_dir/tui-screenshots" make test-container-docker
        run_step docker-workspace make test-workspace-docker
    else
        printf '%s\n' "docker unavailable" | tee "$artifact_dir/logs/docker.status"
        status=1
    fi
else
    printf '%s\n' "skipped by SSHF_REGRESSION_DOCKER=0" > "$artifact_dir/logs/docker.status"
fi

if command -v podman >/dev/null 2>&1 && podman info >/dev/null 2>&1; then
    run_step podman-container-pty env SSHF_SCREENSHOT_DIR="$artifact_dir/tui-screenshots" make test-container-podman
else
    printf '%s\n' "skipped: podman service unavailable" > "$artifact_dir/logs/podman-container-pty.status"
fi

printf 'finished_utc=%s\n' "$(date -u +%Y%m%dT%H%M%SZ)" >> "$artifact_dir/manifest.txt"
printf 'result=%s\n' "$([ "$status" -eq 0 ] && printf pass || printf fail)" >> "$artifact_dir/manifest.txt"
printf '\nArtifacts: %s\n' "$artifact_dir"
exit "$status"
