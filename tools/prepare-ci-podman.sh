#!/bin/sh
set -eu

# GitHub's ubuntu-24.04 image currently has a Podman/crun rollout mismatch.
# Container creation may work while an interactive `podman exec --tty` fails.
# Select the already installed runc through Podman's supported containers.conf
# interface. Keep the workaround CI-only, observable and removable once the
# runner rollout is complete. No network access or unpinned download is used.
podman_bin=${SSHF_CI_PODMAN_BIN:-podman}
preferred_runtime=${SSHF_CI_PODMAN_RUNTIME:-runc}
containers_conf=${CONTAINERS_CONF:-${RUNNER_TEMP:-/tmp}/sshfleet-ci-containers.conf}
rootful=${SSHF_CI_PODMAN_ROOTFUL:-0}
if [ "${GITHUB_ACTIONS:-}" = true ]; then
    rootful=${SSHF_CI_PODMAN_ROOTFUL:-1}
fi

if command -v "$preferred_runtime" >/dev/null 2>&1; then
    mkdir -p "$(dirname -- "$containers_conf")"
    {
        printf '%s\n' '[engine]'
        printf 'runtime = "%s"\n' "$preferred_runtime"
    } >"$containers_conf"
    if [ "${rootful:-0}" = 1 ]; then
        podman_path=$(command -v "$podman_bin")
        wrapper_dir=${SSHF_CI_PODMAN_WRAPPER_DIR:-${RUNNER_TEMP:-/tmp}/sshfleet-ci-bin}
        wrapper=$wrapper_dir/podman
        mkdir -p "$wrapper_dir"
        {
            printf '%s\n' '#!/bin/sh'
            printf 'exec sudo env CONTAINERS_CONF=%s %s "$@"\n' "$(printf '%s' "$containers_conf" | sed "s/'/'\\\\''/g; s/.*/'&'/")" "$(printf '%s' "$podman_path" | sed "s/'/'\\\\''/g; s/.*/'&'/")"
        } >"$wrapper"
        chmod 0755 "$wrapper"
        runtime_path=$(sudo env CONTAINERS_CONF="$containers_conf" "$podman_path" info --format '{{.Host.OCIRuntime.Path}}')
        if [ -n "${GITHUB_PATH:-}" ]; then
            printf '%s\n' "$wrapper_dir" >>"$GITHUB_PATH"
        fi
        printf 'ci podman mode: rootful wrapper %s\n' "$wrapper"
    else
        runtime_path=$(CONTAINERS_CONF="$containers_conf" "$podman_bin" info --format '{{.Host.OCIRuntime.Path}}')
    fi
    case "$runtime_path" in
        */"$preferred_runtime") ;;
        *)
            printf 'ci podman runtime: wanted %s, got %s\n' "$preferred_runtime" "$runtime_path" >&2
            exit 1
            ;;
    esac
    printf 'ci podman runtime: %s via %s\n' "$runtime_path" "$containers_conf"
    exit 0
fi

# Fallback for images without runc: align an old configured crun with the
# compatible preinstalled build.
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
