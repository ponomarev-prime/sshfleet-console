#!/usr/bin/env bash

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
    printf '%s\n' 'source this file: . ./tools/activate.sh' >&2
    exit 1
fi

script_dir=$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
case ":$PATH:" in
    *":$script_dir/bin:"*) ;;
    *) PATH="$script_dir/bin:$PATH" ;;
esac
export PATH
unset script_dir
