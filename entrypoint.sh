#!/bin/sh
set -e

RAAG_DATA_DIR="${RAAG_DATA_DIR:-/home/raag/.local/share/raag}"
RAAG_SOCKET="${RAAG_SOCKET:-$RAAG_DATA_DIR/raag.sock}"

mkdir -p "$RAAG_DATA_DIR"

set -- --socket "$RAAG_SOCKET" --music-path "${RAAG_MUSIC:-/music}" --p2p
if [ -n "$RAAG_DATA_DIR_OVERRIDE" ]; then
    set -- "$@" --data-dir "$RAAG_DATA_DIR_OVERRIDE"
fi
if [ -n "$RAAG_NO_SCAN" ]; then
    set -- "$@" --no-scan
fi

echo "Starting raagd with arguments: $*"
exec ./raagd "$@"
