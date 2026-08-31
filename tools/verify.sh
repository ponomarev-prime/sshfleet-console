#!/bin/sh
set -eu

script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
tmp_dir=$(mktemp -d)
trap 'rm -rf -- "$tmp_dir"' EXIT HUP INT TERM

if command -v shellcheck >/dev/null 2>&1; then
    shellcheck "$script_dir/check.sh" "$script_dir/verify.sh" "$script_dir/activate.sh" "$script_dir/bin/"*
fi

for tool in sshfleet sshf lf dtop nvim bat batcat; do
    actual_path=$(bash -c '. "$1"; command -v "$2"' sh "$script_dir/activate.sh" "$tool")
    test "$actual_path" = "$script_dir/bin/$tool"
done

if command -v fish >/dev/null 2>&1; then
    for tool in sshfleet sshf lf dtop nvim bat batcat; do
        # Fish expands argv inside its own process.
        # shellcheck disable=SC2016
        actual_path=$(fish -c 'source "$argv[1]"; source "$argv[1]"; type -p "$argv[2]"' \
            "$script_dir/activate.fish" "$tool")
        test "$actual_path" = "$script_dir/bin/$tool"
    done
    printf '%s\n' 'Fish activation: ok'
fi

"$script_dir/bin/nvim" --headless \
    '+lua assert(vim.o.number and vim.o.relativenumber and vim.o.cursorline)' \
    +qa

expected_bat_config="$script_dir/config/bat/config"
actual_bat_config=$("$script_dir/bin/bat" --config-file)
test "$actual_bat_config" = "$expected_bat_config"
"$script_dir/bin/batcat" --version >/dev/null

mkdir -p "$tmp_dir/launcher-bin" "$tmp_dir/launcher-links"
ln -s "$script_dir/bin/sshfleet" "$tmp_dir/launcher-links/sshfleet-real"
ln -s ../launcher-links/sshfleet-real "$tmp_dir/launcher-bin/sshfleet"

for shell_name in sh bash zsh fish; do
    if command -v "$shell_name" >/dev/null 2>&1; then
        version_output=$(PATH="$tmp_dir/launcher-bin:$PATH" "$shell_name" -c \
            'cd / && sshfleet -version')
        case "$version_output" in
            'sshfleet '*) ;;
            *)
                printf 'unexpected %s launcher output: %s\n' \
                    "$shell_name" "$version_output" >&2
                exit 1
                ;;
        esac
        printf '%s launcher: ok\n' "$shell_name"
    else
        printf '%s launcher: skipped (shell unavailable)\n' "$shell_name"
    fi
done

if [ -L "$HOME/.local/bin/sshfleet" ]; then
    "$HOME/.local/bin/sshfleet" -version >/dev/null
    printf '%s\n' 'Installed sshfleet launcher: ok'
fi

if command -v script >/dev/null 2>&1 && command -v timeout >/dev/null 2>&1; then
    mkdir -p "$tmp_dir/lf-root"
    printf '%s\n' 'sshfleet lf preview' >"$tmp_dir/lf-root/example.txt"
    (sleep 1; printf q) | TERM=xterm-256color timeout 8 script -qfec \
        "$script_dir/bin/lf $tmp_dir/lf-root" "$tmp_dir/lf.typescript" >/dev/null

    if [ -L "$HOME/.local/bin/sshfleet" ]; then
        (sleep 1; printf q) | TERM=xterm-256color timeout 10 script -qfec \
            "$HOME/.local/bin/sshfleet --config /dev/null --no-user-ssh-config --no-probe" \
            "$tmp_dir/sshfleet.typescript" >/dev/null
        printf '%s\n' 'Installed sshfleet PTY: ok'
    fi

    if command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1; then
        (sleep 2; printf q) | TERM=xterm-256color timeout 10 script -qfec \
            "$script_dir/bin/dtop" "$tmp_dir/dtop.typescript" >/dev/null
        printf '%s\n' 'dtop PTY: ok'
    else
        printf '%s\n' 'dtop PTY: skipped (Docker daemon unavailable)'
    fi
else
    printf '%s\n' 'PTY checks: skipped (script or timeout unavailable)'
fi

printf '%s\n' 'toolchain verify: ok'
