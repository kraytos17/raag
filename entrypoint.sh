#!/bin/sh
set -e

PORT="${PORT:-8080}"
LIBP2P_PORT="${LIBP2P_PORT:-45678}"

set -- --http-port "$PORT" --libp2p-port "$LIBP2P_PORT" --relay
if [ "$TLS_ENABLED" = "true" ]; then
    set -- "$@" --tls
    if [ -n "$TLS_CERT_FILE" ] && [ -n "$TLS_KEY_FILE" ]; then
        set -- "$@" --tls-cert "$TLS_CERT_FILE" --tls-key "$TLS_KEY_FILE"
    fi
fi

if [ -n "$AUTH_KEY" ]; then
    OLD_IFS="$IFS"
    IFS=','
    for key in $AUTH_KEY; do
        key=$(echo "$key" | xargs)
        if [ -n "$key" ]; then
            set -- "$@" --auth-key "$key"
        fi
    done
    IFS="$OLD_IFS"
fi

# Note: AUTH_KEY is handled by the raag application
echo "Starting raag with arguments: $*"
exec ./raag "$@"
