#!/bin/sh
# Operator-only entrypoint: this task selector cannot be overridden by arguments.
set -eu
script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
exec python3 "$script_dir/upgrade_pending.py" 230 "$@"
