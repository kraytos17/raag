#!/bin/sh
set -e

PORT="${PORT:-8080}"
LIBP2P_PORT="${LIBP2P_PORT:-45678}"

ARGS="--http-port $PORT --libp2p-port $LIBP2P_PORT --relay"
if [ "$TLS_ENABLED" = "true" ]; then
    ARGS="$ARGS --tls"

    if [ -n "$TLS_CERT_FILE" ] && [ -n "$TLS_KEY_FILE" ]; then
        ARGS="$ARGS --tls-cert $TLS_CERT_FILE --tls-key $TLS_KEY_FILE"
    fi
fi

if [ -n "$AUTH_KEY" ]; then
    IFS=','
    for key in $AUTH_KEY; do
        key=$(echo "$key" | xargs)
        if [ -n "$key" ]; then
            ARGS="$ARGS --auth-key $key"
        fi
    done
    unset IFS
fi

# Note: AUTH_SECRET is handled internally by the tracker application
echo "Starting tracker with arguments: $ARGS"
exec ./tracker $ARGS
