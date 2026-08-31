#!/bin/sh
set -eu

# GitHub's ubuntu-24.04 image can contain a recent Podman configured to use an
# older /usr/bin/crun while the compatible crun is installed in /usr/local/bin.
# Keep the workaround narrow, observable and removable once the runner rollout
# is complete. No network access or unpinned download is involved.
podman_bin=${SSHF_CI_PODMAN_BIN:-podman}
active_crun=${SSHF_CI_CRUN_ACTIVE:-/usr/bin/crun}
preferred_crun=${SSHF_CI_CRUN_PREFERRED:-/usr/local/bin/crun}

runtime_path=$($podman_bin info --format '{{.Host.OCIRuntime.Path}}')
if [ "$runtime_path" != "$active_crun" ]; then
    printf 'ci podman runtime: %s (no alignment needed)\n' "$runtime_path"
    exit 0
fi
if [ ! -x "$preferred_crun" ]; then
    printf 'ci podman runtime: compatible candidate is missing: %s\n' "$preferred_crun" >&2
    exit 1
fi
if cmp -s "$preferred_crun" "$active_crun"; then
    printf 'ci podman runtime: %s already matches %s\n' "$active_crun" "$preferred_crun"
    exit 0
fi

if [ "${SSHF_CI_CRUN_INSTALL_WITHOUT_SUDO:-0}" = 1 ]; then
    install -m 0755 "$preferred_crun" "$active_crun"
else
    sudo install -m 0755 "$preferred_crun" "$active_crun"
fi
cmp -s "$preferred_crun" "$active_crun"
printf 'ci podman runtime: aligned %s with %s\n' "$active_crun" "$preferred_crun"
"$active_crun" --version
