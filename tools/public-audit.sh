#!/bin/sh
set -eu

script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
repo_root=$(CDPATH='' cd -- "$script_dir/.." && pwd)
cd "$repo_root"

git diff --check

if git ls-files | grep -E '(^|/)(coverage\.out|\.env|id_(rsa|ed25519)|known_hosts|config\.toml)$' >/dev/null; then
    printf '%s\n' 'public audit: generated, secret-prone, or private config file is tracked' >&2
    git ls-files | grep -E '(^|/)(coverage\.out|\.env|id_(rsa|ed25519)|known_hosts|config\.toml)$' >&2
    exit 1
fi

if git grep -n -I -i -E '(/home/alex|git\.pregel|mbdg|skala|ALEX_PREGEL|BEGIN [A-Z ]*PRIVATE KEY)' -- ':!tools/src/**' ':!tools/public-audit.sh' >/dev/null; then
    printf '%s\n' 'public audit: private infrastructure marker found in tracked content' >&2
    git grep -n -I -i -E '(/home/alex|git\.pregel|mbdg|skala|ALEX_PREGEL|BEGIN [A-Z ]*PRIVATE KEY)' -- ':!tools/src/**' ':!tools/public-audit.sh' >&2
    exit 1
fi

if command -v gitleaks >/dev/null 2>&1; then
    gitleaks git --no-banner --redact .
else
    printf '%s\n' 'public audit: gitleaks unavailable (install it before publishing)' >&2
    exit 1
fi

for url in $(git config -f .gitmodules --get-regexp '\.url$' | awk '{print $2}'); do
    case "$url" in
        https://github.com/*) ;;
        *) printf 'public audit: non-public submodule URL: %s\n' "$url" >&2; exit 1 ;;
    esac
done

printf '%s\n' 'public audit: ok'
