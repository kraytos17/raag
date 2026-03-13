# Raag

Raag is a terminal-first music player featuring local playback, playlist management, and libp2p-based peer discovery for sharing music across machines.

## What It Does

- Play music from a local directory
- Manage playlists and playback from the CLI or TUI
- Discover peers via mDNS, DHT, and an optional tracker with multi-address peer records
- Listen on TCP and QUIC, and traverse NAT using hole punching, UPnP, and AutoNAT
- Run as a daemon to maintain peer connections between commands
- Authenticate tracker registrations using a key derived from the peer's persistent libp2p identity

## Quick Start

Build the binaries:

```bash
go build -o bin/raag ./cmd/raag
go build -o bin/tracker ./tracker/cmd/tracker
```

Play locally:

```bash
./bin/raag --tui
```

Start in network mode:

```bash
./bin/raag --network
```

For persistent peer connectivity, use daemon mode:

```bash
./bin/raag daemon --network
```

Print the full derived tracker auth key:

```bash
./bin/raag --network network auth-key
```

## Core Commands

Playback:

```bash
./bin/raag play <song>
./bin/raag pause
./bin/raag resume
./bin/raag stop
./bin/raag next
./bin/raag previous
./bin/raag seek [seconds]
./bin/raag volume [0-100]
./bin/raag queue
./bin/raag nowplaying
```

Library:

```bash
./bin/raag library list
./bin/raag library search [query]
./bin/raag library rescan
./bin/raag library add <path>
./bin/raag library remove <title>
```

Playlists:

```bash
./bin/raag playlist create <name>
./bin/raag playlist delete <name>
./bin/raag playlist list
./bin/raag playlist add <playlist> <song>
./bin/raag playlist remove <playlist> <index>
./bin/raag playlist songs <playlist>
./bin/raag playlist play <playlist>
```

Peers and networking:

```bash
./bin/raag peers list
./bin/raag peers info
./bin/raag peers ping <peerID>
./bin/raag peers connect <multiaddr>
./bin/raag peers disconnect <peerID>
./bin/raag peers tracker <url>
./bin/raag peers bootstrap <multiaddr>
./bin/raag network status
```

Daemon and config:

```bash
./bin/raag daemon
./bin/raag status
./bin/raag config show
./bin/raag config set <key> <value>
./bin/raag config reset
```

## Important Flags

Client flags:

| Flag | Default | Purpose |
| --- | --- | --- |
| `--network` | `false` | Enable peer discovery and libp2p networking |
| `--host` | `0.0.0.0` | Bind address |
| `--port` | `45678` | Peer listen port |
| `--tracker` | `https://raag-production.up.railway.app` | Tracker URL |
| `--dht` | `true` | Enable DHT discovery |
| `--bootstrap` | empty | Explicit DHT bootstrap peers |
| `--max-peers` | `100` | Peer limit |
| `--music-dir` | `./music` | Local music directory |
| `--tui` | `false` | Start the TUI |
| `--json` | `false` | JSON log output |

Tracker flags:

| Flag | Default | Purpose |
| --- | --- | --- |
| `--http-port` | `8080` | HTTP API port |
| `--libp2p-port` | `45678` | Tracker libp2p host port |
| `--relay` | `true` | Starts tracker libp2p host; relay is not advertised in current P0 |
| `--auth-key` | none | Trusted derived auth public key; repeatable |

## Runtime Files

Raag stores runtime state in `~/.config/raag/`:

### Persistent Configuration (`config.yaml`)
- `host` - listen address (default: 0.0.0.0)
- `port` - listen port (default: 45678)
- `rendezvous` - DHT rendezvous string
- `tracker_url` - tracker URL for peer discovery
- `dht_enabled` - enable DHT discovery
- `max_peers` - maximum connected peers
- `bootstrap_peers` - DHT bootstrap peers
- `music_dir` - local music directory

### Runtime State (`state.json`)
- `last_song` - last played song
- `position` - playback position in seconds
- `volume` - current volume (0-100)
- `queue` - current playback queue
- `current_idx` - current queue index
- `shuffle` - shuffle mode enabled
- `repeat` - repeat mode enabled
- `last_played` - timestamp of last played song

### Other Files
- `identity.key` - persistent libp2p identity key
- `daemon.sock` - daemon control socket
- `playlists.json` - playlist data
- `peers.json` - persisted discovered-peer cache

`identity.key` is critical: it stabilizes the peer ID across restarts and serves as the root of tracker authentication.

## Architecture

Raag has two runtime pieces:

- the `raag` client, which owns the music library, player, peer identity, and libp2p host
- the optional `tracker`, which provides peer registration over HTTP for cross-network connectivity

### End-to-End Design

The system operates as follows:

1. A Raag client starts and loads `~/.config/raag/identity.key`
2. That identity key determines the libp2p peer ID
3. The client derives a tracker auth key from the identity key
4. The client starts a libp2p host and enables discovery and NAT traversal features
5. The client discovers peers via mDNS, DHT, tracker, or a combination of them
6. If a tracker is configured, the client registers multiple advertised addresses and a signed auth token with the tracker
7. The tracker validates the registration and returns peer records keyed by peer ID, including multiple addresses
8. Peers connect directly when possible, and use the currently implemented discovery plus NAT-traversal path
9. The daemon keeps those connections alive so the CLI can query or control the node without rebuilding network state each time

### Client Architecture

The `raag` binary is responsible for:

- configuration loading and persistence
- local library and playlist management
- playback control
- libp2p host creation
- peer discovery and registration
- daemon socket support for persistent background operation

Conceptually:

```text
raag CLI / TUI
   |
   +-- config + storage
   +-- library + playlists + player
   +-- network manager
          |
          +-- persistent identity.key
          +-- libp2p host
          +-- mDNS discovery
          +-- DHT discovery
          +-- tracker registration / fetch (peer_id + multiple addrs)
          +-- TCP + QUIC listeners
          +-- ResourceManager
          +-- NAT traversal (hole punching, UPnP, AutoNAT)
          +-- peer presence protocol (/raag/ping/1.0.0)
```

### Tracker Architecture

The `tracker` binary is intentionally smaller than a full peer node. The current P0 implementation performs two primary functions:

- serves an HTTP API for peer registration and discovery
- optionally starts a libp2p host, but relay advertising is intentionally disabled until relay behavior is fully verified

Conceptually:

```text
tracker
  |
  +-- HTTP API
  |     +-- /register
  |     +-- /peers
  |     +-- /addr
  |     +-- /health
  |
  +-- optional libp2p host (relay not advertised in current P0)
  |
  +-- peer records keyed by peer_id
  |     +-- addrs[]
  |     +-- last_seen
  |
  +-- trusted auth key list
```

### Discovery Flow

Raag supports the following discovery mechanisms:

- `mDNS` for machines on the same LAN
- `DHT` for decentralized discovery using bootstrap peers
- `tracker` for cross-network registration and multi-address peer listing

In practice, the tracker proves most useful when peers reside on different networks or operate behind NAT.

### Connection Flow

Upon discovering a peer, Raag attempts connection over libp2p. The current P0 implementation supports the following connection paths:

- direct connection using one of the peer's advertised TCP or QUIC addresses
- hole punching when both peers are reachable enough for NAT traversal

Circuit-relay support is not advertised in P0 because it has not been verified end to end yet.

### Peer Presence Protocol

Raag includes a built-in peer presence protocol (`/raag/ping/1.0.0`) that enables direct peer-to-peer presence awareness.

**How it works:**

- When peers connect, they automatically exchange hello/pong messages
- When peers disconnect, they send goodbye/left notifications
- This happens directly peer-to-peer, not through the tracker

**Message flow:**

```
Peer A connects to Peer B
        ↓
Peer A sends "hello" directly to Peer B (P2P stream)
        ↓
Peer B responds with "pong" (P2P stream)
        ↓
Both log: "Peer X is online!"

--- Later ---

Peer A disconnects
        ↓
Peer A sends "left" directly to Peer B (P2P stream)
        ↓
Peer B logs: "Peer A has left the network"
```

**Manual ping:**

You can also manually ping a peer:

```bash
./bin/raag peers ping <peer-id>
```

This is useful for testing direct peer-to-peer connectivity.

### Auth Flow in the Architecture

Tracker authentication is not a separate account system. Rather, it constitutes a cryptographic proof that the registering client controls the persistent libp2p identity it claims.

The critical binding is as follows:

```text
identity.key -> libp2p peer ID
identity.key -> derived tracker auth key
derived tracker auth key -> signed registration token
registration token + peer_id + addrs[] -> tracker verification
```

This means frequent disconnects and reconnects are acceptable: as long as `identity.key` remains unchanged, the peer ID and derived auth key remain consistent.

### Daemon Architecture

Daemon mode exists because peer-to-peer networking is inherently stateful. Running one-off commands repeatedly would otherwise recreate the libp2p host and lose active peer state.

Daemon mode provides:

- One long-lived libp2p host
- Stable peer connections
- Periodic tracker registration heartbeat
- Periodic tracker peer refresh
- Single source of truth for current network state
- Unix socket interface for short-lived CLI commands

In the current P0 implementation, these commands are daemon-backed when the daemon is running:

- `network status`
- `network auth-key`
- `peers list`
- `peers info`
- `peers ping`
- `peers connect`
- `peers disconnect`
- `peers tracker`
- `peers bootstrap`

Recommended pattern:

```bash
./bin/raag daemon --network --tracker https://your-tracker.example.com
./bin/raag peers list
./bin/raag network status
./bin/raag network auth-key
```

## Network Modes

### LAN Discovery

For machines on the same local network, execute:

```bash
./bin/raag --network
```

Raag automatically uses mDNS and runs the libp2p stack with NAT traversal helpers enabled.

### Cross-Network Discovery

For peers on different networks, run a tracker on a public host and configure clients to use it:

```bash
./bin/tracker --http-port 8080 --libp2p-port 45678
./bin/raag daemon --network --tracker https://your-tracker.example.com
```

The tracker delivers:

- HTTP peer registration and peer listing
- peer records keyed by `peer_id` with multiple advertised addresses
- a stable rendezvous point for peers on different networks
- periodic liveness tracking via client heartbeats

Current P0 note:

- Tracker relay advertising is intentionally disabled until relay behavior is fully implemented and tested

## Tracker Authentication

Tracker authentication is derived from the peer's libp2p identity. A separate long-lived `auth.key` file no longer exists.

### How It Works

1. Raag loads or creates `~/.config/raag/identity.key`
2. Raag derives an ed25519 tracker auth key from the marshaled identity key bytes using a fixed Raag salt
3. Raag signs a tracker registration token containing:
   - `peer_id`
   - derived auth public key
   - issued timestamp
   - expiry timestamp
   - random nonce
4. The tracker verifies:
   - the request includes `addrs`, `peer_id`, and `auth_data`
   - every advertised address embeds the same `peer_id`
   - the `peer_id` in the signed token matches the request
   - the token signature is valid for the derived auth public key
   - the derived auth public key is in the tracker's trusted key list

If any of those checks fail, registration is rejected.

### Why This Is Better

- Reconnects maintain the same peer identity and tracker auth identity
- A separate tracker-only private key is no longer required
- Tracker trust is bound to the libp2p identity that owns the peer ID
- Spoofing `peer_id` or announcing a different multiaddr is rejected at registration time

### Get Your Trusted Auth Key

Run:

```bash
./bin/raag --network network auth-key
```

This prints the full public key the tracker should trust for this peer.

Example output:

```text
263e6f196c27a0599bc79ef8146ed279bbc098b9143b3ebbd461f24e5f790a1e
```

You can still use `./bin/raag --network network status` if you want the abbreviated key alongside the rest of the network state.

### Start an Authenticated Tracker

To trust one peer:

```bash
./bin/tracker \
  --http-port 8080 \
  --libp2p-port 45678 \
  --auth-key 263e6f196c27a0599bc79ef8146ed279bbc098b9143b3ebbd461f24e5f790a1e
```

To trust multiple peers, repeat `--auth-key`:

```bash
./bin/tracker \
  --auth-key KEY_ONE \
  --auth-key KEY_TWO \
  --auth-key KEY_THREE
```

If no `--auth-key` values are provided, the tracker accepts registrations without enforcing authentication.

## Deployment

### Local Tracker

```bash
make build-tracker
./bin/tracker --http-port 8080 --libp2p-port 45678 --auth-key <derived-auth-public-key>
```

### Environment Variables

The tracker supports the following environment variables:

| Variable | Description |
|----------|-------------|
| `PORT` | HTTP API listen port (defaults to 8080) |
| `AUTH_KEY` | Trusted auth key(s) for peer authentication. Comma-separated for multiple keys. |

Examples:

```bash
# Single key
AUTH_KEY=263e6f196c27a0599bc79ef8146ed279bbc098b9143b3ebbd461f24e5f790a1e

# Multiple keys
AUTH_KEY=key1,key2,key3
```

### Docker

Build and run:

```bash
docker build -t raag-tracker .
docker run --rm -e PORT=8080 -e AUTH_KEY=<your-key> -p 8080:8080 -p 45678:45678 raag-tracker
```

The provided `Dockerfile` builds the tracker binary, exposes ports `8080` and `45678`, and starts the tracker with `PORT`-aware HTTP binding.

For multiple keys, use comma-separated:
```bash
-e AUTH_KEY=key1,key2,key3
```

### Docker Compose

The provided `docker-compose.yml` starts the tracker service and exposes both ports. To enable authentication, uncomment and set the `AUTH_KEY` environment variable:

```yaml
environment:
  PORT: "8080"
  AUTH_KEY: "your-key-here"  # Comma-separated for multiple keys
```

### Railway

The repo includes `railway.json` for Dockerfile-based deployment.

Recommended Railway setup:

1. Deploy this repo as a Dockerfile service
2. Let Railway provide the HTTP `PORT` environment variable; the Docker entrypoint now honors it automatically
3. Expose libp2p port `45678` if your deployment needs peer-to-peer reachability metadata
4. Add the `AUTH_KEY` environment variable in Railway's Variables tab

**Railway Configuration:**

| Variable | Value | Notes |
|----------|-------|-------|
| `PORT` | Auto-provided by Railway | Don't set manually |
| `AUTH_KEY` | Your derived auth key | Add as a **Variable** (or **Secret** for production) |

For multiple keys, separate with commas:
```
AUTH_KEY=key1,key2,key3
```

The start command remains default:
```bash
./tracker --http-port ${PORT:-8080} --libp2p-port 45678 --relay
```

If `~/.config/raag/identity.key` is rotated or deleted on a client, its peer ID and derived auth key change accordingly, requiring an update to the tracker trust list.

## Typical Cross-Network Setup

On the public server:

```bash
./bin/tracker --http-port 8080 --libp2p-port 45678 --auth-key <peer-a-key> --auth-key <peer-b-key>
```

On each client:

```bash
./bin/raag daemon --network --tracker https://your-tracker.example.com
```

Useful client checks:

```bash
./bin/raag network status
./bin/raag peers list
./bin/raag peers info
```

## Address Advertisement And Peer Records

The current network stack advertises multiple dial candidates to the tracker instead of a single guessed address.

Tracker peer records now contain:

- `peer_id`
- `addrs[]`
- `last_seen`

This improves small-scale WAN connectivity by allowing peers to attempt multiple transport/address combinations.

In the current implementation:

- Peers register a ranked set of advertised addresses
- QUIC addresses are preferred over TCP addresses when ordering candidates
- Non-dialable addresses such as loopback and unspecified addresses are filtered out
- Fetched peer records are reconstructed into `peer.AddrInfo` with multiple addresses

This is not yet a full observed-address or signed-peer-record design, but it represents a substantial improvement over single-address registration.

## Peer Persistence And Bootstrap Hygiene

Raag now treats bootstrap peers and discovered peers as distinct entities.

- `bootstrap_peers` remain explicit configuration
- Discovered peers are stored in `peers.json`
- Discovered peers are no longer written back into bootstrap config automatically

This keeps the bootstrap set stable and avoids gradually polluting config with arbitrary discovered peers.

## Resource Management

The libp2p host now enables a ResourceManager in addition to the connection manager, improving robustness by providing bounded network-resource behavior under churn or unexpected peer activity.

The connection manager watermarks are now derived from configured peer limits rather than relying solely on hardcoded defaults.

## File Transfer

Song sharing in the current P0 implementation uses a framed libp2p transfer protocol.

The transfer path operates as follows:

- sender opens a dedicated share stream on `constants.ShareProtocolID`
- sender writes a length-prefixed JSON metadata frame
- sender streams the exact file payload bytes after metadata
- receiver reads metadata with explicit framing, validates it, and enforces limits
- receiver writes into a temporary file, verifies byte count and optional hash, then renames on success
- receiver rescans the library after a successful transfer

The framed metadata includes:

- protocol version
- title
- artist
- album
- original filename
- file extension
- total file size
- SHA-256 digest

P0 transfer safety behavior:

- max metadata size limit
- max file size limit
- idle stream timeout
- temp-file writes with cleanup on failure
- extension preservation instead of forcing `.mp3`

## NAT Traversal

Raag enables several traversal strategies in network mode:

- dual TCP and QUIC listeners
- hole punching
- UPnP port mapping
- AutoNAT reachability detection

This reduces the amount of manual router configuration needed for direct peer connections.

Current P0 note:

- Relay is intentionally not advertised by the tracker until that path is fully verified

## Development

Build everything:

```bash
go build ./...
```

Run tests:

```bash
go test ./...
```

Useful make targets:

```bash
make build
make build-tracker
make test
make fmt
make vet
```

## Troubleshooting

No peers discovered:

```bash
./bin/raag --network --tracker https://your-tracker.example.com
./bin/raag network status
```

Verification checklist:

- The client is running with `--network`
- The tracker URL is reachable
- The tracker trusts the client's derived auth key
- The client's `identity.key` has not changed unexpectedly
- The daemon is running if daemon-backed peer state is expected
- The tracker is returning at least one dialable advertised address for each peer

Tracker rejects registration:

- Confirm the `Auth Key:` shown by `./bin/raag --network network status`
- Or use `./bin/raag --network network auth-key` for the full key
- Confirm the tracker was started with the full matching `--auth-key`
- Confirm the client's `identity.key` was not deleted or replaced

Peer identity changed unexpectedly:

- check `~/.config/raag/identity.key`
- if it was removed, Raag generates a new peer identity and a new derived auth key
- update the tracker trust list to match the new derived auth key

Peer ping/hello not working:

- Ensure peers are connected (`./bin/raag peers list` shows connected peers)
- Check logs for hello/pong messages
- Verify peer-to-peer connectivity with manual ping: `./bin/raag peers ping <peer-id>`
- If ping fails, peers may not be directly connectable (NAT/firewall issues)

## License

Apache License 2.0
