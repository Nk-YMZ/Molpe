#!/bin/bash
set -euo pipefail
umask 077

APP_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
# Explicit overrides let the packaged app be tested with disposable data.
DATA_ROOT="${MOLPE_APP_DATA_ROOT:-$HOME/Library/Application Support/Molpe}"
CACHE_ROOT="${MOLPE_APP_CACHE_ROOT:-$HOME/Library/Caches/Molpe}"
export XDG_CONFIG_HOME="$DATA_ROOT/config"
export XDG_DATA_HOME="$DATA_ROOT/data"
export XDG_CACHE_HOME="$CACHE_ROOT"
export XDG_RUNTIME_DIR="$CACHE_ROOT/run"
export TMPDIR="$CACHE_ROOT/tmp"
export PATH="$APP_ROOT/Contents/Resources/bin:$APP_ROOT/Contents/Helpers:/usr/bin:/bin:/usr/sbin:/sbin"
mkdir -p "$XDG_RUNTIME_DIR" "$TMPDIR"

"$APP_ROOT/Contents/Helpers/molpe" "$@"
