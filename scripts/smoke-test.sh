#!/usr/bin/env bash
# smoke-test.sh — end-to-end LAN P2P verification with two local daemons.
#
# Simulates two hosts on one machine by giving each raagd its own $HOME
# (config dir, data dir, DB, socket are all HOME-derived). Two distinct
# listen ports avoid the 0.0.0.0:7844 collision; both share broadcast_port
# 7846 (SO_REUSEPORT lets them co-bind, already covered by unit tests).
#
# Asserts the full loop: discovery -> connect -> cross-peer search ->
# streaming playback -> active peer metrics.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
RAAG="$ROOT/bin/raag"
RAAGD="$ROOT/bin/raagd"
# Unique service tag so a stale daemon left over from an interrupted run (still
# announcing on the system-wide mDNS port 5353) can't be discovered by this run.
MDNS_TAG="raag-smoke-$$"
A_HOME="$(mktemp -d /tmp/raag-smoke-a.XXXXXX)"
B_HOME="$(mktemp -d /tmp/raag-smoke-b.XXXXXX)"
A_SOCK="$A_HOME/.local/share/raag/raag.sock"
B_SOCK="$B_HOME/.local/share/raag/raag.sock"
A_PID=""
B_PID=""

log()  { printf '\033[1;36m[smoke]\033[0m %s\n' "$*"; }
fail() { printf '\033[1;31m[smoke] FAIL:\033[0m %s\n' "$*"; exit 1; }

# Kill any raagd left over from an interrupted run so it can't keep announcing
# on mDNS port 5353 or the broadcast ports. Match on the bare "raagd" so it also
# catches daemons started via `go run` (temp exe under /tmp/go-build*/exe/) or
# a relocated binary, whose cmdline lacks the repo-absolute path. Idempotent
# and safe for a test.
kill_orphans() {
  pkill -f '[r]aagd' 2>/dev/null && log "killed leftover raagd daemon(s)" || true
}

# Kill the daemon process group (children started with setsid), wait for a
# graceful Badger flush, then remove the temp homes.
cleanup() {
  for pid in "$A_PID" "$B_PID"; do
    [ -n "$pid" ] || continue
    # The daemon was started in its own process group; kill the whole group.
    kill -- -"$pid" 2>/dev/null || kill "$pid" 2>/dev/null || true
  done
  [ -n "$A_PID" ] && wait "$A_PID" 2>/dev/null || true
  [ -n "$B_PID" ] && wait "$B_PID" 2>/dev/null || true
  rm -rf "$A_HOME" "$B_HOME"
}
trap cleanup EXIT

# --- prereqs -----------------------------------------------------------
[ -x "$RAAG" ] || { log "building binaries first"; make -C "$ROOT" build; }
[ -x "$RAAGD" ] || { log "building binaries first"; make -C "$ROOT" build; }

command -v ffmpeg >/dev/null 2>&1 || fail "ffmpeg required to generate test audio"
command -v timeout >/dev/null 2>&1 || fail "GNU timeout required"

# --- config + audio ----------------------------------------------------
write_config() { # $1=home $2=listen_port $3=broadcast_port $4=broadcast_target
  local home="$1" port="$2" bcport="$3" target="$4"
  mkdir -p "$home/.config/raag" "$home/.local/share/raag" "$home/Music"
  cat > "$home/.config/raag/config.toml" <<EOF
[library]
paths = ["$home/Music"]
watch = false
scan_on_start = true

[daemon]
log_level = "warn"

[p2p]
enabled = true
lan_only = true
listen_addrs = ["/ip4/0.0.0.0/tcp/$port", "/ip4/0.0.0.0/udp/$port/quic-v1"]
mdns_service_tag = "$MDNS_TAG"
broadcast_enabled = true
broadcast_port = $bcport
broadcast_target = "$target"
max_peers = 10
announce_library = true

[privacy]
share_library_manifest = true
share_play_history = false

[transcoder]
ffmpeg_path = "ffmpeg"
stream_codec = "opus"
stream_bitrate = "128k"
EOF
}

gen_audio() { # $1=home $2=prefix
  local home="$1" prefix="$2"
  # FLAC source exercises the transcode path when a different codec is requested.
  ffmpeg -nostdin -loglevel error -f lavfi -i "sine=frequency=440:duration=1" \
    -ac 1 -ar 44100 "$home/Music/${prefix}_sine.flac"
  ffmpeg -nostdin -loglevel error -f lavfi -i "sine=frequency=880:duration=1" \
    -ac 1 -ar 44100 "$home/Music/${prefix}_tone.wav"
}

# A: listen 7844, broadcast listener 7846 -> announces to B's broadcast port (7946)
# B: listen 7944, broadcast listener 7946... distinct ports needed to avoid the
# broadcast UDP bind colliding with the QUIC listener on the same port.
# A listens broadcast on 7846 (default), B on 7946.
write_config "$A_HOME" 7844 7846 "127.0.0.1:7946"
write_config "$B_HOME" 7944 7946 "127.0.0.1:7846"
gen_audio "$A_HOME" "a"
gen_audio "$B_HOME" "b"

# --- start daemons -----------------------------------------------------
start_daemon() { # $1=home $2=label -> sets PID
  local home="$1" label="$2"
  # setsid puts the daemon in its own process group so cleanup can kill the
  # whole group even if a child spawned subprocesses.
  HOME="$home" setsid "$RAAGD" --music-path "$home/Music" &
  local pid=$!
  log "started $label (pid $pid, home $home)"
  case "$label" in
    A) A_PID="$pid" ;;
    B) B_PID="$pid" ;;
  esac
}

kill_orphans
start_daemon "$A_HOME" "A"
start_daemon "$B_HOME" "B"

# --- wait for daemons --------------------------------------------------
wait_daemon() { # $1=socket $2=label
  local sock="$1" label="$2"
  local deadline=$((SECONDS + 20))
  while [ $SECONDS -lt $deadline ]; do
    if "$RAAG" --socket "$sock" daemon status 2>/dev/null | grep -q running; then
      log "$label daemon ready"
      return 0
    fi
    sleep 0.5
  done
  fail "$label daemon did not become ready (socket $sock)"
}

wait_daemon "$A_SOCK" "A"
wait_daemon "$B_SOCK" "B"

# --- assertion helpers -------------------------------------------------
# poll_out runs the command in a loop until its stdout matches the regex.
poll_out() { # $1=desc, $2=timeout_seconds, $3=regex, rest=command
  local desc="$1" timeout_s="$2" regex="$3"
  shift 3
  local deadline=$((SECONDS + timeout_s))
  while [ $SECONDS -lt $deadline ]; do
    if "$@" 2>/dev/null | grep -qE "$regex"; then
      return 0
    fi
    sleep 0.5
  done
  return 1
}

# --- 1. discovery + connect --------------------------------------------
log "waiting for A and B to discover and connect to each other..."
# Resolve B's peer ID from its own network status, then assert A's peers list
# contains it. This is resilient to unrelated entries (e.g. a leftover mDNS
# announcement) while still proving the real connect.
B_PEER_ID="$("$RAAG" --socket "$B_SOCK" network 2>/dev/null | awk '/Self Peer ID:/{print $NF}')"
[ -n "$B_PEER_ID" ] || fail "could not resolve B's peer id from network status"
log "B peer id: $B_PEER_ID"

if ! poll_out "A sees B connected" 30 "$B_PEER_ID" "$RAAG" --socket "$A_SOCK" peers list; then
  echo "--- A peers list ---"
  "$RAAG" --socket "$A_SOCK" peers list 2>&1 || true
  echo "--- B peers list ---"
  "$RAAG" --socket "$B_SOCK" peers list 2>&1 || true
  echo "--- A network status ---"
  "$RAAG" --socket "$A_SOCK" network 2>&1 || true
  echo "--- B network status ---"
  "$RAAG" --socket "$B_SOCK" network 2>&1 || true
  fail "A did not connect to B (loopback mDNS/broadcast)"
fi
log "peers connected (A sees B)"

# --- 2. cross-peer search ----------------------------------------------
log "verifying cross-peer search (A searches B's library)..."
if ! poll_out "search finds B track" 15 "b_tone" "$RAAG" --socket "$A_SOCK" search --peers "b_tone"; then
  echo "--- A search --peers 'b_tone' ---"
  "$RAAG" --socket "$A_SOCK" search --peers "b_tone" 2>&1 || true
  fail "cross-peer search did not find B's track"
fi
log "cross-peer search OK"

# --- 3. get B's track ID, then stream it on A ---------------------------
log "resolving B's track id via B's lib list..."
B_ID="$("$RAAG" --socket "$B_SOCK" lib list 2>/dev/null | grep "b_tone" | cut -f1 | head -1)"
[ -n "$B_ID" ] || fail "could not resolve B track id from lib list"
log "B track id: $B_ID"

log "playing B's track on A (streaming over loopback)..."
"$RAAG" --socket "$A_SOCK" play --id "$B_ID" >/dev/null 2>&1 || fail "play --id failed on A"

if ! poll_out "A status shows playing" 15 "^state: playing" "$RAAG" --socket "$A_SOCK" status; then
  echo "--- A status ---"
  "$RAAG" --socket "$A_SOCK" status 2>&1 || true
  fail "A did not reach playing state"
fi
STATUS="$("$RAAG" --socket "$A_SOCK" status 2>&1)"
echo "$STATUS" | grep -q "b_tone" || fail "A is playing, but not the expected B track"
log "streaming playback OK (A playing B's track)"

# --- 4. active peer metrics --------------------------------------------
log "verifying live peer metrics via debug peers..."
if ! poll_out "debug peers shows connected+metrics" 10 "^[^[:space:]]+[[:space:]]+yes[[:space:]]" "$RAAG" --socket "$A_SOCK" debug peers; then
  echo "--- A debug peers ---"
  "$RAAG" --socket "$A_SOCK" debug peers 2>&1 || true
  fail "debug peers did not show connected peer with metrics"
fi
log "peer metrics OK"

log "ALL SMOKE TESTS PASSED"
