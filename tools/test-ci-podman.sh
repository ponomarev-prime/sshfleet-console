#!/bin/sh
set -eu

script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
tmp=${TMPDIR:-$script_dir/../.tmp}/test-ci-podman.$$
trap 'rm -rf "$tmp"' EXIT HUP INT TERM
mkdir -p "$tmp/bin"

active=$tmp/bin/crun-active
preferred=$tmp/bin/crun-preferred
podman=$tmp/bin/podman
runc=$tmp/bin/runc
containers_conf=$tmp/containers.conf
sudo=$tmp/bin/sudo

printf '%s\n' '#!/bin/sh' 'printf old-crun' >"$active"
printf '%s\n' '#!/bin/sh' 'printf new-crun' >"$preferred"
printf '%s\n' '#!/bin/sh' 'exit 0' >"$runc"
# shellcheck disable=SC2016 # literal body of the generated fake sudo helper.
printf '%s\n' '#!/bin/sh' 'test "$1" = env && shift' 'exec env "$@"' >"$sudo"
cat >"$podman" <<EOF
#!/bin/sh
printf '%s\n' "\${FAKE_RUNTIME_PATH:-$runc}"
EOF
chmod 0755 "$active" "$preferred" "$podman" "$runc" "$sudo"

PATH="$tmp/bin:$PATH" \
CONTAINERS_CONF=$containers_conf \
SSHF_CI_PODMAN_BIN=$podman \
SSHF_CI_PODMAN_ROOTFUL=0 \
    "$script_dir/prepare-ci-podman.sh" >/dev/null

grep -F 'runtime = "runc"' "$containers_conf" >/dev/null

github_path=$tmp/github-path
wrapper_dir=$tmp/wrapper
PATH="$tmp/bin:$PATH" \
GITHUB_ACTIONS=true \
GITHUB_PATH=$github_path \
CONTAINERS_CONF=$containers_conf \
SSHF_CI_PODMAN_BIN=$podman \
SSHF_CI_PODMAN_WRAPPER_DIR=$wrapper_dir \
    "$script_dir/prepare-ci-podman.sh" >/dev/null

test "$(cat "$github_path")" = "$wrapper_dir"
PATH="$tmp/bin:$PATH" "$wrapper_dir/podman" info | grep -F "$runc" >/dev/null

PATH=/usr/bin:/bin \
FAKE_RUNTIME_PATH=$active \
SSHF_CI_PODMAN_BIN=$podman \
SSHF_CI_PODMAN_RUNTIME=missing-runtime \
SSHF_CI_CRUN_ACTIVE=$active \
SSHF_CI_CRUN_PREFERRED=$preferred \
SSHF_CI_CRUN_INSTALL_WITHOUT_SUDO=1 \
    "$script_dir/prepare-ci-podman.sh" >/dev/null

cmp -s "$active" "$preferred"
test "$($active --version)" = new-crun
printf '%s\n' 'ci podman runtime contract: ok'
