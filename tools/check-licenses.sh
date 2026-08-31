#!/bin/sh
set -eu

script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
repo_root=$(CDPATH='' cd -- "$script_dir/.." && pwd)
notices=$repo_root/THIRD_PARTY_NOTICES.md
licenses_dir=$repo_root/third_party/licenses
work=$(mktemp -d)
trap 'rm -rf -- "$work"' EXIT HUP INT TERM

cd "$repo_root"
go list -deps -f '{{with .Module}}{{if .Version}}{{.Path}}|{{.Version}}|{{.Dir}}{{end}}{{end}}' ./cmd/sshf \
    | sed '/^$/d' \
    | sort -u > "$work/modules"

module_count=$(wc -l < "$work/modules" | tr -d ' ')
notice_count=$(grep -c '^| `' "$notices")
if [ "$module_count" -ne "$notice_count" ]; then
    printf 'license inventory count mismatch: linked=%s notices=%s\n' "$module_count" "$notice_count" >&2
    exit 1
fi

while IFS='|' read -r module version module_dir; do
    upstream=
    for candidate in LICENSE LICENSE.txt; do
        if [ -f "$module_dir/$candidate" ]; then
            upstream=$module_dir/$candidate
            break
        fi
    done
    if [ -z "$upstream" ]; then
        printf 'missing upstream license for %s %s\n' "$module" "$version" >&2
        exit 1
    fi

    filename=$(printf '%s.txt' "$module" | tr '/' '_')
    preserved=$licenses_dir/$filename
    if [ ! -f "$preserved" ]; then
        printf 'missing preserved license: %s\n' "$preserved" >&2
        exit 1
    fi
    if ! cmp "$upstream" "$preserved" >/dev/null; then
        printf 'preserved license differs from module cache: %s %s\n' "$module" "$version" >&2
        exit 1
    fi

    hash=$(sha256sum "$preserved" | awk '{print $1}')
    row=$(grep -F "| \`$module\` | \`$version\` |" "$notices" || true)
    if [ -z "$row" ]; then
        printf 'missing notice row: %s %s\n' "$module" "$version" >&2
        exit 1
    fi
    printf '%s\n' "$row" | grep -F "(third_party/licenses/$filename)" >/dev/null || {
        printf 'wrong preserved license path in notice: %s\n' "$module" >&2
        exit 1
    }
    printf '%s\n' "$row" | grep -F "$hash" >/dev/null || {
        printf 'wrong license hash in notice: %s\n' "$module" >&2
        exit 1
    }
done < "$work/modules"

# Keep the core license gate independent from optional, uninitialized toolchain
# submodules. This is the SHA-256 of the exact Apache-2.0 canonical text stored
# at the repository root.
canonical_apache_sha256=c71d239df91726fc519c6eb72d318ec65820627232b2f796219e87dcf35d0ab4
actual_apache_sha256=$(sha256sum "$repo_root/LICENSE" | awk '{print $1}')
if [ "$actual_apache_sha256" != "$canonical_apache_sha256" ]; then
    printf '%s\n' 'project LICENSE is not the canonical Apache-2.0 text' >&2
    exit 1
fi
grep -F 'Apache License 2.0' "$notices" >/dev/null
grep -F 'SSH Fleet Console contributors' "$repo_root/NOTICE" >/dev/null

printf 'licenses: %s linked modules verified\n' "$module_count"
