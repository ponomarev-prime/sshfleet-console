#!/bin/sh
set -eu

usage() {
    cat <<'EOF'
Usage: ./install.sh [--full] [--prebuilt] [--prefix DIR] [--bin-dir DIR]

Installs SSH Fleet Console for the current user without sudo or shell-rc changes.

  default       build and install the self-contained sshfleet binary
  --full        also build/install pinned lf, dtop, Neovim, bat and workspace bundle
  --prebuilt    install binaries already present in this release archive
  --prefix DIR  application files (default: $XDG_DATA_HOME/sshfleet)
  --bin-dir DIR command symlinks (default: $XDG_BIN_HOME or ~/.local/bin)
EOF
}

resolve_script() {
    target=$1
    while [ -L "$target" ]; do
        target_dir=$(CDPATH='' cd -P "$(dirname "$target")" && pwd)
        link_target=$(readlink "$target")
        case "$link_target" in
            /*) target=$link_target ;;
            *) target=$target_dir/$link_target ;;
        esac
    done
    target_dir=$(CDPATH='' cd -P "$(dirname "$target")" && pwd)
    printf '%s/%s\n' "$target_dir" "$(basename "$target")"
}

need() {
    command -v "$1" >/dev/null 2>&1 || {
        printf 'sshfleet installer: required command not found: %s\n' "$1" >&2
        exit 1
    }
}

script_path=$(resolve_script "$0")
source_root=$(CDPATH='' cd -P "$(dirname "$script_path")" && pwd)
data_home=${XDG_DATA_HOME:-"${HOME:?HOME is required}/.local/share"}
user_bin=${XDG_BIN_HOME:-"${HOME:?HOME is required}/.local/bin"}
prefix=$data_home/sshfleet
bin_dir=$user_bin
mode=core
prebuilt=false

while [ "$#" -gt 0 ]; do
    case "$1" in
        --full) mode=full ;;
        --prebuilt) prebuilt=true ;;
        --prefix)
            [ "$#" -ge 2 ] || { printf '%s\n' 'sshfleet installer: --prefix needs a directory' >&2; exit 2; }
            prefix=$2
            shift
            ;;
        --bin-dir)
            [ "$#" -ge 2 ] || { printf '%s\n' 'sshfleet installer: --bin-dir needs a directory' >&2; exit 2; }
            bin_dir=$2
            shift
            ;;
        -h|--help) usage; exit 0 ;;
        *) printf 'sshfleet installer: unknown option: %s\n' "$1" >&2; usage >&2; exit 2 ;;
    esac
    shift
done

if [ "$prebuilt" = true ] && [ "$mode" = full ]; then
    printf '%s\n' 'sshfleet installer: --prebuilt archives contain core only; use a source checkout for --full' >&2
    exit 2
fi

case "$prefix" in ''|/) printf '%s\n' 'sshfleet installer: unsafe --prefix' >&2; exit 2 ;; esac
case "$bin_dir" in ''|/) printf '%s\n' 'sshfleet installer: unsafe --bin-dir' >&2; exit 2 ;; esac
if [ -L "$prefix" ]; then
    printf '%s\n' 'sshfleet installer: --prefix must not be a symlink' >&2
    exit 2
fi

for command_name in install ln mktemp cp mv date uname; do
    need "$command_name"
done

if [ "$prebuilt" = false ]; then
    need make
    need go
    if [ "$mode" = full ]; then
        if [ "$(uname -s)" != Linux ] || [ "$(uname -m)" != x86_64 ]; then
            printf '%s\n' 'sshfleet installer: --full currently supports Linux/x86_64; core is portable' >&2
            exit 1
        fi
        for command_name in git cargo cmake; do
            need "$command_name"
        done
        make -C "$source_root" toolchain-ready remote-bundle
    fi
    make -C "$source_root" build
fi

for file in "$source_root/bin/sshfleet" "$source_root/tools/bin/sshfleet" "$source_root/tools/bin/sshf"; do
    [ -x "$file" ] || { printf 'sshfleet installer: missing executable: %s\n' "$file" >&2; exit 1; }
done

install -d -m 0755 "$prefix" "$prefix/versions" "$bin_dir"
stage=$(mktemp -d "$prefix/versions/.install.XXXXXX")
cleanup() {
    if [ -n "${stage:-}" ] && [ -d "$stage" ]; then
        rm -rf -- "$stage"
    fi
}
trap cleanup EXIT HUP INT TERM
chmod 0755 "$stage"
install -d -m 0755 "$stage/bin" "$stage/tools/bin"
install -m 0755 "$source_root/bin/sshfleet" "$stage/bin/sshfleet"
install -m 0755 "$source_root/tools/bin/sshfleet" "$stage/tools/bin/sshfleet"
install -m 0755 "$source_root/tools/bin/sshf" "$stage/tools/bin/sshf"

if [ "$mode" = full ]; then
    for launcher in lf dtop nvim bat batcat sshfleet-preview; do
        install -m 0755 "$source_root/tools/bin/$launcher" "$stage/tools/bin/$launcher"
    done
    cp -a "$source_root/tools/config" "$stage/tools/config"
    install -d -m 0755 "$stage/.toolchain"
    cp -a "$source_root/.toolchain/bin" "$stage/.toolchain/bin"
    cp -a "$source_root/.toolchain/dist" "$stage/.toolchain/dist"
    cp -a "$source_root/.toolchain/remote" "$stage/.toolchain/remote"
fi

install_id=$(date -u +%Y%m%dT%H%M%SZ)-$$
version_dir=$prefix/versions/$install_id
mv "$stage" "$version_dir"
stage=
current_link=$prefix/.current-$$
ln -s "versions/$install_id" "$current_link"
mv -Tf "$current_link" "$prefix/current"
ln -sfn "$prefix/current/tools/bin/sshfleet" "$bin_dir/sshfleet"

install_compat_alias() {
    alias_name=$1
    alias_target=$2
    alias_path=$bin_dir/$alias_name
    if [ ! -e "$alias_path" ] && [ ! -L "$alias_path" ]; then
        ln -s "$alias_target" "$alias_path"
        printf 'Compatibility command installed: %s\n' "$alias_path"
        return
    fi
    if [ -L "$alias_path" ]; then
        existing_target=$(readlink "$alias_path")
        case "$existing_target" in
            "$prefix/current/tools/bin/sshfleet"|"$prefix/current/tools/bin/sshf")
                ln -sfn "$alias_target" "$alias_path"
                printf 'Compatibility command updated: %s\n' "$alias_path"
                return
                ;;
        esac
    fi
    printf 'SSH Fleet Console: %s already exists; alias was not changed\n' "$alias_path" >&2
}

install_compat_alias sshf "$prefix/current/tools/bin/sshf"
install_compat_alias sf "$prefix/current/tools/bin/sshfleet"

printf 'SSH Fleet Console installed: %s\n' "$bin_dir/sshfleet"
printf 'Mode: %s\n' "$mode"
printf 'Versioned root: %s\n' "$version_dir"
case ":${PATH:-}:" in
    *:"$bin_dir":*) ;;
    *) printf 'Add %s to PATH, then run: sshfleet healthcheck\n' "$bin_dir" ;;
esac
